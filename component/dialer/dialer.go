package dialer

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/metacubex/mihomo/common/atomic"
	"github.com/metacubex/mihomo/component/keepalive"
	"github.com/metacubex/mihomo/component/mptcp"
	"github.com/metacubex/mihomo/component/resolver"
)

const (
	DefaultTCPTimeout = 5 * time.Second
	DefaultUDPTimeout = DefaultTCPTimeout

	dualStackFallbackTimeout = 300 * time.Millisecond
)

var (
	tcpConcurrent = atomic.NewBool(false)
)

func SetTcpConcurrent(concurrent bool) {
	tcpConcurrent.Store(concurrent)
}

func GetTcpConcurrent() bool {
	return tcpConcurrent.Load()
}

func DialContext(ctx context.Context, network, address string, options ...Option) (net.Conn, error) {
	opt := applyOptions(options...)

	if opt.network == 4 || opt.network == 6 {
		if strings.Contains(network, "tcp") {
			network = "tcp"
		} else {
			network = "udp"
		}

		network = fmt.Sprintf("%s%d", network, opt.network)
	}

	ips, port, err := parseAddr(ctx, network, address, opt.resolver)
	if err != nil {
		return nil, err
	}
	switch network {
	case "tcp4", "tcp6", "udp4", "udp6":
		if opt.tfo {
			return DialSerial(ctx, network, ips, port, opt)
		}
		return DialParallel(ctx, network, ips, port, opt.prefer == 6, familyDialDelay(), opt)
	case "tcp", "udp":
		if opt.tfo {
			return DialSerial(ctx, network, ips, port, opt)
		}
		return dualStackDialContext(ctx, familyDialFunc(), network, ips, port, opt)
	default:
		return nil, ErrorInvalidedNetworkStack
	}
}

func familyDialDelay() time.Duration {
	if GetTcpConcurrent() {
		return 0
	}
	return dualStackFallbackTimeout
}

func familyDialFunc() dialFunc {
	delay := familyDialDelay()
	return func(ctx context.Context, network string, ips []netip.Addr, port string, opt option) (net.Conn, error) {
		return DialParallel(ctx, network, ips, port, opt.prefer == 6, delay, opt)
	}
}

func ListenPacket(ctx context.Context, network, address string, rAddrPort netip.AddrPort, options ...Option) (net.PacketConn, error) {
	opt := applyOptions(options...)

	lc, address, err := listenConfig(network, address, rAddrPort, opt)
	if err != nil {
		return nil, err
	}
	return lc.ListenPacket(ctx, network, address)
}

// Listen creates a TCP listener with the same socket policy as ListenPacket.
func Listen(ctx context.Context, network, address string, options ...Option) (net.Listener, error) {
	lc, address, err := listenConfig(network, address, netip.AddrPort{}, applyOptions(options...))
	if err != nil {
		return nil, err
	}
	return lc.Listen(ctx, network, address)
}

func listenConfig(network, address string, rAddrPort netip.AddrPort, opt option) (*net.ListenConfig, string, error) {
	lc := &net.ListenConfig{}
	if opt.addrReuse {
		addrReuseToListenConfig(lc)
	}
	if DefaultSocketHook != nil { // ignore interfaceName, routingMark when DefaultSocketHook not null (in CMFA)
		socketHookToListenConfig(lc)
	} else {
		if opt.interfaceName == "" {
			opt.interfaceName = DefaultInterface.Load()
		}
		if opt.interfaceName == "" {
			if finder := DefaultInterfaceFinder.Load(); finder != nil {
				opt.interfaceName = finder.FindInterfaceName(rAddrPort.Addr().Unmap())
			}
		}
		if rAddrPort.Addr().Unmap().IsLoopback() || listenAddressIsLoopback(address) {
			// avoid "The requested address is not valid in its context."
			opt.interfaceName = ""
		}
		if opt.interfaceName != "" {
			bind := bindIfaceToListenConfig
			if opt.fallbackBind {
				bind = fallbackBindIfaceToListenConfig
			}
			addr, err := bind(opt.interfaceName, lc, network, address, rAddrPort)
			if err != nil {
				return nil, "", err
			}
			address = addr
		}
		if opt.routingMark == 0 {
			opt.routingMark = int(DefaultRoutingMark.Load())
		}
		if opt.routingMark != 0 {
			bindMarkToListenConfig(opt.routingMark, lc, network, address)
		}
	}

	return lc, address, nil
}

