package dialer

import (
	"context"
	"errors"
	"io"
	"net"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/metacubex/mihomo/component/resolver"

	"github.com/miekg/dns"
)

type staticResolver struct {
	ips []netip.Addr
}

func (r *staticResolver) LookupIP(context.Context, string) ([]netip.Addr, error) {
	return append([]netip.Addr(nil), r.ips...), nil
}
func (r *staticResolver) LookupIPv4(context.Context, string) ([]netip.Addr, error) {
	var out []netip.Addr
	for _, ip := range r.ips {
		if ip.Unmap().Is4() {
			out = append(out, ip)
		}
	}
	if len(out) == 0 {
		return nil, resolver.ErrIPNotFound
	}
	return out, nil
}
func (r *staticResolver) LookupIPv6(context.Context, string) ([]netip.Addr, error) {
	var out []netip.Addr
	for _, ip := range r.ips {
		if ip.Is6() && !ip.Is4In6() {
			out = append(out, ip)
		}
	}
	if len(out) == 0 {
		return nil, resolver.ErrIPNotFound
	}
	return out, nil
}
func (*staticResolver) ResolveECH(context.Context, string) ([]byte, error) { return nil, nil }
func (*staticResolver) ExchangeContext(context.Context, *dns.Msg) (*dns.Msg, error) {
	return nil, errors.New("unused")
}
func (*staticResolver) Invalid() bool    { return true }
func (*staticResolver) ClearCache()      {}
func (*staticResolver) ResetConnection() {}

type dialEvent struct {
	addr string
	at   time.Time
}

type recordingDialer struct {
	mu     sync.Mutex
	events []dialEvent
	// hang these addresses until ctx is done
	hang map[string]struct{}
}

func (d *recordingDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	d.mu.Lock()
	d.events = append(d.events, dialEvent{addr: address, at: time.Now()})
	_, hang := d.hang[address]
	d.mu.Unlock()
	if hang {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	c1, c2 := net.Pipe()
	go func() { _ = c2.Close() }()
	return c1, nil
}

func (d *recordingDialer) snapshot() []dialEvent {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]dialEvent, len(d.events))
	copy(out, d.events)
	return out
}

func TestDialParallelDeadFirstIPConnectsInDelay(t *testing.T) {
	dead := netip.MustParseAddr("192.0.2.1")
	live := netip.MustParseAddr("192.0.2.2")
	rec := &recordingDialer{hang: map[string]struct{}{
		net.JoinHostPort(dead.String(), "9"): {},
	}}
	prev := GetTcpConcurrent()
	SetTcpConcurrent(false)
	t.Cleanup(func() { SetTcpConcurrent(prev) })

	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, err := DialContext(ctx, "tcp", "example.test:9",
		WithOnlySingleStack(true),
		WithResolver(&staticResolver{ips: []netip.Addr{dead, live}}),
		WithNetDialer(rec),
	)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("DialContext: %v", err)
	}
	defer conn.Close()
	if elapsed > dualStackFallbackTimeout+200*time.Millisecond {
		t.Fatalf("connected in %v, want around %v (not connect-timeout)", elapsed, dualStackFallbackTimeout)
	}
	if elapsed < dualStackFallbackTimeout/2 {
		// first IP hangs; second must wait the stagger
		t.Fatalf("connected in %v, stagger should delay the live IP", elapsed)
	}
	ev := rec.snapshot()
	if len(ev) < 2 {
		t.Fatalf("want at least 2 dials, got %d", len(ev))
	}
}

