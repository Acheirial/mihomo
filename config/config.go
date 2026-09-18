package config

import (
	"fmt"
	"net/netip"
	"time"

	"github.com/metacubex/mihomo/common/orderedmap"
	"github.com/metacubex/mihomo/common/utils"
	"github.com/metacubex/mihomo/common/yaml"
	"github.com/metacubex/mihomo/component/age"
	"github.com/metacubex/mihomo/component/auth"
	"github.com/metacubex/mihomo/component/fakeip"
	"github.com/metacubex/mihomo/component/geodata"
	"github.com/metacubex/mihomo/component/mitm"
	"github.com/metacubex/mihomo/component/process"
	"github.com/metacubex/mihomo/component/resolver"
	"github.com/metacubex/mihomo/component/sniffer"
	"github.com/metacubex/mihomo/component/trie"
	C "github.com/metacubex/mihomo/constant"
	P "github.com/metacubex/mihomo/constant/provider"
	"github.com/metacubex/mihomo/dns"
	LC "github.com/metacubex/mihomo/listener/config"
	"github.com/metacubex/mihomo/log"
	T "github.com/metacubex/mihomo/tunnel"
)

// General config
type General struct {
	Inbound
	Mode              T.TunnelMode            `json:"mode"`
	UnifiedDelay      bool                    `json:"unified-delay"`
	LogLevel          log.LogLevel            `json:"log-level"`
	IPv6              bool                    `json:"ipv6"`
	Interface         string                  `json:"interface-name"`
	RoutingMark       int                     `json:"routing-mark"`
	GeoXUrl           GeoXUrl                 `json:"geox-url"`
	GeoAutoUpdate     bool                    `json:"geo-auto-update"`
	GeoUpdateInterval int                     `json:"geo-update-interval"`
	GeodataMode       bool                    `json:"geodata-mode"`
	GeodataLoader     string                  `json:"geodata-loader"`
	GeositeMatcher    string                  `json:"geosite-matcher"`
	TCPConcurrent     bool                    `json:"tcp-concurrent"`
	FindProcessMode   process.FindProcessMode `json:"find-process-mode"`
	Sniffing          bool                    `json:"sniffing"`
	GlobalUA          string                  `json:"global-ua"`
	ETagSupport       bool                    `json:"etag-support"`
	KeepAliveIdle     int                     `json:"keep-alive-idle"`
	KeepAliveInterval int                     `json:"keep-alive-interval"`
	DisableKeepAlive  bool                    `json:"disable-keep-alive"`
}

// Inbound config
type Inbound struct {
	Port              int            `json:"port"`
	SocksPort         int            `json:"socks-port"`
	RedirPort         int            `json:"redir-port"`
	TProxyPort        int            `json:"tproxy-port"`
	MixedPort         int            `json:"mixed-port"`
	Tun               LC.Tun         `json:"tun"`
	TuicServer        LC.TuicServer  `json:"tuic-server"`
	ShadowSocksConfig string         `json:"ss-config"`
	VmessConfig       string         `json:"vmess-config"`
	Authentication    []string       `json:"authentication"`
	SkipAuthPrefixes  []netip.Prefix `json:"skip-auth-prefixes"`
	LanAllowedIPs     []netip.Prefix `json:"lan-allowed-ips"`
	LanDisAllowedIPs  []netip.Prefix `json:"lan-disallowed-ips"`
	AllowLan          bool           `json:"allow-lan"`
	BindAddress       string         `json:"bind-address"`
	InboundTfo        bool           `json:"inbound-tfo"`
	InboundMPTCP      bool           `json:"inbound-mptcp"`
}

// GeoXUrl config
type GeoXUrl struct {
	GeoIp   string `json:"geo-ip"`
	Mmdb    string `json:"mmdb"`
	ASN     string `json:"asn"`
	GeoSite string `json:"geo-site"`
}

// Controller config
type Controller struct {
	ExternalController            string
	ExternalControllerTLS         string
	ExternalControllerUnix        string
	ExternalControllerPipe        string
	ExternalControllerRoutingMark int
	ExternalUI                    string
	ExternalUIURL                 string
	ExternalUIName                string
	ExternalDohServer             string
	Secret                        string
	Cors                          Cors
}

