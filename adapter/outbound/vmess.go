package outbound

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"

	N "github.com/metacubex/mihomo/common/net"
	"github.com/metacubex/mihomo/common/utils"
	"github.com/metacubex/mihomo/component/proxydialer"
	C "github.com/metacubex/mihomo/constant"
	"github.com/metacubex/mihomo/ntp"
	"github.com/metacubex/mihomo/transport/mekya"
	"github.com/metacubex/mihomo/transport/mkcp"

	vmess "github.com/metacubex/sing-vmess"
	"github.com/metacubex/sing-vmess/packetaddr"
	M "github.com/metacubex/sing/common/metadata"
)

var ErrUDPRemoteAddrMismatch = errors.New("udp packet dropped due to mismatched remote address")

type Vmess struct {
	*Base
	client *vmess.Client
	option *VmessOption
	stack  *StreamStack
}

type VmessOption struct {
	BasicOption
	Name                string           `proxy:"name"`
	Server              string           `proxy:"server"`
	Port                int              `proxy:"port"`
	UUID                string           `proxy:"uuid"`
	AlterID             int              `proxy:"alterId"`
	Cipher              string           `proxy:"cipher"`
	UDP                 bool             `proxy:"udp,omitempty"`
	Network             string           `proxy:"network,omitempty"`
	TLS                 bool             `proxy:"tls,omitempty"`
	ALPN                []string         `proxy:"alpn,omitempty"`
	SkipCertVerify      bool             `proxy:"skip-cert-verify,omitempty"`
	NameCertVerify      string           `proxy:"name-cert-verify,omitempty"`
	Fingerprint         string           `proxy:"fingerprint,omitempty"`
	Certificate         string           `proxy:"certificate,omitempty"`
	PrivateKey          string           `proxy:"private-key,omitempty"`
	ServerName          string           `proxy:"servername,omitempty"`
	ECHOpts             ECHOptions       `proxy:"ech-opts,omitempty"`
	ShadowTLSOpts       ShadowTLSOptions `proxy:"shadow-tls-opts,omitempty"`
	RestlsOpts          RestlsOptions    `proxy:"restls-opts,omitempty"`
	JLSOpts             JLSOptions       `proxy:"jls-opts,omitempty"`
	RealityOpts         RealityOptions   `proxy:"reality-opts,omitempty"`
	TLSMirrorOpts       TLSMirrorOptions `proxy:"tlsmirror-opts,omitempty"`
	MekyaOpts           MekyaOptions     `proxy:"mekya-opts,omitempty"`
	MKCPOpts            MKCPOptions      `proxy:"mkcp-opts,omitempty"`
	HTTPOpts            HTTPOptions      `proxy:"http-opts,omitempty"`
	HTTP2Opts           HTTP2Options     `proxy:"h2-opts,omitempty"`
	GrpcOpts            GrpcOptions      `proxy:"grpc-opts,omitempty"`
	WSOpts              WSOptions        `proxy:"ws-opts,omitempty"`
	XHTTPOpts           XHTTPOptions     `proxy:"xhttp-opts,omitempty"`
	PacketAddr          bool             `proxy:"packet-addr,omitempty"`
	XUDP                bool             `proxy:"xudp,omitempty"`
	PacketEncoding      string           `proxy:"packet-encoding,omitempty"`
	GlobalPadding       bool             `proxy:"global-padding,omitempty"`
	AuthenticatedLength bool             `proxy:"authenticated-length,omitempty"`
	ClientFingerprint   string           `proxy:"client-fingerprint,omitempty"`
}

type MKCPOptions struct {
	MTU              uint32 `proxy:"mtu,omitempty"`
	TTI              uint32 `proxy:"tti,omitempty"`
	UplinkCapacity   uint32 `proxy:"uplink-capacity,omitempty"`
	DownlinkCapacity uint32 `proxy:"downlink-capacity,omitempty"`
	Congestion       bool   `proxy:"congestion,omitempty"`
	WriteBuffer      uint32 `proxy:"write-buffer,omitempty"`
	ReadBuffer       uint32 `proxy:"read-buffer,omitempty"`
	Seed             string `proxy:"seed,omitempty"`
	Header           string `proxy:"header,omitempty"`
}

func (o MKCPOptions) Build() mkcp.Config {
	return mkcp.Config{
		MTU:              o.MTU,
		TTI:              o.TTI,
		UplinkCapacity:   o.UplinkCapacity,
		DownlinkCapacity: o.DownlinkCapacity,
		Congestion:       o.Congestion,
		WriteBuffer:      o.WriteBuffer,
		ReadBuffer:       o.ReadBuffer,
		Seed:             o.Seed,
		Header:           o.Header,
	}
}

