package common_test

import (
	"net/netip"
	"testing"

	C "github.com/metacubex/mihomo/constant"
	"github.com/metacubex/mihomo/rules/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// hostMetadata builds the metadata a domain matcher consults. RuleHost returns
// SniffHost when it is non-empty and Host otherwise.
func hostMetadata(host string) *C.Metadata {
	return &C.Metadata{Host: host}
}

// TestDomainMatch asserts Domain is an exact host comparison. Only the rule
// pattern is lowercased (NewDomain); the metadata host is compared as-is, so a
// mixed-case host does NOT match -- normalization is the caller's job.
func TestDomainMatch(t *testing.T) {
	rule := common.NewDomain("Foo.COM", "Proxy")

	testCases := []struct {
		name   string
		host   string
		sniff  string
		expect bool
	}{
		{"exact match with lower host", "foo.com", "", true},
		{"upper host does not match", "FOO.COM", "", false},
		{"mixed case host does not match", "Foo.Com", "", false},
		{"trailing dot is a different host", "foo.com.", "", false},
		{"sub-domain is not the domain", "www.foo.com", "", false},
		{"parent domain is not the domain", "oo.com", "", false},
		{"unrelated host", "bar.com", "", false},
		{"empty host", "", "", false},
		{"suffix-only host", ".foo.com", "", false},
		{"sniff host wins over host", "bar.com", "foo.com", true},
		{"sniff host mismatch", "foo.com", "bar.com", false},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			metadata := hostMetadata(tc.host)
			metadata.SniffHost = tc.sniff
			got, adapter := rule.Match(metadata, C.RuleMatchHelper{})
			assert.Equal(t, tc.expect, got)
			if got {
				assert.Equal(t, "Proxy", adapter)
			}
		})
	}

	assert.Equal(t, C.Domain, rule.RuleType())
	// NewDomain lowercases its pattern for storage
	assert.Equal(t, "foo.com", rule.Payload())
}

// TestDomainSuffixMatch asserts DomainSuffix matches the domain itself and any
// deeper label, but never a partial label or the parent domain.
func TestDomainSuffixMatch(t *testing.T) {
	rule := common.NewDomainSuffix("example.COM", "Proxy")

	testCases := []struct {
		name   string
		host   string
		expect bool
	}{
		{"the domain itself", "example.com", true},
		{"sub-domain", "www.example.com", true},
		{"deep sub-domain", "a.b.c.example.com", true},
		// the matcher lowercases the pattern, not the host
		{"upper host does not match", "WWW.EXAMPLE.COM", false},
		// HasSuffix(domain, ".suffix") requires the dot separator, so these
		// partial-label hosts must not match
		{"partial label suffix", "notexample.com", false},
		{"partial label prefix", "www.example.comx", false},
		{"parent domain", "com", false},
		{"unrelated", "example.org", false},
		{"empty host", "", false},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got, _ := rule.Match(hostMetadata(tc.host), C.RuleMatchHelper{})
			assert.Equal(t, tc.expect, got, "host %q", tc.host)
		})
	}

	assert.Equal(t, C.DomainSuffix, rule.RuleType())
	assert.Equal(t, "example.com", rule.Payload())
}

// TestDomainKeywordMatch asserts DomainKeyword is a substring test on the host,
// so it has no notion of label boundaries: it matches inside a label.
func TestDomainKeywordMatch(t *testing.T) {
	rule := common.NewDomainKeyword("example", "Proxy")

	testCases := []struct {
		name   string
		host   string
		expect bool
	}{
		{"keyword is the host", "example", true},
		{"keyword as a label", "www.example.com", true},
		{"keyword inside a label", "www.myexample.com", true},
		{"keyword spanning labels", "www.example", true},
		// the pattern is lowercased, the host is not
		{"upper host does not match", "WWW.EXAMPLE.COM", false},
		{"missing keyword", "www.test.com", false},
		{"empty host", "", false},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got, _ := rule.Match(hostMetadata(tc.host), C.RuleMatchHelper{})
			assert.Equal(t, tc.expect, got, "host %q", tc.host)
		})
	}

	assert.Equal(t, C.DomainKeyword, rule.RuleType())
}