type Cors struct {
	AllowOrigins        []string
	AllowPrivateNetwork bool
}

// Experimental config
type Experimental struct {
	QUICGoDisableGSO          bool
	QUICGoDisableECN          bool
	IP4PEnable                bool
	DisableConnectionTracking bool
	// GOMemoryLimit is the Go runtime soft memory limit in MiB; 0 leaves the runtime default unset.
	GOMemoryLimit uint64
	// GOGCPercent is the Go runtime GC trigger percentage; 0 leaves the runtime default unset.
	GOGCPercent int
}

// IPTables config
type IPTables struct {
	Enable           bool
	InboundInterface string
	Bypass           []string
	DnsRedirect      bool
}

// NTP config
type NTP struct {
	Enable        bool
	Server        string
	Port          int
	Interval      int
	DialerProxy   string
	WriteToSystem bool
}

// DNS config
type DNS struct {
	Enable                bool
	PreferH3              bool
	IPv6                  bool
	IPv6Timeout           uint
	UseHosts              bool
	UseSystemHosts        bool
	NameServer            []dns.NameServer
	Fallback              []dns.NameServer
	FallbackIPFilter      []C.IpMatcher
	FallbackDomainFilter  []C.DomainMatcher
	FallbackLazyQuery     bool
	Listen                string
	ListenRoutingMark     int
	EnhancedMode          C.DNSMode
	DefaultNameserver     []dns.NameServer
	CacheAlgorithm        string
	CacheMaxSize          int
	FakeIPRange           netip.Prefix
	FakeIPPool            *fakeip.Pool
	FakeIPRange6          netip.Prefix
	FakeIPPool6           *fakeip.Pool
	FakeIPSkipper         *fakeip.Skipper
	FakeIPTTL             int
	NameServerPolicy      []dns.Policy
	ProxyServerNameserver []dns.NameServer
	ProxyServerPolicy     []dns.Policy
	DirectNameServer      []dns.NameServer
	DirectFollowPolicy    bool
}

// Profile config
type Profile struct {
	StoreSelected bool
	StoreFakeIP   bool
}

// TLS config
type TLS struct {
	Certificate     string
	PrivateKey      string
	ClientAuthType  string
	ClientAuthCert  string
	EchKey          string
	CustomTrustCert []string
}

// Config is mihomo config manager
type Config struct {
	General       *General
	Controller    *Controller
	Experimental  *Experimental
	IPTables      *IPTables
	NTP           *NTP
	DNS           *DNS
	Hosts         *trie.DomainTrie[resolver.HostValue]
	Profile       *Profile
	Rules         []C.Rule
	SubRules      map[string][]C.Rule
	Users         []auth.AuthUser
	Proxies       map[string]C.Proxy
	Listeners     map[string]C.InboundListener
	Providers     map[string]P.ProxyProvider
	RuleProviders map[string]P.RuleProvider
	Tunnels       []LC.Tunnel
	Sniffer       *sniffer.Config
	MITM          *mitm.Config
	TLS           *TLS
}

type RawCors struct {
	AllowOrigins        []string `yaml:"allow-origins" json:"allow-origins"`
	AllowPrivateNetwork bool     `yaml:"allow-private-network" json:"allow-private-network"`
}

