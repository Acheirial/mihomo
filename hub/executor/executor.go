package executor

import (
	"fmt"
	"math"
	"net"
	"net/netip"
	"os"
	"runtime"
	"runtime/debug"
	"strconv"
	"sync"
	"time"
	_ "unsafe"

	"github.com/metacubex/mihomo/adapter"
	"github.com/metacubex/mihomo/adapter/inbound"
	"github.com/metacubex/mihomo/adapter/outboundgroup"
	"github.com/metacubex/mihomo/component/auth"
	"github.com/metacubex/mihomo/component/ca"
	"github.com/metacubex/mihomo/component/dialer"
	"github.com/metacubex/mihomo/component/fakeip"
	"github.com/metacubex/mihomo/component/geodata"
	mihomoHttp "github.com/metacubex/mihomo/component/http"
	"github.com/metacubex/mihomo/component/iface"
	"github.com/metacubex/mihomo/component/keepalive"
	"github.com/metacubex/mihomo/component/mitm"
	"github.com/metacubex/mihomo/component/profile"
	"github.com/metacubex/mihomo/component/profile/cachefile"
	"github.com/metacubex/mihomo/component/resolver"
	"github.com/metacubex/mihomo/component/resource"
	"github.com/metacubex/mihomo/component/sniffer"
	"github.com/metacubex/mihomo/component/trie"
	"github.com/metacubex/mihomo/component/updater"
	"github.com/metacubex/mihomo/config"
	C "github.com/metacubex/mihomo/constant"
	P "github.com/metacubex/mihomo/constant/provider"
	"github.com/metacubex/mihomo/dns"
	"github.com/metacubex/mihomo/listener"
	authStore "github.com/metacubex/mihomo/listener/auth"
	LC "github.com/metacubex/mihomo/listener/config"
	"github.com/metacubex/mihomo/listener/inner"
	"github.com/metacubex/mihomo/listener/tproxy"
	"github.com/metacubex/mihomo/log"
	"github.com/metacubex/mihomo/ntp/ntp"
	"github.com/metacubex/mihomo/tunnel"
)

var (
	applyMu     sync.Mutex
	mux         sync.Mutex
	lastDNS     *config.DNS
	lastDNSIPv6 bool
)

func readConfig(path string) ([]byte, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	if len(data) == 0 {
		return nil, fmt.Errorf("configuration file %s is empty", path)
	}

	return data, err
}

// Parse config with default config path
func Parse() (*config.Config, error) {
	return ParseWithPath(C.Path.Config())
}

// ParseWithPath parse config with custom config path
func ParseWithPath(path string) (*config.Config, error) {
	buf, err := readConfig(path)
	if err != nil {
		return nil, err
	}

	return ParseWithBytes(buf)
}

// ParseWithBytes config with buffer
func ParseWithBytes(buf []byte) (*config.Config, error) {
	return config.Parse(buf)
}

