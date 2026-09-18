package trusttunnel

import (
	"context"
	"errors"
	"net"
	"strings"

	"github.com/metacubex/mihomo/adapter/inbound"
	"github.com/metacubex/mihomo/common/sockopt"
	C "github.com/metacubex/mihomo/constant"
	LC "github.com/metacubex/mihomo/listener/config"
	"github.com/metacubex/mihomo/listener/security"
	"github.com/metacubex/mihomo/listener/sing"
	"github.com/metacubex/mihomo/log"
	"github.com/metacubex/mihomo/transport/trusttunnel"

	"github.com/metacubex/tls"
)

type Listener struct {
	closed       bool
	config       LC.TrustTunnelServer
	listeners    []net.Listener
	udpListeners []net.PacketConn
	tlsConfig    *tls.Config
	services     []*trusttunnel.Service
}

func New(config LC.TrustTunnelServer, lc C.InboundListenConfig, tunnel C.Tunnel, additions ...inbound.Addition) (sl *Listener, err error) {
	if len(additions) == 0 {
		additions = []inbound.Addition{
			inbound.WithInName("DEFAULT-TRUSTTUNNEL"),
			inbound.WithSpecialRules(""),
		}
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

	sl = &Listener{
		config:    config,
		tlsConfig: tlsConfig,
	}

	h, err := sing.NewListenerHandler(sing.ListenerConfig{
		Tunnel:    tunnel,
		Type:      C.TRUSTTUNNEL,
		Additions: additions,
	})
	if err != nil {
		return nil, err
	}

	if tlsConfig.GetCertificate == nil {
		return nil, errors.New("disallow using TrustTunnel without certificates config")
	}

	if len(config.Network) == 0 {
		config.Network = []string{"tcp"}
	}
	listenTCP, listenUDP := false, false
	for _, network := range config.Network {
		network = strings.ToLower(network)
		switch {
		case strings.HasPrefix(network, "tcp"):
			listenTCP = true
		case strings.HasPrefix(network, "udp"):
			listenUDP = true
		}
	}

	for _, addr := range strings.Split(config.Listen, ",") {
		addr := addr

		var (
			tcpListener net.Listener
			udpConn     net.PacketConn
		)
		if listenTCP {
			tcpListener, err = lc.Listen(context.Background(), "tcp", addr)
			if err != nil {
				_ = sl.Close()
				return nil, err
			}
			sl.listeners = append(sl.listeners, tcpListener)
		}
		if listenUDP {
			udpConn, err = lc.ListenPacket(context.Background(), "udp", addr)
			if err != nil {
				_ = sl.Close()
				return nil, err
			}

			if err := sockopt.UDPReuseaddr(udpConn); err != nil {
				log.Warnln("Failed to Reuse UDP Address: %s", err)
			}
			sl.udpListeners = append(sl.udpListeners, udpConn)
		}

		service := trusttunnel.NewService(trusttunnel.ServiceOptions{
			Ctx:                   context.Background(),
			Logger:                log.SingLogger,
			Handler:               h,
			ICMPHandler:           nil,
			QUICCongestionControl: config.CongestionController,
			QUICCwnd:              config.CWND,
			QUICBBRProfile:        config.BBRProfile,
		})
		service.UpdateUsers(config.Users)
		err = service.Start(tcpListener, udpConn, tlsConfig)
		if err != nil {
			_ = sl.Close()
			return nil, err
		}

		sl.services = append(sl.services, service)
	}

	return sl, nil
}

func (l *Listener) Close() error {
	l.closed = true
	var retErr error
	for _, lis := range l.services {
		err := lis.Close()
		if err != nil {
			retErr = err
		}
	}
	for _, lis := range l.listeners {
		err := lis.Close()
		if err != nil {
			retErr = err
		}
	}
	for _, lis := range l.udpListeners {
		err := lis.Close()
		if err != nil {
			retErr = err
		}
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
	for _, lis := range l.udpListeners {
		addrList = append(addrList, lis.LocalAddr())
	}
	return
}
