//go:build with_controlplane

package controlplane

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/demuxgroups"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/presets"
)

// GET /v1/controlplane/client/bootstrap — one-shot discovery for mobile/desktop clients.
func (s *Service) handleClientBootstrap(w http.ResponseWriter, r *http.Request) {
	lang := requestLang(r)
	okJSON(w, 200, map[string]any{
		"lang": lang,
		"capabilities": map[string]any{
			"protocols":              true,
			"presets":                true,
			"demux_groups":           demuxInBinary,
			"demux_in_binary":        demuxInBinary,
			"demux_group_match_meta": true,
			"demux_match_tag_vocab":  demuxgroups.MatchTagVocab,
			"port_policy":            "one_tcp_one_udp_per_port",
			"subscription_filters":   true,
			"reality_profiles":       true,
			"cert_manager":           true,
			"inbound_tls_sni_param":  "sni",
			"config_dns_route":       true,
			"demux_action":           "dial",
			"optional_listen_port":   true, // omit listen_port → auto-pick free port (presets + demux)
			"ready_poll":             true,
			"wg_hub":                 true,
			"activate_contract":      "activate:true must succeed (HTTP 201 + activated:true); activate failure → 422 with set already persisted",
		},
		"install_modes": []map[string]any{
			{
				"id":          "single_presets",
				"title":       pickFlowTitle(lang, "Одиночные пресеты", "Single presets"),
				"entry":       "GET /v1/controlplane/protocols",
				"best_for":    pickFlowTitle(lang, "один TCP + один UDP на разных/одном порту", "one TCP + one UDP per port policy"),
				"prefer_port": 443,
			},
			{
				"id":          "demux_group",
				"title":       pickFlowTitle(lang, "Demux-группа", "Demux group"),
				"entry":       "GET /v1/controlplane/demux-groups",
				"best_for":    pickFlowTitle(lang, "много протоколов на одном :443", "many protocols on one :443"),
				"prefer_port": 443,
			},
			{
				"id":       "wg_hub",
				"title":    "WireGuard / AWG hub",
				"entry":    "PUT /v1/controlplane/wg",
				"best_for": "underlay peers (not a demux group)",
			},
		},
		"flows": []map[string]any{
			{
				"id":    "single_presets",
				"title": pickFlowTitle(lang, "Одиночные пресеты", "Single presets"),
				"steps": []map[string]any{
					{"method": "GET", "path": "/v1/controlplane/protocols", "why": "list protocol folders"},
					{"method": "GET", "path": "/v1/controlplane/presets?protocol={tag}", "why": "list tags + metadata + optional_params"},
					{"method": "GET", "path": "/v1/controlplane/ports/availability?port=443", "why": "check free L4 networks (optional if omitting listen_port)"},
					{"method": "POST", "path": "/v1/controlplane/sets/from-presets", "why": "install; listen_port optional (auto); activate:true", "check": "HTTP 201 && response.activated === true"},
					{"method": "GET", "path": "/v1/controlplane/status", "why": "poll until ready.ok === true"},
					{"method": "POST", "path": "/v1/controlplane/users", "why": "create client + subscription_url"},
					{"method": "GET", "path": "/v1/controlplane/subscription-tags?active_only=true", "why": "discover query filters (flow/variant/…)"},
					{"method": "GET", "path": "/v1/sub/{token}?variant=…", "why": "fetch subscription with filters"},
				},
			},
			{
				"id":    "demux_group",
				"title": pickFlowTitle(lang, "Demux-группа", "Demux group"),
				"steps": []map[string]any{
					{"method": "GET", "path": "/v1/controlplane/demux-groups", "why": "list ready stacks"},
					{"method": "GET", "path": "/v1/controlplane/demux-groups/{tag}/substitutions", "why": "slot replacements + demux_compat"},
					{"method": "GET", "path": "/v1/controlplane/ports/availability?port=443", "why": "demux needs free tcp+udp when group uses both"},
					{"method": "POST", "path": "/v1/controlplane/sets/from-demux-group", "why": "install + activate; listen_port optional", "check": "HTTP 201 && response.activated === true"},
					{"method": "GET", "path": "/v1/controlplane/status", "why": "poll until ready.ok === true"},
					{"method": "POST", "path": "/v1/controlplane/users", "why": "create client + subscription_url"},
					{"method": "GET", "path": "/v1/controlplane/subscription-tags?active_only=true", "why": "discover query filters"},
					{"method": "GET", "path": "/v1/sub/{token}", "why": "fetch subscription"},
				},
			},
		},
		"counts": map[string]any{
			"protocols":    len(presets.Protocols()),
			"presets":      len(presets.All()),
			"demux_groups": demuxgroups.Count(),
		},
		"subscription_filters": map[string]any{
			"discover": "GET /v1/controlplane/subscription-tags?active_only=true",
			"apply_on": "GET /v1/sub/{token}",
			"params": []map[string]any{
				{"name": "set", "type": "string", "repeatable": false, "description": "Filter by inbound set name"},
				{"name": "preset", "type": "string", "repeatable": true, "description": "Filter by preset tag"},
				{"name": "variant", "type": "string", "repeatable": true, "description": "Logical variant e.g. flow-none, flow-xtls-rprx-vision, flow-udp-vision"},
				{"name": "tag", "type": "string", "repeatable": true, "description": "Binding/variant query tag"},
				{"name": "profile", "type": "string", "repeatable": true, "description": "Named profile filter"},
				{"name": "flow", "type": "string", "repeatable": true, "values": []string{"none", "xtls-rprx-vision", "xtls-rprx-vision-udp443", "udp-vision"}, "description": "VLESS flow filter"},
				{"name": "network", "type": "string", "values": []string{"tcp", "udp"}, "description": "L4 network filter"},
				{"name": "strict_filters", "type": "bool", "default": false, "description": "Unknown filter → 400 cp_invalid_sub_filter"},
			},
		},
		"ownership": map[string]any{
			"on_activate": "claims config_mode=controlplane on the agent",
			"vs_panel":    "panel desired-config pull may diverge until CP sets deactivated / ownership reclaimed",
		},
		"hints": map[string]any{
			"prefer_demux_groups_for_443":   true,
			"reality_unique_sni":            true,
			"slot_tls_per_demux_sni":        true,
			"after_activate_poll_ready":     true,
			"demux_is_not_a_protocol_folder": true,
			"use_substitutions_before_install": true,
		},
	})
}

