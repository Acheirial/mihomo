// Package security composes server-side security layers (TLS/ShadowTLS/Restls/JLS/Reality/TLSMirror)
// and transports (ws/grpc/xhttp/mkcp/mekya) for inbound listeners.
//
// It is the inbound counterpart of adapter/outbound.StreamStack: the shared TLS build,
// the mutual-exclusion check for security modes, the security-builder listener wrap and
// the http.Server transport wiring that every leaf listener used to duplicate.
package security

import (
	"errors"
	"net"
	"strings"

	"github.com/metacubex/mihomo/component/ca"
	"github.com/metacubex/mihomo/component/ech"
	"github.com/metacubex/mihomo/listener/jls"
	"github.com/metacubex/mihomo/listener/reality"
	"github.com/metacubex/mihomo/listener/restls"
	"github.com/metacubex/mihomo/listener/shadowtls"
	"github.com/metacubex/mihomo/listener/tlsmirror"
	"github.com/metacubex/mihomo/ntp"

	"github.com/metacubex/tls"
)

// TLSOption is the shared server-side TLS field face.
type TLSOption struct {
	Certificate    string
	PrivateKey     string
	ClientAuthType string
	ClientAuthCert string
	EchKey         string
}

// BuildTLS builds a server-side *tls.Config from the shared fields.
// certRequired=true forces Certificate/PrivateKey non-empty (QUIC variants).
// Returns a config whose GetCertificate closure is set only when a cert pair is given.
func BuildTLS(opt TLSOption, certRequired bool) (*tls.Config, error) {
	tlsConfig := &tls.Config{Time: ntp.Now}

	if opt.Certificate != "" && opt.PrivateKey != "" {
		certLoader, err := ca.NewTLSKeyPairLoader(opt.Certificate, opt.PrivateKey)
		if err != nil {
			return nil, err
		}
		tlsConfig.GetCertificate = func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
			return certLoader()
		}

		if opt.EchKey != "" {
			err = ech.LoadECHKey(opt.EchKey, tlsConfig)
			if err != nil {
				return nil, err
			}
		}
	}
	if certRequired && tlsConfig.GetCertificate == nil {
		return nil, errors.New("certificate and private-key are required")
	}
	tlsConfig.ClientAuth = ca.ClientAuthTypeFromString(opt.ClientAuthType)
	if len(opt.ClientAuthCert) > 0 {
		if tlsConfig.ClientAuth == tls.NoClientCert {
			tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
		}
	}
	if tlsConfig.ClientAuth == tls.VerifyClientCertIfGiven || tlsConfig.ClientAuth == tls.RequireAndVerifyClientCert {
		pool, err := ca.LoadCertificates(opt.ClientAuthCert)
		if err != nil {
			return nil, err
		}
		tlsConfig.ClientCAs = pool
	}
	return tlsConfig, nil
}

// HasCertificate reports whether BuildTLS would set GetCertificate for this option,
// i.e. whether a certificate/private-key pair is present.
func HasCertificate(opt TLSOption) bool {
	return opt.Certificate != "" && opt.PrivateKey != ""
}

// Modes returns the enabled security-mode names for mutual-exclusion checks.
// includeCertificate controls whether a loaded cert pair counts as a mode
// (anytls/sing_vmess count it; socks/http/mixed do not — they use reality instead).
func Modes(includeCertificate, shadowTLS, restls, jls, reality, tlsMirror bool) []string {
	modes := make([]string, 0, 6)
	if includeCertificate {
		modes = append(modes, "certificate")
	}
	if shadowTLS {
		modes = append(modes, "shadow-tls")
	}
	if restls {
		modes = append(modes, "res-tls")
	}
	if jls {
		modes = append(modes, "jls")
	}
	if reality {
		modes = append(modes, "reality")
	}
	if tlsMirror {
		modes = append(modes, "tlsmirror")
	}
	return modes
}

// CheckExclusive reports the exact existing error string; do NOT change the wording.
func CheckExclusive(modes []string) error {
	if len(modes) > 1 {
		return errors.New("security modes are mutually exclusive: " + strings.Join(modes, ", "))
	}
	return nil
}

// Builders holds optional security layer constructors.
type Builders struct {
	ShadowTLS *shadowtls.Builder
	RestLS    *restls.Builder
	JLS       *jls.Builder
	Reality   *reality.Builder
	TLSMirror *tlsmirror.Builder
}

// WrapListener applies exactly one security layer in the existing precedence order:
// shadowTLS > restls > jls > reality > tlsMirror > tls > none.
// When none is set and tlsConfig has no GetCertificate, returns the given listener unchanged.
func WrapListener(l net.Listener, b Builders, tlsConfig *tls.Config) net.Listener {
	if b.ShadowTLS != nil {
		return b.ShadowTLS.NewListener(l)
	} else if b.RestLS != nil {
		return b.RestLS.NewListener(l)
	} else if b.JLS != nil {
		return b.JLS.NewListener(l)
	} else if b.Reality != nil {
		return b.Reality.NewListener(l)
	} else if b.TLSMirror != nil {
		return b.TLSMirror.NewListener(l)
	} else if tlsConfig != nil && tlsConfig.GetCertificate != nil {
		return tls.NewListener(l, tlsConfig)
	}
	return l
}

// HasSecurityLayer reports whether WrapListener would apply any security layer
// (including plain TLS when a certificate pair is loaded).
func HasSecurityLayer(b Builders, tlsConfig *tls.Config) bool {
	return b.ShadowTLS != nil || b.RestLS != nil || b.JLS != nil || b.Reality != nil || b.TLSMirror != nil ||
		(tlsConfig != nil && tlsConfig.GetCertificate != nil)
}
