package sing_vless

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
	"github.com/metacubex/mihomo/transport/vless/encryption"

	"github.com/metacubex/http"
	"github.com/metacubex/sing/common"
	"github.com/metacubex/sing/common/metadata"
	"github.com/metacubex/tls"
)

type Listener struct {
	closed     bool
	config     LC.VlessServer
	listeners  []net.Listener
	service    *Service[string]
	decryption *encryption.ServerInstance
}

func New(config LC.VlessServer, lc C.InboundListenConfig, tunnel C.Tunnel, additions ...inbound.Addition) (sl *Listener, err error) {
	if len(additions) == 0 {
		additions = []inbound.Addition{
			inbound.WithInName("DEFAULT-VLESS"),
			inbound.WithSpecialRules(""),
		}
	}
	h, err := sing.NewListenerHandler(sing.ListenerConfig{
		Tunnel:    tunnel,
		Type:      C.VLESS,
		Additions: additions,
		MuxOption: config.MuxOption,
	})
	if err != nil {
		return nil, err
	}

	service := NewService[string](h)
	service.UpdateUsers(
		common.Map(config.Users, func(it LC.VlessUser) string {
			return it.Username
		}),
		common.Map(config.Users, func(it LC.VlessUser) string {
			return it.UUID
		}),
		common.Map(config.Users, func(it LC.VlessUser) string {
			return it.Flow
		}))

	sl = &Listener{config: config, service: service}

	sl.decryption, err = encryption.NewServer(config.Decryption)
	if err != nil {
		return nil, err
	}
	if sl.decryption != nil {
		decryption := sl.decryption
		defer func() { // decryption must be closed to avoid the goroutine leak
			if err != nil {
				_ = decryption.Close()
			}
		}()
	}

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

	if tlsConfig.ClientAuth != tls.NoClientCert && tlsConfig.GetCertificate == nil {
		return nil, errors.New("client-auth requires certificate")
	}
	if err := security.CheckExclusive(security.Modes(security.HasCertificate(security.TLSOption{
		Certificate:    config.Certificate,
		PrivateKey:     config.PrivateKey,
		ClientAuthType: config.ClientAuthType,
		ClientAuthCert: config.ClientAuthCert,
		EchKey:         config.EchKey,
	}), config.ShadowTLS.Enable, config.ResTLS.Enable, config.JLSConfig.Enable, config.RealityConfig.PrivateKey != "", false)); err != nil {
		return nil, err
	}
	if config.RealityConfig.PrivateKey != "" {
		realityBuilder, err = config.RealityConfig.Build(tunnel)
		if err != nil {
			return nil, err
		}
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
	if config.XHTTPConfig.Path != "" || config.XHTTPConfig.Host != "" || config.XHTTPConfig.Mode != "" {
		transportOption.XHTTP = &config.XHTTPConfig
	}
	wrap, _, err := security.ApplyTransport(&httpServer, tlsConfig, transportOption, func(conn net.Conn) {
		sl.HandleConn(conn, tunnel, additions...)
	})
	if err != nil {
		return nil, err
	}

	for _, addr := range strings.Split(config.Listen, ",") {
		addr := addr

		//TCP
		l0, err := lc.Listen(context.Background(), "tcp", addr)
		if err != nil {
			return nil, err
		}
		l := security.WrapListener(l0, security.Builders{
			ShadowTLS: shadowTLSBuilder,
			RestLS:    restlsBuilder,
			JLS:       jlsBuilder,
			Reality:   realityBuilder,
		}, tlsConfig)
		if l == l0 && sl.decryption == nil && !config.AllowInsecure {
			return nil, errors.New("disallow using Vless without any certificates/shadow-tls/res-tls/jls/reality/decryption/allow-insecure config")
		}
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
	if l.decryption != nil {
		_ = l.decryption.Close()
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
	if l.decryption != nil {
		c, err := l.decryption.Handshake(conn, nil)
		if err != nil {
			_ = conn.Close()
			return
		}
		conn = c
	}
	err := l.service.NewConnection(ctx, conn, metadata.Metadata{
		Protocol: "vless",
		Source:   metadata.SocksaddrFromNet(conn.RemoteAddr()),
	})
	if err != nil {
		_ = conn.Close()
		return
	}
}