func TestDualStackFallbackFamilyNotSYNAtT0(t *testing.T) {
	prev6 := resolver.DisableIPv6
	resolver.DisableIPv6 = false
	t.Cleanup(func() { resolver.DisableIPv6 = prev6 })
	prev := GetTcpConcurrent()
	SetTcpConcurrent(false)
	t.Cleanup(func() { SetTcpConcurrent(prev) })

	v4 := netip.MustParseAddr("192.0.2.1")
	v6 := netip.MustParseAddr("2001:db8::1")
	rec := &recordingDialer{hang: map[string]struct{}{
		net.JoinHostPort(v4.String(), "9"): {},
	}}

	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, err := DialContext(ctx, "tcp", "example.test:9",
		WithPreferIPv4(),
		WithResolver(&staticResolver{ips: []netip.Addr{v4, v6}}),
		WithNetDialer(rec),
	)
	if err != nil {
		t.Fatalf("DialContext: %v", err)
	}
	defer conn.Close()

	ev := rec.snapshot()
	var v6At time.Duration
	var sawV6 bool
	for _, e := range ev {
		if e.addr == net.JoinHostPort(v6.String(), "9") {
			sawV6 = true
			v6At = e.at.Sub(start)
		}
	}
	if !sawV6 {
		t.Fatal("fallback IPv6 was never dialed")
	}
	if v6At < dualStackFallbackTimeout/2 {
		t.Fatalf("fallback family SYN at %v, must not start at t=0", v6At)
	}
}

func TestTcpConcurrentSameFamilyZeroDelay(t *testing.T) {
	a := netip.MustParseAddr("192.0.2.1")
	b := netip.MustParseAddr("192.0.2.2")
	rec := &recordingDialer{hang: map[string]struct{}{
		net.JoinHostPort(a.String(), "9"): {},
	}}
	prev := GetTcpConcurrent()
	SetTcpConcurrent(true)
	t.Cleanup(func() { SetTcpConcurrent(prev) })

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn, err := DialContext(ctx, "tcp", "example.test:9",
		WithOnlySingleStack(true),
		WithResolver(&staticResolver{ips: []netip.Addr{a, b}}),
		WithNetDialer(rec),
	)
	if err != nil {
		t.Fatalf("DialContext: %v", err)
	}
	defer conn.Close()
	deadline := time.Now().Add(200 * time.Millisecond)
	var ev []dialEvent
	for time.Now().Before(deadline) {
		ev = rec.snapshot()
		if len(ev) >= 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(ev) < 2 {
		t.Fatalf("tcp-concurrent should race all IPs immediately, got %d dials", len(ev))
	}
	delta := ev[1].at.Sub(ev[0].at)
	if delta < 0 {
		delta = -delta
	}
	if delta > 50*time.Millisecond {
		t.Fatalf("same-family race delay %v, want ~0", delta)
	}
}

func TestDialTFOInheritsCallerContext(t *testing.T) {
	if DisableTFO {
		t.Skip("TFO disabled on this platform")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			io.Copy(io.Discard, c)
			c.Close()
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	conn, err := DialContext(ctx, "tcp", ln.Addr().String(), WithTFO(true), WithOnlySingleStack(true))
	if err != nil {
		t.Fatalf("DialContext TFO: %v", err)
	}
	cancel()
	_, err = conn.Write([]byte("hello"))
	if err == nil {
		conn.Close()
		t.Fatal("Write after cancel should fail")
	}
	_ = conn.Close()
}

func TestCustomNetDialerIsSingleShotPerIP(t *testing.T) {
	// HE races resolved IPs through the inner dialer. A custom NetDialer
	// (the *net.Dialer path) is invoked once per IP, never as a full
	// proxy handshake. Proxydialer is C.Dialer, not opt.netDialer.
	ip := netip.MustParseAddr("192.0.2.9")
	rec := &recordingDialer{}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn, err := DialContext(ctx, "tcp", "example.test:9",
		WithOnlySingleStack(true),
		WithResolver(&staticResolver{ips: []netip.Addr{ip}}),
		WithNetDialer(rec),
	)
	if err != nil {
		t.Fatalf("DialContext: %v", err)
	}
	conn.Close()
	if n := len(rec.snapshot()); n != 1 {
		t.Fatalf("single IP should dial once, got %d", n)
	}
}
