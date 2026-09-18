package dns

import (
	"context"
	"errors"
	"net"
	"runtime"
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

// TestExchangeContextCancelReleasesGoroutine verifies that cancelling the
// ExchangeContext context does not leak the background exchange goroutine and
// that the abandoned pooled conn is never handed to a later caller.
func TestExchangeContextCancelReleasesGoroutine(t *testing.T) {
	d := &countingDialer{serve: func(conn net.Conn) {
		// never reply: consume the query so the exchange's write completes,
		// then block in Read until the conn is closed (by ctx cancel), which
		// ends this goroutine — otherwise the mock server itself would show up
		// as a leaked goroutine.
		buf := make([]byte, 1024)
		for {
			if _, err := conn.Read(buf); err != nil {
				return
			}
		}
	}}
	c := newUDPClient(d)

	waitGoroutines := func() int {
		// let the runtime settle so blocked-but-not-yet-running goroutines count
		time.Sleep(10 * time.Millisecond)
		return runtime.NumGoroutine()
	}

	base := waitGoroutines()

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	_, err := c.ExchangeContext(ctx, queryA())
	require.ErrorIs(t, err, context.Canceled, "cancelled ctx must return ctx.Err() promptly")

	// the goroutine must not outlive the call past the exchange timeout
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) && waitGoroutines() > base {
	}
	require.LessOrEqual(t, waitGoroutines(), base, "exchange goroutine outlived ExchangeContext")

	// the abandoned conn must not have been put back in the pool
	c.pool.mu.Lock()
	pooled := c.pool.conn
	c.pool.mu.Unlock()
	require.Nil(t, pooled, "conn abandoned after ctx cancel must not be pooled")

	// a second call must dial again instead of reusing the abandoned conn.
	// Its own ctx deadline also bounds the read, so it does not block for 5s.
	ctx2, cancel2 := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel2()
	_, err = c.ExchangeContext(ctx2, queryA())
	require.Error(t, err, "mock server never replies")
	require.Equal(t, int32(2), d.dials.Load(), "abandoned conn must not be reused")
}
