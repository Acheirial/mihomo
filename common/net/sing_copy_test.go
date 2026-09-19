package net

import (
	"bytes"
	"errors"
	"io"
	"net"
	"reflect"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/metacubex/mihomo/common/pool"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingReader struct {
	r     io.Reader
	sizes []int
	ptrs  []uintptr
}

func (r *recordingReader) Read(p []byte) (int, error) {
	r.sizes = append(r.sizes, len(p))
	r.ptrs = append(r.ptrs, reflect.ValueOf(p).Pointer())
	return r.r.Read(p)
}

type errAfter struct {
	remain int
	err    error
}

func (r *errAfter) Read(p []byte) (int, error) {
	if r.remain <= 0 {
		return 0, r.err
	}
	n := len(p)
	if n > r.remain {
		n = r.remain
	}
	r.remain -= n
	return n, nil
}

type errWriter struct {
	n   int
	err error
}

func (w errWriter) Write(p []byte) (int, error) {
	if w.n > len(p) {
		return len(p), w.err
	}
	return w.n, w.err
}

func TestCopyWithIncreaseEOF(t *testing.T) {
	src := bytes.NewReader([]byte("hello"))
	dst := &bytes.Buffer{}
	n, err := copyWithIncrease(dst, src)
	require.ErrorIs(t, err, io.EOF)
	assert.Equal(t, int64(5), n)
	assert.Equal(t, "hello", dst.String())
}

func TestCopyWithIncreaseGrowsAfterThreshold(t *testing.T) {
	payload := make([]byte, copyIncreaseThreshold+pool.RelayBufferSize)
	for i := range payload {
		payload[i] = byte(i)
	}
	src := &recordingReader{r: bytes.NewReader(payload)}
	dst := &bytes.Buffer{}
	n, err := copyWithIncrease(dst, src)
	require.ErrorIs(t, err, io.EOF)
	assert.Equal(t, int64(len(payload)), n)
	assert.Equal(t, payload, dst.Bytes())
	require.Greater(t, len(src.sizes), 1)
	assert.Equal(t, min(8*1024, pool.RelayBufferSize), src.sizes[0])
	assert.Equal(t, 65535, src.sizes[len(src.sizes)-1])
	for _, size := range src.sizes {
		if size != min(8*1024, pool.RelayBufferSize) && size != pool.RelayBufferSize {
			assert.Equal(t, 65535, size)
		}
	}
}

func TestCopyPooledIncreaseReusesBuffer(t *testing.T) {
	payload := make([]byte, copyIncreaseThreshold+pool.RelayBufferSize+2*65535)
	src := &recordingReader{r: bytes.NewReader(payload)}
	dst := &bytes.Buffer{}
	n, err := copyWithIncrease(dst, src)
	require.ErrorIs(t, err, io.EOF)
	assert.Equal(t, int64(len(payload)), n)

	var small, large uintptr
	var smallN, largeN int
	for i, size := range src.sizes {
		ptr := src.ptrs[i]
		switch size {
		case min(8*1024, pool.RelayBufferSize):
			if small == 0 {
				small = ptr
			}
			assert.Equal(t, small, ptr)
			smallN++
		case pool.RelayBufferSize:
			// intermediate tier
		case 65535:
			if large == 0 {
				large = ptr
			}
			assert.Equal(t, large, ptr)
			largeN++
		default:
			t.Fatalf("unexpected buffer len %d", size)
		}
	}
	require.Greater(t, smallN, 1)
	require.Greater(t, largeN, 1)
	assert.NotEqual(t, small, large)
}

func TestCopyWithIncreaseReadError(t *testing.T) {
	want := errors.New("read failed")
	n, err := copyWithIncrease(io.Discard, &errAfter{remain: 8, err: want})
	assert.Equal(t, int64(8), n)
	assert.ErrorIs(t, err, want)
}

func TestCopyWithIncreaseWriteError(t *testing.T) {
	want := errors.New("write failed")
	n, err := copyWithIncrease(errWriter{n: 3, err: want}, bytes.NewReader([]byte("hello")))
	assert.Equal(t, int64(3), n)
	assert.ErrorIs(t, err, want)
}

