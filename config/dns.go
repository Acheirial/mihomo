package config

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strings"

	"github.com/metacubex/mihomo/common/orderedmap"
	"github.com/metacubex/mihomo/common/utils"
	"github.com/metacubex/mihomo/component/cidr"
	"github.com/metacubex/mihomo/component/fakeip"
	"github.com/metacubex/mihomo/component/trie"
	C "github.com/metacubex/mihomo/constant"
	P "github.com/metacubex/mihomo/constant/provider"
	"github.com/metacubex/mihomo/dns"
	"github.com/metacubex/mihomo/log"
	RC "github.com/metacubex/mihomo/rules/common"

	"golang.org/x/exp/slices"
)

func parseNameServer(servers []string, respectRules bool, preferH3 bool) ([]dns.NameServer, error) {
	var nameservers []dns.NameServer

	for idx, server := range servers {
		server = parsePureDNSServer(server)
		u, err := url.Parse(server)
		if err != nil {
			return nil, fmt.Errorf("DNS NameServer[%d] format error: %s", idx, err.Error())
		}

		var proxyName string
		params := map[string]string{}
		for _, s := range strings.Split(u.Fragment, "&") {
			arr := strings.SplitN(s, "=", 2)
			switch len(arr) {
			case 1:
				proxyName = arr[0]
			case 2:
				params[arr[0]] = arr[1]
			}
		}

		var addr, dnsNetType string
		switch u.Scheme {
		case "udp":
			addr, err = hostWithDefaultPort(u.Host, "53")
			dnsNetType = "" // UDP
		case "tcp":
			addr, err = hostWithDefaultPort(u.Host, "53")
			dnsNetType = "tcp" // TCP
		case "tls":
			addr, err = hostWithDefaultPort(u.Host, "853")
			dnsNetType = "tls" // DNS over TLS
		case "http", "https":
			addr, err = hostWithDefaultPort(u.Host, "443")
			dnsNetType = "https" // DNS over HTTPS
			if u.Scheme == "http" {
				addr, err = hostWithDefaultPort(u.Host, "80")
			}
			if err == nil {
				clearURL := url.URL{Scheme: u.Scheme, Host: addr, Path: u.Path, User: u.User}
				addr = clearURL.String()
			}
		case "quic":
			addr, err = hostWithDefaultPort(u.Host, "853")
			dnsNetType = "quic" // DNS over QUIC
		case "system":
			dnsNetType = "system" // System DNS
		case "ts", "tailscale":
			addr = u.Host
			dnsNetType = "tailscale" // Tailscale DNS via proxy name
			if addr == "" {
				err = errors.New("missing Tailscale proxy name")
			}
		case "et", "easytier":
			addr = u.Host
			dnsNetType = "easytier" // EasyTier overlay DNS via proxy name
			if addr == "" {
				err = errors.New("missing EasyTier proxy name")
			}
		case "dhcp":
			addr = server[len("dhcp://"):] // some special notation cannot be parsed by url
			dnsNetType = "dhcp"            // UDP from DHCP
			if addr == "system" {          // Compatible with old writing "dhcp://system"
				dnsNetType = "system"
				addr = ""
			}
		case "rcode":
			dnsNetType = "rcode"
			addr = u.Host
			switch addr {
			case "success",
				"format_error",
				"server_failure",
				"name_error",
				"not_implemented",
				"refused":
			default:
				err = fmt.Errorf("unsupported RCode type: %s", addr)
			}
		default:
			return nil, fmt.Errorf("DNS NameServer[%d] unsupport scheme: %s", idx, u.Scheme)
		}

		if err != nil {
			return nil, fmt.Errorf("DNS NameServer[%d] format error: %s", idx, err.Error())
		}

		if respectRules && len(proxyName) == 0 {
			proxyName = dns.RespectRules
		}

		nameserver := dns.NameServer{
			Net:       dnsNetType,
			Addr:      addr,
			ProxyName: proxyName,
			Params:    params,
			PreferH3:  preferH3,
		}
		if slices.ContainsFunc(nameservers, nameserver.Equal) {
			continue // skip duplicates nameserver
		}

		nameservers = append(nameservers, nameserver)
	}
	return nameservers, nil
}

