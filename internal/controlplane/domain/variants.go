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
	Preset                   string   `json:"preset"`
	SubscriptionTags         []string `json:"subscription_tags,omitempty"`
	EnabledUserVariants      []string `json:"enabled_user_variants,omitempty"`
	EnabledClientProfiles    []string `json:"enabled_client_profiles,omitempty"`
	CredentialInstancePolicy string   `json:"credential_instance_policy,omitempty"`
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
		// lx branch supports this VLESS flow mode.
		FlowValue:                  "xtls-rprx-vision-udp443",
		RequiresUserSymmetricEntry: true,
		SubscriptionDefault:        true,
		QueryTags:                  []string{"variant:flow-udp-vision", "flow:udp-vision", "tag:flow-udp"},
	},
}

