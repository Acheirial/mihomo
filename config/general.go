package config

import (
	"fmt"
	"net/netip"
	"path/filepath"
	_ "unsafe"

	"github.com/metacubex/mihomo/component/geodata"
	"github.com/metacubex/mihomo/component/process"
	C "github.com/metacubex/mihomo/constant"
	"github.com/metacubex/mihomo/log"
	T "github.com/metacubex/mihomo/tunnel"
)

func DefaultRawConfig() *RawConfig {
	return &RawConfig{
		AllowLan:          false,
		BindAddress:       "*",
		LanAllowedIPs:     []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0"), netip.MustParsePrefix("::/0")},
		IPv6:              true,
		Mode:              T.Rule,
		GeoAutoUpdate:     false,
		GeoUpdateInterval: 24,
		GeodataMode:       geodata.GeodataMode(),
		GeodataLoader:     "memconservative",
		UnifiedDelay:      false,
		Authentication:    []string{},
		LogLevel:          log.INFO,
		Hosts:             map[string]any{},
		Rule:              []string{},
		Proxy:             []map[string]any{},
		ProxyGroup:        []map[string]any{},
		TCPConcurrent:     false,
		FindProcessMode:   process.FindProcessStrict,
		GlobalUA:          "clash.meta/" + C.Version,
		ETagSupport:       true,
		DNS: RawDNS{
			Enable:         false,
			IPv6:           false,
			UseHosts:       true,
			UseSystemHosts: true,
			IPv6Timeout:    100,
			EnhancedMode:   C.DNSMapping,
			FakeIPRange:    "198.18.0.1/16",
			FakeIPTTL:      1,
			FallbackFilter: RawFallbackFilter{
				GeoIP:     true,
				GeoIPCode: "CN",
				IPCIDR:    []string{},
				GeoSite:   []string{},
			},
			DefaultNameserver: []string{
				"114.114.114.114",
				"223.5.5.5",
				"8.8.8.8",
				"1.0.0.1",
			},
			NameServer: []string{
				"https://doh.pub/dns-query",
				"tls://223.5.5.5:853",
			},
			FakeIPFilter: []string{
				"dns.msftnsci.com",
				"www.msftnsci.com",
				"www.msftconnecttest.com",
			},
			FakeIPFilterMode: C.FilterBlackList,
		},
		NTP: RawNTP{
			Enable:        false,
			WriteToSystem: false,
			Server:        "time.apple.com",
			Port:          123,
			Interval:      30,
		},
		Tun: RawTun{
			Enable:               false,
			Device:               "",
			Stack:                C.TunGvisor,
			DNSHijack:            []string{"0.0.0.0:53"}, // default hijack all dns query
			AutoRoute:            true,
			AutoDetectInterface:  true,
			Inet6Address:         []netip.Prefix{netip.MustParsePrefix("fdfe:dcba:9876::1/126")},
			RecvMsgX:             true,
			SendMsgX:             false, // In the current implementation, if enabled, the kernel may freeze during multi-thread downloads, so it is disabled by default.
			ProcessorsPerChannel: 1,     // For most users, memory usage is more important than peak performance. Setting this to 1 can significantly reduce memory consumption.
		},
		TuicServer: RawTuicServer{
			Enable:                false,
			Token:                 nil,
			Users:                 nil,
			Certificate:           "",
			PrivateKey:            "",
			Listen:                "",
			CongestionController:  "",
			MaxIdleTime:           15000,
			AuthenticationTimeout: 1000,
			ALPN:                  []string{"h3"},
			MaxUdpRelayPacketSize: 1500,
		},
		IPTables: RawIPTables{
			Enable:           false,
			InboundInterface: "lo",
			Bypass:           []string{},
			DnsRedirect:      true,
		},
		Experimental: RawExperimental{
			// https://github.com/quic-go/quic-go/issues/4178
			// Quic-go currently cannot automatically fall back on platforms that do not support ecn, so this feature is turned off by default.
			QUICGoDisableECN: true,
		},
		Profile: RawProfile{
			StoreSelected: true,
		},
		GeoXUrl: RawGeoXUrl{
			Mmdb:    "https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/geoip.metadb",
			ASN:     "https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/GeoLite2-ASN.mmdb",
			GeoIp:   "https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/geoip.dat",
			GeoSite: "https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/geosite.dat",
		},
		Sniffer: RawSniffer{
			Enable:          false,
			Sniff:           map[string]RawSniffingConfig{},
			ForceDomain:     []string{},
			SkipDomain:      []string{},
			Ports:           []string{},
			ForceDnsMapping: true,
			ParsePureIp:     true,
			OverrideDest:    true,
		},
		ExternalUIURL: "https://github.com/MetaCubeX/metacubexd/archive/refs/heads/gh-pages.zip",
		ExternalControllerCors: RawCors{
			AllowOrigins:        []string{"*"},
			AllowPrivateNetwork: true,
		},
	}
}

//go:linkname temporaryUpdateGeneral
func temporaryUpdateGeneral(general *General) func()

