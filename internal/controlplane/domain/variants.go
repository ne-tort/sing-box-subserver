//go:build with_controlplane

package domain

// VariantFieldScope classifies where a field must be materialized.
type VariantFieldScope string

const (
	FieldScopeUserSymmetric VariantFieldScope = "user_symmetric"
	FieldScopePeerSymmetric VariantFieldScope = "peer_symmetric"
	FieldScopeOutboundOnly  VariantFieldScope = "outbound_only"
)

// UserVariantSpec describes a symmetric client/server user-level variant.
type UserVariantSpec struct {
	Name                       string            `json:"name"`
	ProtocolFamily             string            `json:"protocol_family"`
	Scope                      VariantFieldScope `json:"scope"`
	CredentialField            string            `json:"credential_field"`
	FlowValue                  string            `json:"flow_value,omitempty"`
	RequiresUserSymmetricEntry bool              `json:"requires_user_symmetric_entry"`
	SubscriptionDefault        bool              `json:"subscription_default"`
	QueryTags                  []string          `json:"query_tags,omitempty"`
}

// ClientProfileSpec describes outbound-only overrides.
type ClientProfileSpec struct {
	Name                string            `json:"name"`
	ProtocolFamily      string            `json:"protocol_family"`
	Scope               VariantFieldScope `json:"scope"`
	SubscriptionDefault bool              `json:"subscription_default"`
	QueryTags           []string          `json:"query_tags,omitempty"`
	OutboundOverrides   map[string]any    `json:"outbound_overrides,omitempty"`
}

// SetBinding is a logical set binding that may produce several physical
// inbound users and subscription outbounds during expansion.
type SetBinding struct {
	Preset                   string            `json:"preset"`
	SubscriptionTags         []string          `json:"subscription_tags,omitempty"`
	EnabledUserVariants      []string          `json:"enabled_user_variants,omitempty"`
	EnabledClientProfiles    []string          `json:"enabled_client_profiles,omitempty"`
	CredentialInstancePolicy string            `json:"credential_instance_policy,omitempty"`
	// Params are operator-owned per-binding knobs (e.g. carrier link.room URL).
	// Materialize substitutes {{param.<key>}} in inbound/outbound templates.
	Params map[string]string `json:"params,omitempty"`
}

// VariantCatalogProvider supplies protocol-scoped variant/profile catalogs.
// SQLite-owned protocols (vless, vmess, …) register via SetVariantCatalogProvider.
type VariantCatalogProvider interface {
	UserVariants(protocol string) ([]UserVariantSpec, bool)
	ClientProfiles(protocol string) ([]ClientProfileSpec, bool)
}

var variantCatalogProvider VariantCatalogProvider

// SetVariantCatalogProvider installs the catalog backend (called from catalogsqlite).
func SetVariantCatalogProvider(p VariantCatalogProvider) {
	variantCatalogProvider = p
}

// UserVariantCatalog returns the full user-variant catalog for a protocol.
func UserVariantCatalog(protocol string) []UserVariantSpec {
	if p := variantCatalogProvider; p != nil {
		if out, ok := p.UserVariants(protocol); ok {
			return out
		}
	}
	return nil
}

// ClientProfileCatalog returns the full client-profile catalog for a protocol.
func ClientProfileCatalog(protocol string) []ClientProfileSpec {
	if p := variantCatalogProvider; p != nil {
		if out, ok := p.ClientProfiles(protocol); ok {
			return out
		}
	}
	return nil
}

// UserVariantsForProtocol returns enabled user variants for a binding.
// When binding.EnabledUserVariants is empty, presetDefaults (from the invariant)
// are used; if those are also empty, SubscriptionDefault entries are used.
// Unknown enabled names fall back to SubscriptionDefault entries.
func UserVariantsForProtocol(protocol string, b SetBinding, presetDefaults []string) []UserVariantSpec {
	catalog := UserVariantCatalog(protocol)
	if len(catalog) == 0 {
		return nil
	}
	enabled := b.EnabledUserVariants
	if len(enabled) == 0 && len(presetDefaults) > 0 {
		enabled = presetDefaults
	}
	return resolveEnabledUserVariants(enabled, catalog)
}

// ClientProfilesForProtocol returns outbound-only profiles for a binding.
// Empty enabled + empty presetDefaults → SubscriptionDefault entries only.
// Empty enabled + presetDefaults → those names. Explicit enabled wins.
func ClientProfilesForProtocol(protocol string, b SetBinding, presetDefaults []string) []ClientProfileSpec {
	catalog := ClientProfileCatalog(protocol)
	if len(catalog) == 0 {
		return nil
	}
	enabled := b.EnabledClientProfiles
	if len(enabled) == 0 {
		if len(presetDefaults) > 0 {
			enabled = presetDefaults
		} else {
			out := make([]ClientProfileSpec, 0, len(catalog))
			for _, cp := range catalog {
				if cp.SubscriptionDefault {
					out = append(out, cp)
				}
			}
			return out
		}
	}
	return resolveEnabledClientProfiles(enabled, catalog)
}

func resolveEnabledUserVariants(enabled []string, catalog []UserVariantSpec) []UserVariantSpec {
	if len(catalog) == 0 {
		return nil
	}
	if len(enabled) == 0 {
		// Mirror ClientProfilesForProtocol: empty → SubscriptionDefault only.
		out := make([]UserVariantSpec, 0, len(catalog))
		for _, vv := range catalog {
			if vv.SubscriptionDefault {
				out = append(out, vv)
			}
		}
		return out
	}
	seen := map[string]struct{}{}
	out := make([]UserVariantSpec, 0, len(enabled))
	for _, n := range enabled {
		for _, vv := range catalog {
			if vv.Name != n {
				continue
			}
			if _, ok := seen[n]; ok {
				break
			}
			seen[n] = struct{}{}
			out = append(out, vv)
			break
		}
	}
	if len(out) == 0 {
		out = make([]UserVariantSpec, 0, len(catalog))
		for _, vv := range catalog {
			if vv.SubscriptionDefault {
				out = append(out, vv)
			}
		}
		if len(out) == 0 {
			out = make([]UserVariantSpec, len(catalog))
			copy(out, catalog)
		}
	}
	return out
}

func resolveEnabledClientProfiles(enabled []string, catalog []ClientProfileSpec) []ClientProfileSpec {
	if len(catalog) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]ClientProfileSpec, 0, len(enabled))
	for _, n := range enabled {
		for _, cp := range catalog {
			if cp.Name != n {
				continue
			}
			if _, ok := seen[n]; ok {
				break
			}
			seen[n] = struct{}{}
			out = append(out, cp)
			break
		}
	}
	if len(out) == 0 {
		for _, cp := range catalog {
			if cp.SubscriptionDefault {
				out = append(out, cp)
			}
		}
	}
	return out
}

// ApplyOutboundOverrides merges profile overrides into an outbound map.
// A nil override value deletes the key.
func ApplyOutboundOverrides(ob map[string]any, overrides map[string]any) {
	if ob == nil || len(overrides) == 0 {
		return
	}
	for k, v := range overrides {
		if v == nil {
			delete(ob, k)
			continue
		}
		ob[k] = v
	}
}

// IsKnownClientProfile reports whether name exists in the protocol profile catalog.
func IsKnownClientProfile(protocol, name string) bool {
	for _, cp := range ClientProfileCatalog(protocol) {
		if cp.Name == name {
			return true
		}
	}
	return false
}
