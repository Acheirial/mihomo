package mitm

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	N "github.com/metacubex/mihomo/common/net"
	"github.com/metacubex/mihomo/common/utils"
	"github.com/metacubex/mihomo/component/sniffer"
	C "github.com/metacubex/mihomo/constant"
)

const (
	// maxHelloSize bounds the read-ahead used while extracting the
	// ClientHello, mirroring the TCP sniffer's per-connection budget.
	maxHelloSize = 64 * 1024

	// sniTimeout limits the wait for the complete ClientHello.
	sniTimeout = time.Second

	// handshakeTimeout limits the server-side TLS handshake.
	handshakeTimeout = 5 * time.Second

	// certExpiryMargin is how long before expiry a cached certificate is
	// considered stale, following the Xray-core issuance cache.
	certExpiryMargin = 2 * time.Minute
)

// defaultPorts is the port gate applied when Config.Ports is empty.
var defaultPorts = utils.IntRanges[uint16]{utils.NewRange[uint16](443, 443)}

// Config is the MITM interception configuration.
type Config struct {
	Enable     bool
	CA         string // PEM path or inline; empty = ephemeral self-generated CA (in-memory)
	CAKey      string // PEM path or inline; required if CA is a file and has separate key
	SkipDomain []C.DomainMatcher
	Ports      utils.IntRanges[uint16]
}

// Intercepter terminates client TLS on selected connections, presenting
// dynamically issued certificates signed by a local CA.
type Intercepter struct {
	enable     bool
	ports      utils.IntRanges[uint16]
	skipDomain []C.DomainMatcher
	ca         *caKey

	certMu  sync.RWMutex
	certs   map[string]*tls.Certificate
	issueMu sync.Mutex
}

// New creates an Intercepter from cfg. When cfg.Enable is false, the
// returned Intercepter is inert and Enable reports false.
func New(cfg Config) (*Intercepter, error) {
	i := &Intercepter{
		enable:     cfg.Enable,
		ports:      cfg.Ports,
		skipDomain: cfg.SkipDomain,
		certs:      map[string]*tls.Certificate{},
	}
	if !cfg.Enable {
		return i, nil
	}
	ca, err := newCA(cfg.CA, cfg.CAKey)
	if err != nil {
		return nil, fmt.Errorf("initialize mitm ca: %w", err)
	}
	i.ca = ca
	return i, nil
}

// Enable reports whether interception is active; false if i is nil.
func (i *Intercepter) Enable() bool {
	return i != nil && i.enable
}

// ShouldIntercept reports whether the connection described by metadata
// should be intercepted. An empty metadata.Host is allowed through: the
// real decision is made later from the ClientHello SNI.
func (i *Intercepter) ShouldIntercept(metadata *C.Metadata) bool {
	if !i.Enable() || metadata == nil {
		return false
	}
	ports := i.ports
	if len(ports) == 0 {
		ports = defaultPorts
	}
	if !ports.Check(metadata.DstPort) {
		return false
	}
	if metadata.Host != "" {
		for _, matcher := range i.skipDomain {
			if matcher.MatchDomain(metadata.Host) {
				return false
			}
		}
	}
	return true
}

// Intercept takes a connection whose ClientHello is (partially) buffered,
// extracts the SNI and completes a server-side TLS handshake, returning the
// decrypted connection. The handshake consumes the buffered ClientHello, so
// it never leaks into the decrypted stream.
//
// On any error before the handshake, the buffer is untouched (Peek never
// advances) and the caller may continue with the original connection
// un-intercepted. Once the handshake itself failed, the ClientHello bytes
// were consumed and the client has seen a TLS alert; closing the connection
// is left to the caller.
func (i *Intercepter) Intercept(conn *N.BufferedConn, metadata *C.Metadata) (net.Conn, error) {
	if !i.Enable() || i.ca == nil {
		return nil, errors.New("mitm not enabled")
	}
	if conn == nil {
		return nil, errors.New("nil connection")
	}

	sni, err := peekSNI(conn)
	if err != nil {
		return nil, err
	}
	if metadata != nil && metadata.Host == "" {
		metadata.Host = sni
	}

	tlsConn := tls.Server(conn, &tls.Config{GetCertificate: i.getCertificate})
	_ = tlsConn.SetDeadline(time.Now().Add(handshakeTimeout))
	err = tlsConn.Handshake()
	_ = tlsConn.SetDeadline(time.Time{})
	if err != nil {
		return nil, fmt.Errorf("tls handshake with %s: %w", sni, err)
	}
	return tlsConn, nil
}

