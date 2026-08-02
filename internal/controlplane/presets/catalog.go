//go:build with_controlplane

package presets

import (
	"fmt"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/catalogsqlite"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
)

// AllInvariants returns raw invariant JSON entries (including planned skipped? all loaded).
// VLESS pilot entries come from catalogsqlite, not the JSON loader.
func AllInvariants() []domain.InvariantPreset {
	ensureLoaded()
	mustOK()
	out := make([]domain.InvariantPreset, 0, len(invariants))
	for _, inv := range invariants {
		if catalogsqlite.OwnsProtocol(inv.Protocol) {
			continue
		}
		out = append(out, inv)
	}
	if sq, err := catalogsqlite.AllPresets(); err == nil {
		for _, p := range sq {
			if inv, err := catalogsqlite.GetInvariant(p.Name); err == nil {
				out = append(out, inv)
			}
		}
	}
	return out
}

// All returns stable+lab invariants as ProtocolPreset (compat), lang=ru descriptions.
// Planned/deferred presets are omitted from the catalog (not materialize-listed).
// VLESS is served exclusively from catalogsqlite.
func All() []domain.ProtocolPreset {
	ensureLoaded()
	mustOK()
	out := make([]domain.ProtocolPreset, 0, len(invariants))
	for _, inv := range invariants {
		if catalogsqlite.OwnsProtocol(inv.Protocol) {
			continue
		}
		if inv.Status == "planned" || inv.Status == "deferred" {
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
	if sq, err := catalogsqlite.AllPresets(); err == nil {
		for _, p := range sq {
			if p.Status == "planned" || p.Status == "deferred" {
				continue
			}
			out = append(out, p)
		}
	}
	return out
}

// Get resolves canonical tag or alias to a ProtocolPreset (ru description).
func Get(name string) (domain.ProtocolPreset, error) {
	if catalogsqlite.Owns(name) {
		inv, err := catalogsqlite.GetInvariant(name)
		if err != nil {
			return domain.ProtocolPreset{}, err
		}
		return inv.ToProtocolPreset("ru"), nil
	}
	ensureLoaded()
	mustOK()
	inv, ok := invariantBy[name]
	if !ok {
		return domain.ProtocolPreset{}, fmt.Errorf("unknown preset %q", name)
	}
	if catalogsqlite.OwnsProtocol(inv.Protocol) {
		return domain.ProtocolPreset{}, fmt.Errorf("unknown preset %q (protocol moved to catalogsqlite)", name)
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
	if catalogsqlite.Owns(name) {
		return catalogsqlite.GetInvariant(name)
	}
	ensureLoaded()
	mustOK()
	inv, ok := invariantBy[name]
	if !ok {
		return domain.InvariantPreset{}, fmt.Errorf("unknown preset %q", name)
	}
	if catalogsqlite.OwnsProtocol(inv.Protocol) {
		return domain.InvariantPreset{}, fmt.Errorf("unknown preset %q (protocol moved to catalogsqlite)", name)
	}
	return inv, nil
}

// CanonicalTag maps alias or tag → canonical tag.
func CanonicalTag(name string) (string, bool) {
	if t, ok := catalogsqlite.CanonicalTag(name); ok {
		return t, true
	}
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
	if catalogsqlite.Owns(canonical) {
		inv, err := catalogsqlite.GetInvariant(canonical)
		if err != nil {
			return nil
		}
		return append([]string{}, inv.Aliases...)
	}
	ensureLoaded()
	mustOK()
	inv, ok := invariantBy[canonical]
	if !ok {
		return nil
	}
	return append([]string{}, inv.Aliases...)
}

// Protocols returns protocol metadata in index order (planned/deferred omitted).
// Catalogsqlite-owned protocols (VLESS pilot) are injected even if removed from JSON index.
func Protocols() []domain.ProtocolMeta {
	ensureLoaded()
	mustOK()
	out := make([]domain.ProtocolMeta, 0, len(protocols)+1)
	seen := map[string]struct{}{}
	for _, p := range protocols {
		if p.Status == "planned" || p.Status == "deferred" {
			continue
		}
		if catalogsqlite.OwnsProtocol(p.Tag) {
			if sq, err := catalogsqlite.GetProtocol(p.Tag); err == nil {
				out = append(out, sq)
				seen[p.Tag] = struct{}{}
			}
			continue
		}
		out = append(out, p)
		seen[p.Tag] = struct{}{}
	}
	// Ensure cut-over protocols still appear after JSON removal.
	if catalogsqlite.OwnsProtocol("vless") {
		if _, ok := seen["vless"]; !ok {
			if sq, err := catalogsqlite.GetProtocol("vless"); err == nil {
				// Keep vless near the front for UI stability.
				out = append([]domain.ProtocolMeta{sq}, out...)
			}
		}
	}
	return out
}

// GetProtocol returns protocol metadata by tag.
func GetProtocol(tag string) (domain.ProtocolMeta, error) {
	if catalogsqlite.OwnsProtocol(tag) {
		return catalogsqlite.GetProtocol(tag)
	}
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
	if catalogsqlite.Owns(presetOrAlias) {
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
