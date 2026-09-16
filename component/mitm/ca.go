package mitm

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/netip"
	"os"
	"strings"
	"time"

	C "github.com/metacubex/mihomo/constant"
)

// serialNumberLimit is the maximum value of a certificate serial number,
// as mandated by RFC 5280 section 4.1.2.2.
var serialNumberLimit = new(big.Int).Lsh(big.NewInt(1), 128)

// caKey holds a certificate authority: the parsed certificate plus the
// signer used to issue leaves beneath it.
type caKey struct {
	cert x509.Certificate
	key  crypto.Signer
}

// newCA builds a certificate authority from PEM encoded certificate and key
// material, each either inline PEM or a file path. When both are empty, an
// ephemeral self-signed CA is generated in memory.
func newCA(certPEM, keyPEM string) (*caKey, error) {
	if certPEM == "" && keyPEM == "" {
		return generateCA()
	}

	certData, err := loadPEM(certPEM)
	if err != nil {
		return nil, fmt.Errorf("load ca certificate: %w", err)
	}
	if keyPEM == "" {
		// Allow a combined file holding both the certificate and the key.
		keyPEM = certPEM
	}
	keyData, err := loadPEM(keyPEM)
	if err != nil {
		return nil, fmt.Errorf("load ca key: %w", err)
	}

	block, _ := pem.Decode(certData)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, errors.New("invalid ca certificate pem")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse ca certificate: %w", err)
	}
	if !cert.IsCA {
		return nil, errors.New("certificate is not a ca")
	}
	key, err := parseSigner(keyData)
	if err != nil {
		return nil, fmt.Errorf("parse ca private key: %w", err)
	}
	return &caKey{cert: *cert, key: key}, nil
}

// loadPEM returns the PEM data for an inline value or reads it from a path.
func loadPEM(s string) ([]byte, error) {
	if strings.Contains(s, "-----BEGIN") {
		return []byte(s), nil
	}
	path := C.Path.Resolve(s)
	if !C.Path.IsSafePath(path) {
		return nil, C.Path.ErrNotSafePath(path)
	}
	return os.ReadFile(path)
}

// parseSigner extracts the first supported private key from PEM data.
func parseSigner(pemData []byte) (crypto.Signer, error) {
	var lastErr error = errors.New("no private key found")
	rest := pemData
	for {
		block, rest2 := pem.Decode(rest)
		if block == nil {
			break
		}
		rest = rest2

		var key any
		var err error
		switch block.Type {
		case "PRIVATE KEY":
			key, err = x509.ParsePKCS8PrivateKey(block.Bytes)
		case "EC PRIVATE KEY":
			key, err = x509.ParseECPrivateKey(block.Bytes)
		case "RSA PRIVATE KEY":
			key, err = x509.ParsePKCS1PrivateKey(block.Bytes)
		default:
			continue
		}
		if err != nil {
			lastErr = err
			continue
		}
		if signer, ok := key.(crypto.Signer); ok {
			return signer, nil
		}
	}
	return nil, lastErr
}

// generateCA creates an ephemeral self-signed ECDSA P-256 certificate
// authority valid for ten years.
func generateCA() (*caKey, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate ca key: %w", err)
	}
	serial, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return nil, fmt.Errorf("generate ca serial number: %w", err)
	}

	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "mihomo-mitm-ca", Organization: []string{"mihomo"}},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            0,
		MaxPathLenZero:        true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, key.Public(), key)
	if err != nil {
		return nil, fmt.Errorf("create ca certificate: %w", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, fmt.Errorf("parse ca certificate: %w", err)
	}
	return &caKey{cert: *cert, key: key}, nil
}

// issueLeaf issues a short-lived ECDSA P-256 leaf certificate for host,
// signed by ca. The returned chain starts with the leaf and is followed by
// the CA certificate.
func issueLeaf(ca *caKey, host string) (*tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate leaf key for %s: %w", host, err)
	}
	serial, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return nil, fmt.Errorf("generate leaf serial number for %s: %w", host, err)
	}

	notBefore := time.Now().Add(-time.Hour)
	notAfter := time.Now().AddDate(0, 0, 30)
	if ca.cert.NotAfter.Before(notAfter) {
		notAfter = ca.cert.NotAfter
	}
	if ca.cert.NotBefore.After(notBefore) {
		notBefore = ca.cert.NotBefore
	}

	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: host},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{host},
	}
	if ip, parseErr := netip.ParseAddr(host); parseErr == nil {
		template.DNSNames = nil
		template.IPAddresses = []net.IP{net.IP(ip.Unmap().AsSlice())}
	}

	der, err := x509.CreateCertificate(rand.Reader, template, &ca.cert, key.Public(), ca.key)
	if err != nil {
		return nil, fmt.Errorf("create certificate for %s: %w", host, err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, fmt.Errorf("parse certificate for %s: %w", host, err)
	}
	return &tls.Certificate{
		Certificate: [][]byte{der, ca.cert.Raw},
		PrivateKey:  key,
		Leaf:        leaf,
	}, nil
}
