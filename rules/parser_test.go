package rules_test

import (
	"net/netip"
	"strings"
	"testing"

	C "github.com/metacubex/mihomo/constant"
	"github.com/metacubex/mihomo/rules"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// parseRuleErr asserts the error path only: error must be non-nil and its
// message must contain every keyword. Full message text is intentionally not
// compared so the wording can evolve.
func parseRuleErr(t *testing.T, tp, payload, target string, params []string, keywords ...string) {
	t.Helper()
	_, err := rules.ParseRule(tp, payload, target, params, nil)
	require.Error(t, err, "type %q payload %q", tp, payload)
	for _, keyword := range keywords {
		assert.True(t, strings.Contains(err.Error(), keyword),
			"type %q payload %q: error %q should contain %q", tp, payload, err.Error(), keyword)
	}
}

// TestParseRuleDispatchValid covers every switch case of ParseRule that can be
// constructed without external data files (geosite/geoip/asn/mmdb are excluded:
// their constructors try to download missing dat files) and asserts the parsed
// rule reaches the constructor with the expected type/payload/adapter.
func TestParseRuleDispatchValid(t *testing.T) {
	testCases := []struct {
		name      string
		tp        string
		payload   string
		params    []string
		metadata  *C.Metadata
		wantType  C.RuleType
		wantHit   bool
		skipMatch bool
	}{
		{
			name: "DOMAIN", tp: "DOMAIN", payload: "example.com",
			metadata: &C.Metadata{Host: "example.com"}, wantType: C.Domain, wantHit: true,
		},
		{
			name: "DOMAIN-SUFFIX", tp: "DOMAIN-SUFFIX", payload: "example.com",
			metadata: &C.Metadata{Host: "www.example.com"}, wantType: C.DomainSuffix, wantHit: true,
		},
		{
			name: "DOMAIN-KEYWORD", tp: "DOMAIN-KEYWORD", payload: "example",
			metadata: &C.Metadata{Host: "www.example.com"}, wantType: C.DomainKeyword, wantHit: true,
		},
		{
			name: "DOMAIN-REGEX", tp: "DOMAIN-REGEX", payload: `^www\.example\.com$`,
			metadata: &C.Metadata{Host: "www.example.com"}, wantType: C.DomainRegex, wantHit: true,
		},
		{
			name: "DOMAIN-WILDCARD", tp: "DOMAIN-WILDCARD", payload: "*.example.com",
			metadata: &C.Metadata{Host: "www.example.com"}, wantType: C.DomainWildcard, wantHit: true,
		},
		{
			name: "IP-CIDR", tp: "IP-CIDR", payload: "192.168.1.0/24", params: []string{"no-resolve"},
			metadata: &C.Metadata{DstIP: netip.MustParseAddr("192.168.1.5")},
			wantType: C.IPCIDR, wantHit: true,
		},
		{
			name: "IP-CIDR6", tp: "IP-CIDR6", payload: "fd00::/8", params: []string{"no-resolve"},
			metadata: &C.Metadata{DstIP: netip.MustParseAddr("fd00::1")},
			wantType: C.IPCIDR, wantHit: true,
		},
		{
			name: "SRC-IP-CIDR", tp: "SRC-IP-CIDR", payload: "192.168.1.0/24",
			metadata: &C.Metadata{SrcIP: netip.MustParseAddr("192.168.1.9")},
			wantType: C.SrcIPCIDR, wantHit: true,
		},
		{
			// IPSuffix matches only the trailing bits of the address, not a
			// network prefix: a /24 requires the last 3 bytes to be 1.0.0, so
			// 192.168.1.9 does not match 192.168.1.0/24 (last bytes differ).
			// A /32 compares all bytes and behaves as an exact address match.
			name: "IP-SUFFIX", tp: "IP-SUFFIX", payload: "192.168.1.0/24", params: []string{"no-resolve"},
			metadata: &C.Metadata{DstIP: netip.MustParseAddr("192.168.1.9")},
			wantType: C.IPSuffix, wantHit: false,
		},
		{
			name: "SRC-IP-SUFFIX", tp: "SRC-IP-SUFFIX", payload: "192.168.1.9/32",
			metadata: &C.Metadata{SrcIP: netip.MustParseAddr("192.168.1.9")},
			wantType: C.SrcIPSuffix, wantHit: true,
		},
		{
			name: "SRC-PORT", tp: "SRC-PORT", payload: "1000-2000",
			metadata: &C.Metadata{SrcPort: 1500}, wantType: C.SrcPort, wantHit: true,
		},
		{
			name: "DST-PORT", tp: "DST-PORT", payload: "1000-2000",
			metadata: &C.Metadata{DstPort: 1500}, wantType: C.DstPort, wantHit: true,
		},
		{
			name: "IN-PORT", tp: "IN-PORT", payload: "1000-2000",
			metadata: &C.Metadata{InPort: 1500}, wantType: C.InPort, wantHit: true,
		},
		{
			name: "DSCP", tp: "DSCP", payload: "46",
			metadata: &C.Metadata{DSCP: 46}, wantType: C.DSCP, wantHit: true,
		},
		{
			name: "PROCESS-NAME", tp: "PROCESS-NAME", payload: "curl",
			metadata: &C.Metadata{Process: "curl"}, wantType: C.ProcessName, wantHit: true,
		},
		{
			name: "PROCESS-PATH", tp: "PROCESS-PATH", payload: "/usr/bin/curl",
			metadata: &C.Metadata{ProcessPath: "/usr/bin/curl"}, wantType: C.ProcessPath, wantHit: true,
		},
		{
			name: "PROCESS-NAME-REGEX", tp: "PROCESS-NAME-REGEX", payload: "^cur$",
			metadata: &C.Metadata{Process: "cur"}, wantType: C.ProcessNameRegex, wantHit: true,
		},
		{
			name: "PROCESS-PATH-REGEX", tp: "PROCESS-PATH-REGEX", payload: "curl$",
			metadata: &C.Metadata{ProcessPath: "/usr/bin/curl"}, wantType: C.ProcessPathRegex, wantHit: true,
		},
		{
			name: "PROCESS-NAME-WILDCARD", tp: "PROCESS-NAME-WILDCARD", payload: "cu*",
			metadata: &C.Metadata{Process: "curl"}, wantType: C.ProcessNameWildcard, wantHit: true,
		},
		{
			name: "PROCESS-PATH-WILDCARD", tp: "PROCESS-PATH-WILDCARD", payload: "*/curl",
			metadata: &C.Metadata{ProcessPath: "/usr/bin/curl"}, wantType: C.ProcessPathWildcard, wantHit: true,
		},
		{
			name: "NETWORK", tp: "NETWORK", payload: "tcp",
			metadata: &C.Metadata{NetWork: C.TCP}, wantType: C.Network, wantHit: true,
		},
		{
			name: "IN-TYPE", tp: "IN-TYPE", payload: "SOCKS",
			metadata: &C.Metadata{Type: C.SOCKS5}, wantType: C.InType, wantHit: true,
		},
		{
			name: "IN-USER", tp: "IN-USER", payload: "alice/bob",
			metadata: &C.Metadata{InUser: "bob"}, wantType: C.InUser, wantHit: true,
		},
		{
			name: "IN-NAME", tp: "IN-NAME", payload: "mixed-in",
			metadata: &C.Metadata{InName: "mixed-in"}, wantType: C.InName, wantHit: true,
		},
		{
			name: "REMATCH-NAME", tp: "REMATCH-NAME", payload: "google",
			metadata: &C.Metadata{RematchName: "google"}, wantType: C.RematchName, wantHit: true,
		},
		{
			name: "AND", tp: "AND", payload: "((DOMAIN,example.com),(NETWORK,TCP))",
			metadata: &C.Metadata{Host: "example.com", NetWork: C.TCP},
			wantType: C.AND, wantHit: true,
		},
		{
			name: "NOT", tp: "NOT", payload: "((DOMAIN,example.com))",
			metadata: &C.Metadata{Host: "example.com"}, wantType: C.NOT, wantHit: false,
		},
		{
			// Provider lookup goes through the global tunnel registry, which is
			// nil until config parsing runs, so Match is not exercised here:
			// only construction and field wiring are in scope for dispatch.
			name: "RULE-SET", tp: "RULE-SET", payload: "some-provider",
			metadata: nil, wantType: C.RuleSet, skipMatch: true,
		},
		{
			name: "MATCH", tp: "MATCH", payload: "",
			metadata: &C.Metadata{Host: "anything.com"}, wantType: C.MATCH, wantHit: true,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			rule, err := rules.ParseRule(tc.tp, tc.payload, "MyAdapter", tc.params, nil)
			require.NoError(t, err)
			require.NotNil(t, rule)
			assert.Equal(t, tc.wantType, rule.RuleType())
			// adapter is threaded through to the leaf rule unmodified
			assert.Equal(t, "MyAdapter", rule.Adapter())

			if tc.skipMatch {
				return
			}

			// a rule whose Match never reflects the constructor arguments would
			// mean ParseRule routed to the wrong branch
			got, adapter := rule.Match(tc.metadata, C.RuleMatchHelper{})
			assert.Equal(t, tc.wantHit, got, "match result")
			if got {
				assert.Equal(t, "MyAdapter", adapter)
			}
		})
	}
}

