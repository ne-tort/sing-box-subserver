//go:build with_controlplane

package catalogsqlite

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
)

const protocolVLESS = "vless"

// Owns reports whether tag/alias is served from SQLite (VLESS pilot).
func Owns(tagOrAlias string) bool {
	tagOrAlias = strings.TrimSpace(tagOrAlias)
	if tagOrAlias == "" {
		return false
	}
	conn, err := DB()
	if err != nil {
		return false
	}
	var n int
	err = conn.QueryRow(`SELECT COUNT(1) FROM aliases WHERE alias = ?`, tagOrAlias).Scan(&n)
	return err == nil && n > 0
}

// OwnsProtocol is true for protocols fully cut over to SQLite.
func OwnsProtocol(protocol string) bool {
	return strings.EqualFold(strings.TrimSpace(protocol), protocolVLESS)
}

// CanonicalTag maps alias → canonical tag when owned by SQLite.
func CanonicalTag(name string) (string, bool) {
	conn, err := DB()
	if err != nil {
		return "", false
	}
	var tag string
	err = conn.QueryRow(`SELECT canonical_tag FROM aliases WHERE alias = ?`, strings.TrimSpace(name)).Scan(&tag)
	if err != nil {
		return "", false
	}
	return tag, true
}

// GetInvariant resolves base or ready into a full invariant for materialize/API.
func GetInvariant(name string) (domain.InvariantPreset, error) {
	conn, err := DB()
	if err != nil {
		return domain.InvariantPreset{}, err
	}
	canon, ok := CanonicalTag(name)
	if !ok {
		return domain.InvariantPreset{}, fmt.Errorf("unknown catalogsqlite preset %q", name)
	}
	if inv, err := loadBaseByTag(conn, canon); err == nil {
		return inv, nil
	}
	return loadReadyByTag(conn, canon)
}

