//go:build with_controlplane

package materialize

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/catalogsqlite"
)

func applyCustomPresetInboundKnobs(ib map[string]any, preset string, params map[string]string) {
	applyTuicKnobs(ib, preset, params)
	applyNaiveNetworkKnobs(ib, preset, params)
	applyHy2CustomKnobs(ib, preset, params)
	applyVlessLikeCustomKnobs(ib, preset, params)
	applyShadowsocksCustomKnobs(ib, preset, params)
	applyHysteria1CustomKnobs(ib, preset, params)
	applySocksCustomKnobs(ib, preset, params, true)
	applyHTTPMixedCustomKnobs(ib, preset, params)
	applyTrustTunnelCustomKnobs(ib, preset, params)
	applyAnyTLSCustomKnobs(ib, preset, params, true)
	applyShadowTLSCustomKnobs(ib, preset, params)
	applySudokuCustomKnobs(ib, preset, params)
	applyMieruCustomKnobs(ib, preset, params)
	applyDerpCustomKnobs(ib, preset, params, true)
	applyCloudflaredCustomKnobs(ib, preset, params)
	applyCarrierCustomKnobs(ib, preset, params)
	applyShadowQUICKnobs(ib, params)
	applySnellKnobs(ib, params)
	applyStockBandwidthParams(ib, params)
	applyStockIgnoreClientBandwidth(ib, params)
}

func applyCustomPresetOutboundKnobs(ob map[string]any, preset string, params map[string]string) {
	applyTuicKnobs(ob, preset, params)
	applyNaiveNetworkKnobs(ob, preset, params)
	applyHy2CustomKnobs(ob, preset, params)
	applyVlessLikeCustomKnobs(ob, preset, params)
	applyShadowsocksCustomKnobs(ob, preset, params)
	applyHysteria1CustomKnobs(ob, preset, params)
	applySocksCustomKnobs(ob, preset, params, false)
	applyHTTPMixedCustomKnobs(ob, preset, params)
	applyTrustTunnelCustomKnobs(ob, preset, params)
	applyAnyTLSCustomKnobs(ob, preset, params, false)
	applyShadowTLSCustomKnobs(ob, preset, params)
	applySudokuCustomKnobs(ob, preset, params)
	applyMieruCustomKnobs(ob, preset, params)
	applyDerpCustomKnobs(ob, preset, params, false)
	applyCarrierCustomKnobs(ob, preset, params)
	applyShadowQUICKnobs(ob, params)
	applySnellKnobs(ob, params)
	applyStockBandwidthParams(ob, params)
	applyStockIgnoreClientBandwidth(ob, params)
	applyStockUTLSFingerprint(ob, params)
	stripUTLSForQUICTransport(ob)
}

// applyStockBandwidthParams writes up_mbps/down_mbps from binding params onto
// hysteria / hysteria2 objects (stock presets keep numeric defaults in JSON;
// without this, optional_param_fields are ignored).
func applyStockBandwidthParams(m map[string]any, params map[string]string) {
	typ, _ := m["type"].(string)
	switch typ {
	case "hysteria", "hysteria2":
	default:
		return
	}
	if v, ok := parseUintParam(params["up_mbps"]); ok {
		m["up_mbps"] = v
	}
	if v, ok := parseUintParam(params["down_mbps"]); ok {
		m["down_mbps"] = v
	}
}

func applyStockIgnoreClientBandwidth(m map[string]any, params map[string]string) {
	typ, _ := m["type"].(string)
	if typ != "hysteria2" {
		return
	}
	if strings.TrimSpace(params["ignore_client_bandwidth"]) == "" {
		return
	}
	m["ignore_client_bandwidth"] = strings.EqualFold(strings.TrimSpace(params["ignore_client_bandwidth"]), "true")
}

// applyStockUTLSFingerprint overrides outbound tls.utls.fingerprint when set.
func applyStockUTLSFingerprint(m map[string]any, params map[string]string) {
	if _, isIn := m["listen"]; isIn {
		return
	}
	fp := strings.TrimSpace(params["fingerprint"])
	if fp == "" {
		return
	}
	tls, _ := m["tls"].(map[string]any)
	if tls == nil {
		return
	}
	tls["utls"] = map[string]any{"enabled": true, "fingerprint": fp}
}

