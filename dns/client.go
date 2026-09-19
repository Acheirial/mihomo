package dns

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/metacubex/mihomo/component/resolver"
	C "github.com/metacubex/mihomo/constant"
	"github.com/metacubex/mihomo/log"

	D "github.com/miekg/dns"
)

type contextDialer interface {
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
}

type client struct {
	port    string
	host    string
	dialer  contextDialer
	schema  string
	pool    idleConnPool
	tcpIdle idleConnPool
}

var _ dnsClient = (*client)(nil)

// Address implements dnsClient
func (c *client) Address() string {
	return fmt.Sprintf("%s://%s", c.schema, net.JoinHostPort(c.host, c.port))
}

func (c *client) ExchangeContext(ctx context.Context, m *D.Msg) (*D.Msg, error) {
	network := "udp"
	if c.schema != "udp" {
		network = "tcp"
	}

	addr := net.JoinHostPort(c.host, c.port)
	dial := func(ctx context.Context) (net.Conn, error) {
		return c.dialer.DialContext(ctx, network, addr)
	}

	conn, err := c.pool.Acquire(ctx, dial)
	if err != nil {
		return nil, err
	}

	reuse := true

	// The background exchange goroutine is the sole owner of conn for the
	// duration of the exchange: miekg/dns ignores ctx cancellation, so the
	// exchange runs to completion (bounded by the 5s timeout) unless conn is
	// closed. On ctx cancellation the select below closes conn, which unblocks
	// the goroutine's in-flight read and lets it finish immediately. done is
	// closed once the goroutine is finished with conn, so the deferred release
	// below only ever touches a conn that is actually idle.
	done := make(chan struct{})
	var mu sync.Mutex
	var released bool // true once conn has been released (by either path)

	releaseConn := func(keep bool) {
		mu.Lock()
		defer mu.Unlock()
		if released {
			return
		}
		released = true
		c.pool.Release(conn, keep)
	}

	defer func() {
		// Wait for the goroutine to be done with conn before handing it back.
		// The exchange is bounded by dClient.Timeout, so this is a bounded wait
		// and a wedged exchange cannot keep the caller forever.
		select {
		case <-done:
		case <-time.After(dnsClientTimeout):
			reuse = false
		}
		releaseConn(reuse)
	}()

	// miekg/dns ExchangeContext doesn't respond to context cancel.
	// this is a workaround
	type result struct {
		msg     *D.Msg
		err     error
		dropUDP bool
	}
	ch := make(chan result, 1)
	go func() {
		defer close(done)
		dClient := &D.Client{
			UDPSize: 4096,
			Timeout: dnsClientTimeout,
		}
		dConn := &D.Conn{
			Conn:    conn,
			UDPSize: dClient.UDPSize,
		}

		msg, _, err := dClient.ExchangeWithConn(m, dConn)

		// Resolvers MUST resend queries over TCP if they receive a truncated UDP response (with TC=1 set)!
		if msg != nil && msg.Truncated && network == "udp" {
			network = "tcp"
			log.Debugln("[DNS] Truncated reply from %s:%s for %s over UDP, retrying over TCP", c.host, c.port, m.Question[0].String())
			// The exchange continues over TCP: drop the UDP conn from the pool
			// now (a truncated reply means the path is not safe to reuse) and
			// mark it released so the outer defer never double-releases it.
			releaseConn(false)
			tcpDial := func(ctx context.Context) (net.Conn, error) {
				return c.dialer.DialContext(ctx, "tcp", addr)
			}
			var tcpConn net.Conn
			tcpConn, err = c.tcpPool().Acquire(ctx, tcpDial)
			if err != nil {
				ch <- result{msg: msg, err: err, dropUDP: true}
				return
			}
			dConn.Conn = tcpConn
			msg, _, err = dClient.ExchangeWithConn(m, dConn)
			keepTCP := err == nil
			c.tcpPool().Release(tcpConn, keepTCP)
			ch <- result{msg: msg, err: err, dropUDP: true}
			return
		}

		ch <- result{msg: msg, err: err}
	}()

	select {
	case <-ctx.Done():
		// The query was abandoned on conn, so it must not be reused. Closing it
		// here unblocks the goroutine's in-flight read immediately (miekg
		// returns a use-of-closed-conn error) so the goroutine finishes and
		// closes done within milliseconds, instead of hanging until the 5s
		// exchange timeout on a server that never replies. The outer defer's
		// releaseConn call below then becomes a no-op via the released flag.
		releaseConn(false)
		reuse = false
		return nil, ctx.Err()
	case ret := <-ch:
		if ret.err != nil || ret.dropUDP {
			reuse = false
		}
		return ret.msg, ret.err
	}
}

func (c *client) ResetConnection() {
	c.pool.Close()
	c.tcpIdle.Close()
}

// tcpPool is the idle pool used for truncated UDP→TCP retries. TCP clients
// reuse c.pool; UDP clients keep a separate LIFO≤8 pool so a truncated retry
// does not steal the UDP slot.
func (c *client) tcpPool() *idleConnPool {
	if c.schema == "tcp" {
		return &c.pool
	}
	return &c.tcpIdle
}

func newClient(addr string, resolver resolver.Resolver, netType string, params map[string]string, proxyAdapter C.ProxyAdapter, proxyName string) *client {
	host, port, _ := net.SplitHostPort(addr)
	c := &client{
		port:    port,
		host:    host,
		dialer:  newDNSDialer(resolver, proxyAdapter, proxyName),
		schema:  "udp",
		pool:    newUDPConnPool(),
		tcpIdle: newTCPConnPool(),
	}
	if strings.HasPrefix(netType, "tcp") {
		c.schema = "tcp"
		c.pool = newTCPConnPool()
	}
	return c
}