func dialContext(ctx context.Context, network string, destination netip.Addr, port string, opt option) (net.Conn, error) {
	var address string
	destination, port = resolver.LookupIP4P(destination, port)
	address = net.JoinHostPort(destination.String(), port)

	netDialer := opt.netDialer
	switch netDialer.(type) {
	case nil:
		netDialer = &net.Dialer{}
	case *net.Dialer:
		_netDialer := *netDialer.(*net.Dialer)
		netDialer = &_netDialer // make a copy
	default:
		return netDialer.DialContext(ctx, network, address)
	}

	dialer := netDialer.(*net.Dialer)
	keepalive.SetNetDialer(dialer)
	mptcp.SetNetDialer(dialer, opt.mpTcp)

	if DefaultSocketHook != nil { // ignore interfaceName, routingMark and tfo when DefaultSocketHook not null (in CMFA)
		socketHookToToDialer(dialer)
	} else {
		if opt.interfaceName == "" {
			opt.interfaceName = DefaultInterface.Load()
		}
		if opt.interfaceName == "" {
			if finder := DefaultInterfaceFinder.Load(); finder != nil {
				opt.interfaceName = finder.FindInterfaceName(destination)
			}
		}
		if opt.interfaceName != "" {
			bind := bindIfaceToDialer
			if opt.fallbackBind {
				bind = fallbackBindIfaceToDialer
			}
			if err := bind(opt.interfaceName, dialer, network, destination); err != nil {
				return nil, err
			}
		}
		if opt.routingMark == 0 {
			opt.routingMark = int(DefaultRoutingMark.Load())
		}
		if opt.routingMark != 0 {
			bindMarkToDialer(opt.routingMark, dialer, network, destination)
		}
		if opt.tfo && !DisableTFO {
			return dialTFO(ctx, *dialer, network, address)
		}
	}

	return dialer.DialContext(ctx, network, address)
}

func ICMPControl(destination netip.Addr) func(network, address string, conn syscall.RawConn) error {
	return func(network, address string, conn syscall.RawConn) error {
		if DefaultSocketHook != nil {
			return DefaultSocketHook(network, address, conn)
		}
		dialer := &net.Dialer{}
		interfaceName := DefaultInterface.Load()
		if interfaceName == "" {
			if finder := DefaultInterfaceFinder.Load(); finder != nil {
				interfaceName = finder.FindInterfaceName(destination)
			}
		}
		if interfaceName != "" {
			if err := bindIfaceToDialer(interfaceName, dialer, network, destination); err != nil {
				return err
			}
		}
		routingMark := int(DefaultRoutingMark.Load())
		if routingMark != 0 {
			bindMarkToDialer(routingMark, dialer, network, destination)
		}
		if dialer.ControlContext != nil {
			return dialer.ControlContext(context.TODO(), network, address, conn)
		}
		return nil
	}
}

type dialFunc func(ctx context.Context, network string, ips []netip.Addr, port string, opt option) (net.Conn, error)

func dualStackDialContext(ctx context.Context, dialFn dialFunc, network string, ips []netip.Addr, port string, opt option) (net.Conn, error) {
	ipv4s, ipv6s := resolver.SortationAddr(ips)
	if len(ipv4s) == 0 && len(ipv6s) == 0 {
		return nil, ErrorNoIpAddress
	}
	if len(ipv4s) == 0 {
		return dialFn(ctx, network, ipv6s, port, opt)
	}
	if len(ipv6s) == 0 {
		return dialFn(ctx, network, ipv4s, port, opt)
	}

	preferIPv6 := opt.prefer == 6
	primaries, fallbacks := ipv4s, ipv6s
	if preferIPv6 {
		primaries, fallbacks = ipv6s, ipv4s
	}

	results := make(chan dialResult)
	returned := make(chan struct{})
	defer close(returned)

	startRacer := func(rctx context.Context, addrs []netip.Addr, isPrimary bool) {
		result := dialResult{isPrimary: isPrimary}
		defer func() {
			select {
			case results <- result:
			case <-returned:
				if result.Conn != nil && result.error == nil {
					_ = result.Conn.Close()
				}
			}
		}()
		result.Conn, result.error = dialFn(rctx, network, addrs, port, opt)
	}

	primaryCtx, primaryCancel := context.WithCancel(ctx)
	defer primaryCancel()
	go startRacer(primaryCtx, primaries, true)

	fallbackTimer := time.NewTimer(dualStackFallbackTimeout)
	defer fallbackTimer.Stop()

	var fallbackCancel context.CancelFunc
	defer func() {
		if fallbackCancel != nil {
			fallbackCancel()
		}
	}()

	primaryDone, fallbackDone, fallbackStarted := false, false, false
	var primaryErr, fallbackErr error

	startFallback := func() {
		if fallbackStarted {
			return
		}
		fallbackStarted = true
		var fallbackCtx context.Context
		fallbackCtx, fallbackCancel = context.WithCancel(ctx)
		go startRacer(fallbackCtx, fallbacks, false)
	}

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-fallbackTimer.C:
			startFallback()
		case res := <-results:
			if res.error == nil {
				return res.Conn, nil
			}
			if res.isPrimary {
				primaryDone = true
				primaryErr = res.error
				if !fallbackStarted && fallbackTimer.Stop() {
					startFallback()
				}
			} else {
				fallbackDone = true
				fallbackErr = res.error
			}
			if primaryDone && fallbackDone {
				return nil, errors.Join(fmt.Errorf("connect failed: %w", primaryErr), fmt.Errorf("connect failed: %w", fallbackErr))
			}
		}
	}
}

