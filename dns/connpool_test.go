package dns

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	D "github.com/miekg/dns"
	"github.com/stretchr/testify/require"
)

type countingDialer struct {
	dials atomic.Int32
	serve func(net.Conn)
}

func (d *countingDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	d.dials.Add(1)
	client, server := net.Pipe()
	go func() {
		defer server.Close()
		if d.serve != nil {
			d.serve(server)
		}
	}()
	return client, nil
}

func serveDNS(conn net.Conn) {
	co := &D.Conn{Conn: conn, UDPSize: 4096}
	for {
		msg, err := co.ReadMsg()
		if err != nil {
			return
		}
		resp := new(D.Msg)
		resp.SetReply(msg)
		resp.Answer = []D.RR{
			&D.A{
				Hdr: D.RR_Header{Name: msg.Question[0].Name, Rrtype: D.TypeA, Class: D.ClassINET, Ttl: 60},
				A:   net.IPv4(1, 2, 3, 4),
			},
		}
		if err := co.WriteMsg(resp); err != nil {
			return
		}
	}
}

func queryA() *D.Msg {
	m := new(D.Msg)
	m.SetQuestion(D.Fqdn("example.org."), D.TypeA)
	return m
}

func newUDPClient(d contextDialer) *client {
	return &client{
		host:   "1.1.1.1",
		port:   "53",
		dialer: d,
		schema: "udp",
	}
}

func TestUDPConnPoolReuse(t *testing.T) {
	d := &countingDialer{serve: serveDNS}
	c := newUDPClient(d)
	ctx := context.Background()

	msg, err := c.ExchangeContext(ctx, queryA())
	require.NoError(t, err)
	require.NotNil(t, msg)
	require.Equal(t, int32(1), d.dials.Load())

	msg, err = c.ExchangeContext(ctx, queryA())
	require.NoError(t, err)
	require.NotNil(t, msg)
	require.Equal(t, int32(1), d.dials.Load(), "consecutive UDP Exchange must reuse the pooled conn")
}

func TestUDPConnPoolIdleExpiry(t *testing.T) {
	d := &countingDialer{serve: serveDNS}
	c := newUDPClient(d)
	ctx := context.Background()

	_, err := c.ExchangeContext(ctx, queryA())
	require.NoError(t, err)
	require.Equal(t, int32(1), d.dials.Load())

	c.pool.mu.Lock()
	c.pool.idle = time.Now().Add(-dnsUDPIdleTimeout - time.Second)
	c.pool.mu.Unlock()

	_, err = c.ExchangeContext(ctx, queryA())
	require.NoError(t, err)
	require.Equal(t, int32(2), d.dials.Load(), "idle > 30s must dial a new UDP conn")
}

func TestUDPConnPoolResetConnection(t *testing.T) {
	d := &countingDialer{serve: serveDNS}
	c := newUDPClient(d)
	ctx := context.Background()

	_, err := c.ExchangeContext(ctx, queryA())
	require.NoError(t, err)
	require.Equal(t, int32(1), d.dials.Load())

	c.ResetConnection()

	_, err = c.ExchangeContext(ctx, queryA())
	require.NoError(t, err)
	require.Equal(t, int32(2), d.dials.Load(), "ResetConnection must drop the pooled conn")
}

func TestUDPConnPoolReleaseOnWriteFail(t *testing.T) {
	d := &countingDialer{serve: func(conn net.Conn) {
		_ = conn.Close()
	}}
	c := newUDPClient(d)
	ctx := context.Background()

	_, err := c.ExchangeContext(ctx, queryA())
	require.Error(t, err)
	require.Equal(t, int32(1), d.dials.Load())

	c.pool.mu.Lock()
	pooled := c.pool.conn
	c.pool.mu.Unlock()
	require.Nil(t, pooled, "write fail must Release(false) and not keep the conn")

	d.serve = serveDNS
	_, err = c.ExchangeContext(ctx, queryA())
	require.NoError(t, err)
	require.Equal(t, int32(2), d.dials.Load(), "failed conn must not be reused")
}

func TestUDPConnPoolAcquireDialError(t *testing.T) {
	p := &udpConnPool{}
	want := errors.New("dial failed")
	_, err := p.Acquire(context.Background(), func(context.Context) (net.Conn, error) {
		return nil, want
	})
	require.Equal(t, want, err)
}

func TestTCPExchangeDoesNotUsePool(t *testing.T) {
	d := &countingDialer{serve: serveDNS}
	c := &client{
		host:   "1.1.1.1",
		port:   "53",
		dialer: d,
		schema: "tcp",
	}
	ctx := context.Background()

	_, err := c.ExchangeContext(ctx, queryA())
	require.NoError(t, err)
	_, err = c.ExchangeContext(ctx, queryA())
	require.NoError(t, err)
	require.Equal(t, int32(2), d.dials.Load(), "TCP must dial per query")
}
