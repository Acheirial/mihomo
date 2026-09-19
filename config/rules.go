package config

import (
	"fmt"
	"strings"

	"github.com/metacubex/mihomo/component/cidr"
	"github.com/metacubex/mihomo/component/fakeip"
	"github.com/metacubex/mihomo/component/trie"
	C "github.com/metacubex/mihomo/constant"
	P "github.com/metacubex/mihomo/constant/provider"
	"github.com/metacubex/mihomo/log"
	R "github.com/metacubex/mihomo/rules"
	RB "github.com/metacubex/mihomo/rules/bundle"
	RC "github.com/metacubex/mihomo/rules/common"
	RP "github.com/metacubex/mihomo/rules/provider"
	RW "github.com/metacubex/mihomo/rules/wrapper"
	T "github.com/metacubex/mihomo/tunnel"
)

func parseRuleProviders(cfg *RawConfig) (ruleProviders map[string]P.RuleProvider, err error) {
	RP.SetTunnel(T.Tunnel)
	ruleProviders = map[string]P.RuleProvider{}
	// parse rule provider
	for name, mapping := range cfg.RuleProvider {
		rp, err := RP.ParseRuleProvider(name, mapping, R.ParseRule, RB.MakeBundleFile)
		if err != nil {
			return nil, err
		}

		ruleProviders[name] = rp
	}
	return
}

func parseSubRules(cfg *RawConfig, proxies map[string]C.Proxy, ruleProviders map[string]P.RuleProvider) (subRules map[string][]C.Rule, err error) {
	subRules = map[string][]C.Rule{}
	for name := range cfg.SubRules {
		subRules[name] = make([]C.Rule, 0)
	}
	for name, rawRules := range cfg.SubRules {
		if len(name) == 0 {
			return nil, fmt.Errorf("sub-rule name is empty")
		}
		var rules []C.Rule
		rules, err = parseRules(rawRules, proxies, ruleProviders, subRules, fmt.Sprintf("sub-rules[%s]", name))
		if err != nil {
			return nil, err
		}
		subRules[name] = rules
	}

	if err = verifySubRule(subRules); err != nil {
		return nil, err
	}

	return
}

func verifySubRule(subRules map[string][]C.Rule) error {
	for name := range subRules {
		err := verifySubRuleCircularReferences(name, subRules, []string{})
		if err != nil {
			return err
		}
	}
	return nil
}

func verifySubRuleCircularReferences(n string, subRules map[string][]C.Rule, arr []string) error {
	isInArray := func(v string, array []string) bool {
		for _, c := range array {
			if v == c {
				return true
			}
		}
		return false
	}

	arr = append(arr, n)
	for i, rule := range subRules[n] {
		if rule.RuleType() == C.SubRules {
			if _, ok := subRules[rule.Adapter()]; !ok {
				return fmt.Errorf("sub-rule[%d:%s] error: [%s] not found", i, n, rule.Adapter())
			}
			if isInArray(rule.Adapter(), arr) {
				arr = append(arr, rule.Adapter())
				return fmt.Errorf("sub-rule error: circular references [%s]", strings.Join(arr, "->"))
			}

			if err := verifySubRuleCircularReferences(rule.Adapter(), subRules, arr); err != nil {
				return err
			}
		}
	}
	return nil
}

