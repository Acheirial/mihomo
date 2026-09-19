package outbound

import (
	"net"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

type netConnOnly struct {
	net.Conn
}

func (c netConnOnly) NetConn() net.Conn { return c.Conn }

func listenPair(t *testing.T) (client, peer net.Conn) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	peerCh := make(chan net.Conn, 1)
	go func() {
		c, accErr := ln.Accept()
		if accErr != nil {
			peerCh <- nil
			return
		}
		peerCh <- c
	}()

	client, err = net.Dial("tcp", ln.Addr().String())
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	peer = <-peerCh
	require.NotNil(t, peer)
	t.Cleanup(func() { _ = peer.Close() })
	return client, peer
}

func TestNewConnSyscallConnOnTCP(t *testing.T) {
	client, _ := listenPair(t)
	wrapped := NewConn(client, NewDirect())
	sc, ok := wrapped.(syscall.Conn)
	require.True(t, ok)
	rc, err := sc.SyscallConn()
	require.NoError(t, err)
	require.NotNil(t, rc)
}

func TestNewConnSyscallConnSkipsNetConn(t *testing.T) {
	client, _ := listenPair(t)
	wrapped := NewConn(netConnOnly{Conn: client}, NewDirect())
	sc, ok := wrapped.(syscall.Conn)
	require.True(t, ok)
	_, err := sc.SyscallConn()
	require.Error(t, err, "tls.Conn.NetConn() is ciphertext TCP; SyscallConn must not peel it")
}
