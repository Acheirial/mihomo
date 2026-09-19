package rules

import (
	"strconv"

	"github.com/metacubex/mihomo/common/pool"
	"github.com/metacubex/mihomo/component/trie"
	C "github.com/metacubex/mihomo/constant"
)

// domainSpan is a compiled run of consecutive DOMAIN / DOMAIN-SUFFIX leaves.
// Hosts absent from the DomainSet miss the whole run without scanning leaves.
// Hosts present still walk leaves in original order so first-match and per-leaf
// wrappers stay intact. KEYWORD / REGEX / WILDCARD / GEOSITE / other types
// interrupt the run and are never inserted.
type domainSpan struct {
	set          *trie.DomainSet
	rules        []C.Rule
	adapter      string
	numRules     int
	sameAdapter  bool
	hasWrappers  bool
}

func unwrapRule(r C.Rule) C.Rule {
	for {
		w, ok := r.(C.RuleWrapper)
		if !ok {
			return r
		}
		inner := w.Unwrap()
		if inner == nil || inner == r {
			return r
		}
		r = inner
	}
}

func isDomainSpanLeaf(r C.Rule) bool {
	switch unwrapRule(r).RuleType() {
	case C.Domain, C.DomainSuffix:
		return true
	default:
		return false
	}
}

func insertDomainSpanLeaf(b *trie.DomainSetBuilder, r C.Rule) error {
	leaf := unwrapRule(r)
	switch leaf.RuleType() {
	case C.Domain:
		return b.Insert(leaf.Payload())
	case C.DomainSuffix:
		// DomainSuffix matches the suffix itself and any deeper label.
		// DomainTrie "+." is that encoding (exact + subdomain wildcard).
		return b.Insert("+." + leaf.Payload())
	default:
		return trie.ErrInvalidDomain
	}
}

func newDomainSpan(leaves []C.Rule) (*domainSpan, bool) {
	if len(leaves) < 2 {
		return nil, false
	}
	var builder trie.DomainSetBuilder
	firstAdapter := pool.Intern(leaves[0].Adapter())
	sameAdapter := true
	hasWrappers := false

	for _, r := range leaves {
		if err := insertDomainSpanLeaf(&builder, r); err != nil {
			return nil, false
		}
		ad := pool.Intern(r.Adapter())
		if ad != firstAdapter {
			sameAdapter = false
		}
		if _, isWrapper := r.(C.RuleWrapper); isWrapper {
			hasWrappers = true
		}
	}
	set := builder.Build()
	if set == nil {
		return nil, false
	}

	span := &domainSpan{
		set:         set,
		adapter:     firstAdapter,
		numRules:    len(leaves),
		sameAdapter: sameAdapter,
		hasWrappers: hasWrappers,
	}

	// If all leaves share the identical adapter and none are custom wrappers with active hit/miss requirement,
	// we can avoid retaining the full slice of rules, saving substantial memory.
	if sameAdapter && !hasWrappers {
		span.rules = nil
	} else {
		copied := make([]C.Rule, len(leaves))
		copy(copied, leaves)
		span.rules = copied
	}
	return span, true
}

// CompileDomainSpans replaces consecutive DOMAIN / DOMAIN-SUFFIX runs of length
// >= 2 with a domainSpan. Shorter runs and interrupting types are left as-is.
// Wrappers on the original leaves are preserved so hit/miss counters stay on
// the YAML line that produced them.
func CompileDomainSpans(rules []C.Rule) []C.Rule {
	if len(rules) < 2 {
		return rules
	}
	out := make([]C.Rule, 0, len(rules))
	i := 0
	for i < len(rules) {
		if !isDomainSpanLeaf(rules[i]) {
			out = append(out, rules[i])
			i++
			continue
		}
		j := i + 1
		for j < len(rules) && isDomainSpanLeaf(rules[j]) {
			j++
		}
		if j-i >= 2 {
			if span, ok := newDomainSpan(rules[i:j]); ok {
				out = append(out, span)
				i = j
				continue
			}
		}
		out = append(out, rules[i:j]...)
		i = j
	}
	return out
}

func (s *domainSpan) RuleType() C.RuleType {
	return C.DomainSpan
}

func (s *domainSpan) Match(metadata *C.Metadata, helper C.RuleMatchHelper) (bool, string) {
	host := metadata.RuleHost()
	if host == "" || s.set == nil || !s.set.Has(host) {
		return false, ""
	}
	if s.sameAdapter && !s.hasWrappers {
		return true, s.adapter
	}
	for _, r := range s.rules {
		if ok, adapter := r.Match(metadata, helper); ok {
			return ok, adapter
		}
	}
	return false, ""
}

func (s *domainSpan) Adapter() string {
	return s.adapter
}

func (s *domainSpan) Payload() string {
	return strconv.Itoa(s.numRules) + " domains"
}

func (s *domainSpan) ProviderNames() []string {
	var names []string
	for _, r := range s.rules {
		names = append(names, r.ProviderNames()...)
	}
	return names
}

var _ C.Rule = (*domainSpan)(nil)