func parseRules(rulesConfig []string, proxies map[string]C.Proxy, ruleProviders map[string]P.RuleProvider, subRules map[string][]C.Rule, format string) ([]C.Rule, error) {
	var rules []C.Rule

	// parse rules
	for idx, line := range rulesConfig {
		tp, payload, target, params := RC.ParseRulePayload(line, true)
		if target == "" {
			return nil, fmt.Errorf("%s[%d] [%s] error: format invalid", format, idx, line)
		}

		if _, ok := proxies[target]; !ok {
			if tp != "SUB-RULE" {
				return nil, fmt.Errorf("%s[%d] [%s] error: proxy [%s] not found", format, idx, line, target)
			} else if _, ok = subRules[target]; !ok {
				return nil, fmt.Errorf("%s[%d] [%s] error: sub-rule [%s] not found", format, idx, line, target)
			}
		}

		parsed, parseErr := R.ParseRule(tp, payload, target, params, subRules)
		if parseErr != nil {
			return nil, fmt.Errorf("%s[%d] [%s] error: %s", format, idx, line, parseErr.Error())
		}

		for _, name := range parsed.ProviderNames() {
			if _, ok := ruleProviders[name]; !ok {
				return nil, fmt.Errorf("%s[%d] [%s] error: rule set [%s] not found", format, idx, line, name)
			}
		}

		if format == "rules" { // only wrap top level rules
			parsed = RW.NewRuleWrapper(parsed)
		}

		rules = append(rules, parsed)
	}

	rules = R.CompileDomainSpans(rules)

	return rules, nil
}

func parseFakeIPRules(rawRules []string, ruleProviders map[string]P.RuleProvider) ([]C.Rule, error) {
	var rules []C.Rule

	for idx, line := range rawRules {
		tp, payload, action, params := RC.ParseRulePayload(line, true)

		action = strings.ToLower(action)
		if action != fakeip.UseFakeIP && action != fakeip.UseRealIP {
			return nil, fmt.Errorf("dns.fake-ip-filter[%d] [%s] error: invalid action '%s', must be 'fake-ip' or 'real-ip'", idx, line, action)
		}

		if tp == "RULE-SET" {
			if rp, ok := ruleProviders[payload]; !ok {
				return nil, fmt.Errorf("dns.fake-ip-filter[%d] [%s] error: rule-set '%s' not found", idx, line, payload)
			} else {
				switch rp.Behavior() {
				case P.IPCIDR:
					return nil, fmt.Errorf("dns.fake-ip-filter[%d] [%s] error: rule-set behavior is %s, must be domain or classical", idx, line, rp.Behavior())
				case P.Classical:
					log.Warnln("%s provider is %s, only matching domain rules in fake-ip-filter", rp.Name(), rp.Behavior())
				default:
				}
			}
		}

		parsed, err := R.ParseRule(tp, payload, action, params, nil)
		if err != nil {
			return nil, fmt.Errorf("dns.fake-ip-filter[%d] [%s] error: %w", idx, line, err)
		}

		if !isDomainRule(parsed.RuleType()) && parsed.RuleType() != C.MATCH {
			return nil, fmt.Errorf("dns.fake-ip-filter[%d] [%s] error: rule type '%s' not supported, only domain-based rules allowed", idx, line, tp)
		}

		rules = append(rules, parsed)
	}

	return rules, nil
}

func isDomainRule(rt C.RuleType) bool {
	switch rt {
	case C.Domain, C.DomainSuffix, C.DomainKeyword, C.DomainRegex, C.DomainWildcard, C.GEOSITE, C.RuleSet:
		return true
	default:
		return false
	}
}

func parseIPCIDR(addresses []string, cidrSet *cidr.IpCidrSet, adapterName string, ruleProviders map[string]P.RuleProvider) (matchers []C.IpMatcher, err error) {
	var matcher C.IpMatcher
	for _, ipcidr := range addresses {
		ipcidrLower := strings.ToLower(ipcidr)
		if strings.HasPrefix(ipcidrLower, "geoip:") {
			subkeys := strings.Split(ipcidr, ":")
			subkeys = subkeys[1:]
			subkeys = strings.Split(subkeys[0], ",")
			for _, country := range subkeys {
				matcher, err = RC.NewGEOIP(country, adapterName, false, false)
				if err != nil {
					return nil, err
				}
				matchers = append(matchers, matcher)
			}
		} else if strings.HasPrefix(ipcidrLower, "rule-set:") {
			subkeys := strings.Split(ipcidr, ":")
			subkeys = subkeys[1:]
			subkeys = strings.Split(subkeys[0], ",")
			for _, domainSetName := range subkeys {
				matcher, err = parseIPRuleSet(domainSetName, adapterName, ruleProviders)
				if err != nil {
					return nil, err
				}
				matchers = append(matchers, matcher)
			}
		} else {
			if cidrSet == nil {
				cidrSet = cidr.NewIpCidrSet()
			}
			err = cidrSet.AddIpCidrForString(ipcidr)
			if err != nil {
				return nil, err
			}
		}
	}
	if !cidrSet.IsEmpty() {
		err = cidrSet.Merge()
		if err != nil {
			return nil, err
		}
		matcher = cidrSet
		matchers = append(matchers, matcher)
	}
	return
}

