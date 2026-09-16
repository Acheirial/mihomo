package logic_test

import (
	"net/netip"
	"testing"

	// https://github.com/golang/go/wiki/CodeReviewComments#import-dot
	. "github.com/metacubex/mihomo/rules/logic"

	C "github.com/metacubex/mihomo/constant"
	"github.com/metacubex/mihomo/rules"

	"github.com/stretchr/testify/assert"
)

var ParseRule = rules.ParseRule

func TestAND(t *testing.T) {
	and, err := NewAND("((DOMAIN,baidu.com),(NETWORK,TCP),(DST-PORT,10001-65535))", "DIRECT", ParseRule)
	assert.Equal(t, nil, err)
	assert.Equal(t, "DIRECT", and.Adapter())
	m, _ := and.Match(&C.Metadata{
		Host:    "baidu.com",
		NetWork: C.TCP,
		DstPort: 20000,
	}, C.RuleMatchHelper{})
	assert.Equal(t, true, m)

	and, err = NewAND("(DOMAIN,baidu.com),(NETWORK,TCP),(DST-PORT,10001-65535))", "DIRECT", ParseRule)
	assert.NotEqual(t, nil, err)

	and, err = NewAND("((AND,(DOMAIN,baidu.com),(NETWORK,TCP)),(NETWORK,TCP),(DST-PORT,10001-65535))", "DIRECT", ParseRule)
	assert.Equal(t, nil, err)
}

func TestNOT(t *testing.T) {
	not, err := NewNOT("((DST-PORT,6000-6500))", "REJECT", ParseRule)
	assert.Equal(t, nil, err)
	m, _ := not.Match(&C.Metadata{
		DstPort: 6100,
	}, C.RuleMatchHelper{})
	assert.Equal(t, false, m)

	_, err = NewNOT("(DST-PORT,5600-6666)", "DIRECT", ParseRule)
	assert.NotEqual(t, nil, err)

	_, err = NewNOT("DST-PORT,5600-6666", "DIRECT", ParseRule)
	assert.NotEqual(t, nil, err)

	_, err = NewNOT("((DST-PORT,5600-6666),(DOMAIN,baidu.com))", "DIRECT", ParseRule)
	assert.NotEqual(t, nil, err)

	_, err = NewNOT("(())", "DIRECT", ParseRule)
	assert.NotEqual(t, nil, err)
	_, err = NewNOT("((DST-PORT,6000-6500))", "REJECT", ParseRule)
	assert.NoError(t, err)

	// NOT inverts its single rule: a miss on the inner rule is a hit
	not, err = NewNOT("((DOMAIN,baidu.com))", "REJECT", ParseRule)
	assert.NoError(t, err)

	m, adapter := not.Match(&C.Metadata{Host: "www.baidu.com"}, C.RuleMatchHelper{})
	// Domain is an exact comparison, so www.baidu.com misses the inner rule
	assert.True(t, m)
	assert.Equal(t, "REJECT", adapter)

	m, adapter = not.Match(&C.Metadata{Host: "baidu.com"}, C.RuleMatchHelper{})
	assert.False(t, m)
	assert.Empty(t, adapter)

	// NOT evaluates exactly one rule: a payload with two rules is rejected
	_, err = NewNOT("((DOMAIN,baidu.com),(DOMAIN,google.com))", "DIRECT", ParseRule)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "one rule")
}

func TestOR(t *testing.T) {
	or, err := NewOR("((DOMAIN,baidu.com),(NETWORK,TCP),(DST-PORT,10001-65535))", "DIRECT", ParseRule)
	assert.Equal(t, nil, err)
	m, _ := or.Match(&C.Metadata{
		NetWork: C.TCP,
	}, C.RuleMatchHelper{})
	assert.Equal(t, true, m)
}

// TestANDShortCircuit asserts AND stops evaluating rules as soon as one misses.
// Short-circuiting is observed through the RuleMatchHelper.ResolveIP hook:
// IP-CIDR only calls it when the rule is reached and is allowed to resolve.
func TestANDShortCircuit(t *testing.T) {
	and, err := NewAND("((DOMAIN,baidu.com),(IP-CIDR,1.0.0.0/8))", "DIRECT", ParseRule)
	assert.NoError(t, err)

	t.Run("miss on first rule skips the rest", func(t *testing.T) {
		resolved := 0
		m, adapter := and.Match(&C.Metadata{Host: "www.baidu.com"},
			C.RuleMatchHelper{ResolveIP: func() { resolved++ }})
		// DOMAIN is an exact match, so www.baidu.com misses and IP-CIDR is
		// never consulted: no resolution attempt is made
		assert.False(t, m)
		assert.Equal(t, "DIRECT", adapter)
		assert.Zero(t, resolved)
	})

	t.Run("first rule matches, second consulted", func(t *testing.T) {
		resolved := 0
		// DOMAIN matches, so IP-CIDR is reached: it resolves the host and
		// then reports whether the resolved address is in range
		m, adapter := and.Match(&C.Metadata{Host: "baidu.com", DstIP: netip.MustParseAddr("1.1.1.1")},
			C.RuleMatchHelper{ResolveIP: func() { resolved++ }})
		assert.True(t, m)
		assert.Equal(t, "DIRECT", adapter)
		assert.Equal(t, 1, resolved)

		resolved = 0
		m, adapter = and.Match(&C.Metadata{Host: "baidu.com", DstIP: netip.MustParseAddr("9.9.9.9")},
			C.RuleMatchHelper{ResolveIP: func() { resolved++ }})
		// the host resolved, but the address is outside 1.0.0.0/8
		assert.False(t, m)
		assert.Equal(t, "DIRECT", adapter)
		assert.Equal(t, 1, resolved)
	})
}

