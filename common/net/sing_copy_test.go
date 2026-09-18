package net

import (
	"bytes"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/metacubex/mihomo/common/pool"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingReader struct {
	r     io.Reader
	sizes []int
}

func (r *recordingReader) Read(p []byte) (int, error) {
	r.sizes = append(r.sizes, len(p))
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
	assert.Equal(t, pool.RelayBufferSize, src.sizes[0])
	assert.Equal(t, 65535, src.sizes[len(src.sizes)-1])
	for _, size := range src.sizes {
		if size != pool.RelayBufferSize {
			assert.Equal(t, 65535, size)
		}
	}
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
