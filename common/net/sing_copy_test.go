package net

import (
	"bytes"
	"errors"
	"io"
	"testing"

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
	require.NoError(t, err)
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
	require.NoError(t, err)
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