func applySnellKnobs(m map[string]any, params map[string]string) {
	typ, _ := m["type"].(string)
	if typ != "snell" {
		return
	}
	mode := strings.ToLower(strings.TrimSpace(params["obfs_mode"]))
	if mode == "" {
		return
	}
	switch mode {
	case "off", "none":
		delete(m, "obfs_mode")
		delete(m, "obfs_host")
	default:
		m["obfs_mode"] = mode
	}
}

func applyShadowQUICKnobs(m map[string]any, params map[string]string) {
	typ, _ := m["type"].(string)
	if typ != "shadowquic" {
		return
	}
	if v := strings.TrimSpace(params["congestion_control"]); v != "" {
		m["congestion_control"] = v
	}
	if strings.TrimSpace(params["zero_rtt"]) != "" {
		m["zero_rtt"] = strings.EqualFold(strings.TrimSpace(params["zero_rtt"]), "true")
	}
	if _, isInbound := m["listen"]; !isInbound {
		if strings.TrimSpace(params["udp_over_stream"]) != "" {
			m["udp_over_stream"] = strings.EqualFold(strings.TrimSpace(params["udp_over_stream"]), "true")
		}
	}
}

func applyTuicKnobs(m map[string]any, preset string, params map[string]string) {
	typ, _ := m["type"].(string)
	if typ != "tuic" {
		return
	}
	if v := strings.TrimSpace(params["congestion_control"]); v != "" {
		m["congestion_control"] = v
	}
	if _, isInbound := m["listen"]; !isInbound {
		if v := strings.TrimSpace(params["udp_relay_mode"]); v != "" {
			m["udp_relay_mode"] = v
		}
	}
	if preset != "tuic_custom" {
		return
	}
	if strings.EqualFold(strings.TrimSpace(params["zero_rtt"]), "true") {
		m["zero_rtt_handshake"] = true
	} else {
		delete(m, "zero_rtt_handshake")
	}
}

func applyNaiveNetworkKnobs(m map[string]any, preset string, params map[string]string) {
	if preset != "naive_custom" {
		return
	}
	if !strings.EqualFold(strings.TrimSpace(params["network"]), "udp") {
		return
	}
	m["network"] = "udp"
	if tls, ok := m["tls"].(map[string]any); ok {
		tls["alpn"] = []any{"h3"}
	}
	m["quic_congestion_control"] = "bbr"
	m["quic"] = true
}

func applyHy2CustomKnobs(m map[string]any, preset string, params map[string]string) {
	if preset != "hy2_custom" {
		return
	}
	obfsType := strings.ToLower(strings.TrimSpace(params["obfs_type"]))
	if obfsType == "" {
		obfsType = "none"
	}
	if obfsType == "none" {
		delete(m, "obfs")
	} else {
		obfs, _ := m["obfs"].(map[string]any)
		if obfs == nil {
			obfs = map[string]any{}
			m["obfs"] = obfs
		}
		obfs["type"] = "salamander"
	}

	if v, ok := parseUintParam(params["up_mbps"]); ok {
		m["up_mbps"] = v
	}
	if v, ok := parseUintParam(params["down_mbps"]); ok {
		m["down_mbps"] = v
	}
	if _, present := m["ignore_client_bandwidth"]; present || strings.TrimSpace(params["ignore_client_bandwidth"]) != "" {
		m["ignore_client_bandwidth"] = strings.EqualFold(strings.TrimSpace(params["ignore_client_bandwidth"]), "true")
	}

	if _, hasMasq := m["masquerade"]; !hasMasq && strings.TrimSpace(params["masquerade_mode"]) == "" {
		return
	}
	mode := strings.ToLower(strings.TrimSpace(params["masquerade_mode"]))
	if mode == "" {
		mode = "none"
	}
	switch mode {
	case "none":
		delete(m, "masquerade")
	case "file":
		m["masquerade"] = map[string]any{
			"type":      "file",
			"directory": strings.TrimSpace(params["masquerade_dir"]),
		}
	case "proxy":
		url := strings.TrimSpace(params["masquerade_url"])
		if url == "" {
			url = "https://www.cloudflare.com"
		}
		m["masquerade"] = map[string]any{"type": "proxy", "url": url, "rewrite_host": true}
	case "string":
		m["masquerade"] = map[string]any{
			"type":        "string",
			"status_code": 200,
			"content":     "<html><title>OK</title></html>",
			"headers": map[string]any{
				"Content-Type": []any{"text/html; charset=utf-8"},
			},
		}
	}
}