func TestCopyWithIncreaseShortWrite(t *testing.T) {
	n, err := copyWithIncrease(errWriter{n: 2}, bytes.NewReader([]byte("hello")))
	assert.Equal(t, int64(2), n)
	assert.ErrorIs(t, err, io.ErrShortWrite)
}

func TestRelayClosesOnEOFPeer(t *testing.T) {
	leftConn, rightConn := net.Pipe()

	done := make(chan struct{})
	go func() {
		defer close(done)
		Relay(leftConn, rightConn)
	}()

	// Drain what the relay delivers, so writes never block.
	got := make(chan []byte, 1)
	go func() {
		buf := make([]byte, 5)
		_, err := io.ReadFull(rightConn, buf)
		if err == nil {
			got <- buf
		}
	}()

	if _, err := leftConn.Write([]byte("hello")); err != nil {
		t.Fatalf("write: %v", err)
	}
	select {
	case b := <-got:
		assert.Equal(t, "hello", string(b))
	case <-time.After(time.Second):
		t.Fatal("payload not relayed")
	}

	// Peer closes its write side entirely: source EOF for the relay.
	if err := leftConn.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Relay did not return after peer EOF")
	}

	// Both relayed conns must be fully closed, not half-closed.
	if _, err := rightConn.Read(make([]byte, 1)); err == nil {
		t.Error("rightConn still readable: Relay half-closed instead of closing")
	}
	if _, err := leftConn.Read(make([]byte, 1)); err == nil {
		t.Error("leftConn still readable: Relay half-closed instead of closing")
	}
}

type countReader struct {
	io.Reader
	n *atomic.Int64
}

func (r countReader) UnwrapReader() (io.Reader, []CountFunc) {
	n := r.n
	return r.Reader, []CountFunc{func(v int64) { n.Add(v) }}
}

func (r countReader) ReaderReplaceable() bool { return true }

func (r countReader) Upstream() any { return r.Reader }

type countWriter struct {
	io.Writer
	n *atomic.Int64
}

func (w countWriter) UnwrapWriter() (io.Writer, []CountFunc) {
	n := w.n
	return w.Writer, []CountFunc{func(v int64) { n.Add(v) }}
}

func (w countWriter) WriterReplaceable() bool { return true }

func (w countWriter) Upstream() any { return w.Writer }

func TestCopyWithIncreaseCountFuncOnReplaceable(t *testing.T) {
	payload := []byte("count-me")
	var got atomic.Int64
	n, err := copyWithIncrease(io.Discard, countReader{Reader: bytes.NewReader(payload), n: &got})
	require.ErrorIs(t, err, io.EOF)
	assert.Equal(t, int64(len(payload)), n)
	assert.Equal(t, int64(len(payload)), got.Load())
}

func TestCopyWithIncreaseCachedThenPooled(t *testing.T) {
	inner, server := net.Pipe()
	defer inner.Close()
	defer server.Close()

	go func() {
		_, _ = server.Write([]byte("world"))
		_ = server.Close()
	}()

	src := NewCachedConn(inner, []byte("hello"))
	dst := &bytes.Buffer{}
	n, err := copyWithIncrease(dst, src)
	require.ErrorIs(t, err, io.EOF)
	assert.Equal(t, int64(10), n)
	assert.Equal(t, "helloworld", dst.String())
}

func TestBufferedConnResidualPeekBlocksSyscall(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	bc := NewBufferedConn(c1)
	go func() {
		_, _ = c2.Write([]byte("AB"))
	}()
	b, err := bc.Peek(1)
	require.NoError(t, err)
	require.Equal(t, []byte("A"), b)
	assert.False(t, bc.ReaderReplaceable())

	_, err = bc.SyscallConn()
	require.Error(t, err)
	assert.ErrorIs(t, err, errBufferedConnPeekResidual)
	_, isSyscall := any(bc).(syscall.Conn)
	assert.True(t, isSyscall, "BufferedConn implements syscall.Conn so Copy type-asserts it")
}