// TestParseRuleDispatchInvalid asserts ParseRule surfaces errors for inputs the
func TestParseRuleDispatchInvalid(t *testing.T) {
	t.Run("unsupported type", func(t *testing.T) {
		parseRuleErr(t, "NO-SUCH-TYPE", "example.com", "DIRECT", nil, "unsupported rule type")
	})
	t.Run("empty payload for typed rule", func(t *testing.T) {
		for _, tp := range []string{"DOMAIN", "DOMAIN-SUFFIX", "IP-CIDR", "DST-PORT"} {
			parseRuleErr(t, tp, "", "DIRECT", nil, "missing subsequent parameters")
		}
	})
	t.Run("MATCH tolerates empty payload", func(t *testing.T) {
		// the guard explicitly exempts MATCH, so this must not error
		rule, err := rules.ParseRule("MATCH", "", "DIRECT", nil, nil)
		require.NoError(t, err)
		assert.Equal(t, C.MATCH, rule.RuleType())
	})
	t.Run("invalid payload reaches constructor", func(t *testing.T) {
		// ParseRule delegates value validation; bad values must surface as errors
		parseRuleErr(t, "IP-CIDR", "not-a-cidr", "DIRECT", nil)
		parseRuleErr(t, "DST-PORT", "not-a-port", "DIRECT", nil)
		parseRuleErr(t, "NETWORK", "SCTP", "DIRECT", nil, "unsupported network type")
		parseRuleErr(t, "DSCP", "100", "DIRECT", nil, "exceed 63")
		parseRuleErr(t, "DOMAIN-REGEX", "^(unclosed", "DIRECT", nil)
		parseRuleErr(t, "IN-TYPE", "/ ", "DIRECT", nil, "empty")
	})
}

