package outbound

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/metacubex/mihomo/common/convert"
	"github.com/metacubex/mihomo/component/dialer"
	"github.com/metacubex/mihomo/component/ech"
	tlsC "github.com/metacubex/mihomo/component/tls"
	C "github.com/metacubex/mihomo/constant"
	"github.com/metacubex/mihomo/transport/gun"
	"github.com/metacubex/mihomo/transport/jls"
	"github.com/metacubex/mihomo/transport/mekya"
	"github.com/metacubex/mihomo/transport/mkcp"
	"github.com/metacubex/mihomo/transport/restls"
	"github.com/metacubex/mihomo/transport/shadowtls"
	"github.com/metacubex/mihomo/transport/tlsmirror"
	"github.com/metacubex/mihomo/transport/tuic/common"
	"github.com/metacubex/mihomo/transport/vmess"
	"github.com/metacubex/mihomo/transport/xhttp"

	"github.com/metacubex/http"
	"github.com/metacubex/quic-go"
	"github.com/samber/lo"
)

// StreamStack composes security + transport on top of C.Dialer.
// Protocol handshake stays in the outbound.
type StreamStack struct {
	dialer   C.Dialer
	addr     string
	network  string
	tls      bool
	tlsCfg   *vmess.TLSConfig
	ws       *vmess.WebsocketConfig
	http     *vmess.HTTPConfig
	h2       *vmess.H2Config
	gun      *gun.Client
	xhttp    *xhttp.Client
	mekya    *mekya.Client
	mkcp     mkcp.Config
	dialOpts []dialer.Option
}

// StreamStackOption is the YAML-facing combination of network + TLS fields.
type StreamStackOption struct {
	Dialer            C.Dialer
	Addr              string
	Server            string
	Port              int
	Network           string
	TLS               bool
	ForceTLS          bool
	ALPN              []string
	SkipCertVerify    bool
	NameCertVerify    string
	Fingerprint       string
	Certificate       string
	PrivateKey        string
	ServerName        string
	ClientFingerprint string
	ECH               *ech.Config
	ShadowTLS         *shadowtls.Config
	Restls            *restls.Config
	JLS               *jls.Config
	Reality           *tlsC.RealityConfig
	TLSMirror         *tlsmirror.Config
	TLSMirrorDialer   tlsmirror.EnrollmentDialer
	SecurityMode      string
	WS                WSOptions
	HTTP              HTTPOptions
	H2                HTTP2Options
	Grpc              GrpcOptions
	XHTTP             XHTTPOptions
	MKCP              MKCPOptions
	Mekya             MekyaOptions
	DialOptions       []dialer.Option
	RandomizeWSHostWithoutTLS bool
	WSHost                    string
	DefaultALPN               []string
	DefaultWSALPN             []string
}


func NormalizeNetwork(network string) string {
	switch strings.ToLower(network) {
	case "", "tcp":
		return "tcp"
	case "httpupgrade":
		return "ws"
	case "kcp":
		return "mkcp"
	default:
		return strings.ToLower(network)
	}
}

func checkExclusiveSecurityModes(modes []string) (string, error) {
	if len(modes) > 1 {
		return "", errors.New("security modes are mutually exclusive: " + strings.Join(modes, ", "))
	}
	if len(modes) == 1 {
		return modes[0], nil
	}
	return "", nil
}

func collectSecurityModes(shadowTLS *shadowtls.Config, restls *restls.Config, jls *jls.Config, reality *tlsC.RealityConfig, tlsMirror bool) []string {
	modes := make([]string, 0, 5)
	if shadowTLS != nil {
		modes = append(modes, "ShadowTLS")
	}
	if restls != nil {
		modes = append(modes, "Restls")
	}
	if jls != nil {
		modes = append(modes, "JLS")
	}
	if reality != nil {
		modes = append(modes, "REALITY")
	}
	if tlsMirror {
		modes = append(modes, "TLSMirror")
	}
	return modes
}

