//go:build with_controlplane

package materialize

import (
	"strconv"
	"strings"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
)

func applyCustomPresetInboundKnobs(ib map[string]any, preset string, params map[string]string) {
	applyTuicZeroRTT(ib, preset, params)
	applyNaiveNetworkKnobs(ib, preset, params)
	applyHy2CustomKnobs(ib, preset, params)
	applyVlessLikeCustomKnobs(ib, preset, params)
	applyShadowsocksCustomKnobs(ib, preset, params)
	applyHysteria1CustomKnobs(ib, preset, params)
	applySocksCustomKnobs(ib, preset, params, true)
	applyHTTPMixedCustomKnobs(ib, preset, params, true)
	applyTrustTunnelCustomKnobs(ib, preset, params)
}

func applyCustomPresetOutboundKnobs(ob map[string]any, preset string, params map[string]string) {
	applyTuicZeroRTT(ob, preset, params)
	applyNaiveNetworkKnobs(ob, preset, params)
	applyHy2CustomKnobs(ob, preset, params)
	applyVlessLikeCustomKnobs(ob, preset, params)
	applyShadowsocksCustomKnobs(ob, preset, params)
	applyHysteria1CustomKnobs(ob, preset, params)
	applySocksCustomKnobs(ob, preset, params, false)
	applyHTTPMixedCustomKnobs(ob, preset, params, false)
	applyTrustTunnelCustomKnobs(ob, preset, params)
	stripUTLSForQUICTransport(ob)
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
	if _, isIn := m["ignore_client_bandwidth"]; isIn || params["ignore_client_bandwidth"] != "" {
		m["ignore_client_bandwidth"] = strings.EqualFold(strings.TrimSpace(params["ignore_client_bandwidth"]), "true")
	}

	// Masquerade is inbound-only; outbound templates omit it.
	if _, has := m["masquerade"]; !has && params["masquerade_mode"] == "" {
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
		dir := strings.TrimSpace(params["masquerade_dir"])
		m["masquerade"] = map[string]any{"type": "file", "directory": dir}
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
	if flow := strings.TrimSpace(params["flow"]); flow == "" {
		delete(m, "flow")
	}
	if alpn := strings.TrimSpace(params["alpn"]); alpn != "" {
		if tls, ok := m["tls"].(map[string]any); ok && tls != nil {
			tls["alpn"] = splitCSV(alpn)
		}
	}
	if enc := strings.TrimSpace(params["packet_encoding"]); enc == "" {
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
		typ = strings.ToLower(strings.TrimSpace(asString(tr["type"])))
	}
	tr["type"] = typ
	switch typ {
	case "tcp", "":
		delete(m, "transport")
		return
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
	// Drop empty host/path left by unused optional params.
	if s := strings.TrimSpace(asString(tr["host"])); s == "" {
		delete(tr, "host")
	}
	if s := strings.TrimSpace(asString(tr["path"])); s == "" {
		delete(tr, "path")
	}
	if s := strings.TrimSpace(asString(tr["service_name"])); s == "" {
		delete(tr, "service_name")
	}
	if headers, ok := tr["headers"].(map[string]any); ok {
		if host, ok := headers["Host"].([]any); ok {
			if len(host) == 0 || strings.TrimSpace(asString(host[0])) == "" {
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
	if preset != "socks_custom" {
		return
	}
	if inbound {
		return
	}
	if strings.EqualFold(strings.TrimSpace(params["udp_over_tcp"]), "true") {
		m["udp_over_tcp"] = map[string]any{"enabled": true, "version": 2}
	} else {
		delete(m, "udp_over_tcp")
	}
}

func applyHTTPMixedCustomKnobs(m map[string]any, preset string, params map[string]string, inbound bool) {
	switch preset {
	case "http_custom", "mixed_custom":
	default:
		return
	}
	mode, ok := domain.BindingTLSMode(domain.ProtocolPreset{
		CustomPreset: true,
		ParamMeta: map[string]domain.ParamFieldMeta{
			"tls_mode": {Default: "none"},
		},
	}, params)
	if !ok {
		mode = strings.ToLower(strings.TrimSpace(params["tls_mode"]))
		if mode == "" {
			mode = "none"
		}
	}
	if mode == "none" {
		delete(m, "tls")
		return
	}
	// TLS block kept; materialize attach path fills certs when BindingNeedsPEMTLS.
	if !inbound {
		if fp := strings.TrimSpace(params["fingerprint"]); fp != "" {
			tls, _ := m["tls"].(map[string]any)
			if tls == nil {
				tls = map[string]any{"enabled": true}
				m["tls"] = tls
			}
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
	if params["anti_dpi"] != "" {
		tr["anti_dpi"] = strings.EqualFold(strings.TrimSpace(params["anti_dpi"]), "true")
	}
	if params["enable_protocol_fallback"] != "" {
		tr["enable_protocol_fallback"] = strings.EqualFold(strings.TrimSpace(params["enable_protocol_fallback"]), "true")
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

func asString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return strings.TrimSpace(strings.Trim(strings.ReplaceAll(strings.ReplaceAll(stringify(v), "\"", ""), "[", ""), "]"))
}

func stringify(v any) string {
	switch t := v.(type) {
	case string:
		return t
	default:
		return strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(sprintfAny(t), "\n", ""), "\t", ""))
	}
}

func sprintfAny(v any) string {
	return strings.TrimSpace(
		strings.ReplaceAll(
			strings.ReplaceAll(
				func() string {
					b := make([]byte, 0, 32)
					return string(append(b, []byte(toRaw(v))...))
				}(),
				" ", ""),
			"\"", ""),
	)
}

func toRaw(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case int:
		return strconv.Itoa(t)
	default:
		return ""
	}
}
