package outbound

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"

	N "github.com/metacubex/mihomo/common/net"
	C "github.com/metacubex/mihomo/constant"
	"github.com/metacubex/mihomo/transport/shadowsocks/core"
	"github.com/metacubex/mihomo/transport/trojan"
)

type Trojan struct {
	*Base
	option      *TrojanOption
	hexPassword [trojan.KeyLength]byte
	ssCipher    core.Cipher
	stack       *StreamStack
}

type TrojanOption struct {
	BasicOption
	Name              string           `proxy:"name"`
	Server            string           `proxy:"server"`
	Port              int              `proxy:"port"`
	Password          string           `proxy:"password"`
	ALPN              []string         `proxy:"alpn,omitempty"`
	SNI               string           `proxy:"sni,omitempty"`
	SkipCertVerify    bool             `proxy:"skip-cert-verify,omitempty"`
	NameCertVerify    string           `proxy:"name-cert-verify,omitempty"`
	Fingerprint       string           `proxy:"fingerprint,omitempty"`
	Certificate       string           `proxy:"certificate,omitempty"`
	PrivateKey        string           `proxy:"private-key,omitempty"`
	UDP               bool             `proxy:"udp,omitempty"`
	Network           string           `proxy:"network,omitempty"`
	ECHOpts           ECHOptions       `proxy:"ech-opts,omitempty"`
	ShadowTLSOpts     ShadowTLSOptions `proxy:"shadow-tls-opts,omitempty"`
	RestlsOpts        RestlsOptions    `proxy:"restls-opts,omitempty"`
	JLSOpts           JLSOptions       `proxy:"jls-opts,omitempty"`
	RealityOpts       RealityOptions   `proxy:"reality-opts,omitempty"`
	GrpcOpts          GrpcOptions      `proxy:"grpc-opts,omitempty"`
	WSOpts            WSOptions        `proxy:"ws-opts,omitempty"`
	HTTPOpts          HTTPOptions      `proxy:"http-opts,omitempty"`
	HTTP2Opts         HTTP2Options     `proxy:"h2-opts,omitempty"`
	XHTTPOpts         XHTTPOptions     `proxy:"xhttp-opts,omitempty"`
	MKCPOpts          MKCPOptions      `proxy:"mkcp-opts,omitempty"`
	MekyaOpts         MekyaOptions     `proxy:"mekya-opts,omitempty"`
	SSOpts            TrojanSSOption   `proxy:"ss-opts,omitempty"`
	ClientFingerprint string           `proxy:"client-fingerprint,omitempty"`
}

// TrojanSSOption from https://github.com/p4gefau1t/trojan-go/blob/v0.10.6/tunnel/shadowsocks/config.go#L5
type TrojanSSOption struct {
	Enabled  bool   `proxy:"enabled,omitempty"`
	Method   string `proxy:"method,omitempty"`
	Password string `proxy:"password,omitempty"`
}

func (t *Trojan) StreamConnContext(ctx context.Context, c net.Conn, metadata *C.Metadata) (_ net.Conn, err error) {
	c, err = t.stack.Wrap(ctx, c)
	if err != nil {
		return nil, err
	}
	return t.streamConnContext(ctx, c, metadata)
}

func (t *Trojan) streamConnContext(ctx context.Context, c net.Conn, metadata *C.Metadata) (_ net.Conn, err error) {
	if t.ssCipher != nil {
		c = t.ssCipher.StreamConn(c)
	}

	if ctx.Done() != nil {
		done := N.SetupContextForConn(ctx, c)
		defer done(&err)
	}
	command := trojan.CommandTCP
	if metadata.NetWork == C.UDP {
		command = trojan.CommandUDP
	}
	err = trojan.WriteHeader(c, t.hexPassword, command, serializesSocksAddr(metadata))
	return c, err
}

func (t *Trojan) writeHeaderContext(ctx context.Context, c net.Conn, metadata *C.Metadata) (err error) {
	if ctx.Done() != nil {
		done := N.SetupContextForConn(ctx, c)
		defer done(&err)
	}
	command := trojan.CommandTCP
	if metadata.NetWork == C.UDP {
		command = trojan.CommandUDP
	}
	err = trojan.WriteHeader(c, t.hexPassword, command, serializesSocksAddr(metadata))
	return err
}

func (t *Trojan) dialContext(ctx context.Context) (c net.Conn, err error) {
	return t.stack.Dial(ctx)
}

// DialContext implements C.ProxyAdapter
func (t *Trojan) DialContext(ctx context.Context, metadata *C.Metadata) (_ C.Conn, err error) {
	c, err := t.dialContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s connect error: %w", t.addr, err)
	}
	defer func(c net.Conn) {
		safeConnClose(c, err)
	}(c)

	c, err = t.StreamConnContext(ctx, c, metadata)
	if err != nil {
		return nil, fmt.Errorf("%s connect error: %w", t.addr, err)
	}

	return NewConn(c, t), err
}