func applyVlessLikeCustomKnobs(m map[string]any, preset string, params map[string]string) {
	switch {
	case preset == "vless_custom", preset == "trojan_custom", preset == "vmess_custom":
	case catalogsqlite.Owns(preset):
		// VLESS ready/base from SQLite use constructor templates + knobs.
	default:
		return
	}
	cleanupV2RayTransport(m, params)
	if strings.TrimSpace(params["flow"]) == "" || strings.EqualFold(strings.TrimSpace(params["flow"]), "none") {
		delete(m, "flow")
	} else if v := strings.TrimSpace(params["flow"]); v != "" {
		m["flow"] = v
	}
	if alpn := strings.TrimSpace(params["alpn"]); alpn != "" {
		if tls, ok := m["tls"].(map[string]any); ok && tls != nil {
			tls["alpn"] = splitCSV(alpn)
		}
	}
	enc := strings.TrimSpace(params["packet_encoding"])
	if enc == "" || strings.EqualFold(enc, "none") {
		delete(m, "packet_encoding")
	} else {
		m["packet_encoding"] = enc
	}
	applyVlessMultiplexKnob(m, params)
	applyVlessWSEarlyDataKnob(m, params)
}

func applyVlessMultiplexKnob(m map[string]any, params map[string]string) {
	mux := strings.ToLower(strings.TrimSpace(params["multiplex"]))
	if mux == "" || mux == "none" || mux == "false" || mux == "0" {
		delete(m, "multiplex")
		return
	}
	_, isInbound := m["listen"]
	if isInbound {
		m["multiplex"] = map[string]any{"enabled": true, "padding": true}
		return
	}
	m["multiplex"] = map[string]any{
		"enabled":         true,
		"protocol":        "smux",
		"padding":         true,
		"max_connections": 4,
		"min_streams":     4,
		"max_streams":     16,
	}
}

func applyVlessWSEarlyDataKnob(m map[string]any, params map[string]string) {
	tr, _ := m["transport"].(map[string]any)
	if tr == nil {
		return
	}
	typ := strings.ToLower(strings.TrimSpace(params["transport"]))
	if typ == "" {
		typ = strings.ToLower(strings.TrimSpace(fmt.Sprint(tr["type"])))
	}
	if typ != "ws" {
		delete(tr, "max_early_data")
		delete(tr, "early_data_header_name")
		return
	}
	raw := strings.TrimSpace(params["ws_max_early_data"])
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		delete(tr, "max_early_data")
		delete(tr, "early_data_header_name")
		return
	}
	tr["max_early_data"] = n
	tr["early_data_header_name"] = "Sec-WebSocket-Protocol"
	if headers, ok := tr["headers"].(map[string]any); ok {
		if _, hasUA := headers["User-Agent"]; !hasUA {
			headers["User-Agent"] = []any{"Mozilla/5.0"}
		}
	}
}