// AllPresets returns base + ready as ProtocolPreset (lang=ru descriptions via ToProtocolPreset).
func AllPresets() ([]domain.ProtocolPreset, error) {
	conn, err := DB()
	if err != nil {
		return nil, err
	}
	tags := []string{}
	rows, err := conn.Query(`
SELECT tag FROM preset_bases
UNION ALL
SELECT tag FROM ready_presets
ORDER BY 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	out := make([]domain.ProtocolPreset, 0, len(tags))
	for _, t := range tags {
		inv, err := GetInvariant(t)
		if err != nil {
			return nil, err
		}
		out = append(out, inv.ToProtocolPreset("ru"))
	}
	return out, nil
}

// GetProtocol returns protocol metadata with invariant tags filled.
func GetProtocol(tag string) (domain.ProtocolMeta, error) {
	conn, err := DB()
	if err != nil {
		return domain.ProtocolMeta{}, err
	}
	var p domain.ProtocolMeta
	var i18n, notes, creds string
	err = conn.QueryRow(`
SELECT tag,singbox_type,short_name,status,i18n_json,notes_json,default_cred_fields_json
FROM protocols WHERE tag = ?`, tag).Scan(
		&p.Tag, &p.SingBoxType, &p.ShortName, &p.Status, &i18n, &notes, &creds)
	if err != nil {
		return domain.ProtocolMeta{}, fmt.Errorf("unknown catalogsqlite protocol %q", tag)
	}
	_ = json.Unmarshal([]byte(i18n), &p.I18n)
	_ = json.Unmarshal([]byte(notes), &p.Notes)
	_ = json.Unmarshal([]byte(creds), &p.DefaultCredFields)
	p.InvariantTags = append(p.InvariantTags, mustTags(conn)...)
	return p, nil
}

func mustTags(conn *sql.DB) []string {
	rows, err := conn.Query(`
SELECT tag FROM preset_bases WHERE protocol = ?
UNION ALL
SELECT tag FROM ready_presets WHERE protocol = ?
ORDER BY 1`, protocolVLESS, protocolVLESS)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return out
		}
		out = append(out, t)
	}
	return out
}

func loadBaseByTag(conn *sql.DB, tag string) (domain.InvariantPreset, error) {
	var inv domain.InvariantPreset
	var custom int
	var aliases, traits, scores, demux, reqs string
	var creds, credGen, peer, paramFields, optFields, paramMeta string
	var variants, profiles, i18n, notes, inTpl, outTpl string
	var epTpl sql.NullString
	err := conn.QueryRow(`
SELECT protocol,tag,short_name,status,custom_preset,aliases_json,traits_json,scores_json,demux_hints_json,
  requirements_json,cred_fields_json,cred_generators_json,peer_secret_fields_json,
  param_fields_json,optional_param_fields_json,param_meta_json,
  default_user_variants_json,default_client_profiles_json,i18n_json,client_notes_json,
  inbound_template_json,outbound_template_json,endpoint_template_json
FROM preset_bases WHERE tag = ?`, tag).Scan(
		&inv.Protocol, &inv.Tag, &inv.ShortName, &inv.Status, &custom,
		&aliases, &traits, &scores, &demux, &reqs,
		&creds, &credGen, &peer, &paramFields, &optFields, &paramMeta,
		&variants, &profiles, &i18n, &notes, &inTpl, &outTpl, &epTpl)
	if err != nil {
		return inv, err
	}
	inv.CustomPreset = custom != 0
	inv.SchemaVersion = 1
	unmarshalAll(&inv, aliases, traits, scores, demux, reqs, creds, credGen, peer,
		paramFields, optFields, paramMeta, variants, profiles, i18n, notes, inTpl, outTpl, epTpl)
	return inv, nil
}

func loadReadyByTag(conn *sql.DB, tag string) (domain.InvariantPreset, error) {
	baseTag, err := baseTagForProtocol(conn, protocolVLESS)
	if err != nil {
		return domain.InvariantPreset{}, err
	}
	base, err := loadBaseByTag(conn, baseTag)
	if err != nil {
		return domain.InvariantPreset{}, err
	}
	var shortName, status string
	var aliases, traits, scores, demux, reqs, creds, variants, profiles, i18n, notes string
	var inTpl, outTpl, epTpl sql.NullString
	err = conn.QueryRow(`
SELECT short_name,status,aliases_json,traits_json,scores_json,demux_hints_json,requirements_json,
  cred_fields_json,default_user_variants_json,default_client_profiles_json,
  i18n_json,client_notes_json,inbound_template_json,outbound_template_json,endpoint_template_json
FROM ready_presets WHERE tag = ?`, tag).Scan(
		&shortName, &status, &aliases, &traits, &scores, &demux, &reqs,
		&creds, &variants, &profiles, &i18n, &notes, &inTpl, &outTpl, &epTpl)
	if err != nil {
		return domain.InvariantPreset{}, fmt.Errorf("ready %q: %w", tag, err)
	}
	inv := base
	inv.Tag = tag
	inv.ShortName = shortName
	inv.Status = status
	inv.CustomPreset = true // ready edits use full base schema
	_ = json.Unmarshal([]byte(aliases), &inv.Aliases)
	_ = json.Unmarshal([]byte(traits), &inv.Traits)
	_ = json.Unmarshal([]byte(creds), &inv.CredFields)
	_ = json.Unmarshal([]byte(variants), &inv.DefaultUserVariants)
	_ = json.Unmarshal([]byte(profiles), &inv.DefaultClientProfiles)
	if scores != "" && scores != "null" {
		var s domain.PresetScores
		if json.Unmarshal([]byte(scores), &s) == nil {
			inv.Scores = &s
		}
	}
	if demux != "" && demux != "null" {
		var d domain.DemuxHints
		if json.Unmarshal([]byte(demux), &d) == nil {
			inv.DemuxHints = &d
		}
	}
	if reqs != "" && reqs != "null" {
		var r domain.PresetReqs
		if json.Unmarshal([]byte(reqs), &r) == nil {
			inv.Requirements = &r
		}
	}
	_ = json.Unmarshal([]byte(i18n), &inv.I18n)
	_ = json.Unmarshal([]byte(notes), &inv.ClientNotes)
	if inTpl.Valid && inTpl.String != "" {
		_ = json.Unmarshal([]byte(inTpl.String), &inv.InboundTemplate)
	}
	if outTpl.Valid && outTpl.String != "" {
		_ = json.Unmarshal([]byte(outTpl.String), &inv.OutboundTemplate)
	}
	if epTpl.Valid && epTpl.String != "" {
		_ = json.Unmarshal([]byte(epTpl.String), &inv.EndpointTemplate)
	}
	// Apply ready overrides as ParamMeta defaults (UI prefill + materialize defaults).
	overrides := map[string]string{}
	rows, err := conn.Query(`SELECT key,value FROM ready_param_values WHERE ready_tag = ?`, tag)
	if err != nil {
		return inv, err
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return inv, err
		}
		overrides[k] = v
		meta := inv.ParamMeta[k]
		meta.Default = v
		if inv.ParamMeta == nil {
			inv.ParamMeta = map[string]domain.ParamFieldMeta{}
		}
		inv.ParamMeta[k] = meta
	}
	_ = overrides
	return inv, nil
}

func baseTagForProtocol(conn *sql.DB, protocol string) (string, error) {
	var tag string
	err := conn.QueryRow(`SELECT tag FROM preset_bases WHERE protocol = ?`, protocol).Scan(&tag)
	return tag, err
}

func unmarshalAll(inv *domain.InvariantPreset, aliases, traits, scores, demux, reqs, creds, credGen, peer,
	paramFields, optFields, paramMeta, variants, profiles, i18n, notes, inTpl, outTpl string, epTpl sql.NullString) {
	_ = json.Unmarshal([]byte(aliases), &inv.Aliases)
	_ = json.Unmarshal([]byte(traits), &inv.Traits)
	if scores != "" && scores != "null" {
		var s domain.PresetScores
		if json.Unmarshal([]byte(scores), &s) == nil {
			inv.Scores = &s
		}
	}
	if demux != "" && demux != "null" {
		var d domain.DemuxHints
		if json.Unmarshal([]byte(demux), &d) == nil {
			inv.DemuxHints = &d
		}
	}
	if reqs != "" && reqs != "null" {
		var r domain.PresetReqs
		if json.Unmarshal([]byte(reqs), &r) == nil {
			inv.Requirements = &r
		}
	}
	_ = json.Unmarshal([]byte(creds), &inv.CredFields)
	_ = json.Unmarshal([]byte(credGen), &inv.CredGenerators)
	_ = json.Unmarshal([]byte(peer), &inv.PeerSecretFields)
	_ = json.Unmarshal([]byte(paramFields), &inv.ParamFields)
	_ = json.Unmarshal([]byte(optFields), &inv.OptionalParamFields)
	_ = json.Unmarshal([]byte(paramMeta), &inv.ParamMeta)
	_ = json.Unmarshal([]byte(variants), &inv.DefaultUserVariants)
	_ = json.Unmarshal([]byte(profiles), &inv.DefaultClientProfiles)
	_ = json.Unmarshal([]byte(i18n), &inv.I18n)
	_ = json.Unmarshal([]byte(notes), &inv.ClientNotes)
	_ = json.Unmarshal([]byte(inTpl), &inv.InboundTemplate)
	_ = json.Unmarshal([]byte(outTpl), &inv.OutboundTemplate)
	if epTpl.Valid && epTpl.String != "" && epTpl.String != "null" {
		_ = json.Unmarshal([]byte(epTpl.String), &inv.EndpointTemplate)
	}
}

// EffectiveParams merges base defaults ⊕ ready overrides ⊕ user params, with legacy key aliases.
func EffectiveParams(presetTag string, user map[string]string) (map[string]string, error) {
	inv, err := GetInvariant(presetTag)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for k, meta := range inv.ParamMeta {
		if strings.TrimSpace(meta.Default) != "" {
			out[k] = strings.TrimSpace(meta.Default)
		}
	}
	for k, v := range user {
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if k == "" || v == "" {
			continue
		}
		out[k] = v
	}
	// Legacy stock param names → constructor keys.
	alias := map[string]string{
		"ws_path": "transport_path", "ws_host": "transport_host",
		"hu_path": "transport_path", "hu_host": "transport_host",
		"http_path": "transport_path", "http_host": "transport_host",
	}
	for old, neu := range alias {
		v := strings.TrimSpace(out[old])
		if v == "" {
			continue
		}
		if _, userSet := user[old]; userSet {
			out[neu] = v
			continue
		}
		if strings.TrimSpace(out[neu]) == "" {
			out[neu] = v
		}
	}
	return out, nil
}

// KnobProfile returns the constructor tag used for materialize knobs.
func KnobProfile(presetTag string) string {
	if !Owns(presetTag) {
		return presetTag
	}
	conn, err := DB()
	if err != nil {
		return "vless_custom"
	}
	tag, err := baseTagForProtocol(conn, protocolVLESS)
	if err != nil {
		return "vless_custom"
	}
	return tag
}
