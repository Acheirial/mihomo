package trojan

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"time"

	"github.com/metacubex/mihomo/adapter/inbound"
	N "github.com/metacubex/mihomo/common/net"
	"github.com/metacubex/mihomo/common/utils"
	C "github.com/metacubex/mihomo/constant"
	LC "github.com/metacubex/mihomo/listener/config"
	"github.com/metacubex/mihomo/listener/jls"
	"github.com/metacubex/mihomo/listener/reality"
	"github.com/metacubex/mihomo/listener/restls"
	"github.com/metacubex/mihomo/listener/security"
	"github.com/metacubex/mihomo/listener/shadowtls"
	"github.com/metacubex/mihomo/listener/sing"
	"github.com/metacubex/mihomo/transport/shadowsocks/core"
	"github.com/metacubex/mihomo/transport/socks5"
	"github.com/metacubex/mihomo/transport/trojan"

	"github.com/metacubex/http"
	"github.com/metacubex/smux"
	"github.com/metacubex/tls"
)

type Listener struct {
	closed     bool
	config     LC.TrojanServer
	listeners  []net.Listener
	keys       map[[trojan.KeyLength]byte]string
	pickCipher core.Cipher
	handler    *sing.ListenerHandler
}

func New(config LC.TrojanServer, lc C.InboundListenConfig, tunnel C.Tunnel, additions ...inbound.Addition) (sl *Listener, err error) {
	if len(additions) == 0 {
		additions = []inbound.Addition{
			inbound.WithInName("DEFAULT-TROJAN"),
			inbound.WithSpecialRules(""),
		}
	}
	h, err := sing.NewListenerHandler(sing.ListenerConfig{
		Tunnel:    tunnel,
		Type:      C.TROJAN,
		Additions: additions,
		MuxOption: config.MuxOption,
	})
	if err != nil {
		return nil, err
	}

	keys := make(map[[trojan.KeyLength]byte]string)
	for _, user := range config.Users {
		keys[trojan.Key(user.Password)] = user.Username
	}

	var pickCipher core.Cipher
	if config.TrojanSSOption.Enabled {
		if config.TrojanSSOption.Password == "" {
			return nil, errors.New("empty password")
		}
		if config.TrojanSSOption.Method == "" {
			config.TrojanSSOption.Method = "AES-128-GCM"
		}
		pickCipher, err = core.PickCipher(config.TrojanSSOption.Method, nil, config.TrojanSSOption.Password)
		if err != nil {
			return nil, err
		}
	}
	sl = &Listener{false, config, nil, keys, pickCipher, h}

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
	wrap, _, err := security.ApplyTransport(&httpServer, tlsConfig, security.TransportOption{
		WsPath:          config.WsPath,
		GrpcServiceName: config.GrpcServiceName,
	}, func(conn net.Conn) {
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
		if l == l0 && !config.TrojanSSOption.Enabled && !config.AllowInsecure {
			return nil, errors.New("disallow using Trojan without any certificates/shadow-tls/res-tls/jls/reality/ss/allow-insecure config")
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

				go sl.HandleConn(c, tunnel, additions...)
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
	defer conn.Close()

	if l.pickCipher != nil {
		conn = l.pickCipher.StreamConn(conn)
	}

	var key [trojan.KeyLength]byte
	if _, err := io.ReadFull(conn, key[:]); err != nil {
		//log.Warnln("read key error: %s", err.Error())
		return
	}

	if user, ok := l.keys[key]; ok {
		additions = append(additions, inbound.WithInUser(user))
	} else {
		//log.Warnln("no such key")
		return
	}

	var crlf [2]byte
	if _, err := io.ReadFull(conn, crlf[:]); err != nil {
		//log.Warnln("read crlf error: %s", err.Error())
		return
	}

	l.handleConn(false, conn, tunnel, additions...)
}

func (l *Listener) handleConn(inMux bool, conn net.Conn, tunnel C.Tunnel, additions ...inbound.Addition) {
	if inMux {
		defer conn.Close()
	}

	command, err := socks5.ReadByte(conn)
	if err != nil {
		//log.Warnln("read command error: %s", err.Error())
		return
	}

	switch command {
	case trojan.CommandTCP, trojan.CommandUDP, trojan.CommandMux:
	default:
		//log.Warnln("unknown command: %d", command)
		return
	}

	target, err := socks5.ReadAddr0(conn)
	if err != nil {
		//log.Warnln("read target error: %s", err.Error())
		return
	}

	if !inMux {
		var crlf [2]byte
		if _, err := io.ReadFull(conn, crlf[:]); err != nil {
			//log.Warnln("read crlf error: %s", err.Error())
			return
		}
	}

	switch command {
	case trojan.CommandTCP:
		//tunnel.HandleTCPConn(inbound.NewSocket(target, conn, C.TROJAN, additions...))
		l.handler.HandleSocket(target, conn, additions...)
	case trojan.CommandUDP:
		pc := trojan.NewPacketConn(conn)
		remoteAddr := conn.RemoteAddr()
		connID := utils.NewUUIDV4().String() // make a new SNAT key

		for {
			data, put, addr, err := pc.WaitReadFrom()
			if err != nil {
				if put != nil {
					put()
				}
				break
			}
			target := socks5.ParseAddrToSocksAddr(addr)
			cPacket := &packet{
				pc:      pc,
				rAddr:   remoteAddr,
				payload: data,
				put:     put,
			}
			cPacket.rAddr = N.NewCustomAddr(C.TROJAN.String(), connID, cPacket.rAddr) // for tunnel's handleUDPConn

			tunnel.HandleUDPPacket(inbound.NewPacket(target, cPacket, C.TROJAN, additions...))
		}
	case trojan.CommandMux:
		if inMux {
			//log.Warnln("invalid command: %d", command)
			return
		}
		smuxConfig := smux.DefaultConfig()
		smuxConfig.KeepAliveDisabled = true
		session, err := smux.Server(conn, smuxConfig)
		if err != nil {
			//log.Warnln("smux server error: %s", err.Error())
			return
		}
		defer session.Close()
		for {
			stream, err := session.AcceptStream()
			if err != nil {
				return
			}
			go l.handleConn(true, stream, tunnel, additions...)
		}
	}
}
