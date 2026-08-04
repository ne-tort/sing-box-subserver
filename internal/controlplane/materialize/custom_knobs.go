//go:build with_controlplane

package materialize

import (
	"fmt"
	"net"
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
	applySSHCustomKnobs(ib, preset, params, true)
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
	applySSHCustomKnobs(ob, preset, params, false)
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
	if _, hasListen := m["listen"]; !hasListen {
		delete(m, "ignore_client_bandwidth")
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
	if v := strings.TrimSpace(params["version"]); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 6 {
			m["version"] = n
		}
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
		if host := strings.TrimSpace(params["obfs_host"]); host != "" {
			m["obfs_host"] = host
		}
	}
}

func applyShadowQUICKnobs(m map[string]any, params map[string]string) {
	typ, _ := m["type"].(string)
	if typ != "shadowquic" {
		return
	}
	_, isInbound := m["listen"]
	if addr := strings.TrimSpace(params["jls_addr"]); addr != "" && isInbound {
		up, _ := m["jls_upstream"].(map[string]any)
		if up == nil {
			up = map[string]any{}
			m["jls_upstream"] = up
		}
		up["addr"] = addr
	}
	if sni := strings.TrimSpace(params["jls_server_name"]); sni != "" {
		if isInbound {
			up, _ := m["jls_upstream"].(map[string]any)
			if up == nil {
				up = map[string]any{}
				m["jls_upstream"] = up
			}
			up["server_name"] = sni
		} else {
			m["server_name"] = sni
			m["sni"] = sni
		}
	}
	if v := strings.TrimSpace(params["congestion_control"]); v != "" {
		m["congestion_control"] = v
	}
	if strings.TrimSpace(params["zero_rtt"]) != "" {
		m["zero_rtt"] = strings.EqualFold(strings.TrimSpace(params["zero_rtt"]), "true")
	}
	if !isInbound {
		if strings.TrimSpace(params["udp_over_stream"]) != "" {
			m["udp_over_stream"] = strings.EqualFold(strings.TrimSpace(params["udp_over_stream"]), "true")
		} else {
			delete(m, "udp_over_stream")
		}
	} else {
		delete(m, "udp_over_stream")
	}
	// Outbound must never carry inbound-only jls_upstream.
	if !isInbound {
		delete(m, "jls_upstream")
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
	// zero_rtt applies on constructor + SQLite-owned ready presets (param_values).
	if preset != "tuic_custom" && !catalogsqlite.Owns(preset) {
		return
	}
	if strings.EqualFold(strings.TrimSpace(params["zero_rtt"]), "true") {
		m["zero_rtt_handshake"] = true
	} else {
		delete(m, "zero_rtt_handshake")
	}
}

func applyNaiveNetworkKnobs(m map[string]any, preset string, params map[string]string) {
	switch {
	case preset == "naive_custom":
	case catalogsqlite.Owns(preset):
		proto, ok := catalogsqlite.ProtocolOf(preset)
		if !ok || proto != "naive" {
			return
		}
	default:
		return
	}
	netw := normalizeNaiveNetwork(params["network"])
	_, isIn := m["listen"]
	if netw == "tcp,udp" {
		// Product: H3/QUIC always rides with H2 — one inbound listens both.
		if isIn {
			m["network"] = []any{"tcp", "udp"}
			if tls, ok := m["tls"].(map[string]any); tls != nil && ok {
				tls["alpn"] = []any{"h2", "h3"}
			}
		} else {
			// Outbound split happens in RenderSubscription; default dial is H2.
			delete(m, "network")
			delete(m, "quic")
			delete(m, "quic_congestion_control")
		}
		return
	}
	// tcp = H2 only
	if isIn {
		m["network"] = "tcp"
		if tls, ok := m["tls"].(map[string]any); tls != nil && ok {
			tls["alpn"] = []any{"h2"}
		}
	} else {
		delete(m, "network")
	}
	delete(m, "quic")
	delete(m, "quic_congestion_control")
}

// normalizeNaiveNetwork maps UI/stock values onto product semantics:
// tcp = H2 only; anything enabling H3/QUIC becomes tcp+udp (H2 cannot be disabled).
func normalizeNaiveNetwork(raw string) string {
	netw := strings.ToLower(strings.TrimSpace(raw))
	switch netw {
	case "", "tcp", "h2", "http2":
		return "tcp"
	case "udp", "quic", "h3", "http3", "tcp,udp", "tcp+udp", "both", "dual":
		return "tcp,udp"
	default:
		if strings.Contains(netw, "udp") || strings.Contains(netw, "quic") {
			return "tcp,udp"
		}
		return "tcp"
	}
}

func naiveNetworkIsDual(params map[string]string) bool {
	return normalizeNaiveNetwork(params["network"]) == "tcp,udp"
}

func applyHy2CustomKnobs(m map[string]any, preset string, params map[string]string) {
	switch {
	case preset == "hy2_custom":
	case catalogsqlite.Owns(preset):
		proto, ok := catalogsqlite.ProtocolOf(preset)
		if !ok || proto != "hysteria2" {
			return
		}
	default:
		return
	}
	_, hasListen := m["listen"]
	_, hasServer := m["server"]
	isOutbound := hasServer && !hasListen

	obfsType := strings.ToLower(strings.TrimSpace(params["obfs_type"]))
	if obfsType == "" {
		obfsType = "none"
	}
	obfsPassword := ""
	if prev, ok := m["obfs"].(map[string]any); ok && prev != nil {
		obfsPassword = strings.TrimSpace(fmt.Sprint(prev["password"]))
		if obfsPassword == "<nil>" {
			obfsPassword = ""
		}
	}
	switch obfsType {
	case "none", "off", "false", "0":
		delete(m, "obfs")
	case "salamander":
		obfs := map[string]any{"type": "salamander"}
		if obfsPassword != "" {
			obfs["password"] = obfsPassword
		}
		m["obfs"] = obfs
	case "gecko":
		obfs := map[string]any{
			"type":            "gecko",
			"min_packet_size": 512,
			"max_packet_size": 1200,
		}
		if obfsPassword != "" {
			obfs["password"] = obfsPassword
		}
		m["obfs"] = obfs
	case "gecko_compact":
		obfs := map[string]any{
			"type":            "gecko",
			"min_packet_size": 100,
			"max_packet_size": 300,
		}
		if obfsPassword != "" {
			obfs["password"] = obfsPassword
		}
		m["obfs"] = obfs
	}

	if v, ok := parseUintParam(params["up_mbps"]); ok {
		m["up_mbps"] = v
	}
	if v, ok := parseUintParam(params["down_mbps"]); ok {
		m["down_mbps"] = v
	}
	if !isOutbound && strings.TrimSpace(params["ignore_client_bandwidth"]) != "" {
		m["ignore_client_bandwidth"] = strings.EqualFold(strings.TrimSpace(params["ignore_client_bandwidth"]), "true")
	}
	if isOutbound {
		delete(m, "ignore_client_bandwidth")
	}

	if isOutbound {
		delete(m, "masquerade")
	} else {
		mode := strings.ToLower(strings.TrimSpace(params["masquerade_mode"]))
		if mode == "" {
			mode = "none"
		}
		switch mode {
		case "none", "off", "false", "0":
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
				"headers": map[string]any{
					"Content-Type": []any{"text/html; charset=utf-8"},
					"Server":       []any{"nginx"},
				},
				"content": "<!DOCTYPE html><html><head><title>Welcome</title></head><body><h1>It works!</h1></body></html>",
			}
		}
	}

	realmMode := strings.ToLower(strings.TrimSpace(params["realm_mode"]))
	if realmMode == "" || realmMode == "none" || realmMode == "off" || realmMode == "false" || realmMode == "0" {
		delete(m, "realm")
	}
}

