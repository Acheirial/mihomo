package hysteria2_realm

import (
	"context"
	"fmt"
	"net"
	"regexp"
	"strings"
	"time"

	"github.com/metacubex/mihomo/adapter/inbound"
	C "github.com/metacubex/mihomo/constant"
	LC "github.com/metacubex/mihomo/listener/config"
	"github.com/metacubex/mihomo/listener/security"
	"github.com/metacubex/mihomo/log"

	"github.com/metacubex/http"
)

type Listener struct {
	closed    bool
	config    LC.Hysteria2RealmServer
	listeners []net.Listener
	server    *server
	cancel    func()
}

const (
	DefaultMaxRealms        = 65536
	DefaultMaxRealmsPerIP   = 4
	DefaultRealmNamePattern = defaultRealmNamePattern
)

func DefaultALPN() []string { return []string{"h2", "http/1.1"} }

func New(config LC.Hysteria2RealmServer, lc C.InboundListenConfig, tunnel C.Tunnel, additions ...inbound.Addition) (*Listener, error) {
	if len(additions) == 0 {
		additions = []inbound.Addition{
			inbound.WithInName("DEFAULT-HYSTERIA2-REALM"),
			inbound.WithSpecialRules(""),
		}
	}

	pat, err := regexp.Compile(config.RealmNamePattern)
	if err != nil {
		return nil, fmt.Errorf("invalid realm name pattern %q: %v", config.RealmNamePattern, err)
	}
	s := newServer(serverConfig{
		realmToken:     config.Token,
		maxRealms:      config.MaxRealms,
		maxRealmsPerIP: config.MaxRealmsPerIP,
		proxyHeader:    config.TrustedProxyHeader,
		realmIDPattern: pat,
	})

	tlsConfig, err := security.BuildTLS(security.TLSOption{
		Certificate:    config.Certificate,
		PrivateKey:     config.PrivateKey,
		ClientAuthType: config.ClientAuthType,
		ClientAuthCert: config.ClientAuthCert,
		EchKey:         config.EchKey,
	}, true)
	if err != nil {
		return nil, err
	}

	sl := &Listener{config: config, server: s}

	for _, addr := range strings.Split(config.Listen, ",") {
		addr := addr

		//TCP
		l, err := lc.Listen(context.Background(), "tcp", addr)
		if err != nil {
			return nil, err
		}
		l = security.WrapListener(l, security.Builders{}, tlsConfig)
		sl.listeners = append(sl.listeners, l)

		srv := &http.Server{
			Handler:           s.routes(),
			ReadHeaderTimeout: 10 * time.Second,
		}

		go srv.Serve(l)
	}
	ctx, cancel := context.WithCancel(context.Background())
	sl.cancel = cancel
	go s.reaper(ctx)

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
	if l.cancel != nil {
		l.cancel()
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

func debugf(format string, v ...any) {
	log.Debugln("[RealmServer] "+format, v...)
}
