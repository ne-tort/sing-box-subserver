//go:build with_controlplane

package controlplane

import (
	"fmt"
	"strings"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/presets"
)

// portNetworks returns which L4 networks a set occupies on its listen_port.
func portNetworks(set domain.InboundSet) []string {
	if set.HasDemux() {
		if raw, ok := set.DemuxTemplate["network"].([]any); ok && len(raw) > 0 {
			out := make([]string, 0, len(raw))
			for _, v := range raw {
				s := strings.ToLower(strings.TrimSpace(strAny(v)))
				if s == "tcp" || s == "udp" {
					out = append(out, s)
				}
			}
			if len(out) > 0 {
				return uniqueStrings(out)
			}
		}
		return []string{"tcp", "udp"}
	}
	bindings := set.EffectiveBindings()
	if len(bindings) == 0 {
		return []string{"tcp"}
	}
	p, err := presets.Get(bindings[0].Preset)
	if err != nil {
		return []string{"tcp"}
	}
	return networksFromTraits(p.Traits)
}

func networksFromTraits(traits []string) []string {
	hasTCP, hasUDP := false, false
	for _, t := range traits {
		switch strings.ToLower(t) {
		case "tcp":
			hasTCP = true
		case "udp", "quic", "uot", "udp_over_tcp":
			hasUDP = true
		}
	}
	// QUIC-only presets often only declare udp/quic.
	if !hasTCP && !hasUDP {
		hasTCP = true
	}
	out := make([]string, 0, 2)
	if hasTCP {
		out = append(out, "tcp")
	}
	if hasUDP {
		out = append(out, "udp")
	}
	return out
}

func strAny(v any) string {
	switch t := v.(type) {
	case string:
		return t
	default:
		return ""
	}
}

func uniqueStrings(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// collectUsedPorts gathers public listen ports and demux member private ports.
func collectUsedPorts(sets []domain.InboundSet, extra ...uint16) map[uint16]struct{} {
	used := map[uint16]struct{}{}
	for _, p := range extra {
		if p != 0 {
			used[p] = struct{}{}
		}
	}
	for _, s := range sets {
		if s.ListenPort != 0 {
			used[s.ListenPort] = struct{}{}
		}
		for _, mp := range s.MemberPorts {
			if mp != 0 {
				used[mp] = struct{}{}
			}
		}
	}
	return used
}

// portOccupiedNetworks returns which L4 networks are already taken on port.
func portOccupiedNetworks(sets []domain.InboundSet, port uint16) map[string]struct{} {
	occ := map[string]struct{}{}
	for _, set := range sets {
		if set.ListenPort != port {
			continue
		}
		for _, n := range portNetworks(set) {
			occ[n] = struct{}{}
		}
	}
	return occ
}

// portFreeForNetworks reports whether all need networks are free on port.
func portFreeForNetworks(sets []domain.InboundSet, port uint16, need []string) bool {
	if port == 0 || len(need) == 0 {
		return false
	}
	occ := portOccupiedNetworks(sets, port)
	for _, n := range need {
		if _, ok := occ[n]; ok {
			return false
		}
	}
	return true
}

// suggestListenPort picks a public port for need networks (prefer 443, then common TLS ports).
func suggestListenPort(sets []domain.InboundSet, need []string) (uint16, error) {
	candidates := []uint16{443, 8443, 2053, 2083, 2087, 2096, 9443, 8444, 10443}
	for _, p := range candidates {
		if portFreeForNetworks(sets, p, need) {
			return p, nil
		}
	}
	for p := uint16(10000); p < 20000; p++ {
		if portFreeForNetworks(sets, p, need) {
			return p, nil
		}
	}
	return 0, fmt.Errorf("no free listen_port for networks %v", need)
}
