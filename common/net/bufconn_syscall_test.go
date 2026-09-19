package net

import (
	"net"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBufferedConnSyscallConnRejectsResidualPeek(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()
	go func() {
		_, _ = c2.Write([]byte("hello"))
	}()

	bc := NewBufferedConn(c1)
	_, err := bc.Peek(1)
	require.NoError(t, err)
	require.False(t, bc.ReaderReplaceable())

	_, err = bc.SyscallConn()
	require.ErrorIs(t, err, errBufferedConnPeekResidual)
}

func TestBufferedConnSyscallConnAfterDrain(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	peerCh := make(chan net.Conn, 1)
	go func() {
		c, accErr := ln.Accept()
		if accErr != nil {
			peerCh <- nil
			return
		}
		peerCh <- c
	}()

	client, err := net.Dial("tcp", ln.Addr().String())
	require.NoError(t, err)
	defer client.Close()

	peer := <-peerCh
	require.NotNil(t, peer)
	defer peer.Close()

	bc := NewBufferedConn(client)
	rc, err := bc.SyscallConn()
	require.NoError(t, err)
	require.NotNil(t, rc)

	_, err = peer.Write([]byte("xy"))
	require.NoError(t, err)
	peeked, err := bc.Peek(2)
	require.NoError(t, err)
	require.Equal(t, []byte("xy"), peeked)

	_, err = bc.SyscallConn()
	require.ErrorIs(t, err, errBufferedConnPeekResidual)

	n, err := bc.Discard(2)
	require.NoError(t, err)
	require.Equal(t, 2, n)
	require.True(t, bc.ReaderReplaceable())

	rc, err = bc.SyscallConn()
	require.NoError(t, err)
	require.NotNil(t, rc)
}

func TestNewBufferedConnReusesExisting(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	bc := NewBufferedConn(c1)
	require.Same(t, bc, NewBufferedConn(bc))
}