// DialSerial dials ips one by one. Used for TFO (lazy connect on first write)
// and as the inner attempt of a single address.
func DialSerial(ctx context.Context, network string, ips []netip.Addr, port string, opt option) (net.Conn, error) {
	if len(ips) == 0 {
		return nil, ErrorNoIpAddress
	}
	var errs []error
	for _, ip := range ips {
		if conn, err := dialContext(ctx, network, ip, port, opt); err == nil {
			return conn, nil
		} else {
			errs = append(errs, err)
		}
	}
	return nil, errors.Join(errs...)
}

// DialParallel races ips with RFC 8305-style stagger. delay==0 starts every
// address immediately (tcp-concurrent). Dual-stack family racing stays in
// dualStackDialContext; this only staggers a single address family.
func DialParallel(ctx context.Context, network string, ips []netip.Addr, port string, preferIPv6 bool, delay time.Duration, opt option) (net.Conn, error) {
	if len(ips) == 0 {
		return nil, ErrorNoIpAddress
	}
	if len(ips) == 1 {
		return DialSerial(ctx, network, ips, port, opt)
	}
	if preferIPv6 {
		ipv4s, ipv6s := resolver.SortationAddr(ips)
		ips = append(append([]netip.Addr{}, ipv6s...), ipv4s...)
	}

	results := make(chan dialResult)
	returned := make(chan struct{})
	defer close(returned)

	racer := func(ip netip.Addr) {
		result := dialResult{isPrimary: true, ip: ip}
		defer func() {
			select {
			case results <- result:
			case <-returned:
				if result.Conn != nil && result.error == nil {
					_ = result.Conn.Close()
				}
			}
		}()
		result.Conn, result.error = dialContext(ctx, network, ip, port, opt)
	}

	if delay <= 0 {
		for _, ip := range ips {
			go racer(ip)
		}
		var errs []error
		for i := 0; i < len(ips); i++ {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case res := <-results:
				if res.error == nil {
					return res.Conn, nil
				}
				errs = append(errs, res.error)
			}
		}
		if len(errs) > 0 {
			return nil, errors.Join(errs...)
		}
		return nil, os.ErrDeadlineExceeded
	}

	go racer(ips[0])
	launched := 1
	timer := time.NewTimer(delay)
	defer timer.Stop()

	var errs []error
	remaining := len(ips)
	for remaining > 0 {
		var timerC <-chan time.Time
		if launched < len(ips) {
			timerC = timer.C
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timerC:
			go racer(ips[launched])
			launched++
			if launched < len(ips) {
				timer.Reset(delay)
			}
		case res := <-results:
			remaining--
			if res.error == nil {
				return res.Conn, nil
			}
			errs = append(errs, res.error)
			if launched < len(ips) && timer.Stop() {
				timer.Reset(0)
			}
		}
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return nil, os.ErrDeadlineExceeded
}

type dialResult struct {
	ip netip.Addr
	net.Conn
	error
	isPrimary bool
}

func parseAddr(ctx context.Context, network, address string, preferResolver resolver.Resolver) ([]netip.Addr, string, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, "-1", err
	}

	if preferResolver == nil {
		preferResolver = resolver.ProxyServerHostResolver.Load()
	}

	var ips []netip.Addr
	switch network {
	case "tcp4", "udp4":
		ips, err = resolver.LookupIPv4WithResolver(ctx, host, preferResolver)
	case "tcp6", "udp6":
		ips, err = resolver.LookupIPv6WithResolver(ctx, host, preferResolver)
	default:
		ips, err = resolver.LookupIPWithResolver(ctx, host, preferResolver)
	}
	if err != nil {
		return nil, "-1", fmt.Errorf("dns resolve failed: %w", err)
	}
	for i, ip := range ips {
		if ip.Is4In6() {
			ips[i] = ip.Unmap()
		}
	}
	return ips, port, nil
}

func listenAddressIsLoopback(address string) bool {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		host = address
	}
	ip, err := netip.ParseAddr(host)
	return err == nil && ip.Unmap().IsLoopback()
}

type Dialer struct {
	Opt option
}

func (d Dialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return DialContext(ctx, network, address, WithOption(d.Opt))
}

func (d Dialer) ListenPacket(ctx context.Context, network, address string, rAddrPort netip.AddrPort) (net.PacketConn, error) {
	return ListenPacket(ctx, ParseNetwork(network, rAddrPort.Addr()), address, rAddrPort, WithOption(d.Opt))
}

func NewDialer(options ...Option) Dialer {
	opt := applyOptions(options...)
	return Dialer{Opt: opt}
}
