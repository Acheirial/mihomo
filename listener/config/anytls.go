package config

import (
	"encoding/json"
)

type AnyTLSServer struct {
	Enable         bool              `yaml:"enable" json:"enable"`
	Listen         string            `yaml:"listen" json:"listen"`
	Users          map[string]string `yaml:"users" json:"users,omitempty"`
	Certificate    string            `yaml:"certificate,omitempty" json:"certificate,omitempty"`
	PrivateKey     string            `yaml:"private-key,omitempty" json:"private-key,omitempty"`
	ClientAuthType string            `yaml:"client-auth-type" json:"client-auth-type,omitempty"`
	ClientAuthCert string            `yaml:"client-auth-cert" json:"client-auth-cert,omitempty"`
	EchKey         string            `yaml:"ech-key" json:"ech-key,omitempty"`
	ShadowTLS      ShadowTLS         `yaml:"shadow-tls" json:"shadow-tls,omitempty"`
	ResTLS         ResTLS            `yaml:"res-tls" json:"res-tls,omitempty"`
	JLSConfig      JLSConfig         `yaml:"jls-config" json:"jls-config,omitempty"`
	AllowInsecure  bool              `yaml:"allow-insecure" json:"allow-insecure,omitempty"`
	PaddingScheme  string            `yaml:"padding-scheme" json:"padding-scheme,omitempty"`
	// mkcp/mekya 不支持与 shadow-tls/res-tls/jls 同时使用
	MekyaConfig MekyaConfig `yaml:"mekya-config" json:"mekya-config,omitempty"`
	// mkcp/mekya 不支持与 shadow-tls/res-tls/jls 同时使用
	MKCPConfig MKCPConfig `yaml:"mkcp-config" json:"mkcp-config,omitempty"`
}

func (t AnyTLSServer) String() string {
	b, _ := json.Marshal(t)
	return string(b)
}