func init() {
	dns.ParseNameServer = func(servers []string) ([]dns.NameServer, error) { // using by wireguard
		return parseNameServer(servers, false, false)
	}
}

func parsePureDNSServer(server string) string {
	addPre := func(server string) string {
		return "udp://" + server
	}

	if server == "system" {
		return "system://"
	}

	if ip, err := netip.ParseAddr(server); err != nil {
		if strings.Contains(server, "://") {
			return server
		}
		return addPre(server)
	} else {
		if ip.Is4() {
			return addPre(server)
		} else {
			return addPre("[" + server + "]")
		}
	}
}

func parseNameServerPolicy(nsPolicy *orderedmap.OrderedMap[string, any], adapterName string, ruleProviders map[string]P.RuleProvider, respectRules bool, preferH3 bool) ([]dns.Policy, error) {
	var policy []dns.Policy

	for pair := nsPolicy.Oldest(); pair != nil; pair = pair.Next() {
		k, v := pair.Key, pair.Value
		servers, err := utils.ToStringSlice(v)
		if err != nil {
			return nil, err
		}
		nameservers, err := parseNameServer(servers, respectRules, preferH3)
		if err != nil {
			return nil, err
		}
		kLower := strings.ToLower(k)
		if strings.Contains(kLower, ",") {
			if strings.HasPrefix(kLower, "geosite:") {
				subkeys := strings.Split(k, ":")
				subkeys = subkeys[1:]
				subkeys = strings.Split(subkeys[0], ",")
				for _, subkey := range subkeys {
					newKey := "geosite:" + subkey
					policy = append(policy, dns.Policy{Domain: newKey, NameServers: nameservers})
				}
			} else if strings.HasPrefix(kLower, "rule-set:") {
				subkeys := strings.Split(k, ":")
				subkeys = subkeys[1:]
				subkeys = strings.Split(subkeys[0], ",")
				for _, subkey := range subkeys {
					newKey := "rule-set:" + subkey
					policy = append(policy, dns.Policy{Domain: newKey, NameServers: nameservers})
				}
			} else {
				subkeys := strings.Split(k, ",")
				for _, subkey := range subkeys {
					policy = append(policy, dns.Policy{Domain: subkey, NameServers: nameservers})
				}
			}
		} else {
			if strings.HasPrefix(kLower, "geosite:") {
				policy = append(policy, dns.Policy{Domain: "geosite:" + k[8:], NameServers: nameservers})
			} else if strings.HasPrefix(kLower, "rule-set:") {
				policy = append(policy, dns.Policy{Domain: "rule-set:" + k[9:], NameServers: nameservers})
			} else {
				policy = append(policy, dns.Policy{Domain: k, NameServers: nameservers})
			}
		}
	}

	for idx, p := range policy {
		domain, nameservers := p.Domain, p.NameServers

		if strings.HasPrefix(domain, "rule-set:") {
			domainSetName := domain[9:]
			matcher, err := parseDomainRuleSet(domainSetName, adapterName, ruleProviders)
			if err != nil {
				return nil, err
			}
			policy[idx] = dns.Policy{Matcher: matcher, NameServers: nameservers}
		} else if strings.HasPrefix(domain, "geosite:") {
			country := domain[8:]
			matcher, err := RC.NewGEOSITE(country, adapterName)
			if err != nil {
				return nil, err
			}
			policy[idx] = dns.Policy{Matcher: matcher, NameServers: nameservers}
		} else {
			if _, err := trie.ValidAndSplitDomain(domain); err != nil {
				return nil, fmt.Errorf("%s[%d]: %w", adapterName, idx, err)
			}
		}
	}

	return policy, nil
}

