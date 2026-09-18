package http

import (
	"context"
	"errors"
	"net"

	"github.com/metacubex/mihomo/adapter/inbound"
	C "github.com/metacubex/mihomo/constant"
	authStore "github.com/metacubex/mihomo/listener/auth"
	LC "github.com/metacubex/mihomo/listener/config"
	"github.com/metacubex/mihomo/listener/reality"
	"github.com/metacubex/mihomo/listener/security"

	"github.com/metacubex/tls"
)

type Listener struct {
	listener net.Listener
	addr     string
	closed   bool
}

// RawAddress implements C.Listener
func (l *Listener) RawAddress() string {
	return l.addr
}

// Address implements C.Listener
func (l *Listener) Address() string {
	return l.listener.Addr().String()
}

// Close implements C.Listener
func (l *Listener) Close() error {
	l.closed = true
	return l.listener.Close()
}

func New(addr string, tunnel C.Tunnel, additions ...inbound.Addition) (*Listener, error) {
	return NewWithConfig(LC.AuthServer{Enable: true, Listen: addr, AuthStore: authStore.Default}, inbound.NewListenConfig(), tunnel, additions...)
}

// NewWithAuthenticate
// never change type traits because it's used in CMFA
func NewWithAuthenticate(addr string, tunnel C.Tunnel, authenticate bool, additions ...inbound.Addition) (*Listener, error) {
	store := authStore.Default
	if !authenticate {
		store = authStore.Nil
	}
	return NewWithConfig(LC.AuthServer{Enable: true, Listen: addr, AuthStore: store}, inbound.NewListenConfig(), tunnel, additions...)
}

func NewWithConfig(config LC.AuthServer, lc C.InboundListenConfig, tunnel C.Tunnel, additions ...inbound.Addition) (*Listener, error) {
	isDefault := false
	if len(additions) == 0 {
		isDefault = true
		additions = []inbound.Addition{
			inbound.WithInName("DEFAULT-HTTP"),
			inbound.WithSpecialRules(""),
		}
	}

	l, err := lc.Listen(context.Background(), "tcp", config.Listen)
	if err != nil {
		return nil, err
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
	var realityBuilder *reality.Builder

	if config.RealityConfig.PrivateKey != "" {
		if tlsConfig.GetCertificate != nil {
			return nil, errors.New("certificate is unavailable in reality")
		}
		if tlsConfig.ClientAuth != tls.NoClientCert {
			return nil, errors.New("client-auth is unavailable in reality")
		}
		realityBuilder, err = config.RealityConfig.Build(tunnel)
		if err != nil {
			return nil, err
		}
	}

	l = security.WrapListener(l, security.Builders{Reality: realityBuilder}, tlsConfig)

	hl := &Listener{
		listener: l,
		addr:     config.Listen,
	}

	go func() {
		for {
			conn, err := hl.listener.Accept()
			if err != nil {
				if hl.closed {
					break
				}
				continue
			}

			store := config.AuthStore
			if isDefault || store == authStore.Default { // only apply on default listener
				if !inbound.IsRemoteAddrDisAllowed(conn.RemoteAddr()) {
					_ = conn.Close()
					continue
				}
				if inbound.SkipAuthRemoteAddr(conn.RemoteAddr()) {
					store = authStore.Nil
				}
			}
			go HandleConn(conn, tunnel, store, additions...)
		}
	}()

	return hl, nil
}
