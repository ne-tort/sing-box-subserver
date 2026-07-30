//go:build with_controlplane

package presets

import (
	"fmt"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
)

// All returns stable+lab invariants as ProtocolPreset (compat), lang=ru descriptions.
// Planned protocols without invariants are omitted.
func All() []domain.ProtocolPreset {
	ensureLoaded()
	mustOK()
	out := make([]domain.ProtocolPreset, 0, len(invariants))
	for _, inv := range invariants {
		if inv.Status == "planned" {
			continue
		}
		endpoint := false
		for _, tr := range inv.Traits {
			if tr == "endpoint" {
				endpoint = true
				break
			}
		}
		if !endpoint && len(inv.InboundTemplate) == 0 {
			continue
		}
		if endpoint && len(inv.EndpointTemplate) == 0 {
			continue
		}
		out = append(out, inv.ToProtocolPreset("ru"))
	}
	return out
}

// Get resolves canonical tag or alias to a ProtocolPreset (ru description).
func Get(name string) (domain.ProtocolPreset, error) {
	ensureLoaded()
	mustOK()
	inv, ok := invariantBy[name]
	if !ok {
		return domain.ProtocolPreset{}, fmt.Errorf("unknown preset %q", name)
	}
	endpoint := false
	for _, tr := range inv.Traits {
		if tr == "endpoint" {
			endpoint = true
			break
		}
	}
	if endpoint {
		if len(inv.EndpointTemplate) == 0 {
			return domain.ProtocolPreset{}, fmt.Errorf("preset %q has no endpoint_template (status=%s)", inv.Tag, inv.Status)
		}
	} else if len(inv.InboundTemplate) == 0 {
		return domain.ProtocolPreset{}, fmt.Errorf("preset %q has no templates (status=%s)", inv.Tag, inv.Status)
	}
	return inv.ToProtocolPreset("ru"), nil
}

// GetInvariant returns the full invariant (canonical or alias).
func GetInvariant(name string) (domain.InvariantPreset, error) {
	ensureLoaded()
	mustOK()
	inv, ok := invariantBy[name]
	if !ok {
		return domain.InvariantPreset{}, fmt.Errorf("unknown preset %q", name)
	}
	return inv, nil
}

// CanonicalTag maps alias or tag → canonical tag.
func CanonicalTag(name string) (string, bool) {
	ensureLoaded()
	mustOK()
	t, ok := canonicalTag[name]
	return t, ok
}

// Names returns canonical tags for materialize-capable presets.
func Names() []string {
	all := All()
	names := make([]string, 0, len(all))
	for _, p := range all {
		names = append(names, p.Name)
	}
	return names
}

// AliasesOf returns aliases for a canonical tag (not including itself).
func AliasesOf(canonical string) []string {
	ensureLoaded()
	mustOK()
	inv, ok := invariantBy[canonical]
	if !ok {
		return nil
	}
	return append([]string{}, inv.Aliases...)
}

// Protocols returns protocol metadata in index order.
func Protocols() []domain.ProtocolMeta {
	ensureLoaded()
	mustOK()
	out := make([]domain.ProtocolMeta, len(protocols))
	copy(out, protocols)
	return out
}

// GetProtocol returns protocol metadata by tag.
func GetProtocol(tag string) (domain.ProtocolMeta, error) {
	ensureLoaded()
	mustOK()
	p, ok := protocolBy[tag]
	if !ok {
		return domain.ProtocolMeta{}, fmt.Errorf("unknown protocol %q", tag)
	}
	return p, nil
}

// CredsFor finds user.creds map for a preset name or any of its aliases.
func CredsFor(creds map[string]map[string]any, presetOrAlias string) map[string]any {
	if creds == nil {
		return nil
	}
	if c := creds[presetOrAlias]; c != nil {
		return c
	}
	ensureLoaded()
	mustOK()
	inv, ok := invariantBy[presetOrAlias]
	if !ok {
		return nil
	}
	if c := creds[inv.Tag]; c != nil {
		return c
	}
	for _, a := range inv.Aliases {
		if c := creds[a]; c != nil {
			return c
		}
	}
	return nil
}

// CredKeysForEnsure returns canonical tag plus aliases (for ensureCreds mirroring).
func CredKeysForEnsure(p domain.ProtocolPreset) []string {
	keys := []string{p.Name}
	keys = append(keys, p.Aliases...)
	return keys
}

func mustOK() {
	if loadErr != nil {
		panic(loadErr)
	}
}
