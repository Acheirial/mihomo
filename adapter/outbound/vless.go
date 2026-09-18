package outbound

import (
	"context"
	"fmt"
	"net"
	"strconv"

	N "github.com/metacubex/mihomo/common/net"
	"github.com/metacubex/mihomo/common/utils"
	C "github.com/metacubex/mihomo/constant"
	"github.com/metacubex/mihomo/transport/vless"
	"github.com/metacubex/mihomo/transport/vless/encryption"

	vmessSing "github.com/metacubex/sing-vmess"
	"github.com/metacubex/sing-vmess/packetaddr"
	M "github.com/metacubex/sing/common/metadata"
)

type Vless struct {
	*Base
	client     *vless.Client
	option     *VlessOption
	encryption *encryption.ClientInstance
	stack      *StreamStack
}

type VlessOption struct {
	BasicOption
	Name              string            `proxy:"name"`
	Server            string            `proxy:"server"`
	Port              int               `proxy:"port"`
	UUID              string            `proxy:"uuid"`
	Flow              string            `proxy:"flow,omitempty"`
	TLS               bool              `proxy:"tls,omitempty"`
	ALPN              []string          `proxy:"alpn,omitempty"`
	UDP               bool              `proxy:"udp,omitempty"`
	PacketAddr        bool              `proxy:"packet-addr,omitempty"`
	XUDP              bool              `proxy:"xudp,omitempty"`
	PacketEncoding    string            `proxy:"packet-encoding,omitempty"`
	Encryption        string            `proxy:"encryption,omitempty"`
	Network           string            `proxy:"network,omitempty"`
	ECHOpts           ECHOptions        `proxy:"ech-opts,omitempty"`
	ShadowTLSOpts     ShadowTLSOptions  `proxy:"shadow-tls-opts,omitempty"`
	RestlsOpts        RestlsOptions     `proxy:"restls-opts,omitempty"`
	JLSOpts           JLSOptions        `proxy:"jls-opts,omitempty"`
	RealityOpts       RealityOptions    `proxy:"reality-opts,omitempty"`
	HTTPOpts          HTTPOptions       `proxy:"http-opts,omitempty"`
	HTTP2Opts         HTTP2Options      `proxy:"h2-opts,omitempty"`
	GrpcOpts          GrpcOptions       `proxy:"grpc-opts,omitempty"`
	WSOpts            WSOptions         `proxy:"ws-opts,omitempty"`
	XHTTPOpts         XHTTPOptions      `proxy:"xhttp-opts,omitempty"`
	MKCPOpts          MKCPOptions       `proxy:"mkcp-opts,omitempty"`
	MekyaOpts         MekyaOptions      `proxy:"mekya-opts,omitempty"`
	WSHeaders         map[string]string `proxy:"ws-headers,omitempty"`
	SkipCertVerify    bool              `proxy:"skip-cert-verify,omitempty"`
	NameCertVerify    string            `proxy:"name-cert-verify,omitempty"`
	Fingerprint       string            `proxy:"fingerprint,omitempty"`
	Certificate       string            `proxy:"certificate,omitempty"`
	PrivateKey        string            `proxy:"private-key,omitempty"`
	ServerName        string            `proxy:"servername,omitempty"`
	ClientFingerprint string            `proxy:"client-fingerprint,omitempty"`
}