// TestParseRuleParams asserts the params slice is honoured where it is consumed
// (IP-CIDR / RULE-SET src and no-resolve flags).
func TestParseRuleParams(t *testing.T) {
	t.Run("IP-CIDR src flag flips rule type", func(t *testing.T) {
		rule, err := rules.ParseRule("IP-CIDR", "192.168.1.0/24", "DIRECT", []string{"src"}, nil)
		require.NoError(t, err)
		// NewIPCIDR(WithIPCIDRSourceIP(true)) also forces no-resolve
		assert.Equal(t, C.SrcIPCIDR, rule.RuleType())

		rule, err = rules.ParseRule("IP-CIDR", "192.168.1.0/24", "DIRECT", []string{"no-resolve"}, nil)
		require.NoError(t, err)
		assert.Equal(t, C.IPCIDR, rule.RuleType())
	})
	t.Run("SRC-* variants ignore params and force src+no-resolve", func(t *testing.T) {
		rule, err := rules.ParseRule("SRC-IP-CIDR", "192.168.1.0/24", "DIRECT", nil, nil)
		require.NoError(t, err)
		assert.Equal(t, C.SrcIPCIDR, rule.RuleType())
	})
	t.Run("no-resolve keeps match free of DNS resolution", func(t *testing.T) {
		resolved := false
		rule, err := rules.ParseRule("IP-CIDR", "192.168.1.0/24", "DIRECT", []string{"no-resolve"}, nil)
		require.NoError(t, err)
		ok, _ := rule.Match(&C.Metadata{DstIP: netip.MustParseAddr("192.168.1.1")},
			C.RuleMatchHelper{ResolveIP: func() { resolved = true }})
		assert.True(t, ok)
		assert.False(t, resolved, "no-resolve rule must not call ResolveIP")
	})
	t.Run("missing params still parse (defaults)", func(t *testing.T) {
		rule, err := rules.ParseRule("IP-CIDR", "192.168.1.0/24", "DIRECT", nil, nil)
		require.NoError(t, err)
		assert.Equal(t, C.IPCIDR, rule.RuleType())
	})
}
