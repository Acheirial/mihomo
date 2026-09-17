package dns

import (
	"context"
	"fmt"
	"net"
	"strings"
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
	port   string
	host   string
	dialer contextDialer
	schema string
	pool   udpConnPool
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

	var conn net.Conn
	var err error
	if c.schema == "udp" {
		conn, err = c.pool.Acquire(ctx, dial)
	} else {
		conn, err = dial(ctx)
	}
	if err != nil {
		return nil, err
	}

	reuse := c.schema == "udp"
	defer func() {
		if reuse {
			c.pool.Release(conn, true)
			return
		}
		if c.schema == "udp" {
			c.pool.Release(conn, false)
			return
		}
		_ = conn.Close()
	}()

	// miekg/dns ExchangeContext doesn't respond to context cancel.
	// this is a workaround
	type result struct {
		msg     *D.Msg
		err     error
		tcpConn net.Conn
		dropUDP bool
	}
	ch := make(chan result, 1)
	go func() {
		dClient := &D.Client{
			UDPSize: 4096,
			Timeout: 5 * time.Second,
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
			var tcpConn net.Conn
			tcpConn, err = c.dialer.DialContext(ctx, network, addr)
			if err != nil {
				ch <- result{msg: msg, err: err, dropUDP: true}
				return
			}
			dConn.Conn = tcpConn
			msg, _, err = dClient.ExchangeWithConn(m, dConn)
			ch <- result{msg: msg, err: err, tcpConn: tcpConn, dropUDP: true}
			return
		}

		ch <- result{msg: msg, err: err}
	}()

	select {
	case <-ctx.Done():
		reuse = false
		return nil, ctx.Err()
	case ret := <-ch:
		if ret.tcpConn != nil {
			_ = ret.tcpConn.Close()
		}
		if ret.err != nil || ret.dropUDP {
			reuse = false
			return ret.msg, ret.err
		}
		return ret.msg, nil
	}
}

func (c *client) ResetConnection() {
	c.pool.Close()
}

func newClient(addr string, resolver resolver.Resolver, netType string, params map[string]string, proxyAdapter C.ProxyAdapter, proxyName string) *client {
	host, port, _ := net.SplitHostPort(addr)
	c := &client{
		port:   port,
		host:   host,
		dialer: newDNSDialer(resolver, proxyAdapter, proxyName),
		schema: "udp",
	}
	if strings.HasPrefix(netType, "tcp") {
		c.schema = "tcp"
	}
	return c
}