type XHTTPOptions struct {
	Path                 string                 `proxy:"path,omitempty"`
	Host                 string                 `proxy:"host,omitempty"`
	Mode                 string                 `proxy:"mode,omitempty"`
	Headers              map[string]string      `proxy:"headers,omitempty"`
	NoGRPCHeader         bool                   `proxy:"no-grpc-header,omitempty"`
	XPaddingBytes        string                 `proxy:"x-padding-bytes,omitempty"`
	XPaddingObfsMode     bool                   `proxy:"x-padding-obfs-mode,omitempty"`
	XPaddingKey          string                 `proxy:"x-padding-key,omitempty"`
	XPaddingHeader       string                 `proxy:"x-padding-header,omitempty"`
	XPaddingPlacement    string                 `proxy:"x-padding-placement,omitempty"`
	XPaddingMethod       string                 `proxy:"x-padding-method,omitempty"`
	UplinkHTTPMethod     string                 `proxy:"uplink-http-method,omitempty"`
	SessionPlacement     string                 `proxy:"session-placement,omitempty"`
	SessionKey           string                 `proxy:"session-key,omitempty"`
	SessionTable         string                 `proxy:"session-table,omitempty"`
	SessionLength        string                 `proxy:"session-length,omitempty"`
	SeqPlacement         string                 `proxy:"seq-placement,omitempty"`
	SeqKey               string                 `proxy:"seq-key,omitempty"`
	UplinkDataPlacement  string                 `proxy:"uplink-data-placement,omitempty"`
	UplinkDataKey        string                 `proxy:"uplink-data-key,omitempty"`
	UplinkChunkSize      string                 `proxy:"uplink-chunk-size,omitempty"`
	ScMaxEachPostBytes   string                 `proxy:"sc-max-each-post-bytes,omitempty"`
	ScMinPostsIntervalMs string                 `proxy:"sc-min-posts-interval-ms,omitempty"`
	ReuseSettings        *XHTTPReuseSettings    `proxy:"reuse-settings,omitempty"` // aka XMUX
	DownloadSettings     *XHTTPDownloadSettings `proxy:"download-settings,omitempty"`
}

type XHTTPReuseSettings struct {
	MaxConcurrency   string `proxy:"max-concurrency,omitempty"`
	MaxConnections   string `proxy:"max-connections,omitempty"`
	CMaxReuseTimes   string `proxy:"c-max-reuse-times,omitempty"`
	HMaxRequestTimes string `proxy:"h-max-request-times,omitempty"`
	HMaxReusableSecs string `proxy:"h-max-reusable-secs,omitempty"`
	HKeepAlivePeriod int    `proxy:"h-keep-alive-period,omitempty"`
}

type XHTTPDownloadSettings struct {
	// xhttp part
	Path          *string             `proxy:"path,omitempty"`
	Host          *string             `proxy:"host,omitempty"`
	Headers       *map[string]string  `proxy:"headers,omitempty"`
	ReuseSettings *XHTTPReuseSettings `proxy:"reuse-settings,omitempty"` // aka XMUX
	// proxy part
	Server            *string           `proxy:"server,omitempty"`
	Port              *int              `proxy:"port,omitempty"`
	TLS               *bool             `proxy:"tls,omitempty"`
	ALPN              *[]string         `proxy:"alpn,omitempty"`
	ECHOpts           *ECHOptions       `proxy:"ech-opts,omitempty"`
	ShadowTLSOpts     *ShadowTLSOptions `proxy:"shadow-tls-opts,omitempty"`
	RestlsOpts        *RestlsOptions    `proxy:"restls-opts,omitempty"`
	JLSOpts           *JLSOptions       `proxy:"jls-opts,omitempty"`
	RealityOpts       *RealityOptions   `proxy:"reality-opts,omitempty"`
	SkipCertVerify    *bool             `proxy:"skip-cert-verify,omitempty"`
	NameCertVerify    *string           `proxy:"name-cert-verify,omitempty"`
	Fingerprint       *string           `proxy:"fingerprint,omitempty"`
	Certificate       *string           `proxy:"certificate,omitempty"`
	PrivateKey        *string           `proxy:"private-key,omitempty"`
	ServerName        *string           `proxy:"servername,omitempty"`
	ClientFingerprint *string           `proxy:"client-fingerprint,omitempty"`
}

func (v *Vless) StreamConnContext(ctx context.Context, c net.Conn, metadata *C.Metadata) (_ net.Conn, err error) {
	c, err = v.stack.Wrap(ctx, c)
	if err != nil {
		return nil, err
	}
	return v.streamConnContext(ctx, c, metadata)
}

