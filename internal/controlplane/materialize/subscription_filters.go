//go:build with_controlplane

package materialize

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/presets"
)

// SubscriptionCatalog lists filter values valid for the current active sets.
type SubscriptionCatalog struct {
	Sets     map[string]struct{}
	Presets  map[string]struct{}
	Variants map[string]struct{}
	Tags     map[string]struct{}
	Profiles map[string]struct{}
}

// BuildSubscriptionCatalog indexes discoverable subscription filters from active sets.
func BuildSubscriptionCatalog(sets []domain.InboundSet) SubscriptionCatalog {
	cat := SubscriptionCatalog{
		Sets:     map[string]struct{}{},
		Presets:  map[string]struct{}{},
		Variants: map[string]struct{}{},
		Tags:     map[string]struct{}{},
		Profiles: map[string]struct{}{},
	}
	for _, set := range sets {
		cat.Sets[set.Name] = struct{}{}
		for _, b := range set.EffectiveBindings() {
			if b.Preset != "" {
				cat.Presets[b.Preset] = struct{}{}
			}
			for _, p := range b.EnabledClientProfiles {
				cat.Profiles[p] = struct{}{}
			}
			for _, t := range b.SubscriptionTags {
				cat.Tags[t] = struct{}{}
			}
			p, err := presets.Get(b.Preset)
			if err != nil {
				continue
			}
			for _, vv := range domain.UserVariantsForProtocol(p.Protocol, b) {
				cat.Variants[vv.Name] = struct{}{}
				for _, t := range vv.QueryTags {
					cat.Tags[t] = struct{}{}
				}
			}
		}
	}
	return cat
}

// Validate checks strict subscription filters against the catalog.
func (cat SubscriptionCatalog) Validate(filters SubscriptionFilters) error {
	if filters.Set != "" {
		if _, ok := cat.Sets[filters.Set]; !ok {
			return fmt.Errorf("unknown set %q", filters.Set)
		}
	}
	for _, p := range filters.Presets {
		if _, ok := cat.Presets[p]; !ok {
			return fmt.Errorf("unknown preset %q", p)
		}
	}
	for _, v := range filters.Variants {
		if _, ok := cat.Variants[v]; !ok {
			return fmt.Errorf("unknown variant %q", v)
		}
	}
	for _, t := range filters.Tags {
		if _, ok := cat.Tags[t]; !ok {
			return fmt.Errorf("unknown tag %q", t)
		}
	}
	for _, p := range filters.Profiles {
		if _, ok := cat.Profiles[p]; !ok {
			return fmt.Errorf("unknown profile %q", p)
		}
	}
	for _, f := range filters.Flow {
		if !allowedFlowFilter(f) {
			return fmt.Errorf("unknown flow %q", f)
		}
	}
	if filters.Network != "" && filters.Network != "tcp" && filters.Network != "udp" {
		return fmt.Errorf("unknown network %q", filters.Network)
	}
	return nil
}

func allowedFlowFilter(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "none", "tcp", "xtls", "xtls-rprx-vision", "udp-vision", "xtls-rprx-vision-udp443":
		return true
	default:
		return false
	}
}

func sortOutboundsByTag(outbounds []any) {
	sort.SliceStable(outbounds, func(i, j int) bool {
		a, _ := outbounds[i].(map[string]any)
		b, _ := outbounds[j].(map[string]any)
		return fmt.Sprint(a["tag"]) < fmt.Sprint(b["tag"])
	})
}