// getCertificate returns the cached certificate for the SNI or issues a
// fresh one. The issue path is globally serialized: only one goroutine
// issues at a time, trading throughput for correctness.
func (i *Intercepter) getCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	host := hello.ServerName
	if host == "" {
		if addr, ok := hello.Conn.RemoteAddr().(*net.TCPAddr); ok {
			host = addr.IP.String()
		}
	}
	if host == "" {
		return nil, errors.New("no server name for certificate")
	}

	if cert, ok := i.lookupCert(host); ok {
		return cert, nil
	}

	i.issueMu.Lock()
	defer i.issueMu.Unlock()

	// Another goroutine may have issued the certificate while this one
	// waited for the issue lock.
	if cert, ok := i.lookupCert(host); ok {
		return cert, nil
	}

	cert, err := issueLeaf(i.ca, host)
	if err != nil {
		return nil, err
	}
	i.pruneAndStore(host, cert)
	return cert, nil
}

// lookupCert returns a cached certificate that is not about to expire.
func (i *Intercepter) lookupCert(host string) (*tls.Certificate, bool) {
	i.certMu.RLock()
	defer i.certMu.RUnlock()
	cert, found := i.certs[host]
	if !found || cert.Leaf == nil || !cert.Leaf.NotAfter.After(time.Now().Add(certExpiryMargin)) {
		return nil, false
	}
	return cert, true
}

// pruneAndStore stores cert for host and drops expired entries.
func (i *Intercepter) pruneAndStore(host string, cert *tls.Certificate) {
	expiry := time.Now().Add(certExpiryMargin)
	i.certMu.Lock()
	defer i.certMu.Unlock()
	for h, c := range i.certs {
		if c.Leaf != nil && c.Leaf.NotAfter.Before(expiry) {
			delete(i.certs, h)
		}
	}
	i.certs[host] = cert
}

// peekSNI extracts the SNI from the buffered ClientHello, growing the read
// buffer as data arrives within a one second budget. Peek never advances the
// reader, so the buffered bytes stay available to a later handshake or a
// plain passthrough.
func peekSNI(conn *N.BufferedConn) (string, error) {
	deadline := time.Now().Add(sniTimeout)
	want := conn.Buffered()
	if want == 0 {
		want = 1
	}
	for {
		if want > maxHelloSize && want > conn.Buffered() {
			return "", fmt.Errorf("client hello too large: %d bytes", want)
		}
		conn.Grow(want)

		_ = conn.SetReadDeadline(deadline)
		_, err := conn.Peek(want)
		_ = conn.SetReadDeadline(time.Time{})
		if err != nil {
			return "", fmt.Errorf("peek client hello: %w", err)
		}
		data, _ := conn.Peek(conn.Buffered())

		domain, err := sniffer.SniffTLS(data)
		if err == nil {
			return *domain, nil
		}
		// Only a "need more data" report (wrapped ErrNoClue) is retryable;
		// anything else is a permanent not-TLS verdict.
		if !errors.Is(err, sniffer.ErrNoClue) {
			return "", fmt.Errorf("not tls or no sni: %w", err)
		}
		want = neededLength(err, len(data)+1)
	}
}

// neededLength recovers the total length requested by a
// sniffer.errNeedAtLeastData error from its rendered message, falling back
// to minLength when the message does not carry one.
func neededLength(err error, minLength int) int {
	msg := err.Error()
	idx := strings.LastIndexByte(msg, ':')
	if idx >= 0 {
		if n, perr := strconv.Atoi(strings.TrimSpace(msg[idx+1:])); perr == nil && n > minLength {
			return n
		}
	}
	return minLength
}
