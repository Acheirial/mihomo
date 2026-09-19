package net

import (
	"net"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

type netConnOnly struct {
	net.Conn
	inner net.Conn
}

func (c netConnOnly) NetConn() net.Conn { return c.inner }

type upstreamWrap struct {
	net.Conn
	inner any
}

func (c upstreamWrap) Upstream() any { return c.inner }

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

func TestFindWithUpstreamSkipsNetConn(t *testing.T) {
	client, _ := listenPair(t)
	wrapped := netConnOnly{inner: client}
	_, ok := FindWithUpstream[syscall.Conn](wrapped, nil)
	require.False(t, ok, "tls.Conn.NetConn() is the inner TCP; walking it lets splice skip TLS")
}

func TestFindWithUpstreamWalksWithUpstream(t *testing.T) {
	client, _ := listenPair(t)
	wrapped := upstreamWrap{inner: client}
	sc, ok := FindWithUpstream[syscall.Conn](wrapped, nil)
	require.True(t, ok)
	rc, err := sc.SyscallConn()
	require.NoError(t, err)
	require.NotNil(t, rc)
}

func TestFindUpstreamStillWalksNetConn(t *testing.T) {
	client, _ := listenPair(t)
	wrapped := netConnOnly{inner: client}
	sc, ok := FindUpstream[syscall.Conn](wrapped, nil)
	require.True(t, ok, "JLS UserFromConn still needs NetConn() to reach the inner tls.Conn")
	rc, err := sc.SyscallConn()
	require.NoError(t, err)
	require.NotNil(t, rc)
}
