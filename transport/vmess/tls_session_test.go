package vmess

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"testing"
	"time"

	tlsC "github.com/metacubex/mihomo/component/tls"

	"github.com/metacubex/tls"
)

func TestStreamTLSConnResumesWithPerConfigCache(t *testing.T) {
	certPEM, keyPEM := testSelfSignedCert(t)
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}

	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{cert},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			tlsConn := c.(*tls.Conn)
			_ = tlsConn.Handshake()
			_ = tlsConn.Close()
		}
	}()

	cache := tlsC.NewSharedClientSessionCache(64)
	cfg := &TLSConfig{
		Host:           "localhost",
		SkipCertVerify: true,
		SessionCache:   cache,
	}

	dial := func() *tls.Conn {
		raw, err := net.Dial("tcp", ln.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		c, err := StreamTLSConn(context.Background(), raw, cfg)
		if err != nil {
			t.Fatal(err)
		}
		return c.(*tls.Conn)
	}

	first := dial()
	if first.ConnectionState().DidResume {
		t.Fatal("first handshake resumed")
	}
	// TLS 1.3 stores the session ticket after Handshake returns, on the
	// first post-handshake NewSessionTicket read.
	first.SetReadDeadline(time.Now().Add(time.Second))
	_, _ = first.Read(make([]byte, 1))
	_ = first.Close()

	if _, ok := cache.TLS().Get("localhost"); !ok {
		t.Fatal("session cache empty after first handshake")
	}

	second := dial()
	if !second.ConnectionState().DidResume {
		t.Fatal("second handshake did not resume session ticket")
	}
	_ = second.Close()
	_ = ln.Close()
	<-done
}

func testSelfSignedCert(t *testing.T) (certPEM, keyPEM []byte) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	return
}
