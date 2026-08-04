//go:build with_controlplane

package catalogsqlite

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"path"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
)

func init() {
	domain.SetVariantCatalogProvider(sqliteVariantProvider{})
}

type sqliteVariantProvider struct{}

func (sqliteVariantProvider) UserVariants(protocol string) ([]domain.UserVariantSpec, bool) {
	if !OwnsProtocol(protocol) {
		return nil, false
	}
	out, err := loadUserVariants(protocol)
	if err != nil {
		return nil, true // owned but empty/failed — do not fall through to Go hardcode
	}
	return out, true
}

func (sqliteVariantProvider) ClientProfiles(protocol string) ([]domain.ClientProfileSpec, bool) {
	if !OwnsProtocol(protocol) {
		return nil, false
	}
	out, err := loadClientProfiles(protocol)
	if err != nil {
		return nil, true
	}
	return out, true
}

type variantsSeedFile struct {
	Protocol       string                     `json:"protocol"`
	UserVariants   []userVariantSeed          `json:"user_variants"`
	ClientProfiles []clientProfileSeed        `json:"client_profiles"`
}

type userVariantSeed struct {
	Name                       string   `json:"name"`
	Scope                      string   `json:"scope"`
	CredentialField            string   `json:"credential_field"`
	FlowValue                  string   `json:"flow_value"`
	RequiresUserSymmetricEntry bool     `json:"requires_user_symmetric_entry"`
	SubscriptionDefault        bool     `json:"subscription_default"`
	QueryTags                  []string `json:"query_tags"`
}

type clientProfileSeed struct {
	Name                string         `json:"name"`
	Scope               string         `json:"scope"`
	SubscriptionDefault bool           `json:"subscription_default"`
	QueryTags           []string       `json:"query_tags"`
	OutboundOverrides   map[string]any `json:"outbound_overrides"`
}

func insertVariantsSeed(conn *sql.DB, protocol string) error {
	raw, err := catalogRefFS.ReadFile(path.Join("ref", protocol, "variants.json"))
	if err != nil {
		return fmt.Errorf("variants.json: %w", err)
	}
	var seed variantsSeedFile
	if err := json.Unmarshal(raw, &seed); err != nil {
		return fmt.Errorf("variants.json: %w", err)
	}
	if seed.Protocol != "" && seed.Protocol != protocol {
		return fmt.Errorf("variants.json protocol=%q want %q", seed.Protocol, protocol)
	}
	// Protocols may omit both catalogs (e.g. Trojan: password-only, no variants/profiles).
	// Empty is allowed; openCatalog only requires the protocol row + presets.
	for i, vv := range seed.UserVariants {
		tags, _ := json.Marshal(vv.QueryTags)
		req := 0
		if vv.RequiresUserSymmetricEntry {
			req = 1
		}
		def := 0
		if vv.SubscriptionDefault {
			def = 1
		}
		if _, err := conn.Exec(`
INSERT INTO user_variants(
  protocol,name,scope,credential_field,flow_value,
  requires_user_symmetric_entry,subscription_default,query_tags_json,sort_order
) VALUES(?,?,?,?,?,?,?,?,?)`,
			protocol, vv.Name, vv.Scope, vv.CredentialField, vv.FlowValue,
			req, def, string(tags), i); err != nil {
			return fmt.Errorf("user_variant %s: %w", vv.Name, err)
		}
	}
	for i, cp := range seed.ClientProfiles {
		tags, _ := json.Marshal(cp.QueryTags)
		ov := cp.OutboundOverrides
		if ov == nil {
			ov = map[string]any{}
		}
		ovJSON, _ := json.Marshal(ov)
		def := 0
		if cp.SubscriptionDefault {
			def = 1
		}
		if _, err := conn.Exec(`
INSERT INTO client_profiles(
  protocol,name,scope,subscription_default,query_tags_json,outbound_overrides_json,sort_order
) VALUES(?,?,?,?,?,?,?)`,
			protocol, cp.Name, cp.Scope, def, string(tags), string(ovJSON), i); err != nil {
			return fmt.Errorf("client_profile %s: %w", cp.Name, err)
		}
	}
	return nil
}

func loadUserVariants(protocol string) ([]domain.UserVariantSpec, error) {
	conn, err := DB()
	if err != nil {
		return nil, err
	}
	rows, err := conn.Query(`
SELECT name,scope,credential_field,flow_value,requires_user_symmetric_entry,
       subscription_default,query_tags_json
FROM user_variants WHERE protocol = ? ORDER BY sort_order, name`, protocol)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.UserVariantSpec
	for rows.Next() {
		var (
			name, scope, cred, flow, tagsJSON string
			req, def                          int
		)
		if err := rows.Scan(&name, &scope, &cred, &flow, &req, &def, &tagsJSON); err != nil {
			return nil, err
		}
		var tags []string
		_ = json.Unmarshal([]byte(tagsJSON), &tags)
		out = append(out, domain.UserVariantSpec{
			Name:                       name,
			ProtocolFamily:             protocol,
			Scope:                      domain.VariantFieldScope(scope),
			CredentialField:            cred,
			FlowValue:                  flow,
			RequiresUserSymmetricEntry: req != 0,
			SubscriptionDefault:        def != 0,
			QueryTags:                  tags,
		})
	}
	return out, rows.Err()
}

func loadClientProfiles(protocol string) ([]domain.ClientProfileSpec, error) {
	conn, err := DB()
	if err != nil {
		return nil, err
	}
	rows, err := conn.Query(`
SELECT name,scope,subscription_default,query_tags_json,outbound_overrides_json
FROM client_profiles WHERE protocol = ? ORDER BY sort_order, name`, protocol)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ClientProfileSpec
	for rows.Next() {
		var (
			name, scope, tagsJSON, ovJSON string
			def                           int
		)
		if err := rows.Scan(&name, &scope, &def, &tagsJSON, &ovJSON); err != nil {
			return nil, err
		}
		var tags []string
		_ = json.Unmarshal([]byte(tagsJSON), &tags)
		var ov map[string]any
		_ = json.Unmarshal([]byte(ovJSON), &ov)
		if len(ov) == 0 {
			ov = nil
		}
		out = append(out, domain.ClientProfileSpec{
			Name:                name,
			ProtocolFamily:      protocol,
			Scope:               domain.VariantFieldScope(scope),
			SubscriptionDefault: def != 0,
			QueryTags:           tags,
			OutboundOverrides:   ov,
		})
	}
	return out, rows.Err()
}
