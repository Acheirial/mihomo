package config

import "encoding/json"

type SnellServer struct {
	Listen    string
	Psk       string
	Version   int
	UDP       bool
	ObfsMode  string
	ObfsHost  string
	ShadowTLS ShadowTLS `yaml:"shadow-tls" json:"shadow-tls,omitempty"`
	ResTLS    ResTLS    `yaml:"res-tls" json:"res-tls,omitempty"`
	JLSConfig JLSConfig `yaml:"jls-config" json:"jls-config,omitempty"`
	// mkcp/mekya 不支持与 shadow-tls/res-tls/jls 同时使用
	MekyaConfig MekyaConfig `yaml:"mekya-config" json:"mekya-config,omitempty"`
	// mkcp/mekya 不支持与 shadow-tls/res-tls/jls 同时使用
	MKCPConfig MKCPConfig `yaml:"mkcp-config" json:"mkcp-config,omitempty"`
}

func (c SnellServer) String() string {
	b, _ := json.Marshal(c)
	return string(b)
}
