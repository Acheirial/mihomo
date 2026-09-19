package outboundgroup

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"

	"github.com/metacubex/mihomo/common/utils"
	C "github.com/metacubex/mihomo/constant"
	P "github.com/metacubex/mihomo/constant/provider"
)

type stubProxy struct {
	name string
}

func (p *stubProxy) Name() string           { return p.name }
func (p *stubProxy) Type() C.AdapterType    { return C.Direct }
func (p *stubProxy) Addr() string           { return "" }
func (p *stubProxy) SupportUDP() bool       { return true }
func (p *stubProxy) ProxyInfo() C.ProxyInfo { return C.ProxyInfo{} }
func (p *stubProxy) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]string{"name": p.name})
}
func (p *stubProxy) DialContext(context.Context, *C.Metadata) (C.Conn, error) {
	return nil, C.ErrNotSupport
}
func (p *stubProxy) ListenPacketContext(context.Context, *C.Metadata) (C.PacketConn, error) {
	return nil, C.ErrNotSupport
}
func (p *stubProxy) SupportUOT() bool                             { return false }
func (p *stubProxy) IsL3Protocol(*C.Metadata) bool                { return false }
func (p *stubProxy) Unwrap(*C.Metadata, bool) C.Proxy             { return nil }
func (p *stubProxy) Close() error                                 { return nil }
func (p *stubProxy) Adapter() C.ProxyAdapter                      { return p }
func (p *stubProxy) AliveForTestUrl(string) bool                  { return true }
func (p *stubProxy) DelayHistory() []C.DelayHistory               { return nil }
func (p *stubProxy) ExtraDelayHistories() map[string]C.ProxyState { return nil }
func (p *stubProxy) LastDelayForTestUrl(string) uint16            { return 1 }
func (p *stubProxy) URLTest(context.Context, string, utils.IntRanges[uint16]) (uint16, error) {
	return 1, nil
}

type staticProvider struct {
	name    string
	proxies []C.Proxy
	version atomic.Uint32
	touches atomic.Int32
}

func (p *staticProvider) Name() string               { return p.name }
func (p *staticProvider) VehicleType() P.VehicleType { return P.Compatible }
func (p *staticProvider) Type() P.ProviderType       { return P.Proxy }
func (p *staticProvider) Initial() error             { return nil }
func (p *staticProvider) Update() error              { return nil }
func (p *staticProvider) Proxies() []C.Proxy         { return p.proxies }
func (p *staticProvider) Count() int                 { return len(p.proxies) }
func (p *staticProvider) Touch()                     { p.touches.Add(1) }
func (p *staticProvider) HealthCheck()               {}
func (p *staticProvider) Version() uint32            { return p.version.Load() }
func (p *staticProvider) RegisterHealthCheckTask(string, utils.IntRanges[uint16], string, uint) {
}
func (p *staticProvider) HealthCheckURL() string { return "" }

func namedProxy(name string) C.Proxy { return &stubProxy{name: name} }

func TestSelectorCachesSelectedPointer(t *testing.T) {
	a := namedProxy("a")
	b := namedProxy("b")
	pd := &staticProvider{name: "p", proxies: []C.Proxy{a, b}}
	sel, err := NewSelector(GroupCommonOption{Name: "sel"}, SelectorOption{DefaultSelected: "a"}, namedProxy("COMPATIBLE"), []P.ProxyProvider{pd})
	if err != nil {
		t.Fatal(err)
	}
	got := sel.Unwrap(nil, false)
	if got != a {
		t.Fatalf("first unwrap want a, got %s", got.Name())
	}
	if sel.selectedProxy != a {
		t.Fatal("selected pointer not cached")
	}
	got2 := sel.Unwrap(nil, false)
	if got2 != a {
		t.Fatalf("cached unwrap want a, got %s", got2.Name())
	}
	if pd.touches.Load() != 0 {
		t.Fatalf("false-touch unwrap should not Touch, got %d", pd.touches.Load())
	}

	if err := sel.Set("b"); err != nil {
		t.Fatal(err)
	}
	got3 := sel.Unwrap(nil, false)
	if got3 != b {
		t.Fatalf("after Set want b, got %s", got3.Name())
	}
	sel.ForceSet("a")
	got4 := sel.Unwrap(nil, false)
	if got4 != a {
		t.Fatalf("after ForceSet want a, got %s", got4.Name())
	}

	pd.version.Add(1)
	got5 := sel.Unwrap(nil, false)
	if got5.Name() != "a" {
		t.Fatalf("after version bump still want a, got %s", got5.Name())
	}
}

func TestURLTestFastInvalidatesOnVersion(t *testing.T) {
	a := namedProxy("a")
	b := namedProxy("b")
	pd := &staticProvider{name: "p", proxies: []C.Proxy{a, b}}
	u, err := NewURLTest(GroupCommonOption{Name: "ut", URL: C.DefaultTestURL}, URLTestOption{}, namedProxy("COMPATIBLE"), []P.ProxyProvider{pd})
	if err != nil {
		t.Fatal(err)
	}
	first := u.Unwrap(nil, false)
	if first == nil {
		t.Fatal("fast returned nil")
	}
	u.Unwrap(nil, true)
	if pd.touches.Load() == 0 {
		t.Fatal("touch=true should Touch providers")
	}
	before := pd.touches.Load()
	u.Unwrap(nil, false)
	if pd.touches.Load() != before {
		t.Fatal("touch=false must not Touch")
	}

	pd.proxies = []C.Proxy{b}
	pd.version.Add(1)
	got := u.Unwrap(nil, false)
	if got.Name() != "b" {
		t.Fatalf("after provider change want b, got %s", got.Name())
	}
}

func TestGetProxiesCacheHitUsesRLock(t *testing.T) {
	a := namedProxy("a")
	pd := &staticProvider{name: "p", proxies: []C.Proxy{a}}
	gb := NewGroupBase(GroupBaseOption{
		Name:          "g",
		Type:          C.Selector,
		EmptyFallback: namedProxy("COMPATIBLE"),
		Providers:     []P.ProxyProvider{pd},
	})
	first := gb.GetProxies(false)
	if len(first) != 1 || first[0] != a {
		t.Fatalf("first GetProxies: %+v", first)
	}
	second := gb.GetProxies(false)
	if len(second) != 1 || second[0] != a {
		t.Fatalf("cached GetProxies: %+v", second)
	}
	gb.GetProxies(true)
	if pd.touches.Load() == 0 {
		t.Fatal("GetProxies(true) should Touch")
	}
}
