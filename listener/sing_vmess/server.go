package sing_vmess

import (
	"context"
	"errors"
	"net"
	"strings"
	"time"

	"github.com/metacubex/mihomo/adapter/inbound"
	C "github.com/metacubex/mihomo/constant"
	LC "github.com/metacubex/mihomo/listener/config"
	"github.com/metacubex/mihomo/listener/jls"
	"github.com/metacubex/mihomo/listener/reality"
	"github.com/metacubex/mihomo/listener/restls"
	"github.com/metacubex/mihomo/listener/security"
	"github.com/metacubex/mihomo/listener/shadowtls"
	"github.com/metacubex/mihomo/listener/sing"
	"github.com/metacubex/mihomo/listener/tlsmirror"

	"github.com/metacubex/http"
	"github.com/metacubex/mhurl"
	"github.com/metacubex/mihomo/ntp"
	vmess "github.com/metacubex/sing-vmess"
	"github.com/metacubex/sing/common"
	"github.com/metacubex/sing/common/metadata"
	"github.com/metacubex/tls"
)

type Listener struct {
	closed    bool
	config    LC.VmessServer
	listeners []net.Listener
	service   *vmess.Service[string]
}

var _listener *Listener

func New(config LC.VmessServer, lc C.InboundListenConfig, tunnel C.Tunnel, additions ...inbound.Addition) (sl *Listener, err error) {
	if len(additions) == 0 {
		additions = []inbound.Addition{
			inbound.WithInName("DEFAULT-VMESS"),
			inbound.WithSpecialRules(""),
		}
		defer func() {
			_listener = sl
		}()
	}
	h, err := sing.NewListenerHandler(sing.ListenerConfig{
		Tunnel:    tunnel,
		Type:      C.VMESS,
		Additions: additions,
		MuxOption: config.MuxOption,
	})
	if err != nil {
		return nil, err
	}
	if config.MekyaConfig.Enable {
		if config.MKCPConfig.Enable {
			return nil, errors.New("mkcp-config is unavailable in mekya")
		}
		if config.WsPath != "" || config.GrpcServiceName != "" {
			return nil, errors.New("ws and grpc are unavailable in mekya")
		}
	}

	service := vmess.NewService[string](h, vmess.ServiceWithDisableHeaderProtection(), vmess.ServiceWithTimeFunc(ntp.Now))
	err = service.UpdateUsers(
		common.Map(config.Users, func(it LC.VmessUser) string {
			return it.Username
		}),
		common.Map(config.Users, func(it LC.VmessUser) string {
			return it.UUID
		}),
		common.Map(config.Users, func(it LC.VmessUser) int {
			return it.AlterID
		}))
	if err != nil {
		return nil, err
	}

	err = service.Start()
	if err != nil {
		return nil, err
	}

	sl = &Listener{false, config, nil, service}

	httpServer := http.Server{
		IdleTimeout: 30 * time.Second,
		Protocols:   new(http.Protocols),
	}
	tlsConfig, err := security.BuildTLS(security.TLSOption{
		Certificate:    config.Certificate,
		PrivateKey:     config.PrivateKey,
		ClientAuthType: config.ClientAuthType,
		ClientAuthCert: config.ClientAuthCert,
		EchKey:         config.EchKey,
	}, false)
	if err != nil {
		return nil, err
	}
	var shadowTLSBuilder *shadowtls.Builder
	var restlsBuilder *restls.Builder
	var jlsBuilder *jls.Builder
	var realityBuilder *reality.Builder
	var tlsMirrorBuilder *tlsmirror.Builder

	if tlsConfig.ClientAuth != tls.NoClientCert && tlsConfig.GetCertificate == nil {
		return nil, errors.New("client-auth requires certificate")
	}
	tcpOnlySecurityMode := ""
	if config.ShadowTLS.Enable {
		tcpOnlySecurityMode = "ShadowTLS"
	}
	if config.ResTLS.Enable {
		tcpOnlySecurityMode = "Restls"
	}
	if config.JLSConfig.Enable {
		tcpOnlySecurityMode = "JLS"
	}
	if err := security.CheckExclusive(security.Modes(security.HasCertificate(security.TLSOption{
		Certificate:    config.Certificate,
		PrivateKey:     config.PrivateKey,
		ClientAuthType: config.ClientAuthType,
		ClientAuthCert: config.ClientAuthCert,
		EchKey:         config.EchKey,
	}), config.ShadowTLS.Enable, config.ResTLS.Enable, config.JLSConfig.Enable, config.RealityConfig.PrivateKey != "", config.TLSMirrorConfig.PrimaryKey != "")); err != nil {
		return nil, err
	}
	if config.MKCPConfig.Enable && tcpOnlySecurityMode != "" {
		return nil, errors.New(tcpOnlySecurityMode + " only supports TCP transports")
	}
	if config.RealityConfig.PrivateKey != "" {
		realityBuilder, err = config.RealityConfig.Build(tunnel)
		if err != nil {
			return nil, err
		}
	}
	if config.TLSMirrorConfig.PrimaryKey != "" {
		tlsMirrorBuilder = tlsmirror.Config{
			PrimaryKey:                    config.TLSMirrorConfig.PrimaryKey,
			Dest:                          config.TLSMirrorConfig.Dest,
			Proxy:                         config.TLSMirrorConfig.Proxy,
			ExplicitNonceCipherSuites:     config.TLSMirrorConfig.ExplicitNonceCipherSuites,
			DeferInstanceDerivedWriteTime: config.TLSMirrorConfig.DeferInstanceDerivedWriteTime.Build(),
			TransportLayerPadding:         config.TLSMirrorConfig.TransportLayerPadding.Build(),
			ConnectionEnrolment:           config.TLSMirrorConfig.ConnectionEnrolment.Build(),
			SequenceWatermarkingEnabled:   config.TLSMirrorConfig.SequenceWatermarkingEnabled,
		}.Build(tunnel)
		h.Tunnel = tlsMirrorBuilder.WrapTunnel(tunnel)
	}
	if config.ShadowTLS.Enable {
		shadowTLSBuilder, err = shadowtls.New(config.ShadowTLS, tunnel)
		if err != nil {
			return nil, err
		}
	}
	if config.ResTLS.Enable {
		restlsBuilder = restls.New(config.ResTLS, tunnel)
	}
	if config.JLSConfig.Enable {
		jlsBuilder, err = jls.New(config.JLSConfig, tunnel)
		if err != nil {
			return nil, err
		}
	}
	transportOption := security.TransportOption{
		WsPath:          config.WsPath,
		GrpcServiceName: config.GrpcServiceName,
	}
	if config.MKCPConfig.Enable {
		transportOption.MKCP = &config.MKCPConfig
	}
	if config.MekyaConfig.Enable {
		transportOption.Mekya = &config.MekyaConfig
	}
	wrap, udpWrap, err := security.ApplyTransport(&httpServer, tlsConfig, transportOption, func(conn net.Conn) {
		sl.HandleConn(conn, tunnel, additions...)
	})
	if err != nil {
		return nil, err
	}

	for _, addr := range strings.Split(config.Listen, ",") {
		//TCP
		var l net.Listener
		if udpWrap != nil {
			pc, err := lc.ListenPacket(context.Background(), "udp", addr)
			if err != nil {
				return nil, err
			}
			l, err = udpWrap(pc)
			if err != nil {
				_ = pc.Close()
				return nil, err
			}
		} else {
			l, err = lc.Listen(context.Background(), "tcp", addr)
			if err != nil {
				return nil, err
			}
		}
		l = security.WrapListener(l, security.Builders{
			ShadowTLS: shadowTLSBuilder,
			RestLS:    restlsBuilder,
			JLS:       jlsBuilder,
			Reality:   realityBuilder,
			TLSMirror: tlsMirrorBuilder,
		}, tlsConfig)
		if wrap != nil {
			l, err = wrap(l)
			if err != nil {
				return nil, err
			}
		}
		sl.listeners = append(sl.listeners, l)

		go func() {
			if httpServer.Handler != nil {
				_ = httpServer.Serve(l)
				return
			}
			for {
				c, err := l.Accept()
				if err != nil {
					if sl.closed {
						break
					}
					continue
				}

				go sl.HandleConn(c, tunnel)
			}
		}()
	}

	return sl, nil
}