type MekyaOptions struct {
	URL                            string      `proxy:"url,omitempty"`
	H2PoolSize                     int         `proxy:"h2-pool-size,omitempty"`
	MaxWriteDelay                  int         `proxy:"max-write-delay,omitempty"`
	MaxRequestSize                 int         `proxy:"max-request-size,omitempty"`
	PollingIntervalInitial         int         `proxy:"polling-interval-initial,omitempty"`
	MaxWriteSize                   int         `proxy:"max-write-size,omitempty"`
	MaxWriteDurationMs             int         `proxy:"max-write-duration-ms,omitempty"`
	MaxSimultaneousWriteConnection int         `proxy:"max-simultaneous-write-connection,omitempty"`
	PacketWritingBuffer            int         `proxy:"packet-writing-buffer,omitempty"`
	KCP                            MKCPOptions `proxy:"kcp,omitempty"`
}

func (o MekyaOptions) Build() mekya.Config {
	return mekya.Config{
		KCP:                            o.KCP.Build(),
		URL:                            o.URL,
		H2PoolSize:                     o.H2PoolSize,
		MaxWriteDelay:                  o.MaxWriteDelay,
		MaxRequestSize:                 o.MaxRequestSize,
		PollingIntervalInitial:         o.PollingIntervalInitial,
		MaxWriteSize:                   o.MaxWriteSize,
		MaxWriteDurationMs:             o.MaxWriteDurationMs,
		MaxSimultaneousWriteConnection: o.MaxSimultaneousWriteConnection,
		PacketWritingBuffer:            o.PacketWritingBuffer,
	}
}

type HTTPOptions struct {
	Method  string              `proxy:"method,omitempty"`
	Path    []string            `proxy:"path,omitempty"`
	Headers map[string][]string `proxy:"headers,omitempty"`
}

type HTTP2Options struct {
	Host []string `proxy:"host,omitempty"`
	Path string   `proxy:"path,omitempty"`
}

type GrpcOptions struct {
	GrpcServiceName string `proxy:"grpc-service-name,omitempty"`
	GrpcUserAgent   string `proxy:"grpc-user-agent,omitempty"`
	PingInterval    int    `proxy:"ping-interval,omitempty"`
	MaxConnections  int    `proxy:"max-connections,omitempty"`
	MinStreams      int    `proxy:"min-streams,omitempty"`
	MaxStreams      int    `proxy:"max-streams,omitempty"`
}

type WSOptions struct {
	Path                     string            `proxy:"path,omitempty"`
	Headers                  map[string]string `proxy:"headers,omitempty"`
	MaxEarlyData             int               `proxy:"max-early-data,omitempty"`
	EarlyDataHeaderName      string            `proxy:"early-data-header-name,omitempty"`
	V2rayHttpUpgrade         bool              `proxy:"v2ray-http-upgrade,omitempty"`
	V2rayHttpUpgradeFastOpen bool              `proxy:"v2ray-http-upgrade-fast-open,omitempty"`
}

func (v *Vmess) StreamConnContext(ctx context.Context, c net.Conn, metadata *C.Metadata) (_ net.Conn, err error) {
	c, err = v.stack.Wrap(ctx, c)
	if err != nil {
		return nil, err
	}
	return v.streamConnContext(ctx, c, metadata)
}

func (v *Vmess) streamConnContext(ctx context.Context, c net.Conn, metadata *C.Metadata) (conn net.Conn, err error) {
	useEarly := N.NeedHandshake(c)
	if !useEarly {
		if ctx.Done() != nil {
			done := N.SetupContextForConn(ctx, c)
			defer done(&err)
		}
	}
	if metadata.NetWork == C.UDP {
		if v.option.XUDP {
			var globalID [8]byte
			if metadata.SourceValid() {
				globalID = utils.GlobalID(metadata.SourceAddress())
			}
			if useEarly {
				conn = v.client.DialEarlyXUDPPacketConn(c,
					globalID,
					M.SocksaddrFromNet(metadata.UDPAddr()))
			} else {
				conn, err = v.client.DialXUDPPacketConn(c,
					globalID,
					M.SocksaddrFromNet(metadata.UDPAddr()))
			}
		} else if v.option.PacketAddr {
			if useEarly {
				conn = v.client.DialEarlyPacketConn(c,
					M.ParseSocksaddrHostPort(packetaddr.SeqPacketMagicAddress, 443))
			} else {
				conn, err = v.client.DialPacketConn(c,
					M.ParseSocksaddrHostPort(packetaddr.SeqPacketMagicAddress, 443))
			}
			conn = packetaddr.NewBindConn(conn)
		} else {
			if useEarly {
				conn = v.client.DialEarlyPacketConn(c,
					M.SocksaddrFromNet(metadata.UDPAddr()))
			} else {
				conn, err = v.client.DialPacketConn(c,
					M.SocksaddrFromNet(metadata.UDPAddr()))
			}
		}
	} else {
		if useEarly {
			conn = v.client.DialEarlyConn(c,
				M.ParseSocksaddrHostPort(metadata.String(), metadata.DstPort))
		} else {
			conn, err = v.client.DialConn(c,
				M.ParseSocksaddrHostPort(metadata.String(), metadata.DstPort))
		}
	}
	if err != nil {
		conn = nil
	}
	return
}
func (v *Vmess) dialContext(ctx context.Context) (c net.Conn, err error) {
	return v.stack.Dial(ctx)
}