func parseDomain(domains []string, domainSetBuilder *trie.DomainSetBuilder, adapterName string, ruleProviders map[string]P.RuleProvider) (matchers []C.DomainMatcher, err error) {
	var matcher C.DomainMatcher
	for idx, domain := range domains {
		domainLower := strings.ToLower(domain)
		if strings.HasPrefix(domainLower, "geosite:") {
			subkeys := strings.Split(domain, ":")
			subkeys = subkeys[1:]
			subkeys = strings.Split(subkeys[0], ",")
			for _, country := range subkeys {
				matcher, err = RC.NewGEOSITE(country, adapterName)
				if err != nil {
					return nil, fmt.Errorf("%s[%d] %q: %w", adapterName, idx, domain, err)
				}
				matchers = append(matchers, matcher)
			}
		} else if strings.HasPrefix(domainLower, "rule-set:") {
			subkeys := strings.Split(domain, ":")
			subkeys = subkeys[1:]
			subkeys = strings.Split(subkeys[0], ",")
			for _, domainSetName := range subkeys {
				matcher, err = parseDomainRuleSet(domainSetName, adapterName, ruleProviders)
				if err != nil {
					return nil, fmt.Errorf("%s[%d] %q: %w", adapterName, idx, domain, err)
				}
				matchers = append(matchers, matcher)
			}
		} else {
			if domainSetBuilder == nil {
				domainSetBuilder = &trie.DomainSetBuilder{}
			}
			err = domainSetBuilder.Insert(domain)
			if err != nil {
				return nil, fmt.Errorf("%s[%d]: %w", adapterName, idx, err)
			}
		}
	}
	if !domainSetBuilder.IsEmpty() {
		matcher = domainSetBuilder.Build()
		matchers = append(matchers, matcher)
	}
	return
}

func parseIPRuleSet(domainSetName string, adapterName string, ruleProviders map[string]P.RuleProvider) (C.IpMatcher, error) {
	if rp, ok := ruleProviders[domainSetName]; !ok {
		return nil, fmt.Errorf("not found rule-set: %s", domainSetName)
	} else {
		switch rp.Behavior() {
		case P.Domain:
			return nil, fmt.Errorf("rule provider type error, expect ipcidr,actual %s", rp.Behavior())
		case P.Classical:
			log.Warnln("%s provider is %s, only matching it contain ip rule", rp.Name(), rp.Behavior())
		default:
		}
	}
	return RP.NewRuleSet(domainSetName, adapterName, false, true)
}

func parseDomainRuleSet(domainSetName string, adapterName string, ruleProviders map[string]P.RuleProvider) (C.DomainMatcher, error) {
	if rp, ok := ruleProviders[domainSetName]; !ok {
		return nil, fmt.Errorf("not found rule-set: %s", domainSetName)
	} else {
		switch rp.Behavior() {
		case P.IPCIDR:
			return nil, fmt.Errorf("rule provider type error, expect domain,actual %s", rp.Behavior())
		case P.Classical:
			log.Warnln("%s provider is %s, only matching it contain domain rule", rp.Name(), rp.Behavior())
		default:
		}
	}
	return RP.NewRuleSet(domainSetName, adapterName, false, true)
}