func (l *Listener) Close() error {
	l.closed = true
	var retErr error
	for _, lis := range l.listeners {
		err := lis.Close()
		if err != nil {
			retErr = err
		}
	}
	err := l.service.Close()
	if err != nil {
		retErr = err
	}
	return retErr
}

func (l *Listener) Config() string {
	return l.config.String()
}

func (l *Listener) AddrList() (addrList []net.Addr) {
	for _, lis := range l.listeners {
		addrList = append(addrList, lis.Addr())
	}
	return
}

func (l *Listener) HandleConn(conn net.Conn, tunnel C.Tunnel, additions ...inbound.Addition) {
	ctx := sing.WithAdditions(context.TODO(), additions...)
	err := l.service.NewConnection(ctx, conn, metadata.Metadata{
		Protocol: "vmess",
		Source:   metadata.SocksaddrFromNet(conn.RemoteAddr()),
	})
	if err != nil {
		_ = conn.Close()
		return
	}
}

func HandleVmess(conn net.Conn, tunnel C.Tunnel, additions ...inbound.Addition) bool {
	if _listener != nil && _listener.service != nil {
		go _listener.HandleConn(conn, tunnel, additions...)
		return true
	}
	return false
}

func ParseVmessURL(s string) (addr, username, password string, err error) {
	u, err := mhurl.Parse(s) // we need multiple hosts url supports
	if err != nil {
		return
	}

	addr = u.Host
	if u.User != nil {
		username = u.User.Username()
		password, _ = u.User.Password()
	}
	return
}
