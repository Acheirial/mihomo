package process

import (
	"net/netip"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProcessCacheTTLAndCap(t *testing.T) {
	processCache.Clear()
	old := processLookup
	t.Cleanup(func() {
		processLookup = old
		processCache.Clear()
	})

	var calls atomic.Int32
	processLookup = func(network string, ip netip.Addr, srcPort int) (uint32, string, error) {
		calls.Add(1)
		return uint32(srcPort), "/bin/cached", nil
	}

	ip := netip.MustParseAddr("127.0.0.1")
	uid, path, err := FindProcessName(TCP, ip, 1234)
	require.NoError(t, err)
	assert.Equal(t, uint32(1234), uid)
	assert.Equal(t, "/bin/cached", path)
	assert.Equal(t, int32(1), calls.Load())

	uid, path, err = FindProcessName(TCP, ip, 1234)
	require.NoError(t, err)
	assert.Equal(t, uint32(1234), uid)
	assert.Equal(t, "/bin/cached", path)
	assert.Equal(t, int32(1), calls.Load(), "same five-tuple within TTL must not look up again")

	_, _, err = FindProcessName(UDP, ip, 1234)
	require.NoError(t, err)
	assert.Equal(t, int32(2), calls.Load(), "network is part of the cache key")

	time.Sleep(processCacheTTL + 50*time.Millisecond)
	_, _, err = FindProcessName(TCP, ip, 1234)
	require.NoError(t, err)
	assert.Equal(t, int32(3), calls.Load(), "expired entry must look up again")

	processCache.Clear()
	calls.Store(0)
	for i := 0; i < processCacheSize+8; i++ {
		_, _, err = FindProcessName(TCP, ip, 20000+i)
		require.NoError(t, err)
	}
	assert.Equal(t, int32(processCacheSize+8), calls.Load())
	_, _, err = FindProcessName(TCP, ip, 20000)
	require.NoError(t, err)
	assert.Equal(t, int32(processCacheSize+9), calls.Load(), "LRU cap 256 must evict the oldest key")
}