func applyVlessLikeCustomKnobs(m map[string]any, preset string, params map[string]string) {
	switch {
	case preset == "vless_custom", preset == "trojan_custom", preset == "vmess_custom":
	case catalogsqlite.Owns(preset):
		proto, ok := catalogsqlite.ProtocolOf(preset)
		if !ok {
			return
		}
		switch proto {
		case "vless", "vmess", "trojan":
		default:
			return
		}
	default:
		return
	}
	_, isInbound := m["listen"]
	cleanupV2RayTransport(m, params)
	// Flow is user-symmetric (inbound users / subscription variants), never a root inbound field.
	if isInbound {
		delete(m, "flow")
	} else if strings.TrimSpace(params["flow"]) == "" || strings.EqualFold(strings.TrimSpace(params["flow"]), "none") {
		delete(m, "flow")
	} else if v := strings.TrimSpace(params["flow"]); v != "" {
		m["flow"] = v
	}
	if alpn := strings.TrimSpace(params["alpn"]); alpn != "" {
		if tls, ok := m["tls"].(map[string]any); ok && tls != nil {
			tls["alpn"] = splitCSV(alpn)
		}
	}
	// packet_encoding is outbound-only; client profiles may clear/override after knobs.
	if isInbound {
		delete(m, "packet_encoding")
	} else {
		enc := strings.TrimSpace(params["packet_encoding"])
		if enc == "" || strings.EqualFold(enc, "none") {
			delete(m, "packet_encoding")
		} else {
			m["packet_encoding"] = enc
		}
	}
	applyVlessMultiplexKnob(m, params)
	applyVlessWSEarlyDataKnob(m, params)
	applyTrojanFallbackKnob(m, params)
}