// ApplyConfig dispatch configure to all parts without ExternalController
// Note: 包级全局 + 整表替换；热重载 = 全量 ApplyConfig。
// GC 出锁见 .agents/notes/implemented/architecture/2026-09-17-perf-parity.md
// provider I/O 出锁与 Suspend 收窄见 .agents/notes/implemented/architecture/2026-09-18-reload-narrow-suspend.md
func ApplyConfig(cfg *config.Config, force bool) {
	dns.SetDialerFactory(func(r resolver.Resolver, pa C.ProxyAdapter, name string) dns.Dialer {
		return tunnel.NewDNSDialer(r, pa, name)
	})
	applyMu.Lock()
	defer applyMu.Unlock()
	mux.Lock()
	log.SetLevel(cfg.General.LogLevel)

	ca.ResetCertificate()
	for _, c := range cfg.TLS.CustomTrustCert {
		if err := ca.AddCertificate(c); err != nil {
			log.Warnln("%s\nadd error: %s", c, err.Error())
		}
	}

	updateExperimental(cfg.Experimental)
	updateUsers(cfg.Users)
	updateProxies(cfg.Proxies, cfg.Providers)
	updateRules(cfg.Rules, cfg.SubRules, cfg.RuleProviders)
	updateSniffer(cfg.Sniffer)
	updateMITM(cfg.MITM)
	updateHosts(cfg.Hosts)
	updateGeneral(cfg.General, true)
	updateDNS(cfg.DNS, cfg.General.IPv6)
	updateNTP(cfg.NTP) // initialize NTP after DNS because an NTP server may be a hostname.

	needSuspend := listenerNeedsSuspend(cfg.General, cfg.Listeners, cfg.Tunnels, force)
	if needSuspend {
		tunnel.OnSuspend()
	}
	updateListeners(cfg.General, cfg.Listeners, force)
	updateTun(cfg.General) // tun should not care "force"
	updateIPTables(cfg)
	updateTunnels(cfg.Tunnels)
	initInnerTcp()
	// Resume before provider I/O so HTTP RTT cannot hold Suspend or mux.
	tunnel.OnRunning()
	mux.Unlock()

	loadProvider(cfg.Providers)

	mux.Lock()
	updateProfile(cfg)
	mux.Unlock()

	loadProvider(cfg.RuleProviders)

	runtime.GC()
	updateUpdater(cfg)

	resolver.ResetConnection()
}

func initInnerTcp() {
	inner.New(tunnel.Tunnel)
}

func GetGeneral() *config.General {
	ports := listener.GetPorts()
	var authenticator []string
	if auth := authStore.Default.Authenticator(); auth != nil {
		authenticator = auth.Users()
	}

	general := &config.General{
		Inbound: config.Inbound{
			Port:              ports.Port,
			SocksPort:         ports.SocksPort,
			RedirPort:         ports.RedirPort,
			TProxyPort:        ports.TProxyPort,
			MixedPort:         ports.MixedPort,
			Tun:               listener.GetTunConf(),
			TuicServer:        listener.GetTuicConf(),
			ShadowSocksConfig: ports.ShadowSocksConfig,
			VmessConfig:       ports.VmessConfig,
			Authentication:    authenticator,
			SkipAuthPrefixes:  inbound.SkipAuthPrefixes(),
			LanAllowedIPs:     inbound.AllowedIPs(),
			LanDisAllowedIPs:  inbound.DisAllowedIPs(),
			AllowLan:          listener.AllowLan(),
			BindAddress:       listener.BindAddress(),
			InboundTfo:        inbound.Tfo(),
			InboundMPTCP:      inbound.MPTCP(),
		},
		Mode:         tunnel.Mode(),
		UnifiedDelay: adapter.UnifiedDelay.Load(),
		LogLevel:     log.Level(),
		IPv6:         !resolver.DisableIPv6,
		Interface:    dialer.DefaultInterface.Load(),
		RoutingMark:  int(dialer.DefaultRoutingMark.Load()),
		GeoXUrl: config.GeoXUrl{
			GeoIp:   geodata.GeoIpUrl(),
			Mmdb:    geodata.MmdbUrl(),
			ASN:     geodata.ASNUrl(),
			GeoSite: geodata.GeoSiteUrl(),
		},
		GeoAutoUpdate:     updater.GeoAutoUpdate(),
		GeoUpdateInterval: updater.GeoUpdateInterval(),
		GeodataMode:       geodata.GeodataMode(),
		GeodataLoader:     geodata.LoaderName(),
		GeositeMatcher:    geodata.SiteMatcherName(),
		TCPConcurrent:     dialer.GetTcpConcurrent(),
		FindProcessMode:   tunnel.FindProcessMode(),
		Sniffing:          tunnel.IsSniffing(),
		GlobalUA:          mihomoHttp.UA(),
		ETagSupport:       resource.ETag(),
		KeepAliveInterval: int(keepalive.KeepAliveInterval() / time.Second),
		KeepAliveIdle:     int(keepalive.KeepAliveIdle() / time.Second),
		DisableKeepAlive:  keepalive.DisableKeepAlive(),
	}

	return general
}

