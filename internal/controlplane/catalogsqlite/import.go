//go:build with_controlplane

package catalogsqlite

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
)

func importVlessRef(conn *sql.DB) error {
	entries, err := fs.ReadDir(vlessRefFS, "ref/vless")
	if err != nil {
		return err
	}
	var proto domain.ProtocolMeta
	var base domain.InvariantPreset
	var ready []domain.InvariantPreset
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := fs.ReadFile(vlessRefFS, path.Join("ref/vless", e.Name()))
		if err != nil {
			return err
		}
		if e.Name() == "protocol.json" {
			if err := json.Unmarshal(raw, &proto); err != nil {
				return fmt.Errorf("protocol.json: %w", err)
			}
			continue
		}
		var inv domain.InvariantPreset
		if err := json.Unmarshal(raw, &inv); err != nil {
			return fmt.Errorf("%s: %w", e.Name(), err)
		}
		if inv.CustomPreset || inv.Tag == "vless_custom" {
			base = inv
			base.CustomPreset = true
			continue
		}
		ready = append(ready, inv)
	}
	if proto.Tag == "" || base.Tag == "" {
		return fmt.Errorf("vless ref incomplete: protocol=%q base=%q", proto.Tag, base.Tag)
	}
	if err := insertProtocol(conn, proto); err != nil {
		return err
	}
	if err := insertBase(conn, base); err != nil {
		return err
	}
	for _, r := range ready {
		if err := insertReady(conn, base, r); err != nil {
			return err
		}
	}
	return nil
}

func insertProtocol(conn *sql.DB, p domain.ProtocolMeta) error {
	i18n, _ := json.Marshal(p.I18n)
	notes, _ := json.Marshal(p.Notes)
	creds, _ := json.Marshal(p.DefaultCredFields)
	_, err := conn.Exec(`
INSERT INTO protocols(tag,singbox_type,short_name,status,i18n_json,notes_json,default_cred_fields_json)
VALUES(?,?,?,?,?,?,?)`,
		p.Tag, p.SingBoxType, p.ShortName, p.Status, string(i18n), string(notes), string(creds))
	return err
}

