package vmess

import (
	"context"
	"io"
	"net"
	"sync/atomic"
	"testing"

	"github.com/metacubex/http"
	"github.com/metacubex/tls"
)

func TestH2PoolReusesOneTCPConn(t *testing.T) {
	certPEM, keyPEM := testSelfSignedCert(t)
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	serverTLS := &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"h2"},
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	var accepts atomic.Int32
	srv := &http.Server{
		TLSConfig: serverTLS,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(200)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			_, _ = io.Copy(io.Discard, r.Body)
		}),
		ConnState: func(c net.Conn, st http.ConnState) {
			if st == http.StateNew {
				accepts.Add(1)
			}
		},
	}
	go srv.Serve(tls.NewListener(ln, serverTLS))
	defer srv.Close()

	var dials atomic.Int32
	rt := NewH2Transport(func(ctx context.Context) (net.Conn, error) {
		dials.Add(1)
		var d net.Dialer
		raw, err := d.DialContext(ctx, "tcp", ln.Addr().String())
		if err != nil {
			return nil, err
		}
		conn := tls.Client(raw, &tls.Config{
			InsecureSkipVerify: true,
			NextProtos:         []string{"h2"},
			ServerName:         "localhost",
		})
		if err := conn.HandshakeContext(ctx); err != nil {
			_ = raw.Close()
			return nil, err
		}
		return conn, nil
	})
	defer rt.CloseIdleConnections()

	cfg := &H2Config{Hosts: []string{"www.example.com"}, Path: "/"}
	c1, err := StreamH2Conn(context.Background(), rt, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c1.Write([]byte("one")); err != nil {
		t.Fatalf("first write: %v", err)
	}
	c2, err := StreamH2Conn(context.Background(), rt, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c2.Write([]byte("two")); err != nil {
		t.Fatalf("second write: %v", err)
	}
	_ = c1.Close()
	_ = c2.Close()

	if n := dials.Load(); n != 1 {
		t.Fatalf("dials=%d want 1", n)
	}
	if n := accepts.Load(); n != 1 {
		t.Fatalf("accepts=%d want 1", n)
	}
}