func updateListeners(general *config.General, listeners map[string]C.InboundListener, force bool) {
	listener.PatchInboundListeners(listeners, tunnel.Tunnel, true)
	if !force {
		return
	}

	allowLan := general.AllowLan
	listener.SetAllowLan(allowLan)
	inbound.SetSkipAuthPrefixes(general.SkipAuthPrefixes)
	inbound.SetAllowedIPs(general.LanAllowedIPs)
	inbound.SetDisAllowedIPs(general.LanDisAllowedIPs)

	bindAddress := general.BindAddress
	listener.SetBindAddress(bindAddress)
	listener.ReCreateHTTP(general.Port, tunnel.Tunnel)
	listener.ReCreateSocks(general.SocksPort, tunnel.Tunnel)
	listener.ReCreateRedir(general.RedirPort, tunnel.Tunnel)
	listener.ReCreateTProxy(general.TProxyPort, tunnel.Tunnel)
	listener.ReCreateMixed(general.MixedPort, tunnel.Tunnel)
	listener.ReCreateShadowSocks(general.ShadowSocksConfig, tunnel.Tunnel)
	listener.ReCreateVmess(general.VmessConfig, tunnel.Tunnel)
	listener.ReCreateTuic(general.TuicServer, tunnel.Tunnel)
}

func updateTun(general *config.General) {
	listener.ReCreateTun(general.Tun, tunnel.Tunnel)
}

func updateExperimental(c *config.Experimental) {
	tunnel.SetTrackConnections(!c.DisableConnectionTracking)
	if c.QUICGoDisableGSO {
		_ = os.Setenv("QUIC_GO_DISABLE_GSO", strconv.FormatBool(true))
	}
	if c.QUICGoDisableECN {
		_ = os.Setenv("QUIC_GO_DISABLE_ECN", strconv.FormatBool(true))
	}
	resolver.SetIP4PEnable(c.IP4PEnable)
	// GOMemoryLimit is in MiB; 0 unsets the runtime limit (SetMemoryLimit(MaxInt64)).
	if c.GOMemoryLimit > 0 {
		debug.SetMemoryLimit(int64(c.GOMemoryLimit) << 20)
	} else {
		debug.SetMemoryLimit(math.MaxInt64)
	}
	if c.GOGCPercent != 0 {
		debug.SetGCPercent(c.GOGCPercent)
	}
}
func updateNTP(c *config.NTP) {
	if c.Enable {
		ntp.ReCreateNTPService(
			net.JoinHostPort(c.Server, strconv.Itoa(c.Port)),
			time.Duration(c.Interval),
			c.DialerProxy,
			tunnel.Tunnel,
			c.WriteToSystem,
		)
	} else {
		ntp.ReCreateNTPService("", 0, "", nil, false)
	}
}