// DialContext implements C.ProxyAdapter
func (v *Vmess) DialContext(ctx context.Context, metadata *C.Metadata) (_ C.Conn, err error) {
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
func (v *Vmess) ListenPacketContext(ctx context.Context, metadata *C.Metadata) (_ C.PacketConn, err error) {
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

	if pc, ok := c.(net.PacketConn); ok {
		return NewPacketConn(N.NewThreadSafePacketConn(pc), v), nil
	}
	return NewPacketConn(&vmessPacketConn{Conn: c, rAddr: metadata.UDPAddr()}, v), nil
}

// ProxyInfo implements C.ProxyAdapter
func (v *Vmess) ProxyInfo() C.ProxyInfo {
	info := v.Base.ProxyInfo()
	info.DialerProxy = v.option.DialerProxy
	return info
}

func (v *Vmess) Close() error {
	if v.stack != nil {
		return v.stack.Close()
	}
	return nil
}

// SupportUOT implements C.ProxyAdapter
func (v *Vmess) SupportUOT() bool {
	return true
}

func NewVmess(option VmessOption) (*Vmess, error) {
	security := strings.ToLower(option.Cipher)
	var options []vmess.ClientOption
	if option.GlobalPadding {
		options = append(options, vmess.ClientWithGlobalPadding())
	}
	if option.AuthenticatedLength {
		options = append(options, vmess.ClientWithAuthenticatedLength())
	}
	options = append(options, vmess.ClientWithTimeFunc(ntp.Now))
	client, err := vmess.NewClient(option.UUID, security, option.AlterID, options...)
	if err != nil {
		return nil, err
	}

	switch option.PacketEncoding {
	case "packetaddr", "packet":
		option.PacketAddr = true
	case "xudp":
		option.XUDP = true
	}
	if option.XUDP {
		option.PacketAddr = false
	}

	v := &Vmess{
		Base: NewBase(BaseOption{
			Name:         option.Name,
			Addr:         net.JoinHostPort(option.Server, strconv.Itoa(option.Port)),
			Type:         C.Vmess,
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
	securityMode, err := checkExclusiveSecurityModes(collectSecurityModes(shadowTLSConfig, restlsConfig, jlsConfig, realityConfig, option.TLSMirrorOpts.PrimaryKey != ""))
	if err != nil {
		return nil, err
	}
	v.stack, err = NewStreamStack(StreamStackOption{
		Dialer:            v.dialer,
		Addr:              v.addr,
		Server:            option.Server,
		Port:              option.Port,
		Network:           option.Network,
		TLS:               option.TLS,
		ALPN:              option.ALPN,
		SkipCertVerify:    option.SkipCertVerify,
		NameCertVerify:    option.NameCertVerify,
		Fingerprint:       option.Fingerprint,
		Certificate:       option.Certificate,
		PrivateKey:        option.PrivateKey,
		ServerName:        option.ServerName,
		ClientFingerprint: option.ClientFingerprint,
		ECH:               echConfig,
		ShadowTLS:         shadowTLSConfig,
		Restls:            restlsConfig,
		JLS:               jlsConfig,
		Reality:           realityConfig,
		TLSMirror:         option.TLSMirrorOpts.Build(),
		TLSMirrorDialer:   proxydialer.New(v, false).DialContext,
		SecurityMode:      securityMode,
		WS:                option.WSOpts,
		HTTP:              option.HTTPOpts,
		H2:                option.HTTP2Opts,
		Grpc:              option.GrpcOpts,
		XHTTP:             option.XHTTPOpts,
		MKCP:              option.MKCPOpts,
		Mekya:             option.MekyaOpts,
		DialOptions:       v.DialOptions(),
	})
	if err != nil {
		return nil, err
	}
	option.Network = v.stack.Network()

	return v, nil
}

type vmessPacketConn struct {
	net.Conn
	rAddr  net.Addr
	access sync.Mutex
}

// WriteTo implments C.PacketConn.WriteTo
// Since VMess doesn't support full cone NAT by design, we verify if addr matches uc.rAddr, and drop the packet if not.
func (uc *vmessPacketConn) WriteTo(b []byte, addr net.Addr) (int, error) {
	allowedAddr := uc.rAddr
	destAddr := addr
	if allowedAddr.String() != destAddr.String() {
		return 0, ErrUDPRemoteAddrMismatch
	}
	uc.access.Lock()
	defer uc.access.Unlock()
	return uc.Conn.Write(b)
}

func (uc *vmessPacketConn) ReadFrom(b []byte) (int, net.Addr, error) {
	n, err := uc.Conn.Read(b)
	return n, uc.rAddr, err
}
