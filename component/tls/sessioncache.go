package tls

import (
	"container/list"
	"sync"

	"github.com/metacubex/tls"
	utls "github.com/metacubex/utls"
)

type sessionEntry struct {
	ticket []byte
	state  []byte
}

// SharedClientSessionCache stores serialized TLS session tickets so both
// metacubex/tls and utls can resume from the same per-stack cache.
type SharedClientSessionCache struct {
	mu       sync.Mutex
	m        map[string]*list.Element
	q        *list.List
	capacity int
}

type sharedCacheElement struct {
	key   string
	entry *sessionEntry
}

func NewSharedClientSessionCache(capacity int) *SharedClientSessionCache {
	if capacity < 1 {
		capacity = 64
	}
	return &SharedClientSessionCache{
		m:        make(map[string]*list.Element),
		q:        list.New(),
		capacity: capacity,
	}
}

func (c *SharedClientSessionCache) put(sessionKey string, entry *sessionEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if elem, ok := c.m[sessionKey]; ok {
		if entry == nil {
			c.q.Remove(elem)
			delete(c.m, sessionKey)
			return
		}
		elem.Value.(*sharedCacheElement).entry = entry
		c.q.MoveToFront(elem)
		return
	}
	if entry == nil {
		return
	}
	if c.q.Len() >= c.capacity {
		elem := c.q.Back()
		old := elem.Value.(*sharedCacheElement)
		delete(c.m, old.key)
		old.key = sessionKey
		old.entry = entry
		c.q.MoveToFront(elem)
		c.m[sessionKey] = elem
		return
	}
	c.m[sessionKey] = c.q.PushFront(&sharedCacheElement{key: sessionKey, entry: entry})
}

func (c *SharedClientSessionCache) get(sessionKey string) (*sessionEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	elem, ok := c.m[sessionKey]
	if !ok {
		return nil, false
	}
	c.q.MoveToFront(elem)
	return elem.Value.(*sharedCacheElement).entry, true
}

func (c *SharedClientSessionCache) TLS() tls.ClientSessionCache {
	return tlsSessionCache{c}
}

func (c *SharedClientSessionCache) UTLS() utls.ClientSessionCache {
	return utlsSessionCache{c}
}

type tlsSessionCache struct {
	*SharedClientSessionCache
}

func (c tlsSessionCache) Get(sessionKey string) (*tls.ClientSessionState, bool) {
	entry, ok := c.get(sessionKey)
	if !ok || entry == nil {
		return nil, false
	}
	state, err := tls.ParseSessionState(entry.state)
	if err != nil {
		return nil, false
	}
	cs, err := tls.NewResumptionState(entry.ticket, state)
	if err != nil {
		return nil, false
	}
	return cs, true
}

func (c tlsSessionCache) Put(sessionKey string, cs *tls.ClientSessionState) {
	if cs == nil {
		c.put(sessionKey, nil)
		return
	}
	ticket, state, err := cs.ResumptionState()
	if err != nil || state == nil {
		c.put(sessionKey, nil)
		return
	}
	raw, err := state.Bytes()
	if err != nil {
		return
	}
	c.put(sessionKey, &sessionEntry{ticket: ticket, state: raw})
}

type utlsSessionCache struct {
	*SharedClientSessionCache
}

func (c utlsSessionCache) Get(sessionKey string) (*utls.ClientSessionState, bool) {
	entry, ok := c.get(sessionKey)
	if !ok || entry == nil {
		return nil, false
	}
	state, err := utls.ParseSessionState(entry.state)
	if err != nil {
		return nil, false
	}
	cs, err := utls.NewResumptionState(entry.ticket, state)
	if err != nil {
		return nil, false
	}
	return cs, true
}

func (c utlsSessionCache) Put(sessionKey string, cs *utls.ClientSessionState) {
	if cs == nil {
		c.put(sessionKey, nil)
		return
	}
	ticket, state, err := cs.ResumptionState()
	if err != nil || state == nil {
		c.put(sessionKey, nil)
		return
	}
	raw, err := state.Bytes()
	if err != nil {
		return
	}
	c.put(sessionKey, &sessionEntry{ticket: ticket, state: raw})
}