func updateDNS(c *config.DNS, generalIPv6 bool) {
	if dnsConfigUnchanged(c, generalIPv6) {
		return
	}

	if !c.Enable {
		resolver.DefaultResolver.Store(nil)
		resolver.DefaultHostMapper.Store(nil)
		resolver.DefaultService.Store(nil)
		resolver.ProxyServerHostResolver.Store(nil)
		resolver.DirectHostResolver.Store(nil)
		dns.ReCreateServer("", nil, nil)
		lastDNS = c
		lastDNSIPv6 = generalIPv6
		return
	}

	ipv6 := c.IPv6 && generalIPv6
	r := dns.NewResolver(dns.Config{
		Main:                 c.NameServer,
		Fallback:             c.Fallback,
		IPv6:                 ipv6,
		IPv6Timeout:          c.IPv6Timeout,
		FallbackIPFilter:     c.FallbackIPFilter,
		FallbackDomainFilter: c.FallbackDomainFilter,
		FallbackLazyQuery:    c.FallbackLazyQuery,
		Default:              c.DefaultNameserver,
		Policy:               c.NameServerPolicy,
		ProxyServer:          c.ProxyServerNameserver,
		ProxyServerPolicy:    c.ProxyServerPolicy,
		DirectServer:         c.DirectNameServer,
		DirectFollowPolicy:   c.DirectFollowPolicy,
		CacheAlgorithm:       c.CacheAlgorithm,
		CacheMaxSize:         c.CacheMaxSize,
	})
	m := dns.NewEnhancer(dns.EnhancerConfig{
		IPv6:          ipv6,
		EnhancedMode:  c.EnhancedMode,
		FakeIPPool:    c.FakeIPPool,
		FakeIPPool6:   c.FakeIPPool6,
		FakeIPSkipper: c.FakeIPSkipper,
		FakeIPTTL:     c.FakeIPTTL,
		UseHosts:      c.UseHosts,
	})

	// reuse cache of old host mapper
	if old := resolver.DefaultHostMapper.Load(); old != nil {
		m.PatchFrom(old.(*dns.ResolverEnhancer))
	}

	s := dns.NewService(r, m)

	resolver.DefaultResolver.Store(r)
	resolver.DefaultHostMapper.Store(m)
	resolver.DefaultService.Store(s)
	resolver.UseSystemHosts = c.UseSystemHosts

	if r.ProxyResolver.Invalid() {
		resolver.ProxyServerHostResolver.Store(r.ProxyResolver)
	} else {
		resolver.ProxyServerHostResolver.Store(r.Resolver)
	}

	if r.DirectResolver.Invalid() {
		resolver.DirectHostResolver.Store(r.DirectResolver)
	} else {
		resolver.DirectHostResolver.Store(r.Resolver)
	}

	lc := inbound.NewListenConfig()
	lc.SetRouteMark(c.ListenRoutingMark)
	dns.ReCreateServer(c.Listen, lc, s)

	lastDNS = c
	lastDNSIPv6 = generalIPv6
}

func updateHosts(tree *trie.DomainTrie[resolver.HostValue]) {
	resolver.DefaultHosts.Store(resolver.NewHosts(tree))
}

func updateProxies(proxies map[string]C.Proxy, providers map[string]P.ProxyProvider) {
	tunnel.UpdateProxies(proxies, providers)
}

func updateRules(rules []C.Rule, subRules map[string][]C.Rule, ruleProviders map[string]P.RuleProvider) {
	tunnel.UpdateRules(rules, subRules, ruleProviders)
}

func loadProvider[T P.Provider](providers map[string]T) {
	load := func(pv T) {
		name := pv.Name()
		if pv.VehicleType() == P.Compatible {
			log.Infoln("Start initial compatible provider %s", name)
		} else {
			log.Infoln("Start initial provider %s", name)
		}

		if err := pv.Initial(); err != nil {
			switch pv.Type() {
			case P.Proxy:
				{
					log.Errorln("initial proxy provider %s error: %v", name, err)
				}
			case P.Rule:
				{
					log.Errorln("initial rule provider %s error: %v", name, err)
				}
			}
		}
	}

	wg := sync.WaitGroup{}
	ch := make(chan struct{}, concurrentCount)
	for _, pv := range providers {
		pv := pv
		wg.Add(1)
		ch <- struct{}{}
		go func() {
			defer func() { <-ch; wg.Done() }()
			load(pv)
		}()
	}
	wg.Wait()
}

func updateSniffer(snifferConfig *sniffer.Config) {
	dispatcher, err := sniffer.NewDispatcher(snifferConfig)
	if err != nil {
		log.Warnln("initial sniffer failed, err:%v", err)
	}

	tunnel.UpdateSniffer(dispatcher)

	if snifferConfig.Enable {
		log.Infoln("Sniffer is loaded and working")
	} else {
		log.Infoln("Sniffer is closed")
	}
}

func updateMITM(cfg *mitm.Config) {
	if cfg == nil || !cfg.Enable {
		tunnel.UpdateMITM(nil)
		return
	}

	interceptor, err := mitm.New(*cfg)
	if err != nil {
		log.Warnln("initial mitm failed, err:%v", err)
		tunnel.UpdateMITM(nil)
		return
	}
	tunnel.UpdateMITM(interceptor)
	log.Infoln("MITM is loaded and working")
}

func updateTunnels(tunnels []LC.Tunnel) {
	listener.PatchTunnel(tunnels, tunnel.Tunnel)
}

