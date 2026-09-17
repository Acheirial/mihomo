package dns

import (
	"context"
	"net"
	"sync"
	"time"
)

const dnsUDPIdleTimeout = 30 * time.Second

type udpConnPool struct {
	mu   sync.Mutex
	conn net.Conn
	idle time.Time
}

func (p *udpConnPool) Acquire(ctx context.Context, dial func(context.Context) (net.Conn, error)) (net.Conn, error) {
	p.mu.Lock()
	conn := p.conn
	if conn != nil && time.Since(p.idle) < dnsUDPIdleTimeout {
		p.conn = nil
		p.mu.Unlock()
		return conn, nil
	}
	if conn != nil {
		p.conn = nil
		p.mu.Unlock()
		_ = conn.Close()
	} else {
		p.mu.Unlock()
	}
	return dial(ctx)
}

func (p *udpConnPool) Release(conn net.Conn, reuse bool) {
	if conn == nil {
		return
	}
	if !reuse {
		_ = conn.Close()
		p.mu.Lock()
		if p.conn == conn {
			p.conn = nil
		}
		p.mu.Unlock()
		return
	}
	p.mu.Lock()
	old := p.conn
	p.conn = conn
	p.idle = time.Now()
	p.mu.Unlock()
	if old != nil && old != conn {
		_ = old.Close()
	}
}

func (p *udpConnPool) Close() {
	p.mu.Lock()
	conn := p.conn
	p.conn = nil
	p.mu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
}
