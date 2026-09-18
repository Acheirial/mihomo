package security

import (
	"context"
	"errors"
	"net"

	LC "github.com/metacubex/mihomo/listener/config"
	"github.com/metacubex/mihomo/transport/gun"
	"github.com/metacubex/mihomo/transport/mekya"
	"github.com/metacubex/mihomo/transport/mkcp"
	mihomoVMess "github.com/metacubex/mihomo/transport/vmess"
	"github.com/metacubex/mihomo/transport/xhttp"

	"github.com/metacubex/http"
	"github.com/metacubex/tls"
	"golang.org/x/exp/slices"
)

// TransportOption covers the server-side transport selection.
type TransportOption struct {
	WsPath          string
	GrpcServiceName string
	XHTTP           *LC.XHTTPConfig // nil = disabled
	MKCP            *LC.MKCPConfig  // nil = disabled
	Mekya           *LC.MekyaConfig // nil = disabled
}

// ApplyTransport wires ws/grpc/xhttp onto an *http.Server and normalizes ALPN.
// connHandler is invoked for every accepted stream conn.
// Returns wrap: a function to apply mekya around the net.Listener (nil when mekya is
// disabled), and udpWrap: a function to build the mkcp listener on top of a UDP
// PacketConn (nil when MKCP is disabled).
//
// mkcp.Listen needs a net.PacketConn, so it cannot be part of the TCP wrap chain;
// udpWrap is non-nil only when opt.MKCP is enabled, and the caller decides whether
// to open a UDP listener at all (zero-value config must not create a UDP socket).
func ApplyTransport(
	server *http.Server,
	tlsConfig *tls.Config,
	opt TransportOption,
	connHandler func(net.Conn),
) (wrap func(net.Listener) (net.Listener, error), udpWrap func(net.PacketConn) (net.Listener, error), err error) {
	if opt.WsPath != "" {
		httpMux := http.NewServeMux()
		httpMux.HandleFunc(opt.WsPath, func(w http.ResponseWriter, r *http.Request) {
			conn, err := mihomoVMess.StreamUpgradedWebsocketConn(w, r)
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			connHandler(conn)
		})
		server.Handler = httpMux
		server.Protocols.SetHTTP1(true)
		tlsConfig.NextProtos = append(tlsConfig.NextProtos, "http/1.1")
	}
	if opt.GrpcServiceName != "" {
		server.Handler = gun.NewServerHandler(gun.ServerOption{
			ServiceName: opt.GrpcServiceName,
			ConnHandler: connHandler,
			HttpHandler: server.Handler,
		})
		server.Protocols.SetHTTP2(true)
		// SetUnencryptedHTTP2 to ensure we can work in plain http2 and some tls conn is not *tls.Conn (like *reality.Conn)
		//
		// Enable HTTP/2 support unconditionally on the server.
		//
		// Note that this usage is limited to our own net/http fork
		// The standard library also needs to mask the tls.Conn type for the conn returned by the Listener.
		// see: https://github.com/golang/go/issues/79293#issuecomment-4426393534
		server.Protocols.SetUnencryptedHTTP2(true)
		tlsConfig.NextProtos = append([]string{"h2"}, tlsConfig.NextProtos...) // h2 must before http/1.1
	}
	if opt.XHTTP != nil {
		switch opt.XHTTP.Mode {
		case "", "auto", "stream-up", "stream-one", "packet-up":
		default:
			return nil, nil, errors.New("unsupported xhttp mode")
		}
		server.Handler, err = xhttp.NewServerHandler(xhttp.ServerOption{
			Config: xhttp.Config{
				Host:                 opt.XHTTP.Host,
				Path:                 opt.XHTTP.Path,
				Mode:                 opt.XHTTP.Mode,
				XPaddingBytes:        opt.XHTTP.XPaddingBytes,
				XPaddingObfsMode:     opt.XHTTP.XPaddingObfsMode,
				XPaddingKey:          opt.XHTTP.XPaddingKey,
				XPaddingHeader:       opt.XHTTP.XPaddingHeader,
				XPaddingPlacement:    opt.XHTTP.XPaddingPlacement,
				XPaddingMethod:       opt.XHTTP.XPaddingMethod,
				UplinkHTTPMethod:     opt.XHTTP.UplinkHTTPMethod,
				SessionPlacement:     opt.XHTTP.SessionPlacement,
				SessionKey:           opt.XHTTP.SessionKey,
				SeqPlacement:         opt.XHTTP.SeqPlacement,
				SeqKey:               opt.XHTTP.SeqKey,
				UplinkDataPlacement:  opt.XHTTP.UplinkDataPlacement,
				UplinkDataKey:        opt.XHTTP.UplinkDataKey,
				UplinkChunkSize:      opt.XHTTP.UplinkChunkSize,
				NoSSEHeader:          opt.XHTTP.NoSSEHeader,
				ScStreamUpServerSecs: opt.XHTTP.ScStreamUpServerSecs,
				ScMaxBufferedPosts:   opt.XHTTP.ScMaxBufferedPosts,
				ScMaxEachPostBytes:   opt.XHTTP.ScMaxEachPostBytes,
			},
			ConnHandler: connHandler,
			HttpHandler: server.Handler,
		})
		if err != nil {
			return nil, nil, err
		}
		server.Protocols.SetHTTP1(true)
		server.Protocols.SetHTTP2(true)
		// SetUnencryptedHTTP2 to ensure we can work in plain http2 and some tls conn is not *tls.Conn (like *reality.Conn)
		//
		// Enable HTTP/2 support unconditionally on the server.
		//
		// Note that this usage is limited to our own net/http fork
		// The standard library also needs to mask the tls.Conn type for the conn returned by the Listener.
		// see: https://github.com/golang/go/issues/79293#issuecomment-4426393534
		server.Protocols.SetUnencryptedHTTP2(true)
		ensureH2First(tlsConfig)
	}
	if opt.Mekya != nil {
		ensureH2First(tlsConfig)
		mekyaConfig := opt.Mekya.Build()
		wrap = func(l net.Listener) (net.Listener, error) {
			return mekya.Listen(context.Background(), l, mekyaConfig)
		}
	}
	if opt.MKCP != nil {
		mkcpConfig := opt.MKCP.Build()
		udpWrap = func(pc net.PacketConn) (net.Listener, error) {
			return mkcp.Listen(context.Background(), pc, mkcpConfig)
		}
	}
	return wrap, udpWrap, nil
}

// HasHTTPServer reports whether ApplyTransport would set server.Handler,
// i.e. whether the caller must httpServer.Serve(l) instead of running its own accept loop.
func HasHTTPServer(opt TransportOption) bool {
	return opt.WsPath != "" || opt.GrpcServiceName != "" || opt.XHTTP != nil
}

// ensureH2First makes both "h2" and "http/1.1" present in tlsConfig.NextProtos,
// with "h2" before "http/1.1".
func ensureH2First(tlsConfig *tls.Config) {
	if !slices.Contains(tlsConfig.NextProtos, "http/1.1") {
		tlsConfig.NextProtos = append([]string{"http/1.1"}, tlsConfig.NextProtos...)
	}
	if !slices.Contains(tlsConfig.NextProtos, "h2") {
		tlsConfig.NextProtos = append([]string{"h2"}, tlsConfig.NextProtos...)
	}
}
