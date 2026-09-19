package xhttp

import (
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUploadQueueMaxPackets(t *testing.T) {
	q := NewUploadQueue(2)
	assert.NoError(t, q.Push(Packet{Seq: 0, Payload: []byte{'0'}}))
	assert.NoError(t, q.Push(Packet{Seq: 1, Payload: []byte{'1'}}))
	q.mu.Lock()
	assert.Equal(t, 2, len(q.packets))
	q.mu.Unlock()

	done := make(chan error, 1)
	go func() {
		done <- q.Push(Packet{Seq: 2, Payload: []byte{'2'}})
	}()

	buf := make([]byte, 20)
	n, err := q.Read(buf)
	assert.Equal(t, 1, n)
	assert.Equal(t, []byte{'0'}, buf[:n])
	assert.NoError(t, err)
	assert.NoError(t, <-done)

	q.mu.Lock()
	assert.LessOrEqual(t, len(q.packets), 2)
	q.mu.Unlock()

	n, err = q.Read(buf)
	assert.Equal(t, 1, n)
	assert.Equal(t, []byte{'1'}, buf[:n])
	assert.NoError(t, err)

	n, err = q.Read(buf)
	assert.Equal(t, 1, n)
	assert.Equal(t, []byte{'2'}, buf[:n])
	assert.NoError(t, err)
}

func TestUploadQueueReassemblyBound(t *testing.T) {
	q := NewUploadQueue(2)
	assert.NoError(t, q.Push(Packet{Seq: 1, Payload: []byte{'1'}}))
	assert.NoError(t, q.Push(Packet{Seq: 2, Payload: []byte{'2'}}))

	buf := make([]byte, 20)
	n, err := q.Read(buf)
	assert.Equal(t, 0, n)
	assert.ErrorIs(t, err, ErrQueueTooLarge)
}

func TestUploadQueueSeqGap(t *testing.T) {
	q := NewUploadQueue(2)
	err := q.Push(Packet{Seq: 3, Payload: []byte{'3'}})
	assert.ErrorIs(t, err, ErrQueueTooLarge)

	assert.NoError(t, q.Push(Packet{Seq: 2, Payload: []byte{'2'}}))
	q.mu.Lock()
	assert.Equal(t, 1, len(q.packets))
	q.mu.Unlock()
}

func TestUploadQueueCloseDropsPayloads(t *testing.T) {
	q := NewUploadQueue(2)
	assert.NoError(t, q.Push(Packet{Seq: 0, Payload: []byte{'a', 'b'}}))

	buf := make([]byte, 1)
	n, err := q.Read(buf)
	assert.Equal(t, 1, n)
	assert.NoError(t, err)

	assert.NoError(t, q.Push(Packet{Seq: 1, Payload: []byte{'1'}}))
	pr, pw := io.Pipe()
	assert.NoError(t, q.Push(Packet{Reader: pr}))

	assert.NoError(t, q.Close())
	_ = pw.Close()

	assert.ErrorIs(t, q.Push(Packet{Seq: 2, Payload: []byte{'2'}}), io.ErrClosedPipe)

	q.mu.Lock()
	defer q.mu.Unlock()
	assert.Empty(t, q.packets)
	assert.Nil(t, q.buf)
	assert.Nil(t, q.reader)
}