func insertBase(conn *sql.DB, inv domain.InvariantPreset) error {
	row, err := marshalPresetRow(inv)
	if err != nil {
		return err
	}
	_, err = conn.Exec(`
INSERT INTO preset_bases(
  protocol,tag,short_name,status,custom_preset,aliases_json,traits_json,scores_json,demux_hints_json,
  requirements_json,cred_fields_json,cred_generators_json,peer_secret_fields_json,
  param_fields_json,optional_param_fields_json,param_meta_json,
  default_user_variants_json,default_client_profiles_json,i18n_json,client_notes_json,
  inbound_template_json,outbound_template_json,endpoint_template_json
) VALUES(?,?,?,?,1,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		inv.Protocol, inv.Tag, inv.ShortName, inv.Status,
		row.aliases, row.traits, row.scores, row.demux, row.reqs,
		row.creds, row.credGen, row.peer,
		row.paramFields, row.optFields, row.paramMeta,
		row.variants, row.profiles, row.i18n, row.notes,
		row.inTpl, row.outTpl, row.epTpl)
	if err != nil {
		return err
	}
	return registerAliases(conn, inv.Tag, inv.Aliases)
}

func insertReady(conn *sql.DB, base, inv domain.InvariantPreset) error {
	overrides := inferReadyOverrides(inv.Tag, inv)
	useOwnTpl := readyNeedsOwnTemplates(inv.Tag)
	var inTpl, outTpl, epTpl any
	if useOwnTpl {
		b, _ := json.Marshal(inv.InboundTemplate)
		inTpl = string(b)
		b, _ = json.Marshal(inv.OutboundTemplate)
		outTpl = string(b)
		if len(inv.EndpointTemplate) > 0 {
			b, _ = json.Marshal(inv.EndpointTemplate)
			epTpl = string(b)
		}
	}
	row, err := marshalPresetRow(inv)
	if err != nil {
		return err
	}
	_, err = conn.Exec(`
INSERT INTO ready_presets(
  tag,protocol,short_name,status,aliases_json,traits_json,scores_json,demux_hints_json,
  requirements_json,cred_fields_json,default_user_variants_json,default_client_profiles_json,
  i18n_json,client_notes_json,inbound_template_json,outbound_template_json,endpoint_template_json
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		inv.Tag, inv.Protocol, inv.ShortName, inv.Status,
		row.aliases, row.traits, row.scores, row.demux, row.reqs,
		row.creds, row.variants, row.profiles,
		row.i18n, row.notes, inTpl, outTpl, epTpl)
	if err != nil {
		return err
	}
	for k, v := range overrides {
		if _, err := conn.Exec(`INSERT INTO ready_param_values(ready_tag,key,value) VALUES(?,?,?)`, inv.Tag, k, v); err != nil {
			return err
		}
	}
	_ = base
	return registerAliases(conn, inv.Tag, inv.Aliases)
}

func registerAliases(conn *sql.DB, tag string, aliases []string) error {
	if _, err := conn.Exec(`INSERT OR REPLACE INTO aliases(alias,canonical_tag) VALUES(?,?)`, tag, tag); err != nil {
		return err
	}
	for _, a := range aliases {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		if _, err := conn.Exec(`INSERT OR REPLACE INTO aliases(alias,canonical_tag) VALUES(?,?)`, a, tag); err != nil {
			return err
		}
	}
	return nil
}

type presetJSONRow struct {
	aliases, traits, scores, demux, reqs       string
	creds, credGen, peer                       string
	paramFields, optFields, paramMeta          string
	variants, profiles, i18n, notes            string
	inTpl, outTpl, epTpl                       string
}

func marshalPresetRow(inv domain.InvariantPreset) (presetJSONRow, error) {
	m := func(v any) (string, error) {
		b, err := json.Marshal(v)
		if err != nil {
			return "", err
		}
		if v == nil {
			return "null", nil
		}
		return string(b), nil
	}
	var r presetJSONRow
	var err error
	if r.aliases, err = m(inv.Aliases); err != nil {
		return r, err
	}
	if r.traits, err = m(inv.Traits); err != nil {
		return r, err
	}
	if r.scores, err = m(inv.Scores); err != nil {
		return r, err
	}
	if r.demux, err = m(inv.DemuxHints); err != nil {
		return r, err
	}
	if r.reqs, err = m(inv.Requirements); err != nil {
		return r, err
	}
	if r.creds, err = m(inv.CredFields); err != nil {
		return r, err
	}
	if r.credGen, err = m(inv.CredGenerators); err != nil {
		return r, err
	}
	if r.peer, err = m(inv.PeerSecretFields); err != nil {
		return r, err
	}
	if r.paramFields, err = m(inv.ParamFields); err != nil {
		return r, err
	}
	if r.optFields, err = m(inv.OptionalParamFields); err != nil {
		return r, err
	}
	if r.paramMeta, err = m(inv.ParamMeta); err != nil {
		return r, err
	}
	if r.variants, err = m(inv.DefaultUserVariants); err != nil {
		return r, err
	}
	if r.profiles, err = m(inv.DefaultClientProfiles); err != nil {
		return r, err
	}
	if r.i18n, err = m(inv.I18n); err != nil {
		return r, err
	}
	if r.notes, err = m(inv.ClientNotes); err != nil {
		return r, err
	}
	if r.inTpl, err = m(inv.InboundTemplate); err != nil {
		return r, err
	}
	if r.outTpl, err = m(inv.OutboundTemplate); err != nil {
		return r, err
	}
	if r.epTpl, err = m(inv.EndpointTemplate); err != nil {
		return r, err
	}
	return r, nil
}

func readyNeedsOwnTemplates(tag string) bool {
	// Stock templates not fully expressible via base constructor params yet.
	// Own templates REPLACE base templates entirely (see loadReadyByTag clone+replace).
	switch tag {
	case "vless_tls_mux", "vless_hysteria_tls":
		return true
	default:
		return false
	}
}

// inferReadyOverrides maps a legacy stock VLESS tag onto base constructor params.
func inferReadyOverrides(tag string, inv domain.InvariantPreset) map[string]string {
	out := map[string]string{
		"transport":       "tcp",
		"tls_mode":        "tls",
		"flow":            "none",
		"packet_encoding": "xudp",
		"fingerprint":     "chrome",
		"transport_path":  "/vless",
		"transport_host":  "{{server}}",
		"service_name":    "GunService",
		"alpn":            "h2,http/1.1",
	}
	switch {
	case strings.Contains(tag, "_ws_"):
		out["transport"] = "ws"
	case strings.Contains(tag, "_grpc_"):
		out["transport"] = "grpc"
	case strings.Contains(tag, "_httpupgrade_"):
		out["transport"] = "httpupgrade"
	case strings.Contains(tag, "_http_"):
		out["transport"] = "http"
	case strings.Contains(tag, "_quic_"):
		out["transport"] = "quic"
		out["alpn"] = "h3"
	case strings.Contains(tag, "_hysteria_"):
		out["transport"] = "hysteria"
		out["alpn"] = "h3"
	case tag == "vless_tcp":
		out["transport"] = "tcp"
		out["tls_mode"] = "none"
	case tag == "vless_tls" || tag == "vless_tls_mux":
		out["transport"] = "tcp"
		out["tls_mode"] = "tls"
	case tag == "vless_reality":
		out["transport"] = "tcp"
		out["tls_mode"] = "reality"
	}
	if strings.Contains(tag, "reality") {
		out["tls_mode"] = "reality"
	} else if tag != "vless_tcp" && out["tls_mode"] != "none" {
		if strings.Contains(tag, "tls") || out["tls_mode"] == "tls" {
			out["tls_mode"] = "tls"
		}
	}
	// Stock path defaults → constructor keys.
	if m := inv.ParamMeta["ws_path"]; m.Default != "" {
		out["transport_path"] = m.Default
	}
	if m := inv.ParamMeta["hu_path"]; m.Default != "" {
		out["transport_path"] = m.Default
	}
	if m := inv.ParamMeta["http_path"]; m.Default != "" {
		out["transport_path"] = m.Default
	}
	if v := inv.ClientNotes["ws_path_default"]; v != "" {
		out["transport_path"] = v
	}
	if v := inv.ClientNotes["hu_path_default"]; v != "" {
		out["transport_path"] = v
	}
	if v := inv.ClientNotes["http_path_default"]; v != "" {
		out["transport_path"] = v
	}
	return out
}