func (v *Vless) streamConnContext(ctx context.Context, c net.Conn, metadata *C.Metadata) (conn net.Conn, err error) {
	if ctx.Done() != nil {
		done := N.SetupContextForConn(ctx, c)
		defer done(&err)
	}
	if v.encryption != nil {
		c, err = v.encryption.Handshake(c)
		if err != nil {
			return
		}
	}
	if metadata.NetWork == C.UDP {
		if v.option.PacketAddr {
			metadata = &C.Metadata{
				NetWork: C.UDP,
				Host:    packetaddr.SeqPacketMagicAddress,
				DstPort: 443,
			}
		} else {
			metadata = &C.Metadata{ // a clear metadata only contains ip
				NetWork: C.UDP,
				DstIP:   metadata.DstIP,
				DstPort: metadata.DstPort,
			}
		}
		conn, err = v.client.StreamConn(c, parseVlessAddr(metadata, v.option.XUDP))
	} else {
		conn, err = v.client.StreamConn(c, parseVlessAddr(metadata, false))
	}
	if err != nil {
		conn = nil
	}
	return
}
func (v *Vless) dialContext(ctx context.Context) (c net.Conn, err error) {
	return v.stack.Dial(ctx)
}

// DialContext implements C.ProxyAdapter
func (v *Vless) DialContext(ctx context.Context, metadata *C.Metadata) (_ C.Conn, err error) {
	c, err := v.dialContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s connect error: %s", v.addr, err.Error())
	}
	defer func(c net.Conn) {
		safeConnClose(c, err)
	}(c)

	c, err = v.StreamConnContext(ctx, c, metadata)
	if err != nil {
		return nil, fmt.Errorf("%s connect error: %s", v.addr, err.Error())
	}
	return NewConn(c, v), err
}

// ListenPacketContext implements C.ProxyAdapter
func (v *Vless) ListenPacketContext(ctx context.Context, metadata *C.Metadata) (_ C.PacketConn, err error) {
	if err = v.ResolveUDP(ctx, metadata); err != nil {
		return nil, err
	}

	c, err := v.dialContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s connect error: %s", v.addr, err.Error())
	}
	defer func(c net.Conn) {
		safeConnClose(c, err)
	}(c)

	c, err = v.StreamConnContext(ctx, c, metadata)
	if err != nil {
		return nil, fmt.Errorf("%s connect error: %s", v.addr, err.Error())
	}

	if v.option.XUDP {
		var globalID [8]byte
		if metadata.SourceValid() {
			globalID = utils.GlobalID(metadata.SourceAddress())
		}
		return NewPacketConn(N.NewThreadSafePacketConn(
			vmessSing.NewXUDPConn(c,
				globalID,
				M.SocksaddrFromNet(metadata.UDPAddr())),
		), v), nil
	} else if v.option.PacketAddr {
		return NewPacketConn(N.NewThreadSafePacketConn(
			packetaddr.NewConn(v.client.PacketConn(c, metadata.UDPAddr()),
				M.SocksaddrFromNet(metadata.UDPAddr())),
		), v), nil
	}
	return NewPacketConn(N.NewThreadSafePacketConn(v.client.PacketConn(c, metadata.UDPAddr())), v), nil
}

// SupportUOT implements C.ProxyAdapter
func (v *Vless) SupportUOT() bool {
	return true
}

// ProxyInfo implements C.ProxyAdapter
func (v *Vless) ProxyInfo() C.ProxyInfo {
	info := v.Base.ProxyInfo()
	info.DialerProxy = v.option.DialerProxy
	return info
}

func (v *Vless) Close() error {
	if v.stack != nil {
		return v.stack.Close()
	}
	return nil
}

func parseVlessAddr(metadata *C.Metadata, xudp bool) *vless.DstAddr {
	var addrType byte
	var addr []byte
	switch metadata.AddrType() {
	case C.AtypIPv4:
		addrType = vless.AtypIPv4
		addr = make([]byte, net.IPv4len)
		copy(addr[:], metadata.DstIP.AsSlice())
	case C.AtypIPv6:
		addrType = vless.AtypIPv6
		addr = make([]byte, net.IPv6len)
		copy(addr[:], metadata.DstIP.AsSlice())
	case C.AtypDomainName:
		addrType = vless.AtypDomainName
		addr = make([]byte, len(metadata.Host)+1)
		addr[0] = byte(len(metadata.Host))
		copy(addr[1:], metadata.Host)
	}

	return &vless.DstAddr{
		UDP:      metadata.NetWork == C.UDP,
		AddrType: addrType,
		Addr:     addr,
		Port:     metadata.DstPort,
		Mux:      metadata.NetWork == C.UDP && xudp,
	}
}

