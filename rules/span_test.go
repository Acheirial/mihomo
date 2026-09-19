package rules_test

import (
	"fmt"
	"testing"

	C "github.com/metacubex/mihomo/constant"
	"github.com/metacubex/mihomo/rules"
	"github.com/metacubex/mihomo/rules/common"
	"github.com/metacubex/mihomo/rules/wrapper"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func hostMeta(host string) *C.Metadata {
	return &C.Metadata{Host: host}
}

func matchSpan(t *testing.T, list []C.Rule, host string) (bool, string) {
	t.Helper()
	compiled := rules.CompileDomainSpans(list)
	for _, r := range compiled {
		if ok, adapter := r.Match(hostMeta(host), C.RuleMatchHelper{}); ok {
			return ok, adapter
		}
	}
	return false, ""
}

func TestDomainSpanFirstMatchSameHost(t *testing.T) {
	list := []C.Rule{
		common.NewDomain("a.example", "PROXY"),
		common.NewDomain("a.example", "REJECT"),
	}
	compiled := rules.CompileDomainSpans(list)
	require.Len(t, compiled, 1)
	assert.Equal(t, C.DomainSpan, compiled[0].RuleType())

	ok, adapter := compiled[0].Match(hostMeta("a.example"), C.RuleMatchHelper{})
	assert.True(t, ok)
	assert.Equal(t, "PROXY", adapter)
}

func TestDomainSpanExactDoesNotMatchSubdomain(t *testing.T) {
	list := []C.Rule{
		common.NewDomain("foo.com", "PROXY"),
		common.NewDomain("bar.com", "DIRECT"),
	}
	ok, _ := matchSpan(t, list, "www.foo.com")
	assert.False(t, ok)
	ok, adapter := matchSpan(t, list, "foo.com")
	assert.True(t, ok)
	assert.Equal(t, "PROXY", adapter)
}

func TestDomainSpanSuffixDotDelimited(t *testing.T) {
	list := []C.Rule{
		common.NewDomainSuffix("example.com", "PROXY"),
		common.NewDomain("other.org", "DIRECT"),
	}
	testCases := []struct {
		host   string
		expect bool
	}{
		{"example.com", true},
		{"www.example.com", true},
		{"a.b.example.com", true},
		{"notexample.com", false},
		{"www.example.comx", false},
		{"com", false},
		{"example.org", false},
	}
	for _, tc := range testCases {
		tc := tc
		t.Run(tc.host, func(t *testing.T) {
			ok, adapter := matchSpan(t, list, tc.host)
			assert.Equal(t, tc.expect, ok)
			if tc.expect {
				assert.Equal(t, "PROXY", adapter)
			}
		})
	}
}

func TestDomainSpanKeywordInterrupts(t *testing.T) {
	list := []C.Rule{
		common.NewDomain("a.example", "PROXY"),
		common.NewDomainKeyword("example", "REJECT"),
		common.NewDomain("b.example", "DIRECT"),
	}
	compiled := rules.CompileDomainSpans(list)
	require.Len(t, compiled, 3)
	assert.Equal(t, C.Domain, compiled[0].RuleType())
	assert.Equal(t, C.DomainKeyword, compiled[1].RuleType())
	assert.Equal(t, C.Domain, compiled[2].RuleType())
}

func TestDomainSpanSingleDomainUnchanged(t *testing.T) {
	list := []C.Rule{
		common.NewDomain("only.example", "PROXY"),
		common.NewMatch("DIRECT"),
	}
	compiled := rules.CompileDomainSpans(list)
	require.Len(t, compiled, 2)
	assert.Equal(t, C.Domain, compiled[0].RuleType())
	assert.Equal(t, C.MATCH, compiled[1].RuleType())
}

func TestDomainSpanLeafWrapperHitCount(t *testing.T) {
	first := wrapper.NewRuleWrapper(common.NewDomain("a.example", "PROXY"))
	second := wrapper.NewRuleWrapper(common.NewDomain("a.example", "REJECT"))
	compiled := rules.CompileDomainSpans([]C.Rule{first, second})
	require.Len(t, compiled, 1)

	ok, adapter := compiled[0].Match(hostMeta("a.example"), C.RuleMatchHelper{})
	assert.True(t, ok)
	assert.Equal(t, "PROXY", adapter)
	assert.Equal(t, uint64(1), first.HitCount())
	assert.Equal(t, uint64(0), second.HitCount())
	assert.Equal(t, uint64(0), first.MissCount())
	assert.Equal(t, uint64(0), second.MissCount())
}

type countRule struct {
	C.Rule
	n *int
}

func (c *countRule) Match(metadata *C.Metadata, helper C.RuleMatchHelper) (bool, string) {
	*c.n++
	return c.Rule.Match(metadata, helper)
}

func TestDomainSpanMissSkipsLeafScan(t *testing.T) {
	n := 0
	list := make([]C.Rule, 0, 32)
	for i := 0; i < 32; i++ {
		list = append(list, &countRule{
			Rule: common.NewDomain(fmt.Sprintf("block-%d.example", i), "REJECT"),
			n:    &n,
		})
	}
	compiled := rules.CompileDomainSpans(list)
	require.Len(t, compiled, 1)
	ok, _ := compiled[0].Match(hostMeta("unrelated.test"), C.RuleMatchHelper{})
	assert.False(t, ok)
	assert.Equal(t, 0, n)
}

func BenchmarkDomainSpanMiss(b *testing.B) {
	const n = 10000
	list := make([]C.Rule, 0, n+1)
	for i := 0; i < n; i++ {
		list = append(list, common.NewDomain(fmt.Sprintf("block-%d.example", i), "REJECT"))
	}
	list = append(list, common.NewMatch("DIRECT"))
	compiled := rules.CompileDomainSpans(list)
	meta := hostMeta("unrelated.test")
	helper := C.RuleMatchHelper{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		hit := false
		for _, r := range compiled {
			if ok, _ := r.Match(meta, helper); ok {
				hit = true
				break
			}
		}
		if hit {
			b.Fatal("unrelated host must miss the span")
		}
	}
}