type RawDNS struct {
	Enable                       bool                                `yaml:"enable" json:"enable"`
	PreferH3                     bool                                `yaml:"prefer-h3" json:"prefer-h3"`
	IPv6                         bool                                `yaml:"ipv6" json:"ipv6"`
	IPv6Timeout                  uint                                `yaml:"ipv6-timeout" json:"ipv6-timeout"`
	UseHosts                     bool                                `yaml:"use-hosts" json:"use-hosts"`
	UseSystemHosts               bool                                `yaml:"use-system-hosts" json:"use-system-hosts"`
	RespectRules                 bool                                `yaml:"respect-rules" json:"respect-rules"`
	NameServer                   []string                            `yaml:"nameserver" json:"nameserver"`
	Fallback                     []string                            `yaml:"fallback" json:"fallback"`
	FallbackFilter               RawFallbackFilter                   `yaml:"fallback-filter" json:"fallback-filter"`
	FallbackLazyQuery            bool                                `yaml:"fallback-lazy-query" json:"fallback-lazy-query"`
	Listen                       string                              `yaml:"listen" json:"listen"`
	ListenRoutingMark            int                                 `yaml:"listen-routing-mark" json:"listen-routing-mark"`
	EnhancedMode                 C.DNSMode                           `yaml:"enhanced-mode" json:"enhanced-mode"`
	FakeIPRange                  string                              `yaml:"fake-ip-range" json:"fake-ip-range"`
	FakeIPRange6                 string                              `yaml:"fake-ip-range6" json:"fake-ip-range6"`
	FakeIPFilter                 []string                            `yaml:"fake-ip-filter" json:"fake-ip-filter"`
	FakeIPFilterMode             C.FilterMode                        `yaml:"fake-ip-filter-mode" json:"fake-ip-filter-mode"`
	FakeIPTTL                    int                                 `yaml:"fake-ip-ttl" json:"fake-ip-ttl"`
	DefaultNameserver            []string                            `yaml:"default-nameserver" json:"default-nameserver"`
	CacheAlgorithm               string                              `yaml:"cache-algorithm" json:"cache-algorithm"`
	CacheMaxSize                 int                                 `yaml:"cache-max-size" json:"cache-max-size"`
	NameServerPolicy             *orderedmap.OrderedMap[string, any] `yaml:"nameserver-policy" json:"nameserver-policy"`
	ProxyServerNameserver        []string                            `yaml:"proxy-server-nameserver" json:"proxy-server-nameserver"`
	ProxyServerNameserverPolicy  *orderedmap.OrderedMap[string, any] `yaml:"proxy-server-nameserver-policy" json:"proxy-server-nameserver-policy"`
	DirectNameServer             []string                            `yaml:"direct-nameserver" json:"direct-nameserver"`
	DirectNameServerFollowPolicy bool                                `yaml:"direct-nameserver-follow-policy" json:"direct-nameserver-follow-policy"`
}

type RawFallbackFilter struct {
	GeoIP     bool     `yaml:"geoip" json:"geoip"`
	GeoIPCode string   `yaml:"geoip-code" json:"geoip-code"`
	IPCIDR    []string `yaml:"ipcidr" json:"ipcidr"`
	Domain    []string `yaml:"domain" json:"domain"`
	GeoSite   []string `yaml:"geosite" json:"geosite"`
}

type RawClashForAndroid struct {
	AppendSystemDNS   bool   `yaml:"append-system-dns" json:"append-system-dns"`
	UiSubtitlePattern string `yaml:"ui-subtitle-pattern" json:"ui-subtitle-pattern"`
}

type RawNTP struct {
	Enable        bool   `yaml:"enable" json:"enable"`
	Server        string `yaml:"server" json:"server"`
	Port          int    `yaml:"port" json:"port"`
	Interval      int    `yaml:"interval" json:"interval"`
	DialerProxy   string `yaml:"dialer-proxy" json:"dialer-proxy"`
	WriteToSystem bool   `yaml:"write-to-system" json:"write-to-system"`
}

