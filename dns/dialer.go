package dns

import (
	"context"
	"net"
	"net/netip"
	"strconv"

	"github.com/metacubex/mihomo/common/atomic"
	"github.com/metacubex/mihomo/component/dialer"
	"github.com/metacubex/mihomo/component/resolver"
	C "github.com/metacubex/mihomo/constant"
)

const RespectRules = C.DnsRespectRules

type Dialer = interface {
	DialContext(ctx context.Context, network, addr string) (net.Conn, error)
	ListenPacket(ctx context.Context, network, addr string) (net.PacketConn, error)
}

type DialerFactory func(r resolver.Resolver, proxyAdapter C.ProxyAdapter, proxyName string) Dialer

var newDNSDialer = atomic.NewTypedValue(DialerFactory(defaultDNSDialer))

func SetDialerFactory(f DialerFactory) {
	if f == nil {
		panic("dns: SetDialerFactory requires a non-nil factory")
	}
	newDNSDialer.Store(f)
}

func defaultDNSDialer(r resolver.Resolver, proxyAdapter C.ProxyAdapter, proxyName string) Dialer {
	if proxyAdapter != nil || proxyName != "" {
		panic("dns: dialer factory is not set; call dns.SetDialerFactory before constructing DNS clients that use a proxy or respect-rules")
	}
	return &directDialer{r: r}
}

type directDialer struct {
	r resolver.Resolver
}

func (d *directDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	var opts []dialer.Option
	if d.r != nil {
		opts = append(opts, dialer.WithResolver(d.r))
	}
	return dialer.DialContext(ctx, network, addr, opts...)
}

func (d *directDialer) ListenPacket(ctx context.Context, network, addr string) (net.PacketConn, error) {
	var opts []dialer.Option
	if d.r != nil {
		opts = append(opts, dialer.WithResolver(d.r))
	}
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, err
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		r := d.r
		if r == nil {
			r = resolver.DefaultResolver.Load()
		}
		ip, err = resolver.ResolveIPWithResolver(ctx, host, r)
		if err != nil {
			return nil, err
		}
	}
	return dialer.NewDialer(opts...).ListenPacket(ctx, network, "", netip.AddrPortFrom(ip, uint16(port)))
}
