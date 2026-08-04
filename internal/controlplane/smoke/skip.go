//go:build with_controlplane

package smoke

import (
	"fmt"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/presets"
)

// SkipReason explains why a preset cannot be hairpin-smoke tested.
func SkipReason(preset string) string {
	p, err := presets.Get(preset)
	if err != nil {
		return "unknown_preset"
	}
	if len(p.OutboundTemplate) == 0 {
		return "no_outbound_template"
	}
	for _, t := range p.Traits {
		if t == "inbound_only" {
			return "inbound_only"
		}
	}
	switch p.Protocol {
	case "carrier":
		return "carrier_no_hairpin"
	case "cloudflared":
		return "cloudflared_no_hairpin"
	case "wireguard":
		return "wireguard_endpoint"
	}
	return ""
}

// InboundTagFor builds the materialize inbound tag.
func InboundTagFor(setName, preset string) string {
	return fmt.Sprintf("cp-in-%s-%s", setName, preset)
}