type RawTun struct {
	Enable              bool       `yaml:"enable" json:"enable"`
	Device              string     `yaml:"device" json:"device"`
	Stack               C.TUNStack `yaml:"stack" json:"stack"`
	DNSHijack           []string   `yaml:"dns-hijack" json:"dns-hijack"`
	AutoRoute           bool       `yaml:"auto-route" json:"auto-route"`
	AutoDetectInterface bool       `yaml:"auto-detect-interface" json:"auto-detect-interface"`

	MTU        uint32 `yaml:"mtu" json:"mtu,omitempty"`
	GSO        bool   `yaml:"gso" json:"gso,omitempty"`
	GSOMaxSize uint32 `yaml:"gso-max-size" json:"gso-max-size,omitempty"`
	//Inet4Address           []netip.Prefix `yaml:"inet4-address" json:"inet4-address,omitempty"`
	Inet6Address                          []netip.Prefix `yaml:"inet6-address" json:"inet6-address,omitempty"`
	IPRoute2TableIndex                    int            `yaml:"iproute2-table-index" json:"iproute2-table-index,omitempty"`
	IPRoute2RuleIndex                     int            `yaml:"iproute2-rule-index" json:"iproute2-rule-index,omitempty"`
	AutoRedirect                          bool           `yaml:"auto-redirect" json:"auto-redirect,omitempty"`
	AutoRedirectInputMark                 uint32         `yaml:"auto-redirect-input-mark" json:"auto-redirect-input-mark,omitempty"`
	AutoRedirectOutputMark                uint32         `yaml:"auto-redirect-output-mark" json:"auto-redirect-output-mark,omitempty"`
	AutoRedirectIPRoute2FallbackRuleIndex int            `yaml:"auto-redirect-iproute2-fallback-rule-index" json:"auto-redirect-iproute2-fallback-rule-index,omitempty"`
	LoopbackAddress                       []netip.Addr   `yaml:"loopback-address" json:"loopback-address,omitempty"`
	StrictRoute                           bool           `yaml:"strict-route" json:"strict-route,omitempty"`
	RouteAddress                          []netip.Prefix `yaml:"route-address" json:"route-address,omitempty"`
	RouteAddressSet                       []string       `yaml:"route-address-set" json:"route-address-set,omitempty"`
	RouteExcludeAddress                   []netip.Prefix `yaml:"route-exclude-address" json:"route-exclude-address,omitempty"`
	RouteExcludeAddressSet                []string       `yaml:"route-exclude-address-set" json:"route-exclude-address-set,omitempty"`
	IncludeInterface                      []string       `yaml:"include-interface" json:"include-interface,omitempty"`
	ExcludeInterface                      []string       `yaml:"exclude-interface" json:"exclude-interface,omitempty"`
	IncludeUID                            []uint32       `yaml:"include-uid" json:"include-uid,omitempty"`
	IncludeUIDRange                       []string       `yaml:"include-uid-range" json:"include-uid-range,omitempty"`
	ExcludeUID                            []uint32       `yaml:"exclude-uid" json:"exclude-uid,omitempty"`
	ExcludeUIDRange                       []string       `yaml:"exclude-uid-range" json:"exclude-uid-range,omitempty"`
	ExcludeSrcPort                        []uint16       `yaml:"exclude-src-port" json:"exclude-src-port,omitempty"`
	ExcludeSrcPortRange                   []string       `yaml:"exclude-src-port-range" json:"exclude-src-port-range,omitempty"`
	ExcludeDstPort                        []uint16       `yaml:"exclude-dst-port" json:"exclude-dst-port,omitempty"`
	ExcludeDstPortRange                   []string       `yaml:"exclude-dst-port-range" json:"exclude-dst-port-range,omitempty"`
	IncludeAndroidUser                    []int          `yaml:"include-android-user" json:"include-android-user,omitempty"`
	IncludePackage                        []string       `yaml:"include-package" json:"include-package,omitempty"`
	ExcludePackage                        []string       `yaml:"exclude-package" json:"exclude-package,omitempty"`
	IncludeMACAddress                     []string       `yaml:"include-mac-address" json:"include-mac-address,omitempty"`
	ExcludeMACAddress                     []string       `yaml:"exclude-mac-address" json:"exclude-mac-address,omitempty"`
	EndpointIndependentNat                bool           `yaml:"endpoint-independent-nat" json:"endpoint-independent-nat,omitempty"`
	UDPTimeout                            int64          `yaml:"udp-timeout" json:"udp-timeout,omitempty"`
	ICMPTimeout                           int64          `yaml:"icmp-timeout" json:"icmp-timeout,omitempty"`
	DisableICMPForwarding                 bool           `yaml:"disable-icmp-forwarding" json:"disable-icmp-forwarding,omitempty"`
	FileDescriptor                        int            `yaml:"file-descriptor" json:"file-descriptor"`

	Inet4RouteAddress        []netip.Prefix `yaml:"inet4-route-address" json:"inet4-route-address,omitempty"`
	Inet6RouteAddress        []netip.Prefix `yaml:"inet6-route-address" json:"inet6-route-address,omitempty"`
	Inet4RouteExcludeAddress []netip.Prefix `yaml:"inet4-route-exclude-address" json:"inet4-route-exclude-address,omitempty"`
	Inet6RouteExcludeAddress []netip.Prefix `yaml:"inet6-route-exclude-address" json:"inet6-route-exclude-address,omitempty"`

	// darwin special config
	RecvMsgX bool `yaml:"recvmsgx" json:"recvmsgx,omitempty"`
	SendMsgX bool `yaml:"sendmsgx" json:"sendmsgx,omitempty"`

	// gvisor special config (Non-public option; do not include it in the document.)
	ProcessorsPerChannel int `yaml:"processors-per-channel" json:"processors-per-channel,omitempty"`
}