func cleanupV2RayTransport(m map[string]any, params map[string]string) {
	tr, _ := m["transport"].(map[string]any)
	if tr == nil {
		return
	}
	typ := strings.ToLower(strings.TrimSpace(params["transport"]))
	if typ == "" {
		typ = strings.ToLower(strings.TrimSpace(fmt.Sprint(tr["type"])))
	}
	if typ == "" || typ == "tcp" || typ == "<nil>" {
		delete(m, "transport")
		return
	}
	tr["type"] = typ
	switch typ {
	case "ws", "httpupgrade":
		delete(tr, "service_name")
		delete(tr, "password")
		delete(tr, "version")
	case "http":
		delete(tr, "service_name")
		delete(tr, "password")
		delete(tr, "version")
	case "grpc":
		delete(tr, "path")
		delete(tr, "host")
		delete(tr, "headers")
		delete(tr, "password")
		delete(tr, "version")
	case "quic":
		delete(tr, "path")
		delete(tr, "host")
		delete(tr, "headers")
		delete(tr, "service_name")
		delete(tr, "password")
		delete(tr, "version")
	case "hysteria":
		delete(tr, "path")
		delete(tr, "host")
		delete(tr, "headers")
		delete(tr, "service_name")
	}
	pruneEmptyStringKey(tr, "host")
	pruneEmptyStringKey(tr, "path")
	pruneEmptyStringKey(tr, "service_name")
	if headers, ok := tr["headers"].(map[string]any); ok {
		if host, ok := headers["Host"].([]any); ok {
			if len(host) == 0 || strings.TrimSpace(fmt.Sprint(host[0])) == "" || fmt.Sprint(host[0]) == "<nil>" {
				delete(headers, "Host")
			}
		}
		if len(headers) == 0 {
			delete(tr, "headers")
		}
	}
}

func applyShadowsocksCustomKnobs(m map[string]any, preset string, params map[string]string) {
	if preset != "shadowsocks_custom" {
		return
	}
	net := strings.ToLower(strings.TrimSpace(params["network"]))
	if net == "" {
		net = "tcp"
	}
	m["network"] = net
	if strings.EqualFold(strings.TrimSpace(params["udp_over_tcp"]), "true") {
		m["udp_over_tcp"] = map[string]any{"enabled": true, "version": 2}
	} else {
		delete(m, "udp_over_tcp")
	}
}

func applyHysteria1CustomKnobs(m map[string]any, preset string, params map[string]string) {
	if preset != "hysteria_custom" {
		return
	}
	if v, ok := parseUintParam(params["up_mbps"]); ok {
		m["up_mbps"] = v
	}
	if v, ok := parseUintParam(params["down_mbps"]); ok {
		m["down_mbps"] = v
	}
	obfs := strings.TrimSpace(params["obfs"])
	if obfs == "" || strings.EqualFold(obfs, "none") {
		delete(m, "obfs")
	} else {
		m["obfs"] = obfs
	}
}

func applySocksCustomKnobs(m map[string]any, preset string, params map[string]string, inbound bool) {
	if preset != "socks_custom" || inbound {
		return
	}
	if strings.EqualFold(strings.TrimSpace(params["udp_over_tcp"]), "true") {
		m["udp_over_tcp"] = map[string]any{"enabled": true, "version": 2}
	} else {
		delete(m, "udp_over_tcp")
	}
}

func applyHTTPMixedCustomKnobs(m map[string]any, preset string, params map[string]string) {
	switch preset {
	case "http_custom", "mixed_custom":
	default:
		return
	}
	if preset == "mixed_custom" {
		if ot := strings.ToLower(strings.TrimSpace(params["outbound_type"])); ot == "http" || ot == "socks" {
			// Only rewrite subscription outbounds (no listen field).
			if _, isIn := m["listen"]; !isIn {
				m["type"] = ot
			}
		}
	}
	mode := strings.ToLower(strings.TrimSpace(params["tls_mode"]))
	if mode == "" {
		mode = "none"
	}
	if mode == "none" {
		delete(m, "tls")
		return
	}
	tls, _ := m["tls"].(map[string]any)
	if tls == nil {
		tls = map[string]any{"enabled": true, "alpn": []any{"h2", "http/1.1"}}
		m["tls"] = tls
	}
	if fp := strings.TrimSpace(params["fingerprint"]); fp != "" {
		if _, isIn := m["listen"]; !isIn {
			tls["utls"] = map[string]any{"enabled": true, "fingerprint": fp}
		}
	}
}

