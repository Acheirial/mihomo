package config

import (
	"fmt"
	"strings"

	"github.com/metacubex/mihomo/common/utils"
	"github.com/metacubex/mihomo/component/sniffer"
	P "github.com/metacubex/mihomo/constant/provider"
	snifferTypes "github.com/metacubex/mihomo/constant/sniffer"
	"github.com/metacubex/mihomo/log"
)

func parseSniffer(snifferRaw RawSniffer, ruleProviders map[string]P.RuleProvider) (*sniffer.Config, error) {
	snifferConfig := &sniffer.Config{
		Enable:          snifferRaw.Enable,
		ForceDnsMapping: snifferRaw.ForceDnsMapping,
		ParsePureIp:     snifferRaw.ParsePureIp,
	}
	loadSniffer := make(map[snifferTypes.Type]sniffer.SnifferConfig)

	if len(snifferRaw.Sniff) != 0 {
		for sniffType, sniffConfig := range snifferRaw.Sniff {
			find := false
			ports, err := utils.NewUnsignedRangesFromList[uint16](sniffConfig.Ports)
			if err != nil {
				return nil, err
			}
			overrideDest := snifferRaw.OverrideDest
			if sniffConfig.OverrideDest != nil {
				overrideDest = *sniffConfig.OverrideDest
			}
			for _, snifferType := range snifferTypes.List {
				if snifferType.String() == strings.ToUpper(sniffType) {
					find = true
					loadSniffer[snifferType] = sniffer.SnifferConfig{
						Ports:        ports,
						OverrideDest: overrideDest,
					}
				}
			}

			if !find {
				return nil, fmt.Errorf("not find the sniffer[%s]", sniffType)
			}
		}
	} else {
		if snifferConfig.Enable && len(snifferRaw.Sniffing) != 0 {
			// Deprecated: Use Sniff instead
			log.Warnln("Deprecated: Use Sniff instead")
		}
		globalPorts, err := utils.NewUnsignedRangesFromList[uint16](snifferRaw.Ports)
		if err != nil {
			return nil, err
		}

		for _, snifferName := range snifferRaw.Sniffing {
			find := false
			for _, snifferType := range snifferTypes.List {
				if snifferType.String() == strings.ToUpper(snifferName) {
					find = true
					loadSniffer[snifferType] = sniffer.SnifferConfig{
						Ports:        globalPorts,
						OverrideDest: snifferRaw.OverrideDest,
					}
				}
			}

			if !find {
				return nil, fmt.Errorf("not find the sniffer[%s]", snifferName)
			}
		}
	}

	snifferConfig.Sniffers = loadSniffer

	forceDomain, err := parseDomain(snifferRaw.ForceDomain, nil, "sniffer.force-domain", ruleProviders)
	if err != nil {
		return nil, err
	}
	snifferConfig.ForceDomain = forceDomain

	skipSrcAddress, err := parseIPCIDR(snifferRaw.SkipSrcAddress, nil, "sniffer.skip-src-address", ruleProviders)
	if err != nil {
		return nil, fmt.Errorf("error in skip-src-address, error:%w", err)
	}
	snifferConfig.SkipSrcAddress = skipSrcAddress

	skipDstAddress, err := parseIPCIDR(snifferRaw.SkipDstAddress, nil, "sniffer.skip-dst-address", ruleProviders)
	if err != nil {
		return nil, fmt.Errorf("error in skip-dst-address, error:%w", err)
	}
	snifferConfig.SkipDstAddress = skipDstAddress

	skipDomain, err := parseDomain(snifferRaw.SkipDomain, nil, "sniffer.skip-domain", ruleProviders)
	if err != nil {
		return nil, err
	}
	snifferConfig.SkipDomain = skipDomain

	return snifferConfig, nil
}
