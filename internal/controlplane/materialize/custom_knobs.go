//go:build with_controlplane

package materialize

import (
	"fmt"
	"strconv"
	"strings"
)

func applyCustomPresetInboundKnobs(ib map[string]any, preset string, params map[string]string) {
	applyTuicZeroRTT(ib, preset, params)
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
	applyStockBandwidthParams(ib, params)
}

func applyCustomPresetOutboundKnobs(ob map[string]any, preset string, params map[string]string) {
	applyTuicZeroRTT(ob, preset, params)
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
	applyStockBandwidthParams(ob, params)
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

func applyTuicZeroRTT(m map[string]any, preset string, params map[string]string) {
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
	switch preset {
	case "vless_custom", "trojan_custom", "vmess_custom":
	default:
		return
	}
	cleanupV2RayTransport(m, params)
	if strings.TrimSpace(params["flow"]) == "" {
		delete(m, "flow")
	}
	if alpn := strings.TrimSpace(params["alpn"]); alpn != "" {
		if tls, ok := m["tls"].(map[string]any); ok && tls != nil {
			tls["alpn"] = splitCSV(alpn)
		}
	}
	if strings.TrimSpace(params["packet_encoding"]) == "" {
		delete(m, "packet_encoding")
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
		return
	}
	if v := strings.TrimSpace(params["mode"]); v != "" {
		tr["upstream_protocol"] = v
	}
	if strings.TrimSpace(params["anti_dpi"]) != "" {
		tr["anti_dpi"] = strings.EqualFold(strings.TrimSpace(params["anti_dpi"]), "true")
	}
	if strings.TrimSpace(params["enable_protocol_fallback"]) != "" {
		tr["enable_protocol_fallback"] = strings.EqualFold(strings.TrimSpace(params["enable_protocol_fallback"]), "true")
	}
}

func applyAnyTLSCustomKnobs(m map[string]any, preset string, params map[string]string, inbound bool) {
	if preset != "anytls_custom" {
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
	if strings.EqualFold(strings.TrimSpace(params["idle_session"]), "true") {
		m["idle_session_check_interval"] = "30s"
		m["idle_session_timeout"] = "30s"
		m["min_idle_session"] = 0
	} else {
		delete(m, "idle_session_check_interval")
		delete(m, "idle_session_timeout")
		delete(m, "min_idle_session")
	}
}

func applyShadowTLSCustomKnobs(m map[string]any, preset string, params map[string]string) {
	if preset != "shadowtls_custom" {
		return
	}
	if _, ok := m["strict_mode"]; ok || strings.TrimSpace(params["strict_mode"]) != "" {
		m["strict_mode"] = !strings.EqualFold(strings.TrimSpace(params["strict_mode"]), "false")
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
	if preset != "mieru_custom" {
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
	if preset != "derp_custom" {
		return
	}
	if strings.TrimSpace(params["websocket"]) != "" {
		m["websocket"] = strings.EqualFold(strings.TrimSpace(params["websocket"]), "true")
	}
	_ = inbound
}

func applyCloudflaredCustomKnobs(m map[string]any, preset string, params map[string]string) {
	if preset != "cloudflared_custom" {
		return
	}
	if strings.TrimSpace(params["post_quantum"]) != "" {
		m["post_quantum"] = !strings.EqualFold(strings.TrimSpace(params["post_quantum"]), "false")
	}
	if v, ok := parseUintParam(params["ha_connections"]); ok {
		m["ha_connections"] = v
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