func applyTrustTunnelCustomKnobs(m map[string]any, preset string, params map[string]string) {
	if preset != "trusttunnel_custom" {
		return
	}
	tr, _ := m["transport"].(map[string]any)
	if tr == nil {
		tr = map[string]any{}
		m["transport"] = tr
	}
	if v := strings.TrimSpace(params["mode"]); v != "" {
		tr["upstream_protocol"] = normalizeTrustTunnelUpstream(v)
	}
	if strings.TrimSpace(params["anti_dpi"]) != "" {
		tr["anti_dpi"] = strings.EqualFold(strings.TrimSpace(params["anti_dpi"]), "true")
	}
	if strings.TrimSpace(params["enable_protocol_fallback"]) != "" {
		tr["enable_protocol_fallback"] = strings.EqualFold(strings.TrimSpace(params["enable_protocol_fallback"]), "true")
	}
	if strings.TrimSpace(params["force_http1_connect"]) != "" {
		tr["force_http1_connect"] = strings.EqualFold(strings.TrimSpace(params["force_http1_connect"]), "true")
	}
	if strings.TrimSpace(params["enable_udp"]) != "" {
		m["enable_udp"] = strings.EqualFold(strings.TrimSpace(params["enable_udp"]), "true")
	}
}

// normalizeTrustTunnelUpstream maps UI mode aliases to TrustTunnel wire values.
func normalizeTrustTunnelUpstream(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "h2", "http2", "http/2":
		return "http2"
	case "h3", "http3", "http/3", "quic":
		return "http3"
	case "auto", "":
		return "auto"
	default:
		return strings.ToLower(strings.TrimSpace(v))
	}
}

func applyAnyTLSCustomKnobs(m map[string]any, preset string, params map[string]string, inbound bool) {
	typ, _ := m["type"].(string)
	if typ != "anytls" && preset != "anytls_custom" {
		return
	}
	if alpn := strings.TrimSpace(params["alpn"]); alpn != "" {
		if tls, ok := m["tls"].(map[string]any); ok && tls != nil {
			tls["alpn"] = splitCSV(alpn)
		}
	}
	if inbound {
		return
	}
	if fp := strings.TrimSpace(params["fingerprint"]); fp != "" {
		if tls, ok := m["tls"].(map[string]any); ok && tls != nil {
			tls["utls"] = map[string]any{"enabled": true, "fingerprint": fp}
		}
	}
	// idle_session: custom constructor or explicit override on any anytls outbound
	if strings.TrimSpace(params["idle_session"]) == "" && preset != "anytls_custom" && preset != "anytls_idle" {
		return
	}
	if preset == "anytls_idle" || strings.EqualFold(strings.TrimSpace(params["idle_session"]), "true") {
		m["idle_session_check_interval"] = "30s"
		m["idle_session_timeout"] = "30s"
		m["min_idle_session"] = 0
	} else if preset == "anytls_custom" || strings.TrimSpace(params["idle_session"]) != "" {
		delete(m, "idle_session_check_interval")
		delete(m, "idle_session_timeout")
		delete(m, "min_idle_session")
	}
}

func applyShadowTLSCustomKnobs(m map[string]any, preset string, params map[string]string) {
	typ, _ := m["type"].(string)
	if typ != "shadowtls" && preset != "shadowtls_custom" {
		return
	}
	if _, ok := m["strict_mode"]; ok || strings.TrimSpace(params["strict_mode"]) != "" {
		m["strict_mode"] = !strings.EqualFold(strings.TrimSpace(params["strict_mode"]), "false")
	}
	if fp := strings.TrimSpace(params["fingerprint"]); fp != "" {
		if tls, ok := m["tls"].(map[string]any); ok && tls != nil {
			if _, isIn := m["listen"]; !isIn {
				tls["utls"] = map[string]any{"enabled": true, "fingerprint": fp}
			}
		}
	}
}

func applySudokuCustomKnobs(m map[string]any, preset string, params map[string]string) {
	if preset != "sudoku_custom" {
		return
	}
	if v, ok := parseUintParam(params["padding_min"]); ok {
		m["padding_min"] = v
	}
	if v, ok := parseUintParam(params["padding_max"]); ok {
		m["padding_max"] = v
	}
}