// GET /v1/controlplane/ports/availability?port=443
func (s *Service) handlePortsAvailability(w http.ResponseWriter, r *http.Request) {
	portStr := strings.TrimSpace(r.URL.Query().Get("port"))
	if portStr == "" {
		failJSON(w, 400, "bad_request", "port query required")
		return
	}
	n, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil || n == 0 {
		failJSON(w, 400, "bad_request", "invalid port")
		return
	}
	port := uint16(n)
	sets, err := s.store.LoadSets()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	occupied := map[string][]string{} // network → set names
	for _, set := range sets {
		if set.ListenPort != port {
			continue
		}
		for _, netw := range portNetworks(set) {
			occupied[netw] = append(occupied[netw], set.Name)
		}
	}
	if hub, err := s.store.LoadWgHub(); err == nil && hub.ListenPort == port {
		occupied["udp"] = append(occupied["udp"], "wg-hub")
	}
	free := make([]string, 0, 2)
	for _, netw := range []string{"tcp", "udp"} {
		if len(occupied[netw]) == 0 {
			free = append(free, netw)
		}
	}
	okJSON(w, 200, map[string]any{
		"port":      port,
		"free":      free,
		"occupied":  occupied,
		"can_tcp":   len(occupied["tcp"]) == 0,
		"can_udp":   len(occupied["udp"]) == 0,
		"can_demux": len(occupied["tcp"]) == 0 && len(occupied["udp"]) == 0,
		"policy":    "At most one TCP and one UDP occupant per port; demux groups need both free for tcp+udp networks.",
	})
}

func pickFlowTitle(lang, ru, en string) string {
	if strings.HasPrefix(strings.ToLower(lang), "en") {
		return en
	}
	return ru
}
