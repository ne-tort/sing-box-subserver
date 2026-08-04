//go:build with_controlplane

package presets

import (
	"fmt"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/catalogsqlite"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
)

// AllInvariants returns every base+ready invariant from catalogsqlite.
func AllInvariants() []domain.InvariantPreset {
	sq, err := catalogsqlite.AllPresets()
	if err != nil {
		return nil
	}
	out := make([]domain.InvariantPreset, 0, len(sq))
	for _, p := range sq {
		inv, err := catalogsqlite.GetInvariant(p.Name)
		if err != nil {
			continue
		}
		out = append(out, inv)
	}
	return out
}

// All returns stable+lab invariants as ProtocolPreset (compat), lang=ru descriptions.
// Planned/deferred presets are omitted from the catalog (not materialize-listed).
func All() []domain.ProtocolPreset {
	sq, err := catalogsqlite.AllPresets()
	if err != nil {
		return nil
	}
	out := make([]domain.ProtocolPreset, 0, len(sq))
	for _, p := range sq {
		if p.Status == "planned" || p.Status == "deferred" {
			continue
		}
		endpoint := false
		for _, tr := range p.Traits {
			if tr == "endpoint" {
				endpoint = true
				break
			}
		}
		if !endpoint && len(p.InboundTemplate) == 0 {
			continue
		}
		if endpoint && len(p.EndpointTemplate) == 0 {
			continue
		}
		out = append(out, p)
	}
	return out
}

// Get resolves canonical tag or alias to a ProtocolPreset (ru description).
func Get(name string) (domain.ProtocolPreset, error) {
	inv, err := catalogsqlite.GetInvariant(name)
	if err != nil {
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
	inv, err := catalogsqlite.GetInvariant(name)
	if err != nil {
		return domain.InvariantPreset{}, fmt.Errorf("unknown preset %q", name)
	}
	return inv, nil
}

// CanonicalTag maps alias or tag → canonical tag.
func CanonicalTag(name string) (string, bool) {
	return catalogsqlite.CanonicalTag(name)
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
	inv, err := catalogsqlite.GetInvariant(canonical)
	if err != nil {
		return nil
	}
	return append([]string{}, inv.Aliases...)
}

// Protocols returns protocol metadata (planned/deferred omitted).
func Protocols() []domain.ProtocolMeta {
	tags, err := catalogsqlite.ListOwnedProtocols()
	if err != nil {
		return nil
	}
	out := make([]domain.ProtocolMeta, 0, len(tags))
	var vless *domain.ProtocolMeta
	for _, tag := range tags {
		sq, err := catalogsqlite.GetProtocol(tag)
		if err != nil {
			continue
		}
		if sq.Status == "planned" || sq.Status == "deferred" {
			continue
		}
		if tag == "vless" {
			cp := sq
			vless = &cp
			continue
		}
		out = append(out, sq)
	}
	// Keep vless near the front for UI stability.
	if vless != nil {
		out = append([]domain.ProtocolMeta{*vless}, out...)
	}
	return out
}

// GetProtocol returns protocol metadata by tag.
func GetProtocol(tag string) (domain.ProtocolMeta, error) {
	p, err := catalogsqlite.GetProtocol(tag)
	if err != nil {
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
	inv, err := catalogsqlite.GetInvariant(presetOrAlias)
	if err != nil {
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
