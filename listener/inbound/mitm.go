package inbound

import LC "github.com/metacubex/mihomo/listener/config"

type Mitm struct {
	Enable        bool     `inbound:"enable,omitempty"`
	Hosts         []string `inbound:"hosts,omitempty"`
	CACertificate string   `inbound:"ca-certificate,omitempty"`
	CAPrivateKey  string   `inbound:"ca-private-key,omitempty"`
	StoreCA       bool     `inbound:"store-ca,omitempty"`
}

func (m Mitm) Build() *LC.Mitm {
	if !m.Enable {
		return nil
	}
	return &LC.Mitm{
		Enable:        m.Enable,
		Hosts:         m.Hosts,
		CACertificate: m.CACertificate,
		CAPrivateKey:  m.CAPrivateKey,
		StoreCA:       m.StoreCA,
	}
}