type RawTuicServer struct {
	Enable                bool              `yaml:"enable" json:"enable"`
	Listen                string            `yaml:"listen" json:"listen"`
	Token                 []string          `yaml:"token" json:"token"`
	Users                 map[string]string `yaml:"users" json:"users,omitempty"`
	Certificate           string            `yaml:"certificate" json:"certificate"`
	PrivateKey            string            `yaml:"private-key" json:"private-key"`
	CongestionController  string            `yaml:"congestion-controller" json:"congestion-controller,omitempty"`
	MaxIdleTime           int               `yaml:"max-idle-time" json:"max-idle-time,omitempty"`
	AuthenticationTimeout int               `yaml:"authentication-timeout" json:"authentication-timeout,omitempty"`
	ALPN                  []string          `yaml:"alpn" json:"alpn,omitempty"`
	MaxUdpRelayPacketSize int               `yaml:"max-udp-relay-packet-size" json:"max-udp-relay-packet-size,omitempty"`
	CWND                  int               `yaml:"cwnd" json:"cwnd,omitempty"`
}

type RawIPTables struct {
	Enable           bool     `yaml:"enable" json:"enable"`
	InboundInterface string   `yaml:"inbound-interface" json:"inbound-interface"`
	Bypass           []string `yaml:"bypass" json:"bypass"`
	DnsRedirect      bool     `yaml:"dns-redirect" json:"dns-redirect"`
}

type RawExperimental struct {
	Fingerprints              []string `yaml:"fingerprints" json:"fingerprints"`
	QUICGoDisableGSO          bool     `yaml:"quic-go-disable-gso" json:"quic-go-disable-gso"`
	QUICGoDisableECN          bool     `yaml:"quic-go-disable-ecn" json:"quic-go-disable-ecn"`
	IP4PEnable                bool     `yaml:"dialer-ip4p-convert" json:"dialer-ip4p-convert"`
	DisableConnectionTracking bool     `yaml:"disable-connection-tracking" json:"disable-connection-tracking"`
	GOMemoryLimit             uint64   `yaml:"go-memory-limit" json:"go-memory-limit"` // MiB, 0 = unset
	GOGCPercent               int      `yaml:"go-gc-percent" json:"go-gc-percent"`     // 0 = unset
}

type RawProfile struct {
	StoreSelected bool `yaml:"store-selected" json:"store-selected"`
	StoreFakeIP   bool `yaml:"store-fake-ip" json:"store-fake-ip"`
}

type RawGeoXUrl struct {
	GeoIp   string `yaml:"geoip" json:"geoip"`
	Mmdb    string `yaml:"mmdb" json:"mmdb"`
	ASN     string `yaml:"asn" json:"asn"`
	GeoSite string `yaml:"geosite" json:"geosite"`
}

type RawSniffer struct {
	Enable          bool     `yaml:"enable" json:"enable"`
	OverrideDest    bool     `yaml:"override-destination" json:"override-destination"`
	Sniffing        []string `yaml:"sniffing" json:"sniffing"`
	ForceDomain     []string `yaml:"force-domain" json:"force-domain"`
	SkipSrcAddress  []string `yaml:"skip-src-address" json:"skip-src-address"`
	SkipDstAddress  []string `yaml:"skip-dst-address" json:"skip-dst-address"`
	SkipDomain      []string `yaml:"skip-domain" json:"skip-domain"`
	Ports           []string `yaml:"port-whitelist" json:"port-whitelist"`
	ForceDnsMapping bool     `yaml:"force-dns-mapping" json:"force-dns-mapping"`
	ParsePureIp     bool     `yaml:"parse-pure-ip" json:"parse-pure-ip"`
	MITM            *RawMITM `yaml:"mitm" json:"mitm"`

	Sniff map[string]RawSniffingConfig `yaml:"sniff" json:"sniff"`
}

