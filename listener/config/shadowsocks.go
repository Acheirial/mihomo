package config

import (
	"github.com/metacubex/mihomo/listener/sing"

	"encoding/json"
)

type ShadowsocksServer struct {
	Enable     bool
	Listen     string
	Password   string
	Cipher     string
	Udp        bool
	MuxOption  sing.MuxOption `yaml:"mux-option" json:"mux-option,omitempty"`
	ShadowTLS  ShadowTLS      `yaml:"shadow-tls" json:"shadow-tls,omitempty"`
	ResTLS     ResTLS         `yaml:"res-tls" json:"res-tls,omitempty"`
	JLSConfig  JLSConfig      `yaml:"jls-config" json:"jls-config,omitempty"`
	KcpTun     KcpTun         `yaml:"kcp-tun" json:"kcp-tun,omitempty"`
	SimpleObfs SimpleObfs     `yaml:"simple-obfs" json:"simple-obfs,omitempty"`
	// mkcp/mekya 不支持与 shadow-tls/res-tls/jls 同时使用
	MekyaConfig MekyaConfig `yaml:"mekya-config" json:"mekya-config,omitempty"`
	// mkcp/mekya 不支持与 shadow-tls/res-tls/jls 同时使用
	MKCPConfig MKCPConfig `yaml:"mkcp-config" json:"mkcp-config,omitempty"`
}

type SimpleObfs struct {
	Enable bool
	Mode   string
}

func (t ShadowsocksServer) String() string {
	b, _ := json.Marshal(t)
	return string(b)
}