// ListenPacketContext implements C.ProxyAdapter
func (t *Trojan) ListenPacketContext(ctx context.Context, metadata *C.Metadata) (_ C.PacketConn, err error) {
	if err = t.ResolveUDP(ctx, metadata); err != nil {
		return nil, err
	}

	c, err := t.dialContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s connect error: %w", t.addr, err)
	}
	defer func(c net.Conn) {
		safeConnClose(c, err)
	}(c)

	c, err = t.StreamConnContext(ctx, c, metadata)
	if err != nil {
		return nil, fmt.Errorf("%s connect error: %w", t.addr, err)
	}

	pc := trojan.NewPacketConn(c)
	return NewPacketConn(pc, t), err
}

// SupportUOT implements C.ProxyAdapter
func (t *Trojan) SupportUOT() bool {
	return true
}

// ProxyInfo implements C.ProxyAdapter
func (t *Trojan) ProxyInfo() C.ProxyInfo {
	info := t.Base.ProxyInfo()
	info.DialerProxy = t.option.DialerProxy
	return info
}

func (t *Trojan) Close() error {
	if t.stack != nil {
		return t.stack.Close()
	}
	return nil
}

func NewTrojan(option TrojanOption) (*Trojan, error) {
	addr := net.JoinHostPort(option.Server, strconv.Itoa(option.Port))

	if option.SNI == "" {
		option.SNI = option.Server
	}

	t := &Trojan{
		Base: NewBase(BaseOption{
			Name:         option.Name,
			Addr:         addr,
			Type:         C.Trojan,
			ProviderName: option.ProviderName,
			UDP:          option.UDP,
			TFO:          option.TFO,
			MPTCP:        option.MPTCP,
			Interface:    option.Interface,
			RoutingMark:  option.RoutingMark,
			Prefer:       option.IPVersion,
		}),
		option:      &option,
		hexPassword: trojan.Key(option.Password),
	}
	t.dialer = option.NewDialer(t.DialOptions())

	echConfig, err := option.ECHOpts.Parse()
	if err != nil {
		return nil, err
	}
	shadowTLSConfig, err := option.ShadowTLSOpts.Parse()
	if err != nil {
		return nil, err
	}
	restlsConfig, err := option.RestlsOpts.Parse(option.SNI, option.ClientFingerprint)
	if err != nil {
		return nil, err
	}
	jlsConfig, err := option.JLSOpts.Parse()
	if err != nil {
		return nil, err
	}
	realityConfig, err := option.RealityOpts.Parse()
	if err != nil {
		return nil, err
	}
	securityMode, err := checkExclusiveSecurityModes(collectSecurityModes(shadowTLSConfig, restlsConfig, jlsConfig, realityConfig, false))
	if err != nil {
		return nil, err
	}

	if option.SSOpts.Enabled {
		if option.SSOpts.Password == "" {
			return nil, errors.New("empty password")
		}
		if option.SSOpts.Method == "" {
			option.SSOpts.Method = "AES-128-GCM"
		}
		ciph, err := core.PickCipher(option.SSOpts.Method, nil, option.SSOpts.Password)
		if err != nil {
			return nil, err
		}
		t.ssCipher = ciph
	}

	alpn := option.ALPN
	wsALPN := trojan.DefaultWebsocketALPN
	if option.ALPN != nil {
		wsALPN = option.ALPN
	} else {
		alpn = trojan.DefaultALPN
	}
	t.stack, err = NewStreamStack(StreamStackOption{
		Dialer:            t.dialer,
		Addr:              t.addr,
		Server:            option.Server,
		Port:              option.Port,
		Network:           option.Network,
		TLS:               true,
		ForceTLS:          true,
		ALPN:              alpn,
		SkipCertVerify:    option.SkipCertVerify,
		NameCertVerify:    option.NameCertVerify,
		Fingerprint:       option.Fingerprint,
		Certificate:       option.Certificate,
		PrivateKey:        option.PrivateKey,
		ServerName:        option.SNI,
		ClientFingerprint: option.ClientFingerprint,
		ECH:               echConfig,
		ShadowTLS:         shadowTLSConfig,
		Restls:            restlsConfig,
		JLS:               jlsConfig,
		Reality:           realityConfig,
		SecurityMode:      securityMode,
		WS:                option.WSOpts,
		HTTP:              option.HTTPOpts,
		H2:                option.HTTP2Opts,
		Grpc:              option.GrpcOpts,
		XHTTP:             option.XHTTPOpts,
		MKCP:              option.MKCPOpts,
		Mekya:             option.MekyaOpts,
		DialOptions:       t.DialOptions(),
		WSHost:            option.SNI,
		DefaultALPN:       trojan.DefaultALPN,
		DefaultWSALPN:     wsALPN,
	})
	if err != nil {
		return nil, err
	}
	option.Network = t.stack.Network()

	return t, nil
}
