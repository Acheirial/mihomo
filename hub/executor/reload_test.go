package executor

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/metacubex/mihomo/adapter"
	"github.com/metacubex/mihomo/adapter/outbound"
	"github.com/metacubex/mihomo/common/utils"
	"github.com/metacubex/mihomo/component/auth"
	"github.com/metacubex/mihomo/component/process"
	"github.com/metacubex/mihomo/component/resolver"
	"github.com/metacubex/mihomo/component/sniffer"
	"github.com/metacubex/mihomo/component/trie"
	"github.com/metacubex/mihomo/config"
	C "github.com/metacubex/mihomo/constant"
	P "github.com/metacubex/mihomo/constant/provider"
	"github.com/metacubex/mihomo/dns"
	"github.com/metacubex/mihomo/log"
	"github.com/metacubex/mihomo/tunnel"
)

type delayProxyProvider struct {
	name  string
	delay time.Duration
	in    *atomic.Bool
}

func (p *delayProxyProvider) Name() string               { return p.name }
func (p *delayProxyProvider) VehicleType() P.VehicleType { return P.HTTP }
func (p *delayProxyProvider) Type() P.ProviderType       { return P.Proxy }
func (p *delayProxyProvider) Update() error              { return nil }
func (p *delayProxyProvider) Proxies() []C.Proxy         { return nil }
func (p *delayProxyProvider) Count() int                 { return 0 }
func (p *delayProxyProvider) Touch()                     {}
func (p *delayProxyProvider) HealthCheck()               {}
func (p *delayProxyProvider) Version() uint32            { return 0 }
func (p *delayProxyProvider) HealthCheckURL() string     { return "" }
func (p *delayProxyProvider) RegisterHealthCheckTask(url string, expectedStatus utils.IntRanges[uint16], filter string, interval uint) {
}

func (p *delayProxyProvider) Initial() error {
	if p.in != nil {
		p.in.Store(true)
	}
	time.Sleep(p.delay)
	if p.in != nil {
		p.in.Store(false)
	}
	return nil
}

func minimalConfig() *config.Config {
	return &config.Config{
		General: &config.General{
			Inbound: config.Inbound{
				BindAddress: "*",
			},
			Mode:            tunnel.Rule,
			LogLevel:        log.SILENT,
			FindProcessMode: process.FindProcessOff,
		},
		Controller:   &config.Controller{},
		Experimental: &config.Experimental{},
		IPTables:     &config.IPTables{},
		NTP:          &config.NTP{},
		DNS:          &config.DNS{Enable: false},
		Hosts:        trie.New[resolver.HostValue](),
		Profile:      &config.Profile{},
		TLS:          &config.TLS{},
		Sniffer:      &sniffer.Config{},
		Users:        []auth.AuthUser{},
		Proxies: map[string]C.Proxy{
			"DIRECT":      adapter.NewProxy(outbound.NewDirect()),
			"REJECT":      adapter.NewProxy(outbound.NewReject()),
			"PASS":        adapter.NewProxy(outbound.NewPass()),
			"COMPATIBLE":  adapter.NewProxy(outbound.NewCompatible()),
			"REJECT-DROP": adapter.NewProxy(outbound.NewRejectDrop()),
		},
		Providers:     map[string]P.ProxyProvider{},
		RuleProviders: map[string]P.RuleProvider{},
		Listeners:     map[string]C.InboundListener{},
	}
}