func NewVless(option VlessOption) (*Vless, error) {
	var addons *vless.Addons
	if len(option.Flow) >= 16 {
		option.Flow = option.Flow[:16]
		if option.Flow != vless.XRV {
			return nil, fmt.Errorf("unsupported xtls flow type: %s", option.Flow)
		}
		addons = &vless.Addons{
			Flow: option.Flow,
		}
	}

	switch option.PacketEncoding {
	case "packetaddr", "packet":
		option.PacketAddr = true
		option.XUDP = false
	default: // https://github.com/XTLS/Xray-core/pull/1567#issuecomment-1407305458
		if !option.PacketAddr {
			option.XUDP = true
		}
	}
	if option.XUDP {
		option.PacketAddr = false
	}

	client, err := vless.NewClient(option.UUID, addons)
	if err != nil {
		return nil, err
	}

	v := &Vless{
		Base: NewBase(BaseOption{
			Name:         option.Name,
			Addr:         net.JoinHostPort(option.Server, strconv.Itoa(option.Port)),
			Type:         C.Vless,
			ProviderName: option.ProviderName,
			UDP:          option.UDP,
			XUDP:         option.XUDP,
			TFO:          option.TFO,
			MPTCP:        option.MPTCP,
			Interface:    option.Interface,
			RoutingMark:  option.RoutingMark,
			Prefer:       option.IPVersion,
		}),
		client: client,
		option: &option,
	}
	v.dialer = option.NewDialer(v.DialOptions())

	v.encryption, err = encryption.NewClient(option.Encryption)
	if err != nil {
		return nil, err
	}

	echConfig, err := v.option.ECHOpts.Parse()
	if err != nil {
		return nil, err
	}
	shadowTLSConfig, err := option.ShadowTLSOpts.Parse()
	if err != nil {
		return nil, err
	}
	restlsConfig, err := option.RestlsOpts.Parse(option.ServerName, option.ClientFingerprint)
	if err != nil {
		return nil, err
	}
	jlsConfig, err := option.JLSOpts.Parse()
	if err != nil {
		return nil, err
	}
	realityConfig, err := v.option.RealityOpts.Parse()
	if err != nil {
		return nil, err
	}
	securityMode, err := checkExclusiveSecurityModes(collectSecurityModes(shadowTLSConfig, restlsConfig, jlsConfig, realityConfig, false))
	if err != nil {
		return nil, err
	}
	wsOpts := option.WSOpts
	if len(option.WSHeaders) != 0 && len(wsOpts.Headers) == 0 {
		wsOpts.Headers = option.WSHeaders
	}
	v.stack, err = NewStreamStack(StreamStackOption{
		Dialer:                    v.dialer,
		Addr:                      v.addr,
		Server:                    option.Server,
		Port:                      option.Port,
		Network:                   option.Network,
		TLS:                       option.TLS,
		ALPN:                      option.ALPN,
		SkipCertVerify:            option.SkipCertVerify,
		NameCertVerify:            option.NameCertVerify,
		Fingerprint:               option.Fingerprint,
		Certificate:               option.Certificate,
		PrivateKey:                option.PrivateKey,
		ServerName:                option.ServerName,
		ClientFingerprint:         option.ClientFingerprint,
		ECH:                       echConfig,
		ShadowTLS:                 shadowTLSConfig,
		Restls:                    restlsConfig,
		JLS:                       jlsConfig,
		Reality:                   realityConfig,
		SecurityMode:              securityMode,
		WS:                        wsOpts,
		HTTP:                      option.HTTPOpts,
		H2:                        option.HTTP2Opts,
		Grpc:                      option.GrpcOpts,
		XHTTP:                     option.XHTTPOpts,
		MKCP:                      option.MKCPOpts,
		Mekya:                     option.MekyaOpts,
		DialOptions:               v.DialOptions(),
		RandomizeWSHostWithoutTLS: true,
	})
	if err != nil {
		return nil, err
	}
	option.Network = v.stack.Network()

	return v, nil
}