func updateUpdater(cfg *config.Config) {
	general := cfg.General
	updater.SetGeoAutoUpdate(general.GeoAutoUpdate)
	updater.SetGeoUpdateInterval(general.GeoUpdateInterval)

	controller := cfg.Controller
	updater.DefaultUiUpdater = updater.NewUiUpdater(controller.ExternalUI, controller.ExternalUIURL, controller.ExternalUIName)
	updater.DefaultUiUpdater.AutoDownloadUI()
}

//go:linkname temporaryUpdateGeneral github.com/metacubex/mihomo/config.temporaryUpdateGeneral
func temporaryUpdateGeneral(general *config.General) func() {
	oldGeneral := GetGeneral()
	updateGeneral(general, false)
	return func() {
		updateGeneral(oldGeneral, false)
	}
}

func updateGeneral(general *config.General, logging bool) {
	tunnel.SetMode(general.Mode)
	tunnel.SetFindProcessMode(general.FindProcessMode)
	resolver.DisableIPv6 = !general.IPv6

	dialer.SetTcpConcurrent(general.TCPConcurrent)
	if logging && general.TCPConcurrent {
		log.Infoln("Use tcp concurrent")
	}

	inbound.SetTfo(general.InboundTfo)
	inbound.SetMPTCP(general.InboundMPTCP)

	keepalive.SetKeepAliveIdle(time.Duration(general.KeepAliveIdle) * time.Second)
	keepalive.SetKeepAliveInterval(time.Duration(general.KeepAliveInterval) * time.Second)
	keepalive.SetDisableKeepAlive(general.DisableKeepAlive)

	adapter.UnifiedDelay.Store(general.UnifiedDelay)

	dialer.DefaultInterface.Store(general.Interface)
	dialer.DefaultRoutingMark.Store(int32(general.RoutingMark))
	if logging && general.RoutingMark > 0 {
		log.Infoln("Use routing mark: %#x", general.RoutingMark)
	}

	iface.FlushCache()

	geodata.SetGeodataMode(general.GeodataMode)
	geodata.SetLoader(general.GeodataLoader)
	geodata.SetSiteMatcher(general.GeositeMatcher)
	geodata.SetGeoIpUrl(general.GeoXUrl.GeoIp)
	geodata.SetGeoSiteUrl(general.GeoXUrl.GeoSite)
	geodata.SetMmdbUrl(general.GeoXUrl.Mmdb)
	geodata.SetASNUrl(general.GeoXUrl.ASN)
	mihomoHttp.SetUA(general.GlobalUA)
	resource.SetETag(general.ETagSupport)
}

func updateUsers(users []auth.AuthUser) {
	authenticator := auth.NewAuthenticator(users)
	authStore.Default.SetAuthenticator(authenticator)
	if authenticator != nil {
		log.Infoln("Authentication of local server updated")
	}
}

func updateProfile(cfg *config.Config) {
	profileCfg := cfg.Profile

	profile.StoreSelected.Store(profileCfg.StoreSelected)
	if profileCfg.StoreSelected {
		patchSelectGroup(cfg.Proxies)
	}
}

func patchSelectGroup(proxies map[string]C.Proxy) {
	mapping := cachefile.Cache().SelectedMap()
	if mapping == nil {
		return
	}

	for name, outbound := range proxies {
		selector, ok := outbound.Adapter().(outboundgroup.SelectAble)
		if !ok {
			continue
		}

		selected, exist := mapping[name]
		if !exist {
			continue
		}

		selector.ForceSet(selected)
	}
}