type RawMITM struct {
	Enable     bool     `yaml:"enable" json:"enable"`
	CA         string   `yaml:"ca" json:"ca"`
	CAKey      string   `yaml:"ca-key" json:"ca-key"`
	SkipDomain []string `yaml:"skip-domain" json:"skip-domain"`
	Ports      []string `yaml:"port-whitelist" json:"port-whitelist"`
	Rules      []string `yaml:"rules" json:"rules"`
}

type RawSniffingConfig struct {
	Ports        []string `yaml:"ports" json:"ports"`
	OverrideDest *bool    `yaml:"override-destination" json:"override-destination"`
}

type RawTLS struct {
	Certificate     string   `yaml:"certificate" json:"certificate"`
	PrivateKey      string   `yaml:"private-key" json:"private-key"`
	ClientAuthType  string   `yaml:"client-auth-type" json:"client-auth-type"`
	ClientAuthCert  string   `yaml:"client-auth-cert" json:"client-auth-cert"`
	EchKey          string   `yaml:"ech-key" json:"ech-key"`
	CustomTrustCert []string `yaml:"custom-certifactes" json:"custom-certifactes"`
}

type RawConfig struct {
	Port                          int                     `yaml:"port" json:"port"`
	SocksPort                     int                     `yaml:"socks-port" json:"socks-port"`
	RedirPort                     int                     `yaml:"redir-port" json:"redir-port"`
	TProxyPort                    int                     `yaml:"tproxy-port" json:"tproxy-port"`
	MixedPort                     int                     `yaml:"mixed-port" json:"mixed-port"`
	ShadowSocksConfig             string                  `yaml:"ss-config" json:"ss-config"`
	VmessConfig                   string                  `yaml:"vmess-config" json:"vmess-config"`
	InboundTfo                    bool                    `yaml:"inbound-tfo" json:"inbound-tfo"`
	InboundMPTCP                  bool                    `yaml:"inbound-mptcp" json:"inbound-mptcp"`
	Authentication                []string                `yaml:"authentication" json:"authentication"`
	SkipAuthPrefixes              []netip.Prefix          `yaml:"skip-auth-prefixes" json:"skip-auth-prefixes"`
	LanAllowedIPs                 []netip.Prefix          `yaml:"lan-allowed-ips" json:"lan-allowed-ips"`
	LanDisAllowedIPs              []netip.Prefix          `yaml:"lan-disallowed-ips" json:"lan-disallowed-ips"`
	AllowLan                      bool                    `yaml:"allow-lan" json:"allow-lan"`
	BindAddress                   string                  `yaml:"bind-address" json:"bind-address"`
	Mode                          T.TunnelMode            `yaml:"mode" json:"mode"`
	UnifiedDelay                  bool                    `yaml:"unified-delay" json:"unified-delay"`
	LogLevel                      log.LogLevel            `yaml:"log-level" json:"log-level"`
	IPv6                          bool                    `yaml:"ipv6" json:"ipv6"`
	ExternalController            string                  `yaml:"external-controller" json:"external-controller"`
	ExternalControllerRoutingMark int                     `yaml:"external-controller-routing-mark" json:"external-controller-routing-mark"`
	ExternalControllerPipe        string                  `yaml:"external-controller-pipe" json:"external-controller-pipe"`
	ExternalControllerUnix        string                  `yaml:"external-controller-unix" json:"external-controller-unix"`
	ExternalControllerTLS         string                  `yaml:"external-controller-tls" json:"external-controller-tls"`
	ExternalControllerCors        RawCors                 `yaml:"external-controller-cors" json:"external-controller-cors"`
	ExternalUI                    string                  `yaml:"external-ui" json:"external-ui"`
	ExternalUIURL                 string                  `yaml:"external-ui-url" json:"external-ui-url"`
	ExternalUIName                string                  `yaml:"external-ui-name" json:"external-ui-name"`
	ExternalDohServer             string                  `yaml:"external-doh-server" json:"external-doh-server"`
	Secret                        string                  `yaml:"secret" json:"secret"`
	Interface                     string                  `yaml:"interface-name" json:"interface-name"`
	RoutingMark                   int                     `yaml:"routing-mark" json:"routing-mark"`
	Tunnels                       []LC.Tunnel             `yaml:"tunnels" json:"tunnels"`
	GeoAutoUpdate                 bool                    `yaml:"geo-auto-update" json:"geo-auto-update"`
	GeoUpdateInterval             int                     `yaml:"geo-update-interval" json:"geo-update-interval"`
	GeodataMode                   bool                    `yaml:"geodata-mode" json:"geodata-mode"`
	GeodataLoader                 string                  `yaml:"geodata-loader" json:"geodata-loader"`
	GeositeMatcher                string                  `yaml:"geosite-matcher" json:"geosite-matcher"`
	TCPConcurrent                 bool                    `yaml:"tcp-concurrent" json:"tcp-concurrent"`
	FindProcessMode               process.FindProcessMode `yaml:"find-process-mode" json:"find-process-mode"`
	GlobalClientFingerprint       string                  `yaml:"global-client-fingerprint" json:"global-client-fingerprint"`
	GlobalUA                      string                  `yaml:"global-ua" json:"global-ua"`
	ETagSupport                   bool                    `yaml:"etag-support" json:"etag-support"`
	KeepAliveIdle                 int                     `yaml:"keep-alive-idle" json:"keep-alive-idle"`
	KeepAliveInterval             int                     `yaml:"keep-alive-interval" json:"keep-alive-interval"`
	DisableKeepAlive              bool                    `yaml:"disable-keep-alive" json:"disable-keep-alive"`

	ProxyProvider map[string]map[string]any `yaml:"proxy-providers" json:"proxy-providers"`
	RuleProvider  map[string]map[string]any `yaml:"rule-providers" json:"rule-providers"`
	Proxy         []map[string]any          `yaml:"proxies" json:"proxies"`
	ProxyGroup    []map[string]any          `yaml:"proxy-groups" json:"proxy-groups"`
	Rule          []string                  `yaml:"rules" json:"rule"`
	SubRules      map[string][]string       `yaml:"sub-rules" json:"sub-rules"`
	Listeners     []map[string]any          `yaml:"listeners" json:"listeners"`
	Hosts         map[string]any            `yaml:"hosts" json:"hosts"`
	DNS           RawDNS                    `yaml:"dns" json:"dns"`
	NTP           RawNTP                    `yaml:"ntp" json:"ntp"`
	Tun           RawTun                    `yaml:"tun" json:"tun"`
	TuicServer    RawTuicServer             `yaml:"tuic-server" json:"tuic-server"`
	IPTables      RawIPTables               `yaml:"iptables" json:"iptables"`
	Experimental  RawExperimental           `yaml:"experimental" json:"experimental"`
	Profile       RawProfile                `yaml:"profile" json:"profile"`
	GeoXUrl       RawGeoXUrl                `yaml:"geox-url" json:"geox-url"`
	Sniffer       RawSniffer                `yaml:"sniffer" json:"sniffer"`
	TLS           RawTLS                    `yaml:"tls" json:"tls"`

	ClashForAndroid RawClashForAndroid `yaml:"clash-for-android" json:"clash-for-android"`
}

