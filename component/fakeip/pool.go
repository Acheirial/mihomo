package fakeip

import (
	"errors"
	"net/netip"
	"strings"
	"sync"

	"github.com/metacubex/mihomo/component/profile/cachefile"

	"go4.org/netipx"
)

const (
	offsetKey = "key-offset-fake-ip"
	cycleKey  = "key-cycle-fake-ip"
)

type store interface {
	GetByHost(host string) (netip.Addr, bool)
	PutByHost(host string, ip netip.Addr)
	GetByIP(ip netip.Addr) (string, bool)
	PutByIP(ip netip.Addr, host string)
	DelByIP(ip netip.Addr)
	Exist(ip netip.Addr) bool
	CloneTo(store)
	FlushFakeIP() error
}

// Pool is an implementation about fake ip generator without storage
type Pool struct {
	gateway netip.Addr
	first   netip.Addr
	last    netip.Addr
	offset  netip.Addr
	cycle   bool
	allocMu sync.Mutex
	ipnet   netip.Prefix
	store   store
}

// Lookup return a fake ip with host
func (p *Pool) Lookup(host string) netip.Addr {
	// RFC4343: DNS Case Insensitive, we SHOULD return result with all cases.
	host = strings.ToLower(host)
	if ip, exist := p.store.GetByHost(host); exist {
		return ip
	}

	p.allocMu.Lock()
	defer p.allocMu.Unlock()

	// Recheck under allocMu: another Lookup may have filled the mapping
	// while we waited, and get() must not allocate a second IP.
	if ip, exist := p.store.GetByHost(host); exist {
		return ip
	}

	ip := p.get(host)
	p.store.PutByHost(host, ip)
	return ip
}

// LookBack return host with the fake ip
func (p *Pool) LookBack(ip netip.Addr) (string, bool) {
	return p.store.GetByIP(ip)
}

// Exist returns if given ip exists in fake-ip pool
func (p *Pool) Exist(ip netip.Addr) bool {
	return p.store.Exist(ip)
}

// Gateway return gateway ip
func (p *Pool) Gateway() netip.Addr {
	return p.gateway
}

// Broadcast return the last ip
func (p *Pool) Broadcast() netip.Addr {
	return p.last
}

// IPNet return raw ipnet
func (p *Pool) IPNet() netip.Prefix {
	return p.ipnet
}

func (p *Pool) CloneFrom(o *Pool) {
	o.store.CloneTo(p.store)
}

func (p *Pool) get(host string) netip.Addr {
	offset := p.offset.Next()

	cycle := p.cycle
	if !offset.Less(p.last) {
		cycle = true
		offset = p.first
	}

	if cycle || p.store.Exist(offset) {
		p.store.DelByIP(offset)
	}

	// Put the mapping before exposing offset so a concurrent LookBack
	// cannot observe a recycled IP with no host (fake DNS record missing).
	p.store.PutByIP(offset, host)
	p.offset = offset
	p.cycle = cycle
	return offset
}

func (p *Pool) FlushFakeIP() error {
	p.allocMu.Lock()
	defer p.allocMu.Unlock()
	err := p.store.FlushFakeIP()
	if err == nil {
		p.cycle = false
		p.offset = p.first.Prev()
	}
	return err
}

func (p *Pool) StoreState() {
	if s, ok := p.store.(*cachefileStore); ok {
		p.allocMu.Lock()
		offset, cycle := p.offset, p.cycle
		p.allocMu.Unlock()
		s.PutByHost(offsetKey, offset)
		if cycle {
			s.PutByHost(cycleKey, offset)
		}
	}
}

func (p *Pool) restoreState() {
	if s, ok := p.store.(*cachefileStore); ok {
		if _, exist := s.GetByHost(cycleKey); exist {
			p.cycle = true
		}

		if offset, exist := s.GetByHost(offsetKey); exist {
			if p.ipnet.Contains(offset) {
				p.offset = offset
			} else {
				_ = p.FlushFakeIP()
			}
		} else if s.Exist(p.first) {
			_ = p.FlushFakeIP()
		}
	}
}

type Options struct {
	IPNet netip.Prefix

	// Size sets the maximum number of entries in memory
	// and does not work if Persistence is true
	Size int

	// Persistence will save the data to disk.
	// Size will not work and record will be fully stored.
	Persistence bool
}

// New return Pool instance
func New(options Options) (*Pool, error) {
	var (
		hostAddr = options.IPNet.Masked().Addr()
		gateway  = hostAddr.Next()
		first    = gateway.Next().Next().Next() // default start with 198.18.0.4
		last     = netipx.PrefixLastIP(options.IPNet)
	)

	if !options.IPNet.IsValid() || !first.IsValid() || !first.Less(last) {
		return nil, errors.New("ipnet don't have valid ip")
	}

	pool := &Pool{
		gateway: gateway,
		first:   first,
		last:    last,
		offset:  first.Prev(),
		cycle:   false,
		ipnet:   options.IPNet,
	}
	if options.Persistence {
		pool.store = newCachefileStore(cachefile.Cache(), options.IPNet)
	} else {
		pool.store = newMemoryStore(options.Size)
	}

	pool.restoreState()

	return pool, nil
}