// TestDomainWildcardMatch covers the label-boundary semantics that DOMAIN and
// DOMAIN-SUFFIX alone cannot express, for contrast with the above.
func TestDomainWildcardMatch(t *testing.T) {
	rule, err := common.NewDomainWildcard("*.example.com", "Proxy")
	require.NoError(t, err)

	testCases := []struct {
		name   string
		host   string
		expect bool
	}{
		{"sub-domain matches", "www.example.com", true},
		{"the pattern's own domain does not match", "example.com", false},
		{"deep sub-domain matches", "a.b.example.com", true},
		{"partial label does not match", "www.example.comx", false},
		{"unrelated host", "example.org", false},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got, _ := rule.Match(hostMetadata(tc.host), C.RuleMatchHelper{})
			assert.Equal(t, tc.expect, got, "host %q", tc.host)
		})
	}
}

// TestIPCIDRMatch asserts IPCIDR contains the destination address and honours
// the src/no-resolve options ParseRule feeds it.
func TestIPCIDRMatch(t *testing.T) {
	rule, err := common.NewIPCIDR("192.168.1.0/24", "Proxy",
		common.WithIPCIDRSourceIP(false), common.WithIPCIDRNoResolve(true))
	require.NoError(t, err)
	assert.Equal(t, C.IPCIDR, rule.RuleType())

	testCases := []struct {
		name   string
		dstIP  string
		srcIP  string
		expect bool
	}{
		{"network address", "192.168.1.0", "", true},
		{"host in range", "192.168.1.128", "", true},
		{"broadcast address", "192.168.1.255", "", true},
		{"just outside range", "192.168.2.1", "", false},
		{"different family", "::1", "", false},
		{"unset ip", "", "", false},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			metadata := &C.Metadata{}
			if tc.dstIP != "" {
				metadata.DstIP = netip.MustParseAddr(tc.dstIP)
			}
			if tc.srcIP != "" {
				metadata.SrcIP = netip.MustParseAddr(tc.srcIP)
			}
			got, _ := rule.Match(metadata, C.RuleMatchHelper{})
			assert.Equal(t, tc.expect, got)
		})
	}

	t.Run("no-resolve suppresses helper", func(t *testing.T) {
		called := false
		ok, _ := rule.Match(&C.Metadata{DstIP: netip.MustParseAddr("192.168.1.5")},
			C.RuleMatchHelper{ResolveIP: func() { called = true }})
		assert.True(t, ok)
		assert.False(t, called, "no-resolve must not trigger DNS resolution")
	})

	t.Run("resolve helper is consulted without no-resolve", func(t *testing.T) {
		resolveRule, err := common.NewIPCIDR("192.168.1.0/24", "Proxy")
		require.NoError(t, err)
		called := false
		ok, _ := resolveRule.Match(&C.Metadata{DstIP: netip.MustParseAddr("192.168.1.5")},
			C.RuleMatchHelper{ResolveIP: func() { called = true }})
		assert.True(t, ok)
		assert.True(t, called, "IPCIDR without no-resolve asks for DNS resolution")
	})

	t.Run("src mode matches source address", func(t *testing.T) {
		srcRule, err := common.NewIPCIDR("10.0.0.0/8", "Proxy", common.WithIPCIDRSourceIP(true))
		require.NoError(t, err)
		assert.Equal(t, C.SrcIPCIDR, srcRule.RuleType())

		ok, _ := srcRule.Match(&C.Metadata{
			SrcIP: netip.MustParseAddr("10.1.2.3"),
			DstIP: netip.MustParseAddr("8.8.8.8"),
		}, C.RuleMatchHelper{})
		assert.True(t, ok, "src rule matches the source address")

		ok, _ = srcRule.Match(&C.Metadata{
			SrcIP: netip.MustParseAddr("9.9.9.9"),
			DstIP: netip.MustParseAddr("10.1.2.3"),
		}, C.RuleMatchHelper{})
		assert.False(t, ok, "src rule ignores the destination address")
	})

	t.Run("invalid prefix is rejected", func(t *testing.T) {
		_, err := common.NewIPCIDR("192.168.1", "Proxy")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "payloadRule error")
	})
}