// Parse config
func Parse(buf []byte) (*Config, error) {
	rawCfg, err := UnmarshalRawConfig(buf)
	if err != nil {
		return nil, err
	}

	return ParseRawConfig(rawCfg)
}

func UnmarshalRawConfig(buf []byte) (*RawConfig, error) {
	// config with default value
	rawCfg := DefaultRawConfig()

	// decrypt config
	buf, err := age.DecryptBytes(buf)
	if err != nil {
		return nil, fmt.Errorf("decrypt config error: %w", err)
	}

	if err := yaml.Unmarshal(buf, rawCfg); err != nil {
		return nil, err
	}

	return rawCfg, nil
}

func ParseRawConfig(rawCfg *RawConfig) (*Config, error) {
	config := &Config{}
	log.Infoln("Start initial configuration in progress") //Segment finished in xxm
	startTime := time.Now()

	general, err := parseGeneral(rawCfg)
	if err != nil {
		return nil, err
	}
	config.General = general

	// We need to temporarily apply some configuration in general and roll back after parsing the complete configuration.
	// The loading and downloading of geodata in the parseRules and parseRuleProviders rely on these.
	// This implementation is very disgusting, but there is currently no better solution
	rollback := temporaryUpdateGeneral(config.General)
	defer rollback()

	controller, err := parseController(rawCfg)
	if err != nil {
		return nil, err
	}
	config.Controller = controller

	experimental, err := parseExperimental(rawCfg)
	if err != nil {
		return nil, err
	}
	config.Experimental = experimental

	iptables, err := parseIPTables(rawCfg)
	if err != nil {
		return nil, err
	}
	config.IPTables = iptables

	ntpCfg, err := parseNTP(rawCfg)
	if err != nil {
		return nil, err
	}
	config.NTP = ntpCfg

	profile, err := parseProfile(rawCfg)
	if err != nil {
		return nil, err
	}
	config.Profile = profile

	tlsCfg, err := parseTLS(rawCfg)
	if err != nil {
		return nil, err
	}
	config.TLS = tlsCfg

	proxies, providers, err := parseProxies(rawCfg)
	if err != nil {
		return nil, err
	}
	config.Proxies = proxies
	config.Providers = providers

	listeners, err := parseListeners(rawCfg)
	if err != nil {
		return nil, err
	}
	config.Listeners = listeners

	log.Infoln("Geodata Loader mode: %s", geodata.LoaderName())
	log.Infoln("Geosite Matcher implementation: %s", geodata.SiteMatcherName())
	ruleProviders, err := parseRuleProviders(rawCfg)
	if err != nil {
		return nil, err
	}
	config.RuleProviders = ruleProviders

	subRules, err := parseSubRules(rawCfg, proxies, ruleProviders)
	if err != nil {
		return nil, err
	}
	config.SubRules = subRules

	rules, err := parseRules(rawCfg.Rule, proxies, ruleProviders, subRules, "rules")
	if err != nil {
		return nil, err
	}
	config.Rules = rules

	hosts, err := parseHosts(rawCfg)
	if err != nil {
		return nil, err
	}
	config.Hosts = hosts

	parseIPV6(rawCfg) // must before DNS and Tun

	dnsCfg, err := parseDNS(rawCfg, ruleProviders)
	if err != nil {
		return nil, err
	}
	config.DNS = dnsCfg

	err = parseTun(rawCfg.Tun, dnsCfg, config.General)
	if err != nil {
		return nil, err
	}

	err = parseTuicServer(rawCfg.TuicServer, config.General)
	if err != nil {
		return nil, err
	}

	config.Users = parseAuthentication(rawCfg.Authentication)

	config.Tunnels = rawCfg.Tunnels
	// verify tunnels
	for _, t := range config.Tunnels {
		if len(t.Proxy) > 0 {
			if _, ok := config.Proxies[t.Proxy]; !ok {
				return nil, fmt.Errorf("tunnel proxy %s not found", t.Proxy)
			}
		}
	}

	config.Sniffer, err = parseSniffer(rawCfg.Sniffer, ruleProviders)
	if err != nil {
		return nil, err
	}

	if rawCfg.Sniffer.MITM != nil && rawCfg.Sniffer.MITM.Enable {
		ports, err := utils.NewUnsignedRangesFromList[uint16](rawCfg.Sniffer.MITM.Ports)
		if err != nil {
			return nil, fmt.Errorf("error in sniffer mitm port-whitelist, error: %w", err)
		}
		mitmSkipDomain, err := parseDomain(rawCfg.Sniffer.MITM.SkipDomain, nil, "sniffer.mitm.skip-domain", ruleProviders)
		if err != nil {
			return nil, err
		}
		config.MITM = &mitm.Config{
			Enable:     true,
			CA:         rawCfg.Sniffer.MITM.CA,
			CAKey:      rawCfg.Sniffer.MITM.CAKey,
			SkipDomain: mitmSkipDomain,
			Ports:      ports,
			Rules:      rawCfg.Sniffer.MITM.Rules,
		}
	}

	elapsedTime := time.Since(startTime) / time.Millisecond                     // duration in ms
	log.Infoln("Initial configuration complete, total time: %dms", elapsedTime) //Segment finished in xxm

	return config, nil
}