func applyTrojanFallbackKnob(m map[string]any, params map[string]string) {
	typ, _ := m["type"].(string)
	if typ != "trojan" {
		return
	}
	_, isInbound := m["listen"]
	if !isInbound {
		delete(m, "fallback")
		delete(m, "fallback_for_alpn")
		return
	}
	raw := strings.TrimSpace(params["fallback"])
	mode := strings.ToLower(raw)
	if mode == "" || mode == "none" || mode == "false" || mode == "0" || mode == "off" {
		delete(m, "fallback")
		delete(m, "fallback_for_alpn")
		return
	}
	host := "127.0.0.1"
	port := 18080
	if mode != "local" {
		h, p, err := net.SplitHostPort(raw)
		if err != nil {
			delete(m, "fallback")
			delete(m, "fallback_for_alpn")
			return
		}
		host = strings.TrimSpace(h)
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil || host == "" || n <= 0 || n > 65535 {
			delete(m, "fallback")
			delete(m, "fallback_for_alpn")
			return
		}
		port = n
	}
	target := map[string]any{"server": host, "server_port": port}
	m["fallback"] = target
	m["fallback_for_alpn"] = map[string]any{
		"h2":        map[string]any{"server": host, "server_port": port},
		"http/1.1":  map[string]any{"server": host, "server_port": port},
	}
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
		// sing-box WS/HTTPUpgrade have no top-level host; SNI/Host lives in headers.
		delete(tr, "host")
		delete(tr, "service_name")
		delete(tr, "password")
		delete(tr, "version")
	case "http":
		delete(tr, "service_name")
		delete(tr, "password")
		delete(tr, "version")
		// HTTP transport.host must be a string array.
		if host := strings.TrimSpace(fmt.Sprint(tr["host"])); host != "" && host != "<nil>" {
			tr["host"] = []any{host}
		} else {
			delete(tr, "host")
		}
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
	if typ != "http" {
		pruneEmptyStringKey(tr, "host")
	}
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
	switch {
	case preset == "shadowsocks_custom":
	case catalogsqlite.Owns(preset):
		proto, ok := catalogsqlite.ProtocolOf(preset)
		if !ok || proto != "shadowsocks" {
			return
		}
	default:
		return
	}
	if method := strings.TrimSpace(params["method"]); method != "" {
		m["method"] = method
	}
	net := strings.ToLower(strings.TrimSpace(params["network"]))
	if net == "" {
		net = "tcp"
	}
	m["network"] = net
	_, isInbound := m["listen"]
	if !isInbound && strings.EqualFold(strings.TrimSpace(params["udp_over_tcp"]), "true") {
		m["udp_over_tcp"] = map[string]any{"enabled": true, "version": 2}
	} else {
		delete(m, "udp_over_tcp")
	}
	applyVlessMultiplexKnob(m, params)
}

func applyHysteria1CustomKnobs(m map[string]any, preset string, params map[string]string) {
	switch {
	case preset == "hysteria_custom":
	case catalogsqlite.Owns(preset):
		proto, ok := catalogsqlite.ProtocolOf(preset)
		if !ok || proto != "hysteria" {
			return
		}
	default:
		return
	}
	if v, ok := parseUintParam(params["up_mbps"]); ok {
		m["up_mbps"] = v
	}
	if v, ok := parseUintParam(params["down_mbps"]); ok {
		m["down_mbps"] = v
	}
	obfs := strings.TrimSpace(params["obfs"])
	if obfs == "" || strings.EqualFold(obfs, "none") || strings.EqualFold(obfs, "false") {
		delete(m, "obfs")
		return
	}
	// "peer" / non-none: keep substituted {{peer.obfs}} when present.
	if cur := strings.TrimSpace(fmt.Sprint(m["obfs"])); cur == "" || cur == "<nil>" || strings.Contains(cur, "{{") {
		delete(m, "obfs")
	}
}