// TestParseParams asserts the src flag is recognised and, per implementation,
// implies no-resolve.
func TestParseParams(t *testing.T) {
	testCases := []struct {
		name      string
		params    []string
		wantSrc   bool
		wantNoRes bool
	}{
		{"none", nil, false, false},
		{"no-resolve only", []string{"no-resolve"}, false, true},
		{"src only implies no-resolve", []string{"src"}, true, true},
		{"both", []string{"src", "no-resolve"}, true, true},
		{"unrelated params ignored", []string{"foo"}, false, false},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			isSrc, noResolve := common.ParseParams(tc.params)
			assert.Equal(t, tc.wantSrc, isSrc)
			assert.Equal(t, tc.wantNoRes, noResolve)
		})
	}
}

// TestParseRulePayload asserts the tp/payload/target/params split for both
// needTarget modes, which is the input ParseRule consumes.
func TestParseRulePayload(t *testing.T) {
	testCases := []struct {
		name        string
		rule        string
		needTarget  bool
		wantTp      string
		wantPayload string
		wantTarget  string
		wantParams  []string
	}{
		{
			name: "with target", rule: "IP-CIDR,192.168.1.0/24,DIRECT,no-resolve",
			needTarget: true, wantTp: "IP-CIDR", wantPayload: "192.168.1.0/24",
			wantTarget: "DIRECT", wantParams: []string{"no-resolve"},
		},
		{
			name: "target is the last field for comma-in-payload types", rule: "NOT,((DOMAIN,a.com)),DIRECT",
			needTarget: true, wantTp: "NOT", wantPayload: "((DOMAIN,a.com))", wantTarget: "DIRECT",
		},
		{
			name: "comma-in-payload without target", rule: "NOT,((DOMAIN,a.com))",
			needTarget: false, wantTp: "NOT", wantPayload: "((DOMAIN,a.com))", wantTarget: "",
		},
		{
			name: "without target, target slot becomes a param", rule: "IP-CIDR,192.168.1.0/24,DIRECT,no-resolve",
			needTarget: false, wantTp: "IP-CIDR", wantPayload: "192.168.1.0/24",
			wantParams: []string{"DIRECT", "no-resolve"},
		},
		{
			name: "type only", rule: "MATCH", needTarget: true,
			wantTp: "MATCH", wantPayload: "", wantTarget: "",
		},
		{
			name: "type is upper cased", rule: "domain,example.com,DIRECT",
			needTarget: true, wantTp: "DOMAIN", wantPayload: "example.com", wantTarget: "DIRECT",
		},
		{
			name: "whitespace is trimmed", rule: "DOMAIN , example.com , DIRECT",
			needTarget: true, wantTp: "DOMAIN", wantPayload: "example.com", wantTarget: "DIRECT",
		},
		{
			name: "MATCH target lands in the target field", rule: "MATCH,DIRECT",
			needTarget: true, wantTp: "MATCH", wantTarget: "DIRECT",
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			tp, payload, target, params := common.ParseRulePayload(tc.rule, tc.needTarget)
			assert.Equal(t, tc.wantTp, tp)
			assert.Equal(t, tc.wantPayload, payload)
			assert.Equal(t, tc.wantTarget, target)
			assert.Equal(t, tc.wantParams, params)
		})
	}

	// the empty rule yields an empty type, which is what feeds the
	// "format is error" guard in logic.payloadToRule
	tp, payload, target, params := common.ParseRulePayload("", false)
	assert.Equal(t, "", tp)
	assert.Equal(t, "", payload)
	assert.Equal(t, "", target)
	assert.Empty(t, params)
}