func NewStreamStack(opt StreamStackOption) (*StreamStack, error) {
	rawNetwork := strings.ToLower(opt.Network)
	if rawNetwork == "httpupgrade" {
		opt.WS.V2rayHttpUpgrade = true
	}
	network := NormalizeNetwork(opt.Network)
	if opt.ForceTLS {
		opt.TLS = true
	}
	if opt.SecurityMode != "" && !opt.TLS {
		return nil, fmt.Errorf("%s requires TLS", opt.SecurityMode)
	}
	if network == "mkcp" {
		switch opt.SecurityMode {
		case "ShadowTLS", "Restls", "JLS":
			return nil, fmt.Errorf("%s only supports TCP transports", opt.SecurityMode)
		}
	}

	host, port, _ := net.SplitHostPort(opt.Addr)
	serverName := opt.ServerName
	if serverName == "" {
		serverName = host
	}

	tlsCfg := &vmess.TLSConfig{
		Host:              serverName,
		SkipCertVerify:    opt.SkipCertVerify,
		NameCertVerify:    opt.NameCertVerify,
		FingerPrint:       opt.Fingerprint,
		Certificate:       opt.Certificate,
		PrivateKey:        opt.PrivateKey,
		ClientFingerprint: opt.ClientFingerprint,
		NextProtos:        opt.ALPN,
		ECH:               opt.ECH,
		ShadowTLS:         opt.ShadowTLS,
		Restls:            opt.Restls,
		JLS:               opt.JLS,
		Reality:           opt.Reality,
		TLSMirror:         opt.TLSMirror,
		TLSMirrorDialer:   opt.TLSMirrorDialer,
	}
	if len(tlsCfg.NextProtos) == 0 && len(opt.DefaultALPN) > 0 {
		tlsCfg.NextProtos = opt.DefaultALPN
	}

	s := &StreamStack{
		dialer:   opt.Dialer,
		addr:     opt.Addr,
		network:  network,
		tls:      opt.TLS,
		tlsCfg:   tlsCfg,
		dialOpts: opt.DialOptions,
	}

	switch network {
	case "ws":
		wsHost := host
		if opt.WSHost != "" {
			wsHost = opt.WSHost
		}
		headers := http.Header{}
		if len(opt.WS.Headers) != 0 {
			for key, value := range opt.WS.Headers {
				headers.Add(key, value)
			}
		}
		if !opt.TLS && opt.RandomizeWSHostWithoutTLS {
			if headers.Get("Host") == "" {
				headers.Set("Host", convert.RandHost())
				convert.SetUserAgent(headers)
			}
		}
		s.ws = &vmess.WebsocketConfig{
			Host:                     wsHost,
			Port:                     port,
			Path:                     opt.WS.Path,
			MaxEarlyData:             opt.WS.MaxEarlyData,
			EarlyDataHeaderName:      opt.WS.EarlyDataHeaderName,
			V2rayHttpUpgrade:         opt.WS.V2rayHttpUpgrade,
			V2rayHttpUpgradeFastOpen: opt.WS.V2rayHttpUpgradeFastOpen,
			Headers:                  headers,
		}
		if opt.TLS {
			if opt.ServerName != "" {
				s.tlsCfg.Host = opt.ServerName
			} else if h := headers.Get("Host"); h != "" {
				s.tlsCfg.Host = h
			} else if opt.WSHost != "" {
				s.tlsCfg.Host = opt.WSHost
			}
			if len(opt.DefaultWSALPN) > 0 {
				s.tlsCfg.NextProtos = opt.DefaultWSALPN
			} else {
				s.tlsCfg.NextProtos = []string{"http/1.1"}
			}
			s.tlsCfg.WebsocketALPN = true
		}
	case "http":
		httpHost := host
		s.http = &vmess.HTTPConfig{
			Host:    httpHost,
			Method:  opt.HTTP.Method,
			Path:    opt.HTTP.Path,
			Headers: opt.HTTP.Headers,
		}
	case "h2":
		h2opts := opt.H2
		if len(h2opts.Host) == 0 {
			h2opts.Host = append(h2opts.Host, "www.example.com")
		}
		s.h2 = &vmess.H2Config{
			Hosts: h2opts.Host,
			Path:  h2opts.Path,
		}
	case "grpc":
		dialFn := func(ctx context.Context, _, _ string) (net.Conn, error) {
			c, err := opt.Dialer.DialContext(ctx, "tcp", opt.Addr)
			if err != nil {
				return nil, fmt.Errorf("%s connect error: %s", opt.Addr, err.Error())
			}
			return c, nil
		}
		gunHost := opt.ServerName
		if gunHost == "" {
			gunHost = opt.Addr
		}
		gunConfig := &gun.Config{
			ServiceName:  opt.Grpc.GrpcServiceName,
			UserAgent:    opt.Grpc.GrpcUserAgent,
			Host:         gunHost,
			PingInterval: opt.Grpc.PingInterval,
		}
		var gunTLS *vmess.TLSConfig
		if opt.TLS {
			gunTLS = cloneTLSConfig(tlsCfg)
			gunTLS.NextProtos = []string{"h2"}
			if opt.ServerName == "" {
				gunTLS.Host = host
			}
		}
		s.gun = gun.NewClient(
			func() *gun.Transport {
				return gun.NewTransport(dialFn, gunTLS, gunConfig)
			},
			opt.Grpc.MaxConnections,
			opt.Grpc.MinStreams,
			opt.Grpc.MaxStreams,
		)
	case "mkcp":
		s.mkcp = opt.MKCP.Build()
	case "mekya":
		alpn := opt.ALPN
		if len(alpn) == 0 {
			alpn = []string{"h2", "http/1.1"}
			s.tlsCfg.NextProtos = alpn
		}
		cfg := opt.Mekya.Build()
		if cfg.URL == "" {
			cfg.URL = "https://" + opt.Addr
		}
		client, err := mekya.NewClient(context.Background(), func(ctx context.Context) (net.Conn, error) {
			rawConn, err := opt.Dialer.DialContext(ctx, "tcp", opt.Addr)
			if err != nil {
				return nil, err
			}
			conn, err := s.handshakeTLS(ctx, rawConn, false)
			if err != nil {
				_ = rawConn.Close()
				return nil, err
			}
			return conn, nil
		}, cfg)
		if err != nil {
			return nil, err
		}
		s.mekya = client
	case "xhttp":
		if err := s.setupXHTTP(opt); err != nil {
			return nil, err
		}
	case "tcp":
	default:
		return nil, fmt.Errorf("unsupported network: %s", network)
	}
	return s, nil
}

