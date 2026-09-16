package config

import (
	"github.com/metacubex/mihomo/component/auth"
	"github.com/metacubex/mihomo/listener/reality"
)

// AuthServer for http/socks/mixed server
type AuthServer struct {
	Enable         bool
	Listen         string
	AuthStore      auth.AuthStore
	Certificate    string
	PrivateKey     string
	ClientAuthType string
	ClientAuthCert string
	EchKey         string
	Mitm           *Mitm
	RealityConfig  reality.Config
}

// Mitm for http/mixed server TLS interception
type Mitm struct {
	Enable        bool
	Hosts         []string
	CACertificate string
	CAPrivateKey  string
	StoreCA       bool
}
