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

// Known vless user variants for the initial controlplane variants model.
var VLESSUserVariantSpecs = []UserVariantSpec{
	{
		Name:                       "flow-none",
		ProtocolFamily:             "vless",
		Scope:                      FieldScopeUserSymmetric,
		CredentialField:            "uuid",
		FlowValue:                  "",
		RequiresUserSymmetricEntry: true,
		SubscriptionDefault:        true,
		QueryTags:                  []string{"variant:flow-none", "flow:none", "tag:flow-none"},
	},
	{
		Name:                       "flow-xtls-rprx-vision",
		ProtocolFamily:             "vless",
		Scope:                      FieldScopeUserSymmetric,
		CredentialField:            "uuid_xtls",
		FlowValue:                  "xtls-rprx-vision",
		RequiresUserSymmetricEntry: true,
		SubscriptionDefault:        true,
		QueryTags:                  []string{"variant:flow-xtls-rprx-vision", "flow:xtls-rprx-vision", "tag:flow-xtls"},
	},
	{
		Name:                       "flow-udp-vision",
		ProtocolFamily:             "vless",
		Scope:                      FieldScopeUserSymmetric,
		CredentialField:            "uuid_udp",
		// lx branch supports this VLESS flow mode; not a subscription default
		// (official sagernet sing-box rejects xtls-rprx-vision-udp443).
		FlowValue:                  "xtls-rprx-vision-udp443",
		RequiresUserSymmetricEntry: true,
		SubscriptionDefault:        false,
		QueryTags:                  []string{"variant:flow-udp-vision", "flow:udp-vision", "tag:flow-udp"},
	},
}

// VLESSClientProfileSpecs are outbound-only packet_encoding choices.
var VLESSClientProfileSpecs = []ClientProfileSpec{
	{
		Name:                "pkt-none",
		ProtocolFamily:      "vless",
		Scope:               FieldScopeOutboundOnly,
		SubscriptionDefault: true,
		QueryTags:           []string{"profile:pkt-none", "tag:pkt-none"},
	},
	{
		Name:                "pkt-xudp",
		ProtocolFamily:      "vless",
		Scope:               FieldScopeOutboundOnly,
		SubscriptionDefault: false,
		QueryTags:           []string{"profile:pkt-xudp", "tag:pkt-xudp"},
		OutboundOverrides:   map[string]any{"packet_encoding": "xudp"},
	},
	{
		Name:                "pkt-packetaddr",
		ProtocolFamily:      "vless",
		Scope:               FieldScopeOutboundOnly,
		SubscriptionDefault: false,
		QueryTags:           []string{"profile:pkt-packetaddr", "tag:pkt-packetaddr"},
		OutboundOverrides:   map[string]any{"packet_encoding": "packetaddr"},
	},
}

// VMessClientProfileSpecs are outbound-only security / packet_encoding choices.
var VMessClientProfileSpecs = []ClientProfileSpec{
	{
		Name:                "sec-auto",
		ProtocolFamily:      "vmess",
		Scope:               FieldScopeOutboundOnly,
		SubscriptionDefault: true,
		QueryTags:           []string{"profile:sec-auto", "tag:sec-auto"},
		OutboundOverrides:   map[string]any{"security": "auto"},
	},
	{
		Name:                "sec-aes128",
		ProtocolFamily:      "vmess",
		Scope:               FieldScopeOutboundOnly,
		SubscriptionDefault: false,
		QueryTags:           []string{"profile:sec-aes128", "tag:sec-aes128"},
		OutboundOverrides:   map[string]any{"security": "aes-128-gcm"},
	},
	{
		Name:                "sec-chacha",
		ProtocolFamily:      "vmess",
		Scope:               FieldScopeOutboundOnly,
		SubscriptionDefault: false,
		QueryTags:           []string{"profile:sec-chacha", "tag:sec-chacha"},
		OutboundOverrides:   map[string]any{"security": "chacha20-poly1305"},
	},
	{
		Name:                "pkt-xudp",
		ProtocolFamily:      "vmess",
		Scope:               FieldScopeOutboundOnly,
		SubscriptionDefault: false,
		QueryTags:           []string{"profile:pkt-xudp", "tag:pkt-xudp"},
		OutboundOverrides:   map[string]any{"packet_encoding": "xudp"},
	},
}

// TUICClientProfileSpecs are outbound-only udp_relay_mode choices.
var TUICClientProfileSpecs = []ClientProfileSpec{
	{
		Name:                "udp-native",
		ProtocolFamily:      "tuic",
		Scope:               FieldScopeOutboundOnly,
		SubscriptionDefault: true,
		QueryTags:           []string{"profile:udp-native", "tag:udp-native"},
		OutboundOverrides:   map[string]any{"udp_relay_mode": "native"},
	},
	{
		Name:                "udp-quic",
		ProtocolFamily:      "tuic",
		Scope:               FieldScopeOutboundOnly,
		SubscriptionDefault: false,
		QueryTags:           []string{"profile:udp-quic", "tag:udp-quic"},
		OutboundOverrides:   map[string]any{"udp_relay_mode": "quic"},
	},
}

// UserVariantsForProtocol returns enabled user variants for a binding.
// When binding.EnabledUserVariants is empty, presetDefaults (from the invariant)
// are used; if those are also empty, the full protocol catalog is used.
// Unknown enabled names fall back to the full catalog.
func UserVariantsForProtocol(protocol string, b SetBinding, presetDefaults []string) []UserVariantSpec {
	var catalog []UserVariantSpec
	switch protocol {
	case "vless":
		catalog = VLESSUserVariantSpecs
	default:
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
	var catalog []ClientProfileSpec
	switch protocol {
	case "vless":
		catalog = VLESSClientProfileSpecs
	case "vmess":
		catalog = VMessClientProfileSpecs
	case "tuic":
		catalog = TUICClientProfileSpecs
	default:
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
	var catalog []ClientProfileSpec
	switch protocol {
	case "vless":
		catalog = VLESSClientProfileSpecs
	case "vmess":
		catalog = VMessClientProfileSpecs
	case "tuic":
		catalog = TUICClientProfileSpecs
	default:
		return false
	}
	for _, cp := range catalog {
		if cp.Name == name {
			return true
		}
	}
	return false
}