func updateIPTables(cfg *config.Config) {
	tproxy.CleanupTProxyIPTables()

	iptables := cfg.IPTables
	if runtime.GOOS != "linux" || !iptables.Enable {
		return
	}

	var err error
	defer func() {
		if err != nil {
			log.Errorln("[IPTABLES] setting iptables failed: %s", err.Error())
			os.Exit(2)
		}
	}()

	if cfg.General.Tun.Enable {
		err = fmt.Errorf("when tun is enabled, iptables cannot be set automatically")
		return
	}

	var (
		inboundInterface = "lo"
		bypass           = iptables.Bypass
		tProxyPort       = cfg.General.TProxyPort
		dnsCfg           = cfg.DNS
		DnsRedirect      = iptables.DnsRedirect

		dnsPort netip.AddrPort
	)

	if tProxyPort == 0 {
		err = fmt.Errorf("tproxy-port must be greater than zero")
		return
	}

	if DnsRedirect {
		if !dnsCfg.Enable {
			err = fmt.Errorf("DNS server must be enable")
			return
		}

		dnsPort, err = netip.ParseAddrPort(dnsCfg.Listen)
		if err != nil {
			err = fmt.Errorf("DNS server must be correct")
			return
		}
	}

	if iptables.InboundInterface != "" {
		inboundInterface = iptables.InboundInterface
	}

	dialer.DefaultRoutingMark.CompareAndSwap(0, 2158)

	err = tproxy.SetTProxyIPTables(inboundInterface, bypass, uint16(tProxyPort), DnsRedirect, dnsPort.Port())
	if err != nil {
		return
	}

	log.Infoln("[IPTABLES] Setting iptables completed")
}

func Shutdown() {
	listener.Cleanup()
	for name, p := range tunnel.Providers() {
		if any(p) == nil {
			continue
		}
		if err := p.Close(); err != nil {
			log.Warnln("[Provider] close %s error: %s", name, err.Error())
		}
	}
	for name, p := range tunnel.RuleProviders() {
		if any(p) == nil {
			continue
		}
		if err := p.Close(); err != nil {
			log.Warnln("[Provider] close %s error: %s", name, err.Error())
		}
	}
	tproxy.CleanupTProxyIPTables()
	resolver.StoreFakePoolState()

	log.Warnln("Mihomo shutting down")
}

func listenerNeedsSuspend(general *config.General, listeners map[string]C.InboundListener, tunnels []LC.Tunnel, force bool) bool {
	if listener.WillRebindInboundListeners(listeners, true) {
		return true
	}
	if listener.WillRebindTun(general.Tun) {
		return true
	}
	if listener.WillRebindTunnels(tunnels) {
		return true
	}
	if !force {
		return false
	}
	bind := general.BindAddress
	lan := general.AllowLan
	return listener.WillRebindHTTP(general.Port, bind, lan) ||
		listener.WillRebindSocks(general.SocksPort, bind, lan) ||
		listener.WillRebindRedir(general.RedirPort, bind, lan) ||
		listener.WillRebindTProxy(general.TProxyPort, bind, lan) ||
		listener.WillRebindMixed(general.MixedPort, bind, lan) ||
		listener.WillRebindShadowSocks(general.ShadowSocksConfig) ||
		listener.WillRebindVmess(general.VmessConfig) ||
		listener.WillRebindTuic(general.TuicServer)
}

func dnsConfigUnchanged(c *config.DNS, generalIPv6 bool) bool {
	old := lastDNS
	if old == nil || c == nil {
		return false
	}
	if lastDNSIPv6 != generalIPv6 {
		return false
	}
	if old.Enable != c.Enable ||
		old.PreferH3 != c.PreferH3 ||
		old.IPv6 != c.IPv6 ||
		old.IPv6Timeout != c.IPv6Timeout ||
		old.UseHosts != c.UseHosts ||
		old.UseSystemHosts != c.UseSystemHosts ||
		old.Listen != c.Listen ||
		old.ListenRoutingMark != c.ListenRoutingMark ||
		old.EnhancedMode != c.EnhancedMode ||
		old.CacheAlgorithm != c.CacheAlgorithm ||
		old.CacheMaxSize != c.CacheMaxSize ||
		old.FakeIPRange != c.FakeIPRange ||
		old.FakeIPRange6 != c.FakeIPRange6 ||
		old.FakeIPTTL != c.FakeIPTTL ||
		old.DirectFollowPolicy != c.DirectFollowPolicy ||
		old.FallbackLazyQuery != c.FallbackLazyQuery {
		return false
	}
	if !nameServersEqual(old.NameServer, c.NameServer) ||
		!nameServersEqual(old.Fallback, c.Fallback) ||
		!nameServersEqual(old.DefaultNameserver, c.DefaultNameserver) ||
		!nameServersEqual(old.ProxyServerNameserver, c.ProxyServerNameserver) ||
		!nameServersEqual(old.DirectNameServer, c.DirectNameServer) {
		return false
	}
	if !dnsPoliciesEqual(old.NameServerPolicy, c.NameServerPolicy) ||
		!dnsPoliciesEqual(old.ProxyServerPolicy, c.ProxyServerPolicy) {
		return false
	}
	if !ipMatchersEqual(old.FallbackIPFilter, c.FallbackIPFilter) ||
		!domainMatchersEqual(old.FallbackDomainFilter, c.FallbackDomainFilter) {
		return false
	}
	if !fakeIPPoolEqual(old.FakeIPPool, c.FakeIPPool) ||
		!fakeIPPoolEqual(old.FakeIPPool6, c.FakeIPPool6) {
		return false
	}
	return fakeIPSkipperEqual(old.FakeIPSkipper, c.FakeIPSkipper)
}