func TestApplyConfigSlowProviderDoesNotHoldMux(t *testing.T) {
	inInitial := &atomic.Bool{}
	cfg := minimalConfig()
	cfg.Providers = map[string]P.ProxyProvider{
		"slow": &delayProxyProvider{name: "slow", delay: 200 * time.Millisecond, in: inInitial},
	}

	done := make(chan struct{})
	go func() {
		ApplyConfig(cfg, false)
		close(done)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for !inInitial.Load() {
		if time.Now().After(deadline) {
			t.Fatal("provider Initial never started")
		}
		time.Sleep(time.Millisecond)
	}

	second := make(chan struct{})
	go func() {
		mux.Lock()
		mux.Unlock()
		close(second)
	}()

	select {
	case <-second:
	case <-time.After(50 * time.Millisecond):
		t.Fatal("mux still held during provider Initial")
	}

	<-done
	if tunnel.Status() != tunnel.Running {
		t.Fatalf("status=%s want running", tunnel.Status())
	}
}

func TestApplyConfigSerializesAcrossProviderIO(t *testing.T) {
	var concurrent atomic.Int32
	var maxConcurrent atomic.Int32
	cfg1 := minimalConfig()
	cfg1.Providers = map[string]P.ProxyProvider{
		"slow": &delayProxyProvider{name: "slow1", delay: 80 * time.Millisecond, in: &atomic.Bool{}},
	}
	cfg2 := minimalConfig()
	cfg2.Providers = map[string]P.ProxyProvider{
		"slow": &delayProxyProvider{name: "slow2", delay: 80 * time.Millisecond, in: &atomic.Bool{}},
	}

	wrap := func(p P.ProxyProvider) P.ProxyProvider {
		return &countingProvider{ProxyProvider: p, concurrent: &concurrent, max: &maxConcurrent}
	}
	cfg1.Providers["slow"] = wrap(cfg1.Providers["slow"])
	cfg2.Providers["slow"] = wrap(cfg2.Providers["slow"])

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		ApplyConfig(cfg1, false)
	}()
	go func() {
		defer wg.Done()
		ApplyConfig(cfg2, false)
	}()
	wg.Wait()
	if maxConcurrent.Load() > 1 {
		t.Fatalf("two ApplyConfig overlapped during Initial: max=%d", maxConcurrent.Load())
	}
}

type countingProvider struct {
	P.ProxyProvider
	concurrent *atomic.Int32
	max        *atomic.Int32
}

func (p *countingProvider) Initial() error {
	n := p.concurrent.Add(1)
	for {
		cur := p.max.Load()
		if n <= cur || p.max.CompareAndSwap(cur, n) {
			break
		}
	}
	defer p.concurrent.Add(-1)
	return p.ProxyProvider.Initial()
}

func TestApplyConfigRulesOnlyDoesNotSuspend(t *testing.T) {
	cfg := minimalConfig()
	ApplyConfig(cfg, false)
	if tunnel.Status() != tunnel.Running {
		t.Fatalf("after first apply status=%s", tunnel.Status())
	}

	var sawSuspend atomic.Bool
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				if tunnel.Status() == tunnel.Suspend {
					sawSuspend.Store(true)
					return
				}
				time.Sleep(time.Millisecond)
			}
		}
	}()

	cfg2 := minimalConfig()
	ApplyConfig(cfg2, false)
	close(stop)
	wg.Wait()
	if sawSuspend.Load() {
		t.Fatal("rules/proxies-only force=false ApplyConfig entered Suspend")
	}
	if tunnel.Status() != tunnel.Running {
		t.Fatalf("status=%s want running", tunnel.Status())
	}
}

func TestDNSConfigUnchangedSkipsRebuild(t *testing.T) {
	c := &config.DNS{
		Enable:            true,
		NameServer:        []dns.NameServer{{Net: "udp", Addr: "1.1.1.1:53"}},
		DefaultNameserver: []dns.NameServer{{Net: "udp", Addr: "8.8.8.8:53"}},
		Listen:            "",
	}
	lastDNS = c
	lastDNSIPv6 = true
	if !dnsConfigUnchanged(c, true) {
		t.Fatal("identical DNS config should skip rebuild")
	}
	changed := *c
	changed.Listen = "127.0.0.1:5353"
	if dnsConfigUnchanged(&changed, true) {
		t.Fatal("listen change must rebuild")
	}
}