// TestORShortCircuit asserts OR stops evaluating rules as soon as one hits.
func TestORShortCircuit(t *testing.T) {
	or, err := NewOR("((DOMAIN,baidu.com),(IP-CIDR,1.0.0.0/8))", "DIRECT", ParseRule)
	assert.NoError(t, err)

	t.Run("hit on first rule skips the rest", func(t *testing.T) {
		resolved := 0
		m, adapter := or.Match(&C.Metadata{Host: "baidu.com"},
			C.RuleMatchHelper{ResolveIP: func() { resolved++ }})
		assert.True(t, m)
		assert.Equal(t, "DIRECT", adapter)
		assert.Zero(t, resolved)
	})

	t.Run("no rule matches", func(t *testing.T) {
		resolved := 0
		m, adapter := or.Match(&C.Metadata{Host: "nope.com"},
			C.RuleMatchHelper{ResolveIP: func() { resolved++ }})
		assert.False(t, m)
		assert.Empty(t, adapter)
		assert.Equal(t, 1, resolved)
	})
}
// TestLogicMalformedPayload asserts each malformed payload is rejected with a
// reason. Only the error keyword is asserted: the wording is allowed to evolve.
func TestLogicMalformedPayload(t *testing.T) {
	testCases := []struct {
		name     string
		logic    string
		payload  string
		errKey   string
	}{
		{
			name: "AND missing closing paren", logic: "AND",
			payload: "((DOMAIN,baidu.com),(NETWORK,TCP)",
			// format() walks the string and finds an unclosed '('
			errKey: "missing )",
		},
		{
			name: "OR missing closing paren", logic: "OR",
			payload: "((DOMAIN,baidu.com)",
			errKey: "missing )",
		},
		{
			name: "NOT missing closing paren", logic: "NOT",
			payload: "((DOMAIN,baidu.com)",
			errKey: "missing )",
		},
		{
			name: "missing opening paren", logic: "AND",
			payload: "DOMAIN,baidu.com))",
			// parsePayload requires the payload to start with '('
			errKey: "format error",
		},
		{
			name: "unbalanced with extra closing paren", logic: "AND",
			payload: "((DOMAIN,baidu.com)))",
			// format() pops an empty stack
			errKey: "missing '('",
		},
		{
			name: "empty payload", logic: "AND",
			payload: "",
			errKey: "format error",
		},
		{
			// "(())" yields the sub-payload "()", whose type parses as "()" --
			// not a rule type, so payloadToRule reports a format error
			name: "nested empty group", logic: "AND",
			payload: "(())",
			errKey: "format is error",
		},
		{
			name: "rule with empty payload", logic: "AND",
			payload: "((DOMAIN,))",
			// the empty payload reaches ParseRule's guard
			errKey: "missing subsequent parameters",
		},
		{
			name: "NOT with two rules", logic: "NOT",
			payload: "((DOMAIN,baidu.com),(DOMAIN,google.com))",
			errKey: "one rule",
		},
		{
			name: "unsupported inner rule type", logic: "AND",
			payload: "((MATCH,DIRECT))",
			// logic.payloadToRule rejects MATCH inside a logic group
			errKey: "unsupported rule type",
		},
		{
			name: "invalid inner rule payload", logic: "AND",
			payload: "((IP-CIDR,not-a-cidr))",
			errKey: "payloadRule error",
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			var err error
			switch tc.logic {
			case "AND":
				_, err = NewAND(tc.payload, "DIRECT", ParseRule)
			case "OR":
				_, err = NewOR(tc.payload, "DIRECT", ParseRule)
			case "NOT":
				_, err = NewNOT(tc.payload, "DIRECT", ParseRule)
			default:
				t.Fatalf("unknown logic type %q", tc.logic)
			}
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tc.errKey)
		})
	}
}

// TestAndEmptyPayload documents that "()" produces a rule set with no members
// rather than an error: findSubRuleRange drops the only (whole-string) range,
// so AND is left evaluating nothing.
func TestAndEmptyPayload(t *testing.T) {
	and, err := NewAND("()", "DIRECT", ParseRule)
	assert.NoError(t, err)
	// with no rules to fail, the AND loop vacuously succeeds
	m, adapter := and.Match(&C.Metadata{Host: "baidu.com"}, C.RuleMatchHelper{})
	assert.True(t, m)
	assert.Equal(t, "DIRECT", adapter)
}