func applyMieruCustomKnobs(m map[string]any, preset string, params map[string]string) {
	typ, _ := m["type"].(string)
	if typ != "mieru" && preset != "mieru_custom" {
		return
	}
	if v, ok := parseUintParam(params["mtu"]); ok {
		m["mtu"] = v
	}
	if strings.TrimSpace(fmt.Sprint(m["traffic_pattern"])) == "" {
		delete(m, "traffic_pattern")
	}
}

func applyDerpCustomKnobs(m map[string]any, preset string, params map[string]string, inbound bool) {
	typ, _ := m["type"].(string)
	if typ != "derp" && preset != "derp_custom" {
		return
	}
	if strings.TrimSpace(params["websocket"]) != "" {
		m["websocket"] = strings.EqualFold(strings.TrimSpace(params["websocket"]), "true")
	}
	if v := strings.TrimSpace(params["udp"]); v != "" {
		m["udp"] = normalizeDerpUDP(v)
	}
	if !inbound {
		if fp := strings.TrimSpace(params["fingerprint"]); fp != "" {
			if tls, ok := m["tls"].(map[string]any); ok && tls != nil {
				tls["utls"] = map[string]any{"enabled": true, "fingerprint": fp}
			}
		}
	}
}

// normalizeDerpUDP maps UI aliases to DERP wire udp mode (native|uot).
func normalizeDerpUDP(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "native":
		return "native"
	case "uot", "udp_over_tcp", "udp-over-tcp":
		return "uot"
	case "disabled", "off", "false", "none":
		// Historical UI value; wire has no "disabled" — keep native (TCP/WS flags select path).
		return "native"
	default:
		return strings.ToLower(strings.TrimSpace(v))
	}
}

func applyCloudflaredCustomKnobs(m map[string]any, preset string, params map[string]string) {
	typ, _ := m["type"].(string)
	if typ != "cloudflared" && preset != "cloudflared_custom" {
		return
	}
	if strings.TrimSpace(params["post_quantum"]) != "" {
		m["post_quantum"] = !strings.EqualFold(strings.TrimSpace(params["post_quantum"]), "false")
	}
	if v, ok := parseUintParam(params["ha_connections"]); ok {
		m["ha_connections"] = v
	}
}

// applyCarrierCustomKnobs maps constructor provider/token onto carrier objects.
// Enum value jitsi_sei → provider=jitsi + transport=seichannel.
func applyCarrierCustomKnobs(m map[string]any, preset string, params map[string]string) {
	if preset != "carrier_custom" {
		return
	}
	raw := strings.ToLower(strings.TrimSpace(params["provider"]))
	if raw == "" {
		raw = "jitsi"
	}
	provider := raw
	transport := "datachannel"
	switch raw {
	case "jitsi":
		transport = "datachannel"
	case "jitsi_sei":
		provider = "jitsi"
		transport = "seichannel"
	case "telemost", "wbstream":
		transport = "vp8channel"
	}
	m["provider"] = provider
	link, _ := m["link"].(map[string]any)
	if link == nil {
		link = map[string]any{}
		m["link"] = link
	}
	link["transport"] = transport
	if tok := strings.TrimSpace(params["token"]); tok != "" {
		link["token"] = tok
	} else if provider != "wbstream" {
		delete(link, "token")
	}
	if key := strings.TrimSpace(params["key"]); key != "" {
		link["key"] = key
	}
}

func parseUintParam(s string) (uint64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseUint(s, 10, 32)
	if err != nil || v == 0 {
		return 0, false
	}
	return v, true
}

func splitCSV(s string) []any {
	parts := strings.Split(s, ",")
	out := make([]any, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func pruneEmptyStringKey(m map[string]any, key string) {
	if strings.TrimSpace(fmt.Sprint(m[key])) == "" || fmt.Sprint(m[key]) == "<nil>" {
		delete(m, key)
	}
}
