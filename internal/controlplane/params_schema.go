//go:build with_controlplane

package controlplane

import (
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
)

// buildParamsSchema returns the thin-client form schema for a preset.
// Keys in ParamFields are required:true; listen_port / sni / demux_sni are optional knobs.
func buildParamsSchema(pp domain.ProtocolPreset, detail bool) map[string]any {
	out := map[string]any{
		"listen_port": map[string]any{
			"type":        "uint16",
			"required":    false,
			"description": "Public listen port for single-inbound install. Omit to auto-pick a free port (prefers 443).",
			"constraint":  "At most one TCP and one UDP occupant per port.",
		},
	}
	for _, f := range pp.ParamFields {
		if f == "" || f == "listen_port" {
			continue
		}
		out[f] = map[string]any{
			"type":        "string",
			"required":    true,
			"description": paramFieldDescription(f, pp),
		}
	}
	if detail {
		out["listen_port"] = map[string]any{
			"type":        "uint16",
			"required":    false,
			"description": "Public listen port when installing as a single-inbound set (not demux member). Omit to auto-pick.",
			"constraint":  "At most one TCP and one UDP inbound may share a port across sets.",
		}
		out["demux_sni"] = map[string]any{
			"type":        "string",
			"required":    false,
			"description": "SNI used for demux match / TLS server_name when installed inside a demux group.",
		}
		out["sni"] = map[string]any{
			"type":        "string",
			"required":    false,
			"description": "Optional ACME domain from cert-manager; for TLS non-Reality inbounds. Also syncs demux_sni. Pick from GET /cert-manager domains.",
		}
	}
	return out
}

// presetOptionalParams keeps backward-compat: only non-required knobs.
// Prefer params_schema for thin clients (includes required ParamFields).
func presetOptionalParams(pp domain.ProtocolPreset) map[string]any {
	schema := buildParamsSchema(pp, false)
	out := map[string]any{}
	for k, v := range schema {
		m, ok := v.(map[string]any)
		if !ok {
			continue
		}
		if req, _ := m["required"].(bool); req {
			continue
		}
		out[k] = v
	}
	return out
}

func presetOptionalParamsDetail(pp domain.ProtocolPreset) map[string]any {
	schema := buildParamsSchema(pp, true)
	out := map[string]any{}
	for k, v := range schema {
		m, ok := v.(map[string]any)
		if !ok {
			continue
		}
		if req, _ := m["required"].(bool); req {
			continue
		}
		out[k] = v
	}
	return out
}

func paramFieldDescription(field string, pp domain.ProtocolPreset) string {
	switch field {
	case "room":
		return "Required carrier/SFU meeting or stream URL (e.g. Jitsi/Telemost room link)."
	case "token":
		return "Required cloudflared / tunnel token from the provider dashboard."
	case "masquerade_dir":
		return "Required local directory path for Hy2 file masquerade."
	case "realm_server_url":
		return "Required Hysteria realm control-plane server URL."
	case "realm_id":
		return "Required Hysteria realm identifier."
	case "ws_host", "hu_host", "http_host":
		if hasTrait(pp.Traits, "reality") {
			return "HTTP Host header; materialize aligns to Reality SNI when omitted/default."
		}
		return "HTTP Host header for the transport."
	case "ws_path", "hu_path", "grpc_service_name", "http_path":
		return "Transport path / gRPC service name."
	case "sni":
		return "TLS server_name / ACME domain."
	default:
		return "Preset parameter required by this invariant."
	}
}

func hasTrait(traits []string, want string) bool {
	for _, t := range traits {
		if t == want {
			return true
		}
	}
	return false
}