func (s *StreamStack) Session() bool {
	switch s.network {
	case "grpc", "xhttp", "mekya", "mkcp":
		return true
	default:
		return false
	}
}

func (s *StreamStack) Network() string {
	return s.network
}

func (s *StreamStack) Dial(ctx context.Context) (net.Conn, error) {
	switch s.network {
	case "grpc":
		return s.gun.Dial()
	case "xhttp":
		return s.xhttp.Dial(ctx)
	case "mekya":
		return s.mekya.Dial(ctx)
	case "mkcp":
		raw, err := s.dialer.DialContext(ctx, "udp", s.addr)
		if err != nil {
			return nil, err
		}
		c, err := mkcp.Dial(ctx, raw, s.mkcp)
		if err != nil {
			_ = raw.Close()
			return nil, err
		}
		return c, nil
	default:
		return s.dialer.DialContext(ctx, "tcp", s.addr)
	}
}

func (s *StreamStack) Wrap(ctx context.Context, c net.Conn) (net.Conn, error) {
	if s.Session() {
		return c, nil
	}
	var err error
	switch s.network {
	case "ws":
		if s.tls {
			c, err = vmess.StreamTLSConn(ctx, c, s.tlsCfg)
			if err != nil {
				return nil, err
			}
		}
		return vmess.StreamWebsocketConn(ctx, c, s.ws)
	case "http":
		c, err = s.handshakeTLS(ctx, c, false)
		if err != nil {
			return nil, err
		}
		return vmess.StreamHTTPConn(c, s.http), nil
	case "h2":
		c, err = s.handshakeTLS(ctx, c, true)
		if err != nil {
			return nil, err
		}
		return vmess.StreamH2Conn(ctx, c, s.h2)
	default:
		return s.handshakeTLS(ctx, c, false)
	}
}