func parseDNS(rawCfg *RawConfig, ruleProviders map[string]P.RuleProvider) (*DNS, error) {
	cfg := rawCfg.DNS
	if cfg.Enable && len(cfg.NameServer) == 0 {
		return nil, fmt.Errorf("if DNS configuration is turned on, NameServer cannot be empty")
	}

	if cfg.RespectRules && len(cfg.ProxyServerNameserver) == 0 {
		return nil, fmt.Errorf("if “respect-rules” is turned on, “proxy-server-nameserver” cannot be empty")
	}

	dnsCfg := &DNS{
		Enable:            cfg.Enable,
		Listen:            cfg.Listen,
		ListenRoutingMark: cfg.ListenRoutingMark,
		PreferH3:          cfg.PreferH3,
		IPv6Timeout:       cfg.IPv6Timeout,
		IPv6:              cfg.IPv6,
		UseHosts:          cfg.UseHosts,
		UseSystemHosts:    cfg.UseSystemHosts,
		EnhancedMode:      cfg.EnhancedMode,
		CacheAlgorithm:    cfg.CacheAlgorithm,
		CacheMaxSize:      cfg.CacheMaxSize,
	}
	var err error
	if dnsCfg.NameServer, err = parseNameServer(cfg.NameServer, cfg.RespectRules, cfg.PreferH3); err != nil {
		return nil, err
	}

	if dnsCfg.Fallback, err = parseNameServer(cfg.Fallback, cfg.RespectRules, cfg.PreferH3); err != nil {
		return nil, err
	}

	if dnsCfg.NameServerPolicy, err = parseNameServerPolicy(cfg.NameServerPolicy, "dns.nameserver-policy", ruleProviders, cfg.RespectRules, cfg.PreferH3); err != nil {
		return nil, err
	}

	if dnsCfg.ProxyServerNameserver, err = parseNameServer(cfg.ProxyServerNameserver, false, cfg.PreferH3); err != nil {
		return nil, err
	}

	if dnsCfg.ProxyServerPolicy, err = parseNameServerPolicy(cfg.ProxyServerNameserverPolicy, "dns.proxy-server-nameserver-policy", ruleProviders, false, cfg.PreferH3); err != nil {
		return nil, err
	}
	if len(dnsCfg.ProxyServerPolicy) != 0 && len(dnsCfg.ProxyServerNameserver) == 0 {
		return nil, errors.New("disallow empty `proxy-server-nameserver` when `proxy-server-nameserver-policy` is set")
	}

	if dnsCfg.DirectNameServer, err = parseNameServer(cfg.DirectNameServer, false, cfg.PreferH3); err != nil {
		return nil, err
	}
	dnsCfg.DirectFollowPolicy = cfg.DirectNameServerFollowPolicy

	if len(cfg.DefaultNameserver) == 0 {
		return nil, errors.New("default nameserver should have at least one nameserver")
	}
	if dnsCfg.DefaultNameserver, err = parseNameServer(cfg.DefaultNameserver, false, cfg.PreferH3); err != nil {
		return nil, err
	}
	// check default nameserver is pure ip addr
	for _, ns := range dnsCfg.DefaultNameserver {
		if ns.Net == "system" {
			continue
		}
		host, _, err := net.SplitHostPort(ns.Addr)
		if err != nil || net.ParseIP(host) == nil {
			u, err := url.Parse(ns.Addr)
			if err == nil && net.ParseIP(u.Host) == nil {
				if ip, _, err := net.SplitHostPort(u.Host); err != nil || net.ParseIP(ip) == nil {
					return nil, errors.New("default nameserver should be pure IP")
				}
			}
		}
	}

	if cfg.FakeIPRange != "" {
		dnsCfg.FakeIPRange, err = netip.ParsePrefix(cfg.FakeIPRange)
		if err != nil {
			return nil, err
		}
		if !dnsCfg.FakeIPRange.Addr().Is4() {
			return nil, errors.New("dns.fake-ip-range must be a IPv4 prefix")
		}
	}

	if cfg.FakeIPRange6 != "" {
		dnsCfg.FakeIPRange6, err = netip.ParsePrefix(cfg.FakeIPRange6)
		if err != nil {
			return nil, err
		}
		if !dnsCfg.FakeIPRange6.Addr().Is6() {
			return nil, errors.New("dns.fake-ip-range6 must be a IPv6 prefix")
		}
	}

	if cfg.EnhancedMode == C.DNSFakeIP {
		var fakeIPDomainSetBuilder *trie.DomainSetBuilder
		if cfg.FakeIPFilterMode != C.FilterRule && len(dnsCfg.Fallback) != 0 {
			fakeIPDomainSetBuilder = &trie.DomainSetBuilder{}
			for _, fb := range dnsCfg.Fallback {
				if net.ParseIP(fb.Addr) != nil {
					continue
				}
				if err := fakeIPDomainSetBuilder.Insert(fb.Addr); err != nil {
					log.Warnln("skip fallback nameserver in fake-ip filter: %s", err)
				}
			}
		}

		skipper := &fakeip.Skipper{Mode: cfg.FakeIPFilterMode}

		if cfg.FakeIPFilterMode == C.FilterRule {
			rules, err := parseFakeIPRules(cfg.FakeIPFilter, ruleProviders)
			if err != nil {
				return nil, err
			}
			skipper.Rules = rules
		} else {
			host, err := parseDomain(cfg.FakeIPFilter, fakeIPDomainSetBuilder, "dns.fake-ip-filter", ruleProviders)
			if err != nil {
				return nil, err
			}
			skipper.Host = host
		}

		dnsCfg.FakeIPSkipper = skipper
		dnsCfg.FakeIPTTL = cfg.FakeIPTTL

		if dnsCfg.FakeIPRange.IsValid() {
			pool, err := fakeip.New(fakeip.Options{
				IPNet:       dnsCfg.FakeIPRange,
				Size:        1000,
				Persistence: rawCfg.Profile.StoreFakeIP,
			})
			if err != nil {
				return nil, err
			}
			dnsCfg.FakeIPPool = pool
		}

		if dnsCfg.FakeIPRange6.IsValid() {
			pool6, err := fakeip.New(fakeip.Options{
				IPNet:       dnsCfg.FakeIPRange6,
				Size:        1000,
				Persistence: rawCfg.Profile.StoreFakeIP,
			})
			if err != nil {
				return nil, err
			}
			dnsCfg.FakeIPPool6 = pool6
		}

		if dnsCfg.FakeIPPool == nil && dnsCfg.FakeIPPool6 == nil {
			return nil, errors.New("disallow `fake-ip-range` and `fake-ip-range6` both empty with fake-ip mode")
		}
	}

	if len(cfg.Fallback) != 0 {
		if cfg.FallbackFilter.GeoIP {
			matcher, err := RC.NewGEOIP(cfg.FallbackFilter.GeoIPCode, "dns.fallback-filter.geoip", false, true)
			if err != nil {
				return nil, fmt.Errorf("load GeoIP dns fallback filter error, %w", err)
			}
			dnsCfg.FallbackIPFilter = append(dnsCfg.FallbackIPFilter, matcher.DnsFallbackFilter())
		}
		if len(cfg.FallbackFilter.IPCIDR) > 0 {
			cidrSet := cidr.NewIpCidrSet()
			for idx, ipcidr := range cfg.FallbackFilter.IPCIDR {
				err = cidrSet.AddIpCidrForString(ipcidr)
				if err != nil {
					return nil, fmt.Errorf("DNS FallbackIP[%d] format error: %w", idx, err)
				}
			}
			err = cidrSet.Merge()
			if err != nil {
				return nil, err
			}
			matcher := cidrSet // dns.fallback-filter.ipcidr
			dnsCfg.FallbackIPFilter = append(dnsCfg.FallbackIPFilter, matcher)
		}
		if len(cfg.FallbackFilter.Domain) > 0 {
			var domainSetBuilder trie.DomainSetBuilder
			for idx, domain := range cfg.FallbackFilter.Domain {
				err = domainSetBuilder.Insert(domain)
				if err != nil {
					return nil, fmt.Errorf("DNS FallbackDomain[%d] format error: %w", idx, err)
				}
			}
			matcher := domainSetBuilder.Build() // dns.fallback-filter.domain
			dnsCfg.FallbackDomainFilter = append(dnsCfg.FallbackDomainFilter, matcher)
		}
		if len(cfg.FallbackFilter.GeoSite) > 0 {
			log.Warnln("replace fallback-filter.geosite with nameserver-policy, it will be removed in the future")
			for idx, geoSite := range cfg.FallbackFilter.GeoSite {
				matcher, err := RC.NewGEOSITE(geoSite, "dns.fallback-filter.geosite")
				if err != nil {
					return nil, fmt.Errorf("DNS FallbackGeosite[%d] format error: %w", idx, err)
				}
				dnsCfg.FallbackDomainFilter = append(dnsCfg.FallbackDomainFilter, matcher)
			}
		}
		dnsCfg.FallbackLazyQuery = cfg.FallbackLazyQuery
	}

	return dnsCfg, nil
}
