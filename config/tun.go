package config

import (
	"net/netip"
	"strings"

	"github.com/metacubex/mihomo/component/auth"
	LC "github.com/metacubex/mihomo/listener/config"
)

func parseIPV6(rawCfg *RawConfig) {
	if !rawCfg.IPv6 || !verifyIP6() {
		rawCfg.DNS.FakeIPRange6 = ""
		rawCfg.Tun.Inet6Address = nil
	}
}

func parseTun(rawTun RawTun, dns *DNS, general *General) error {
	tunAddressPrefix := dns.FakeIPRange
	if !tunAddressPrefix.IsValid() {
		tunAddressPrefix = netip.MustParsePrefix("198.18.0.1/16")
	}
	tunAddressPrefix = netip.PrefixFrom(tunAddressPrefix.Addr(), 30)

	general.Tun = LC.Tun{
		Enable:              rawTun.Enable,
		Device:              rawTun.Device,
		Stack:               rawTun.Stack,
		DNSHijack:           rawTun.DNSHijack,
		AutoRoute:           rawTun.AutoRoute,
		AutoDetectInterface: rawTun.AutoDetectInterface,

		MTU:                                   rawTun.MTU,
		GSO:                                   rawTun.GSO,
		GSOMaxSize:                            rawTun.GSOMaxSize,
		Inet4Address:                          []netip.Prefix{tunAddressPrefix},
		Inet6Address:                          rawTun.Inet6Address,
		IPRoute2TableIndex:                    rawTun.IPRoute2TableIndex,
		IPRoute2RuleIndex:                     rawTun.IPRoute2RuleIndex,
		AutoRedirect:                          rawTun.AutoRedirect,
		AutoRedirectInputMark:                 rawTun.AutoRedirectInputMark,
		AutoRedirectOutputMark:                rawTun.AutoRedirectOutputMark,
		AutoRedirectIPRoute2FallbackRuleIndex: rawTun.AutoRedirectIPRoute2FallbackRuleIndex,
		LoopbackAddress:                       rawTun.LoopbackAddress,
		StrictRoute:                           rawTun.StrictRoute,
		RouteAddress:                          rawTun.RouteAddress,
		RouteAddressSet:                       rawTun.RouteAddressSet,
		RouteExcludeAddress:                   rawTun.RouteExcludeAddress,
		RouteExcludeAddressSet:                rawTun.RouteExcludeAddressSet,
		IncludeInterface:                      rawTun.IncludeInterface,
		ExcludeInterface:                      rawTun.ExcludeInterface,
		IncludeUID:                            rawTun.IncludeUID,
		IncludeUIDRange:                       rawTun.IncludeUIDRange,
		ExcludeUID:                            rawTun.ExcludeUID,
		ExcludeUIDRange:                       rawTun.ExcludeUIDRange,
		ExcludeSrcPort:                        rawTun.ExcludeSrcPort,
		ExcludeSrcPortRange:                   rawTun.ExcludeSrcPortRange,
		ExcludeDstPort:                        rawTun.ExcludeDstPort,
		ExcludeDstPortRange:                   rawTun.ExcludeDstPortRange,
		IncludeAndroidUser:                    rawTun.IncludeAndroidUser,
		IncludePackage:                        rawTun.IncludePackage,
		ExcludePackage:                        rawTun.ExcludePackage,
		IncludeMACAddress:                     rawTun.IncludeMACAddress,
		ExcludeMACAddress:                     rawTun.ExcludeMACAddress,
		EndpointIndependentNat:                rawTun.EndpointIndependentNat,
		UDPTimeout:                            rawTun.UDPTimeout,
		ICMPTimeout:                           rawTun.ICMPTimeout,
		DisableICMPForwarding:                 rawTun.DisableICMPForwarding,
		FileDescriptor:                        rawTun.FileDescriptor,

		Inet4RouteAddress:        rawTun.Inet4RouteAddress,
		Inet6RouteAddress:        rawTun.Inet6RouteAddress,
		Inet4RouteExcludeAddress: rawTun.Inet4RouteExcludeAddress,
		Inet6RouteExcludeAddress: rawTun.Inet6RouteExcludeAddress,

		RecvMsgX: rawTun.RecvMsgX,
		SendMsgX: rawTun.SendMsgX,

		ProcessorsPerChannel: rawTun.ProcessorsPerChannel,
	}

	return nil
}

func parseTuicServer(rawTuic RawTuicServer, general *General) error {
	general.TuicServer = LC.TuicServer{
		Enable:                rawTuic.Enable,
		Listen:                rawTuic.Listen,
		Token:                 rawTuic.Token,
		Users:                 rawTuic.Users,
		Certificate:           rawTuic.Certificate,
		PrivateKey:            rawTuic.PrivateKey,
		CongestionController:  rawTuic.CongestionController,
		MaxIdleTime:           rawTuic.MaxIdleTime,
		AuthenticationTimeout: rawTuic.AuthenticationTimeout,
		ALPN:                  rawTuic.ALPN,
		MaxUdpRelayPacketSize: rawTuic.MaxUdpRelayPacketSize,
		CWND:                  rawTuic.CWND,
	}
	return nil
}

func parseAuthentication(rawRecords []string) []auth.AuthUser {
	var users []auth.AuthUser
	for _, line := range rawRecords {
		if user, pass, found := strings.Cut(line, ":"); found {
			users = append(users, auth.AuthUser{User: user, Pass: pass})
		}
	}
	return users
}