func parseGeneral(cfg *RawConfig) (*General, error) {
	if cfg.GlobalClientFingerprint != "" {
		log.Errorln("The `global-client-fingerprint` configuration is removed, please set `client-fingerprint` directly on the proxy instead")
	}
	return &General{
		Inbound: Inbound{
			Port:              cfg.Port,
			SocksPort:         cfg.SocksPort,
			RedirPort:         cfg.RedirPort,
			TProxyPort:        cfg.TProxyPort,
			MixedPort:         cfg.MixedPort,
			ShadowSocksConfig: cfg.ShadowSocksConfig,
			VmessConfig:       cfg.VmessConfig,
			AllowLan:          cfg.AllowLan,
			SkipAuthPrefixes:  cfg.SkipAuthPrefixes,
			LanAllowedIPs:     cfg.LanAllowedIPs,
			LanDisAllowedIPs:  cfg.LanDisAllowedIPs,
			BindAddress:       cfg.BindAddress,
			InboundTfo:        cfg.InboundTfo,
			InboundMPTCP:      cfg.InboundMPTCP,
		},
		UnifiedDelay: cfg.UnifiedDelay,
		Mode:         cfg.Mode,
		LogLevel:     cfg.LogLevel,
		IPv6:         cfg.IPv6,
		Interface:    cfg.Interface,
		RoutingMark:  cfg.RoutingMark,
		GeoXUrl: GeoXUrl{
			GeoIp:   cfg.GeoXUrl.GeoIp,
			Mmdb:    cfg.GeoXUrl.Mmdb,
			ASN:     cfg.GeoXUrl.ASN,
			GeoSite: cfg.GeoXUrl.GeoSite,
		},
		GeoAutoUpdate:     cfg.GeoAutoUpdate,
		GeoUpdateInterval: cfg.GeoUpdateInterval,
		GeodataMode:       cfg.GeodataMode,
		GeodataLoader:     cfg.GeodataLoader,
		GeositeMatcher:    cfg.GeositeMatcher,
		TCPConcurrent:     cfg.TCPConcurrent,
		FindProcessMode:   cfg.FindProcessMode,
		GlobalUA:          cfg.GlobalUA,
		ETagSupport:       cfg.ETagSupport,
		KeepAliveIdle:     cfg.KeepAliveIdle,
		KeepAliveInterval: cfg.KeepAliveInterval,
		DisableKeepAlive:  cfg.DisableKeepAlive,
	}, nil
}

func parseController(cfg *RawConfig) (*Controller, error) {
	if path := cfg.ExternalUI; path != "" && !C.Path.IsSafePath(path) {
		return nil, C.Path.ErrNotSafePath(path)
	}
	if uiName := cfg.ExternalUIName; uiName != "" && !filepath.IsLocal(uiName) {
		return nil, fmt.Errorf("external UI name is not local: %s", uiName)
	}
	return &Controller{
		ExternalController:            cfg.ExternalController,
		ExternalUI:                    cfg.ExternalUI,
		ExternalUIURL:                 cfg.ExternalUIURL,
		ExternalUIName:                cfg.ExternalUIName,
		Secret:                        cfg.Secret,
		ExternalControllerRoutingMark: cfg.ExternalControllerRoutingMark,
		ExternalControllerPipe:        cfg.ExternalControllerPipe,
		ExternalControllerUnix:        cfg.ExternalControllerUnix,
		ExternalControllerTLS:         cfg.ExternalControllerTLS,
		ExternalDohServer:             cfg.ExternalDohServer,
		Cors: Cors{
			AllowOrigins:        cfg.ExternalControllerCors.AllowOrigins,
			AllowPrivateNetwork: cfg.ExternalControllerCors.AllowPrivateNetwork,
		},
	}, nil
}

func parseExperimental(cfg *RawConfig) (*Experimental, error) {
	return &Experimental{
		QUICGoDisableGSO:          cfg.Experimental.QUICGoDisableGSO,
		QUICGoDisableECN:          cfg.Experimental.QUICGoDisableECN,
		IP4PEnable:                cfg.Experimental.IP4PEnable,
		DisableConnectionTracking: cfg.Experimental.DisableConnectionTracking,
		GOMemoryLimit:             cfg.Experimental.GOMemoryLimit,
		GOGCPercent:               cfg.Experimental.GOGCPercent,
	}, nil
}

func parseIPTables(cfg *RawConfig) (*IPTables, error) {
	return &IPTables{
		Enable:           cfg.IPTables.Enable,
		InboundInterface: cfg.IPTables.InboundInterface,
		Bypass:           cfg.IPTables.Bypass,
		DnsRedirect:      cfg.IPTables.DnsRedirect,
	}, nil
}

func parseNTP(cfg *RawConfig) (*NTP, error) {
	return &NTP{
		Enable:        cfg.NTP.Enable,
		Server:        cfg.NTP.Server,
		Port:          cfg.NTP.Port,
		Interval:      cfg.NTP.Interval,
		DialerProxy:   cfg.NTP.DialerProxy,
		WriteToSystem: cfg.NTP.WriteToSystem,
	}, nil
}

func parseProfile(cfg *RawConfig) (*Profile, error) {
	return &Profile{
		StoreSelected: cfg.Profile.StoreSelected,
		StoreFakeIP:   cfg.Profile.StoreFakeIP,
	}, nil
}

func parseTLS(cfg *RawConfig) (*TLS, error) {
	return &TLS{
		Certificate:     cfg.TLS.Certificate,
		PrivateKey:      cfg.TLS.PrivateKey,
		ClientAuthType:  cfg.TLS.ClientAuthType,
		ClientAuthCert:  cfg.TLS.ClientAuthCert,
		EchKey:          cfg.TLS.EchKey,
		CustomTrustCert: cfg.TLS.CustomTrustCert,
	}, nil
}