func applySocksCustomKnobs(m map[string]any, preset string, params map[string]string, inbound bool) {
	switch {
	case preset == "socks_custom":
	case catalogsqlite.Owns(preset):
		proto, ok := catalogsqlite.ProtocolOf(preset)
		if !ok || proto != "socks" {
			return
		}
	default:
		return
	}
	if inbound {
		delete(m, "udp_over_tcp")
		return
	}
	if strings.EqualFold(strings.TrimSpace(params["udp_over_tcp"]), "true") {
		m["udp_over_tcp"] = map[string]any{"enabled": true, "version": 2}
	} else {
		delete(m, "udp_over_tcp")
	}
}

func applyHTTPMixedCustomKnobs(m map[string]any, preset string, params map[string]string) {
	switch {
	case preset == "http_custom", preset == "mixed_custom":
	case catalogsqlite.Owns(preset):
		proto, ok := catalogsqlite.ProtocolOf(preset)
		if !ok || (proto != "http" && proto != "mixed") {
			return
		}
	default:
		return
	}
	proto, _ := catalogsqlite.ProtocolOf(preset)
	if preset == "mixed_custom" || proto == "mixed" {
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
	switch {
	case preset == "trusttunnel_custom":
	case catalogsqlite.Owns(preset):
		proto, ok := catalogsqlite.ProtocolOf(preset)
		if !ok || proto != "trusttunnel" {
			return
		}
	default:
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
		if scheme := strings.TrimSpace(params["padding_scheme"]); scheme != "" {
			lines := splitPaddingScheme(scheme)
			if len(lines) > 0 {
				m["padding_scheme"] = lines
			}
		}
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
	if ws := strings.TrimSpace(params["wildcard_sni"]); ws != "" {
		m["wildcard_sni"] = ws
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
	switch {
	case preset == "sudoku_custom":
	case catalogsqlite.Owns(preset):
		proto, ok := catalogsqlite.ProtocolOf(preset)
		if !ok || proto != "sudoku" {
			return
		}
	default:
		return
	}
	if v := strings.TrimSpace(params["aead_method"]); v != "" {
		m["aead_method"] = v
	}
	if v := strings.TrimSpace(params["multiplex"]); v != "" {
		m["multiplex"] = v
	}
	if v, ok := parseUintParam(params["padding_min"]); ok {
		m["padding_min"] = v
	}
	if v, ok := parseUintParam(params["padding_max"]); ok {
		m["padding_max"] = v
	}
	if fb := strings.TrimSpace(params["fallback"]); fb != "" {
		if _, isIn := m["listen"]; isIn {
			m["fallback"] = fb
		}
	}
	mode := strings.ToLower(strings.TrimSpace(params["httpmask_mode"]))
	if mode == "" || mode == "off" || mode == "none" || mode == "false" {
		delete(m, "httpmask")
	} else {
		pathRoot := strings.TrimSpace(params["httpmask_path"])
		if pathRoot == "" {
			pathRoot = "/sudoku"
		}
		m["httpmask"] = map[string]any{
			"disable":   false,
			"mode":      mode,
			"path_root": pathRoot,
		}
	}
}

func applyMieruCustomKnobs(m map[string]any, preset string, params map[string]string) {
	switch {
	case preset == "mieru_custom":
	case catalogsqlite.Owns(preset):
		proto, ok := catalogsqlite.ProtocolOf(preset)
		if !ok || proto != "mieru" {
			return
		}
	default:
		typ, _ := m["type"].(string)
		if typ != "mieru" {
			return
		}
	}
	if v := strings.TrimSpace(params["transport"]); v != "" {
		m["transport"] = v
	}
	if v, ok := parseUintParam(params["mtu"]); ok {
		m["mtu"] = v
	}
	_, isInbound := m["listen"]
	if !isInbound {
		if mux := strings.TrimSpace(params["multiplexing"]); mux != "" {
			m["multiplexing"] = mux
		}
	}
	if tp := strings.TrimSpace(params["traffic_pattern"]); tp != "" {
		m["traffic_pattern"] = tp
	} else {
		delete(m, "traffic_pattern")
	}
}

func applyDerpCustomKnobs(m map[string]any, preset string, params map[string]string, inbound bool) {
	switch {
	case preset == "derp_custom":
	case catalogsqlite.Owns(preset):
		proto, ok := catalogsqlite.ProtocolOf(preset)
		if !ok || proto != "derp" {
			return
		}
	default:
		typ, _ := m["type"].(string)
		if typ != "derp" {
			return
		}
	}
	if p := strings.TrimSpace(params["path"]); p != "" {
		m["path"] = p
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

func applySSHCustomKnobs(m map[string]any, preset string, params map[string]string, inbound bool) {
	switch {
	case preset == "ssh_custom":
	case catalogsqlite.Owns(preset):
		proto, ok := catalogsqlite.ProtocolOf(preset)
		if !ok || proto != "ssh" {
			return
		}
	default:
		return
	}
	if v := strings.TrimSpace(params["server_version"]); v != "" && inbound {
		m["server_version"] = v
	}
	if v := strings.TrimSpace(params["client_version"]); v != "" && !inbound {
		m["client_version"] = v
	}
	if inbound {
		delete(m, "udp_over_tcp")
		delete(m, "private_key")
		delete(m, "password")
		return
	}
	auth := strings.ToLower(strings.TrimSpace(params["auth_mode"]))
	if auth == "" {
		auth = "password"
	}
	if auth == "pubkey" {
		delete(m, "password")
		// Drop empty private_key placeholders.
		if pk, ok := m["private_key"].([]any); ok {
			clean := make([]any, 0, len(pk))
			for _, item := range pk {
				if strings.TrimSpace(fmt.Sprint(item)) != "" {
					clean = append(clean, item)
				}
			}
			if len(clean) == 0 {
				delete(m, "private_key")
			} else {
				m["private_key"] = clean
			}
		}
	} else {
		delete(m, "private_key")
	}
	if strings.EqualFold(strings.TrimSpace(params["udp_over_tcp"]), "true") {
		m["udp_over_tcp"] = map[string]any{"enabled": true, "version": 2}
	} else {
		delete(m, "udp_over_tcp")
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
	switch {
	case preset == "carrier_custom":
	case catalogsqlite.Owns(preset):
		proto, ok := catalogsqlite.ProtocolOf(preset)
		if !ok || proto != "carrier" {
			return
		}
	default:
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
	case "peer", "vk":
		transport = ""
	}
	m["provider"] = provider
	auth := strings.ToLower(strings.TrimSpace(params["auth_mode"]))
	if auth == "" {
		auth = "shared"
	}
	// Top-level auth exists only on inbound templates.
	if _, ok := m["auth"]; ok {
		m["auth"] = auth
	}
	link, _ := m["link"].(map[string]any)
	if link == nil {
		link = map[string]any{}
		m["link"] = link
	}
	if transport != "" {
		link["transport"] = transport
	} else {
		delete(link, "transport")
	}
	if tok := strings.TrimSpace(params["token"]); tok != "" {
		link["token"] = tok
	} else if provider != "wbstream" {
		delete(link, "token")
	}
	if key := strings.TrimSpace(params["key"]); key != "" {
		link["key"] = key
	} else {
		delete(link, "key")
	}
	if room := strings.TrimSpace(params["room"]); room != "" {
		link["room"] = room
	} else if provider == "peer" || provider == "vk" {
		delete(link, "room")
	}
	if v := strings.TrimSpace(params["vk_hash"]); v != "" {
		link["vk_hash"] = v
	} else {
		delete(link, "vk_hash")
	}
	if v := strings.TrimSpace(params["wrap_password"]); v != "" {
		link["wrap_password"] = v
	} else {
		delete(link, "wrap_password")
	}
	// Drop empty string placeholders left by templates.
	for _, k := range []string{"room", "key", "token", "vk_hash", "wrap_password", "peer", "server", "server_port"} {
		if strings.TrimSpace(fmt.Sprint(link[k])) == "" || fmt.Sprint(link[k]) == "<nil>" {
			delete(link, k)
		}
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

// splitPaddingScheme accepts newline- or semicolon-separated AnyTLS padding_scheme lines.
func splitPaddingScheme(s string) []any {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, ";", "\n")
	parts := strings.Split(s, "\n")
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