func (s *StreamStack) handshakeTLS(ctx context.Context, conn net.Conn, isH2 bool) (net.Conn, error) {
	if !s.tls {
		return conn, nil
	}
	cfg := cloneTLSConfig(s.tlsCfg)
	if isH2 {
		cfg.NextProtos = []string{"h2"}
	}
	return vmess.StreamTLSConn(ctx, conn, cfg)
}

func (s *StreamStack) Close() error {
	var errs []error
	if s.gun != nil {
		if err := s.gun.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if s.xhttp != nil {
		if err := s.xhttp.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if s.mekya != nil {
		if err := s.mekya.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func cloneTLSConfig(cfg *vmess.TLSConfig) *vmess.TLSConfig {
	if cfg == nil {
		return nil
	}
	cp := *cfg
	if cfg.NextProtos != nil {
		cp.NextProtos = append([]string(nil), cfg.NextProtos...)
	}
	return &cp
}

func (s *StreamStack) setupXHTTP(opt StreamStackOption) error {
	requestHost := opt.XHTTP.Host
	if requestHost == "" {
		if opt.ServerName != "" {
			requestHost = opt.ServerName
		} else {
			requestHost = opt.Server
		}
		if ip, err := netip.ParseAddr(requestHost); err == nil && ip.Is6() {
			requestHost = "[" + requestHost + "]"
		}
	}

	var hKeepAlivePeriod time.Duration
	var reuseCfg *xhttp.ReuseConfig
	if opt.XHTTP.ReuseSettings != nil {
		reuseCfg = xhttpReuse(opt.XHTTP.ReuseSettings)
		hKeepAlivePeriod = time.Duration(opt.XHTTP.ReuseSettings.HKeepAlivePeriod) * time.Second
	}

	cfg := xhttpConfig(opt.XHTTP, requestHost, reuseCfg)
	makeTransport := s.xhttpTransport(opt, opt.Addr, opt.TLS, opt.ALPN, opt.SkipCertVerify, opt.NameCertVerify, opt.Fingerprint, opt.Certificate, opt.PrivateKey, opt.ServerName, opt.ClientFingerprint, opt.ECH, opt.ShadowTLS, opt.Restls, opt.JLS, opt.Reality, opt.SecurityMode, hKeepAlivePeriod)

	var makeDownloadTransport xhttp.TransportMaker
	if ds := opt.XHTTP.DownloadSettings; ds != nil {
		if cfg.Mode == "stream-one" {
			return fmt.Errorf(`xhttp mode "stream-one" cannot be used with download-settings`)
		}
		download, err := s.xhttpDownload(opt, cfg, ds, hKeepAlivePeriod)
		if err != nil {
			return err
		}
		cfg.DownloadConfig = download.cfg
		makeDownloadTransport = download.maker
	}

	client, err := xhttp.NewClient(cfg, makeTransport, makeDownloadTransport, opt.Reality != nil)
	if err != nil {
		return err
	}
	s.xhttp = client
	return nil
}

func xhttpReuse(rs *XHTTPReuseSettings) *xhttp.ReuseConfig {
	if rs == nil {
		return nil
	}
	return &xhttp.ReuseConfig{
		MaxConcurrency:   rs.MaxConcurrency,
		MaxConnections:   rs.MaxConnections,
		CMaxReuseTimes:   rs.CMaxReuseTimes,
		HMaxRequestTimes: rs.HMaxRequestTimes,
		HMaxReusableSecs: rs.HMaxReusableSecs,
	}
}

func xhttpConfig(opt XHTTPOptions, host string, reuse *xhttp.ReuseConfig) *xhttp.Config {
	return &xhttp.Config{
		Host:                 host,
		Path:                 opt.Path,
		Mode:                 opt.Mode,
		Headers:              opt.Headers,
		NoGRPCHeader:         opt.NoGRPCHeader,
		XPaddingBytes:        opt.XPaddingBytes,
		XPaddingObfsMode:     opt.XPaddingObfsMode,
		XPaddingKey:          opt.XPaddingKey,
		XPaddingHeader:       opt.XPaddingHeader,
		XPaddingPlacement:    opt.XPaddingPlacement,
		XPaddingMethod:       opt.XPaddingMethod,
		UplinkHTTPMethod:     opt.UplinkHTTPMethod,
		SessionPlacement:     opt.SessionPlacement,
		SessionKey:           opt.SessionKey,
		SessionTable:         opt.SessionTable,
		SessionLength:        opt.SessionLength,
		SeqPlacement:         opt.SeqPlacement,
		SeqKey:               opt.SeqKey,
		UplinkDataPlacement:  opt.UplinkDataPlacement,
		UplinkDataKey:        opt.UplinkDataKey,
		UplinkChunkSize:      opt.UplinkChunkSize,
		ScMaxEachPostBytes:   opt.ScMaxEachPostBytes,
		ScMinPostsIntervalMs: opt.ScMinPostsIntervalMs,
		ReuseConfig:          reuse,
	}
}

func (s *StreamStack) xhttpTransport(
	opt StreamStackOption,
	addr string,
	tlsEnabled bool,
	alpn []string,
	skipCertVerify bool,
	nameCertVerify string,
	fingerprint string,
	certificate string,
	privateKey string,
	serverName string,
	clientFingerprint string,
	echConfig *ech.Config,
	shadowTLS *shadowtls.Config,
	restls *restls.Config,
	jlsCfg *jls.Config,
	reality *tlsC.RealityConfig,
	securityMode string,
	keepAlive time.Duration,
) xhttp.TransportMaker {
	return func() http.RoundTripper {
		return xhttp.NewTransport(
			func(ctx context.Context) (net.Conn, error) {
				return s.dialer.DialContext(ctx, "tcp", addr)
			},
			func(ctx context.Context, raw net.Conn, isH2 bool) (net.Conn, error) {
				if !tlsEnabled {
					return raw, nil
				}
				host, _, _ := net.SplitHostPort(addr)
				tlsOpts := &vmess.TLSConfig{
					Host:              host,
					SkipCertVerify:    skipCertVerify,
					NameCertVerify:    nameCertVerify,
					FingerPrint:       fingerprint,
					Certificate:       certificate,
					PrivateKey:        privateKey,
					ClientFingerprint: clientFingerprint,
					ECH:               echConfig,
					ShadowTLS:         shadowTLS,
					Restls:            restls,
					JLS:               jlsCfg,
					Reality:           reality,
					NextProtos:        alpn,
				}
				if isH2 {
					tlsOpts.NextProtos = []string{"h2"}
				}
				if serverName != "" {
					tlsOpts.Host = serverName
				}
				return vmess.StreamTLSConn(ctx, raw, tlsOpts)
			},
			func(ctx context.Context, cfg *quic.Config) (*quic.Conn, error) {
				host, _, _ := net.SplitHostPort(addr)
				tlsOpts := &vmess.TLSConfig{
					Host:              host,
					SkipCertVerify:    skipCertVerify,
					NameCertVerify:    nameCertVerify,
					FingerPrint:       fingerprint,
					Certificate:       certificate,
					PrivateKey:        privateKey,
					ClientFingerprint: clientFingerprint,
					ECH:               echConfig,
					Reality:           reality,
					NextProtos:        []string{"h3"},
				}
				if serverName != "" {
					tlsOpts.Host = serverName
				}
				if !tlsEnabled {
					return nil, errors.New("xhttp HTTP/3 requires TLS")
				}
				if securityMode != "" {
					return nil, fmt.Errorf("xhttp HTTP/3 does not support %s", securityMode)
				}
				tlsConfig, err := tlsOpts.ToStdConfig()
				if err != nil {
					return nil, err
				}
				err = echConfig.ClientHandle(ctx, tlsConfig)
				if err != nil {
					return nil, err
				}
				_, quicConn, err := common.DialQuic(ctx, addr, s.dialOpts, s.dialer, tlsConfig, cfg, common.DialQuicOption{Early: true})
				return quicConn, err
			},
			alpn,
			keepAlive,
		)
	}
}

type xhttpDownload struct {
	cfg   *xhttp.Config
	maker xhttp.TransportMaker
}

func (s *StreamStack) xhttpDownload(opt StreamStackOption, uplink *xhttp.Config, ds *XHTTPDownloadSettings, hKeepAlivePeriod time.Duration) (*xhttpDownload, error) {
	downloadServer := lo.FromPtrOr(ds.Server, opt.Server)
	downloadPort := lo.FromPtrOr(ds.Port, opt.Port)
	downloadTLS := lo.FromPtrOr(ds.TLS, opt.TLS)
	downloadALPN := lo.FromPtrOr(ds.ALPN, opt.ALPN)
	downloadSkipCertVerify := lo.FromPtrOr(ds.SkipCertVerify, opt.SkipCertVerify)
	downloadNameCertVerify := lo.FromPtrOr(ds.NameCertVerify, opt.NameCertVerify)
	downloadFingerprint := lo.FromPtrOr(ds.Fingerprint, opt.Fingerprint)
	downloadCertificate := lo.FromPtrOr(ds.Certificate, opt.Certificate)
	downloadPrivateKey := lo.FromPtrOr(ds.PrivateKey, opt.PrivateKey)
	downloadServerName := lo.FromPtrOr(ds.ServerName, opt.ServerName)
	downloadClientFingerprint := lo.FromPtrOr(ds.ClientFingerprint, opt.ClientFingerprint)
	downloadEchConfig := opt.ECH
	var err error
	if ds.ECHOpts != nil {
		downloadEchConfig, err = ds.ECHOpts.Parse()
		if err != nil {
			return nil, err
		}
	}
	downloadShadowTLS := opt.ShadowTLS
	if ds.ShadowTLSOpts != nil {
		downloadShadowTLS, err = ds.ShadowTLSOpts.Parse()
		if err != nil {
			return nil, err
		}
	}
	downloadRestls := opt.Restls
	if ds.RestlsOpts != nil {
		downloadRestls, err = ds.RestlsOpts.Parse(downloadServerName, downloadClientFingerprint)
		if err != nil {
			return nil, err
		}
	}
	downloadJLS := opt.JLS
	if ds.JLSOpts != nil {
		downloadJLS, err = ds.JLSOpts.Parse()
		if err != nil {
			return nil, err
		}
	}
	downloadReality := opt.Reality
	if ds.RealityOpts != nil {
		downloadReality, err = ds.RealityOpts.Parse()
		if err != nil {
			return nil, err
		}
	}
	downloadMode, err := checkExclusiveSecurityModes(collectSecurityModes(downloadShadowTLS, downloadRestls, downloadJLS, downloadReality, false))
	if err != nil {
		return nil, fmt.Errorf("xhttp download-settings %w", err)
	}
	if downloadMode != "" && !downloadTLS {
		return nil, fmt.Errorf("xhttp download-settings: %s requires TLS", downloadMode)
	}

	downloadAddr := net.JoinHostPort(downloadServer, strconv.Itoa(downloadPort))
	downloadHost := lo.FromPtrOr(ds.Host, opt.XHTTP.Host)
	if downloadHost == "" {
		if downloadServerName != "" {
			downloadHost = downloadServerName
		} else {
			downloadHost = downloadServer
		}
		if ip, err := netip.ParseAddr(downloadHost); err == nil && ip.Is6() {
			downloadHost = "[" + downloadHost + "]"
		}
	}

	downloadKeepAlive := hKeepAlivePeriod
	downloadCfg := *uplink
	downloadCfg.Host = downloadHost
	downloadCfg.Path = lo.FromPtrOr(ds.Path, opt.XHTTP.Path)
	downloadCfg.Headers = lo.FromPtrOr(ds.Headers, opt.XHTTP.Headers)
	if ds.ReuseSettings != nil {
		downloadCfg.ReuseConfig = xhttpReuse(ds.ReuseSettings)
		downloadKeepAlive = time.Duration(ds.ReuseSettings.HKeepAlivePeriod) * time.Second
	}

	return &xhttpDownload{
		cfg: &downloadCfg,
		maker: s.xhttpTransport(opt, downloadAddr, downloadTLS, downloadALPN, downloadSkipCertVerify, downloadNameCertVerify, downloadFingerprint, downloadCertificate, downloadPrivateKey, downloadServerName, downloadClientFingerprint, downloadEchConfig, downloadShadowTLS, downloadRestls, downloadJLS, downloadReality, downloadMode, downloadKeepAlive),
	}, nil
}