func nameServersEqual(a, b []dns.NameServer) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !a[i].Equal(b[i]) {
			return false
		}
	}
	return true
}

func dnsPoliciesEqual(a, b []dns.Policy) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Domain != b[i].Domain {
			return false
		}
		if !nameServersEqual(a[i].NameServers, b[i].NameServers) {
			return false
		}
		if !domainMatcherEqual(a[i].Matcher, b[i].Matcher) {
			return false
		}
	}
	return true
}

func fakeIPPoolEqual(a, b *fakeip.Pool) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.IPNet() == b.IPNet()
}

func fakeIPSkipperEqual(a, b *fakeip.Skipper) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.Mode != b.Mode {
		return false
	}
	if len(a.Rules) != len(b.Rules) {
		return false
	}
	for i := range a.Rules {
		if a.Rules[i].RuleType() != b.Rules[i].RuleType() ||
			a.Rules[i].Adapter() != b.Rules[i].Adapter() ||
			a.Rules[i].Payload() != b.Rules[i].Payload() {
			return false
		}
	}
	return domainMatchersEqual(a.Host, b.Host)
}

func ipMatchersEqual(a, b []C.IpMatcher) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !ipMatcherEqual(a[i], b[i]) {
			return false
		}
	}
	return true
}

func domainMatchersEqual(a, b []C.DomainMatcher) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !domainMatcherEqual(a[i], b[i]) {
			return false
		}
	}
	return true
}

func ipMatcherEqual(a, b C.IpMatcher) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	if sa, ok := a.(fmt.Stringer); ok {
		if sb, ok := b.(fmt.Stringer); ok {
			return sa.String() == sb.String()
		}
	}
	if pa, ok := a.(interface{ Payload() string }); ok {
		if pb, ok := b.(interface{ Payload() string }); ok {
			return pa.Payload() == pb.Payload()
		}
	}
	return false
}

func domainMatcherEqual(a, b C.DomainMatcher) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	if sa, ok := a.(fmt.Stringer); ok {
		if sb, ok := b.(fmt.Stringer); ok {
			return sa.String() == sb.String()
		}
	}
	if pa, ok := a.(interface{ Payload() string }); ok {
		if pb, ok := b.(interface{ Payload() string }); ok {
			return pa.Payload() == pb.Payload()
		}
	}
	var keysA, keysB []string
	if fa, ok := a.(interface{ Foreach(func(string) bool) }); ok {
		fa.Foreach(func(key string) bool {
			keysA = append(keysA, key)
			return true
		})
	} else {
		return false
	}
	if fb, ok := b.(interface{ Foreach(func(string) bool) }); ok {
		fb.Foreach(func(key string) bool {
			keysB = append(keysB, key)
			return true
		})
	} else {
		return false
	}
	if len(keysA) != len(keysB) {
		return false
	}
	for i := range keysA {
		if keysA[i] != keysB[i] {
			return false
		}
	}
	return true
}
