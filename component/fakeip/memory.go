package fakeip

import (
	"net/netip"
	"sync/atomic"

	"github.com/metacubex/mihomo/common/lru"
	"github.com/metacubex/mihomo/common/pool"
)

type memoryStore struct {
	cacheIP   *lru.LruCache[string, netip.Addr]
	cacheHost *lru.LruCache[netip.Addr, string]
	// inEvict is set while an OnEvict callback runs so the twin Delete's
	// OnEvict does not re-lock the LRU whose callback is already on the stack.
	inEvict atomic.Bool
}

// GetByHost implements store.GetByHost
func (m *memoryStore) GetByHost(host string) (netip.Addr, bool) {
	if ip, exist := m.cacheIP.Get(host); exist {
		// keep host→ip and ip→host LRU ranks aligned on Lookup
		m.cacheHost.Get(ip)
		return ip, true
	}
	return netip.Addr{}, false
}

// PutByHost implements store.PutByHost
func (m *memoryStore) PutByHost(host string, ip netip.Addr) {
	m.cacheIP.Set(pool.Intern(host), ip)
}

// GetByIP implements store.GetByIP.
// Get MoveToBacks cacheHost so LookBack keeps the reverse mapping alive.
// Does not touch cacheIP: taking the opposite LRU lock here recouples LookBack
// onto the Lookup/evict path.
func (m *memoryStore) GetByIP(ip netip.Addr) (string, bool) {
	return m.cacheHost.Get(ip)
}

// PutByIP implements store.PutByIP
func (m *memoryStore) PutByIP(ip netip.Addr, host string) {
	m.cacheHost.Set(ip, pool.Intern(host))
}

// DelByIP implements store.DelByIP
func (m *memoryStore) DelByIP(ip netip.Addr) {
	if host, exist := m.cacheHost.Get(ip); exist {
		m.cacheIP.Delete(host)
	}
	m.cacheHost.Delete(ip)
}

// Exist implements store.Exist
func (m *memoryStore) Exist(ip netip.Addr) bool {
	return m.cacheHost.Exist(ip)
}

// CloneTo implements store.CloneTo
// only for memoryStore to memoryStore
func (m *memoryStore) CloneTo(store store) {
	if ms, ok := store.(*memoryStore); ok {
		m.cacheIP.CloneTo(ms.cacheIP)
		m.cacheHost.CloneTo(ms.cacheHost)
	}
}

// FlushFakeIP implements store.FlushFakeIP
func (m *memoryStore) FlushFakeIP() error {
	m.cacheIP.Clear()
	m.cacheHost.Clear()
	return nil
}

func newMemoryStore(size int) *memoryStore {
	s := &memoryStore{}
	s.cacheHost = lru.New[netip.Addr, string](
		lru.WithSize[netip.Addr, string](size),
		lru.WithEvict[netip.Addr, string](func(_ netip.Addr, host string) {
			if s.cacheIP == nil || !s.inEvict.CompareAndSwap(false, true) {
				return
			}
			defer s.inEvict.Store(false)
			s.cacheIP.Delete(host)
		}),
	)
	s.cacheIP = lru.New[string, netip.Addr](
		lru.WithSize[string, netip.Addr](size),
		lru.WithEvict[string, netip.Addr](func(_ string, ip netip.Addr) {
			if s.cacheHost == nil || !s.inEvict.CompareAndSwap(false, true) {
				return
			}
			defer s.inEvict.Store(false)
			s.cacheHost.Delete(ip)
		}),
	)
	return s
}
