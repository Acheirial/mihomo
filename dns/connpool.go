package dns

import (
	"context"
	"net"
	"sync"
	"time"

	"github.com/metacubex/mihomo/common/deque"
)

// dnsUDPIdleTimeout bounds how long a pooled UDP conn is reused between exchanges.
const dnsUDPIdleTimeout = 30 * time.Second

// dnsTCPIdleTimeout bounds how long a pooled TCP conn is reused between exchanges.
const dnsTCPIdleTimeout = 30 * time.Second

// dnsIdlePoolMax is the LIFO cap for TCP idle conns (aligned with DoT).
const dnsIdlePoolMax = 8

// dnsClientTimeout bounds a single miekg/dns exchange (see client.go): it is
// also the upper bound the caller waits for the background exchange goroutine
// to release its conn.
const dnsClientTimeout = 5 * time.Second

type idleConn struct {
	conn net.Conn
	idle time.Time
}

// idleConnPool is a LIFO idle pool. UDP keeps max=1 (single-slot, no multiplexer);
// TCP uses max=dnsIdlePoolMax. Error/ctx cancel paths must Release(false).
type idleConnPool struct {
	mu      sync.Mutex
	conns   deque.Deque[idleConn]
	max     int
	timeout time.Duration
}

type udpConnPool = idleConnPool

func newUDPConnPool() idleConnPool {
	return idleConnPool{max: 1, timeout: dnsUDPIdleTimeout}
}

func newTCPConnPool() idleConnPool {
	return idleConnPool{max: dnsIdlePoolMax, timeout: dnsTCPIdleTimeout}
}

func (p *idleConnPool) Acquire(ctx context.Context, dial func(context.Context) (net.Conn, error)) (net.Conn, error) {
	now := time.Now()
	p.mu.Lock()
	for p.conns.Len() > 0 {
		item := p.conns.PopBack()
		if now.Sub(item.idle) < p.timeout {
			p.mu.Unlock()
			return item.conn, nil
		}
		_ = item.conn.Close()
	}
	p.mu.Unlock()
	return dial(ctx)
}

func (p *idleConnPool) Release(conn net.Conn, reuse bool) {
	if conn == nil {
		return
	}
	if !reuse {
		_ = conn.Close()
		return
	}
	p.mu.Lock()
	if p.max > 0 && p.conns.Len() >= p.max {
		old := p.conns.PopFront()
		p.mu.Unlock()
		_ = old.conn.Close()
		p.mu.Lock()
	}
	p.conns.PushBack(idleConn{conn: conn, idle: time.Now()})
	p.mu.Unlock()
}

func (p *idleConnPool) Close() {
	p.mu.Lock()
	for p.conns.Len() > 0 {
		item := p.conns.PopFront()
		_ = item.conn.Close()
	}
	p.mu.Unlock()
}
