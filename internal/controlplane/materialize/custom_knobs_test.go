//go:build with_controlplane

package materialize

import "testing"

func TestApplyHy2CustomKnobs(t *testing.T) {
	t.Parallel()
	ib := map[string]any{
		"type": "hysteria2",
		"obfs": map[string]any{"type": "salamander", "password": "x"},
		"masquerade": map[string]any{
			"type":      "file",
			"directory": "/tmp",
		},
		"up_mbps":                 100,
		"down_mbps":               100,
		"ignore_client_bandwidth": true,
	}
	applyHy2CustomKnobs(ib, "hy2_custom", map[string]string{
		"obfs_type":               "none",
		"masquerade_mode":         "proxy",
		"masquerade_url":          "https://example.com",
		"up_mbps":                 "250",
		"down_mbps":               "500",
		"ignore_client_bandwidth": "false",
	})
	if _, ok := ib["obfs"]; ok {
		t.Fatal("obfs_type=none must strip obfs")
	}
	masq, _ := ib["masquerade"].(map[string]any)
	if masq["type"] != "proxy" || masq["url"] != "https://example.com" {
		t.Fatalf("masquerade=%#v", masq)
	}
	if ib["up_mbps"] != uint64(250) || ib["down_mbps"] != uint64(500) {
		t.Fatalf("bandwidth %#v %#v", ib["up_mbps"], ib["down_mbps"])
	}
	if ib["ignore_client_bandwidth"] != false {
		t.Fatalf("ignore_client_bandwidth=%v", ib["ignore_client_bandwidth"])
	}

	owned := map[string]any{
		"type": "hysteria2",
		"listen": "::",
		"obfs": map[string]any{"type": "salamander", "password": "peer-secret"},
		"realm": map[string]any{"server_url": "https://x"},
	}
	applyHy2CustomKnobs(owned, "hy2_gecko", map[string]string{
		"obfs_type":               "gecko",
		"masquerade_mode":         "string",
		"ignore_client_bandwidth": "true",
		"realm_mode":              "none",
	})
	obfs, _ := owned["obfs"].(map[string]any)
	if obfs["type"] != "gecko" || obfs["min_packet_size"] != 512 || obfs["password"] != "peer-secret" {
		t.Fatalf("gecko obfs %#v", obfs)
	}
	if _, ok := owned["realm"]; ok {
		t.Fatal("realm_mode=none must strip realm")
	}
	masq2, _ := owned["masquerade"].(map[string]any)
	if masq2["type"] != "string" {
		t.Fatalf("owned masquerade %#v", masq2)
	}
	ob := map[string]any{"type": "hysteria2", "server": "h", "masquerade": map[string]any{"type": "string"}}
	applyHy2CustomKnobs(ob, "hy2_masquerade", map[string]string{"masquerade_mode": "string", "obfs_type": "none"})
	if _, ok := ob["masquerade"]; ok {
		t.Fatal("outbound must strip masquerade")
	}
}

func TestApplyStockBandwidthParams(t *testing.T) {
	t.Parallel()
	ib := map[string]any{
		"type":      "hysteria2",
		"up_mbps":   100,
		"down_mbps": 100,
	}
	applyCustomPresetInboundKnobs(ib, "hy2", map[string]string{
		"up_mbps":   "80",
		"down_mbps": "200",
	})
	if ib["up_mbps"] != uint64(80) || ib["down_mbps"] != uint64(200) {
		t.Fatalf("stock bandwidth %#v %#v", ib["up_mbps"], ib["down_mbps"])
	}
	masq := map[string]any{"type": "hysteria2"}
	applyCustomPresetInboundKnobs(masq, "hy2_masquerade", map[string]string{
		"up_mbps":   "50",
		"down_mbps": "50",
	})
	if masq["up_mbps"] != uint64(50) || masq["down_mbps"] != uint64(50) {
		t.Fatalf("masquerade inject %#v", masq)
	}
}

func TestApplyCarrierCustomKnobs(t *testing.T) {
	t.Parallel()
	ib := map[string]any{
		"type":     "carrier",
		"provider": "jitsi",
		"link": map[string]any{
			"room":      "https://meet.jit.si/x",
			"transport": "datachannel",
		},
	}
	applyCarrierCustomKnobs(ib, "carrier_custom", map[string]string{
		"provider": "jitsi_sei",
		"token":    "tok",
	})
	if ib["provider"] != "jitsi" {
		t.Fatalf("provider=%v", ib["provider"])
	}
	link := ib["link"].(map[string]any)
	if link["transport"] != "seichannel" || link["token"] != "tok" {
		t.Fatalf("link=%#v", link)
	}
}

func TestApplyTuicKnobs(t *testing.T) {
	t.Parallel()
	ob := map[string]any{
		"type":               "tuic",
		"congestion_control": "bbr",
		"udp_relay_mode":     "native",
	}
	applyTuicKnobs(ob, "tuic", map[string]string{
		"congestion_control": "cubic",
		"udp_relay_mode":     "quic",
	})
	if ob["congestion_control"] != "cubic" || ob["udp_relay_mode"] != "quic" {
		t.Fatalf("tuic knobs %#v", ob)
	}
	custom := map[string]any{"type": "tuic", "listen": "::"}
	applyTuicKnobs(custom, "tuic_custom", map[string]string{"zero_rtt": "true", "congestion_control": "new_reno"})
	if custom["zero_rtt_handshake"] != true || custom["congestion_control"] != "new_reno" {
		t.Fatalf("custom %#v", custom)
	}
	if _, ok := custom["udp_relay_mode"]; ok {
		t.Fatal("inbound must not set udp_relay_mode")
	}
	ready0 := map[string]any{"type": "tuic", "listen": "::"}
	applyTuicKnobs(ready0, "tuic_0rtt", map[string]string{"zero_rtt": "true"})
	if ready0["zero_rtt_handshake"] != true {
		t.Fatalf("owned ready 0rtt %#v", ready0)
	}
	readyOff := map[string]any{"type": "tuic", "listen": "::", "zero_rtt_handshake": true}
	applyTuicKnobs(readyOff, "tuic", map[string]string{"zero_rtt": "false"})
	if _, ok := readyOff["zero_rtt_handshake"]; ok {
		t.Fatalf("zero_rtt=false must strip %#v", readyOff)
	}
}

func TestApplyStockShadowQUICAndShadowTLS(t *testing.T) {
	t.Parallel()
	sq := map[string]any{"type": "shadowquic", "congestion_control": "bbr", "zero_rtt": false}
	applyShadowQUICKnobs(sq, map[string]string{"congestion_control": "cubic", "zero_rtt": "true"})
	if sq["congestion_control"] != "cubic" || sq["zero_rtt"] != true {
		t.Fatalf("shadowquic %#v", sq)
	}
	st := map[string]any{"type": "shadowtls", "strict_mode": true}
	applyShadowTLSCustomKnobs(st, "shadowtls_v3", map[string]string{"strict_mode": "false"})
	if st["strict_mode"] != false {
		t.Fatalf("strict_mode=%v", st["strict_mode"])
	}
	hy := map[string]any{"type": "hysteria2", "listen": "0.0.0.0", "ignore_client_bandwidth": false}
	applyStockIgnoreClientBandwidth(hy, map[string]string{"ignore_client_bandwidth": "true"})
	if hy["ignore_client_bandwidth"] != true {
		t.Fatalf("ignore=%v", hy["ignore_client_bandwidth"])
	}
	hyOut := map[string]any{"type": "hysteria2", "ignore_client_bandwidth": true}
	applyStockIgnoreClientBandwidth(hyOut, map[string]string{"ignore_client_bandwidth": "true"})
	if _, ok := hyOut["ignore_client_bandwidth"]; ok {
		t.Fatalf("outbound must strip ignore_client_bandwidth: %#v", hyOut)
	}
}

func TestApplyAnyTLSAndDerpStockKnobs(t *testing.T) {
	t.Parallel()
	ob := map[string]any{
		"type": "anytls",
		"tls":  map[string]any{"enabled": true, "alpn": []any{"h2"}},
	}
	applyAnyTLSCustomKnobs(ob, "anytls", map[string]string{"alpn": "http/1.1", "fingerprint": "firefox"}, false)
	tls := ob["tls"].(map[string]any)
	alpn := tls["alpn"].([]any)
	if len(alpn) != 1 || alpn[0] != "http/1.1" {
		t.Fatalf("alpn=%#v", alpn)
	}
	utls := tls["utls"].(map[string]any)
	if utls["fingerprint"] != "firefox" {
		t.Fatalf("utls=%#v", utls)
	}

	ib := map[string]any{"type": "anytls", "tls": map[string]any{"enabled": true}}
	applyAnyTLSCustomKnobs(ib, "anytls_custom", map[string]string{
		"padding_scheme": "stop=2\n0=10-10\n1=20-30",
	}, true)
	scheme, _ := ib["padding_scheme"].([]any)
	if len(scheme) != 3 || scheme[0] != "stop=2" || scheme[2] != "1=20-30" {
		t.Fatalf("padding_scheme=%#v", scheme)
	}

	derp := map[string]any{"type": "derp", "udp": "native", "websocket": false}
	applyDerpCustomKnobs(derp, "derp_tls", map[string]string{"udp": "disabled", "websocket": "true"}, true)
	// "disabled" was a historical UI value; wire only has native|uot → map to native.
	if derp["udp"] != "native" || derp["websocket"] != true {
		t.Fatalf("derp=%#v", derp)
	}
	applyDerpCustomKnobs(derp, "derp_tls", map[string]string{"udp": "udp_over_tcp"}, true)
	if derp["udp"] != "uot" {
		t.Fatalf("uot alias %#v", derp)
	}
}

func TestApplyTrojanFallbackKnob(t *testing.T) {
	t.Parallel()
	ib := map[string]any{"type": "trojan", "listen": "0.0.0.0"}
	applyVlessLikeCustomKnobs(ib, "trojan_tls_fallback", map[string]string{
		"transport": "tcp",
		"fallback":  "local",
	})
	fb, _ := ib["fallback"].(map[string]any)
	if fb["server"] != "127.0.0.1" || fb["server_port"] != 18080 {
		t.Fatalf("fallback=%#v", fb)
	}
	alpn, _ := ib["fallback_for_alpn"].(map[string]any)
	if alpn["h2"] == nil || alpn["http/1.1"] == nil {
		t.Fatalf("fallback_for_alpn=%#v", alpn)
	}

	ob := map[string]any{"type": "trojan", "server": "h.example", "fallback": map[string]any{"server": "x"}}
	applyVlessLikeCustomKnobs(ob, "trojan_tls_fallback", map[string]string{"fallback": "local"})
	if _, ok := ob["fallback"]; ok {
		t.Fatal("outbound must strip fallback")
	}

	ibNone := map[string]any{"type": "trojan", "listen": "0.0.0.0", "fallback": map[string]any{"server": "x"}}
	applyVlessLikeCustomKnobs(ibNone, "trojan_custom", map[string]string{"fallback": "none"})
	if _, ok := ibNone["fallback"]; ok {
		t.Fatal("fallback=none must strip")
	}

	ibCustom := map[string]any{"type": "trojan", "listen": "0.0.0.0"}
	applyVlessLikeCustomKnobs(ibCustom, "trojan_custom", map[string]string{"fallback": "10.0.0.1:8080"})
	fb2, _ := ibCustom["fallback"].(map[string]any)
	if fb2["server"] != "10.0.0.1" || fb2["server_port"] != 8080 {
		t.Fatalf("custom fallback=%#v", fb2)
	}
}

func TestCleanupV2RayTransportDropsWSHost(t *testing.T) {
	t.Parallel()
	m := map[string]any{
		"type": "vless",
		"listen": "0.0.0.0",
		"transport": map[string]any{
			"type": "ws",
			"path": "/vless-ws",
			"host": "example.com",
			"headers": map[string]any{"Host": []any{"example.com"}},
		},
	}
	applyVlessLikeCustomKnobs(m, "vless_ws_tls", map[string]string{
		"transport":      "ws",
		"transport_path": "/vless-ws",
		"transport_host": "example.com",
	})
	tr, _ := m["transport"].(map[string]any)
	if _, ok := tr["host"]; ok {
		t.Fatalf("ws host must be removed: %#v", tr)
	}
	if tr["path"] != "/vless-ws" {
		t.Fatalf("path=%v", tr["path"])
	}
}

func TestApplyVlessLikeFlowNone(t *testing.T) {
	t.Parallel()
	m := map[string]any{
		"type":            "vless",
		"flow":            "none",
		"packet_encoding": "none",
		"transport":       map[string]any{"type": "tcp"},
	}
	applyVlessLikeCustomKnobs(m, "vless_custom", map[string]string{
		"transport":       "tcp",
		"flow":            "none",
		"packet_encoding": "none",
	})
	if _, ok := m["flow"]; ok {
		t.Fatalf("flow none must be stripped: %#v", m)
	}
	if _, ok := m["packet_encoding"]; ok {
		t.Fatalf("packet_encoding none must be stripped: %#v", m)
	}
	m2 := map[string]any{"type": "vless", "transport": map[string]any{"type": "tcp"}}
	applyVlessLikeCustomKnobs(m2, "vless_custom", map[string]string{
		"transport": "tcp",
		"flow":      "xtls-rprx-vision",
	})
	if m2["flow"] != "xtls-rprx-vision" {
		t.Fatalf("flow=%v", m2["flow"])
	}
}

func TestApplyStockUTLSAndSnell(t *testing.T) {
	t.Parallel()
	ob := map[string]any{
		"type": "vless",
		"tls": map[string]any{
			"enabled": true,
			"utls":    map[string]any{"enabled": true, "fingerprint": "chrome"},
		},
	}
	applyStockUTLSFingerprint(ob, map[string]string{"fingerprint": "safari"})
	if ob["tls"].(map[string]any)["utls"].(map[string]any)["fingerprint"] != "safari" {
		t.Fatalf("%#v", ob["tls"])
	}
	sn := map[string]any{"type": "snell", "obfs_mode": "http", "obfs_host": "x"}
	applySnellKnobs(sn, map[string]string{"obfs_mode": "off"})
	if _, ok := sn["obfs_mode"]; ok {
		t.Fatalf("obfs must be stripped: %#v", sn)
	}
}

func TestCleanupV2RayTransportTCP(t *testing.T) {
	t.Parallel()
	m := map[string]any{
		"transport": map[string]any{
			"type":         "tcp",
			"path":         "/x",
			"service_name": "GunService",
		},
	}
	cleanupV2RayTransport(m, map[string]string{"transport": "tcp"})
	if _, ok := m["transport"]; ok {
		t.Fatal("tcp transport must be removed")
	}
}

func TestCleanupV2RayTransportGRPC(t *testing.T) {
	t.Parallel()
	m := map[string]any{
		"transport": map[string]any{
			"type":         "grpc",
			"path":         "/x",
			"host":         "h",
			"service_name": "GunService",
			"headers":      map[string]any{"Host": []any{"h"}},
		},
	}
	cleanupV2RayTransport(m, map[string]string{"transport": "grpc"})
	tr := m["transport"].(map[string]any)
	if tr["service_name"] != "GunService" {
		t.Fatalf("service_name=%v", tr["service_name"])
	}
	if _, ok := tr["path"]; ok {
		t.Fatal("grpc must drop path")
	}
	if _, ok := tr["headers"]; ok {
		t.Fatal("grpc must drop headers")
	}
}

func TestApplyTrustTunnelCustomKnobs(t *testing.T) {
	t.Parallel()
	ob := map[string]any{
		"type":        "trusttunnel",
		"enable_udp":  true,
		"transport":   map[string]any{"upstream_protocol": "auto", "anti_dpi": true},
	}
	applyTrustTunnelCustomKnobs(ob, "trusttunnel_custom", map[string]string{
		"mode":                 "h2",
		"anti_dpi":             "false",
		"enable_udp":           "false",
		"force_http1_connect":  "true",
		"enable_protocol_fallback": "false",
	})
	tr := ob["transport"].(map[string]any)
	if tr["upstream_protocol"] != "http2" {
		t.Fatalf("upstream_protocol=%v want http2", tr["upstream_protocol"])
	}
	if tr["anti_dpi"] != false {
		t.Fatalf("anti_dpi=%v", tr["anti_dpi"])
	}
	if tr["force_http1_connect"] != true {
		t.Fatalf("force_http1_connect=%v", tr["force_http1_connect"])
	}
	if ob["enable_udp"] != false {
		t.Fatalf("enable_udp=%v", ob["enable_udp"])
	}
	if normalizeTrustTunnelUpstream("h3") != "http3" {
		t.Fatal("h3 alias")
	}
}

func TestNormalizeDerpUDPAndShadowQUIC(t *testing.T) {
	t.Parallel()
	if normalizeDerpUDP("udp_over_tcp") != "uot" {
		t.Fatal("uot alias")
	}
	if normalizeDerpUDP("disabled") != "native" {
		t.Fatal("disabled→native")
	}
	derp := map[string]any{"type": "derp", "udp": "native", "websocket": false}
	applyDerpCustomKnobs(derp, "derp_custom", map[string]string{"udp": "uot", "websocket": "true"}, true)
	if derp["udp"] != "uot" || derp["websocket"] != true {
		t.Fatalf("%#v", derp)
	}
	sq := map[string]any{"type": "shadowquic", "zero_rtt": true, "udp_over_stream": false}
	applyShadowQUICKnobs(sq, map[string]string{
		"jls_addr":           "www.example.com:443",
		"jls_server_name":    "www.example.com",
		"congestion_control": "cubic",
		"zero_rtt":           "false",
		"udp_over_stream":    "true",
	})
	if sq["congestion_control"] != "cubic" || sq["zero_rtt"] != false || sq["udp_over_stream"] != true {
		t.Fatalf("%#v", sq)
	}
	if sq["server_name"] != "www.example.com" || sq["sni"] != "www.example.com" {
		t.Fatalf("outbound jls sni sync %#v", sq)
	}
	if _, ok := sq["jls_upstream"]; ok {
		t.Fatalf("outbound must not set jls_upstream %#v", sq)
	}
	sqIn := map[string]any{"type": "shadowquic", "listen": "0.0.0.0"}
	applyShadowQUICKnobs(sqIn, map[string]string{
		"udp_over_stream": "true",
		"jls_addr":        "cdn.example:443",
		"jls_server_name": "cdn.example",
	})
	if _, ok := sqIn["udp_over_stream"]; ok {
		t.Fatal("inbound must not set udp_over_stream")
	}
	if _, ok := sqIn["server_name"]; ok {
		t.Fatal("inbound must not mirror jls_server_name to top-level server_name")
	}
	upIn, _ := sqIn["jls_upstream"].(map[string]any)
	if upIn["addr"] != "cdn.example:443" || upIn["server_name"] != "cdn.example" {
		t.Fatalf("inbound jls %#v", upIn)
	}
}

func TestApplyShadowsocksKnobsOwned(t *testing.T) {
	t.Parallel()
	ob := map[string]any{
		"type": "shadowsocks", "server": "1.2.3.4", "method": "aes-128-gcm", "network": "tcp",
	}
	applyShadowsocksCustomKnobs(ob, "ss_aes128_mux", map[string]string{
		"method": "aes-128-gcm", "network": "tcp", "udp_over_tcp": "false", "multiplex": "smux",
	})
	mux, _ := ob["multiplex"].(map[string]any)
	if mux["enabled"] != true || mux["protocol"] != "smux" {
		t.Fatalf("mux=%#v", mux)
	}
	uot := map[string]any{"type": "shadowsocks", "server": "1.2.3.4"}
	applyShadowsocksCustomKnobs(uot, "ss_aes128_uot", map[string]string{
		"method": "aes-128-gcm", "network": "tcp", "udp_over_tcp": "true", "multiplex": "none",
	})
	u, _ := uot["udp_over_tcp"].(map[string]any)
	if u["enabled"] != true || u["version"] != 2 {
		t.Fatalf("uot=%#v", u)
	}
	if _, ok := uot["multiplex"]; ok {
		t.Fatalf("multiplex must be cleared: %#v", uot)
	}
}

func TestApplySocksHTTPMixedOwnedKnobs(t *testing.T) {
	t.Parallel()
	ob := map[string]any{"type": "socks", "server": "1.2.3.4"}
	applySocksCustomKnobs(ob, "socks_uot", map[string]string{"udp_over_tcp": "true"}, false)
	u, _ := ob["udp_over_tcp"].(map[string]any)
	if u["enabled"] != true {
		t.Fatalf("socks uot %#v", ob)
	}
	ib := map[string]any{"type": "http", "listen": "0.0.0.0", "tls": map[string]any{"enabled": true}}
	applyHTTPMixedCustomKnobs(ib, "http", map[string]string{"tls_mode": "none", "fingerprint": "chrome"})
	if _, ok := ib["tls"]; ok {
		t.Fatalf("plain http must strip tls %#v", ib)
	}
	obH := map[string]any{"type": "http", "server": "1.2.3.4", "tls": map[string]any{"enabled": true}}
	applyHTTPMixedCustomKnobs(obH, "http_tls", map[string]string{"tls_mode": "tls", "fingerprint": "firefox"})
	tls := obH["tls"].(map[string]any)
	utls := tls["utls"].(map[string]any)
	if utls["fingerprint"] != "firefox" {
		t.Fatalf("fingerprint %#v", tls)
	}
	obM := map[string]any{"type": "socks", "server": "1.2.3.4"}
	applyHTTPMixedCustomKnobs(obM, "mixed_tls", map[string]string{"tls_mode": "tls", "fingerprint": "chrome", "outbound_type": "http"})
	if obM["type"] != "http" {
		t.Fatalf("outbound_type rewrite %#v", obM)
	}
}

func TestApplyLXOwnedKnobs(t *testing.T) {
	t.Parallel()
	mieru := map[string]any{"type": "mieru", "server": "1.2.3.4", "traffic_pattern": ""}
	applyMieruCustomKnobs(mieru, "mieru_udp", map[string]string{
		"transport": "UDP", "multiplexing": "MULTIPLEXING_HIGH", "mtu": "1400", "traffic_pattern": "",
	})
	if mieru["transport"] != "UDP" || mieru["mtu"] != uint64(1400) {
		t.Fatalf("mieru %#v", mieru)
	}
	if _, ok := mieru["traffic_pattern"]; ok {
		t.Fatalf("empty traffic_pattern must be stripped %#v", mieru)
	}
	derp := map[string]any{"type": "derp", "listen": "0.0.0.0", "tls": map[string]any{"enabled": true}}
	applyDerpCustomKnobs(derp, "derp_uot", map[string]string{"path": "/derp", "websocket": "true", "udp": "uot"}, true)
	if derp["websocket"] != true || derp["udp"] != "uot" {
		t.Fatalf("derp %#v", derp)
	}
	ssh := map[string]any{
		"type": "ssh", "server": "1.2.3.4",
		"password": "x", "private_key": []any{"PEM"},
	}
	applySSHCustomKnobs(ssh, "ssh_pubkey", map[string]string{
		"auth_mode": "pubkey", "udp_over_tcp": "false", "client_version": "SSH-2.0-OpenSSH_8.9",
	}, false)
	if _, ok := ssh["password"]; ok {
		t.Fatalf("pubkey must drop password %#v", ssh)
	}
	if ssh["client_version"] != "SSH-2.0-OpenSSH_8.9" {
		t.Fatalf("client_version %#v", ssh)
	}
	uot := map[string]any{"type": "ssh", "server": "1.2.3.4", "password": "x"}
	applySSHCustomKnobs(uot, "ssh_uot", map[string]string{"auth_mode": "password", "udp_over_tcp": "true"}, false)
	if _, ok := uot["private_key"]; ok {
		t.Fatalf("password mode must drop private_key %#v", uot)
	}
	u, _ := uot["udp_over_tcp"].(map[string]any)
	if u["enabled"] != true {
		t.Fatalf("uot %#v", uot)
	}
	car := map[string]any{
		"type": "carrier", "listen": "0.0.0.0", "auth": "shared",
		"link": map[string]any{"transport": "datachannel", "room": "old"},
	}
	applyCarrierCustomKnobs(car, "carrier_peer_shared", map[string]string{
		"provider": "peer", "auth_mode": "users", "room": "",
	})
	if car["provider"] != "peer" || car["auth"] != "users" {
		t.Fatalf("carrier %#v", car)
	}
	link, _ := car["link"].(map[string]any)
	if _, ok := link["transport"]; ok {
		t.Fatalf("peer must drop transport %#v", link)
	}
	if _, ok := link["room"]; ok {
		t.Fatalf("empty peer room must be stripped %#v", link)
	}
	sudoku := map[string]any{"type": "sudoku", "listen": "0.0.0.0", "httpmask": map[string]any{"mode": "legacy"}}
	applySudokuCustomKnobs(sudoku, "sudoku_pad", map[string]string{
		"aead_method": "chacha20-poly1305", "multiplex": "auto", "httpmask_mode": "off",
		"padding_min": "5", "padding_max": "15", "fallback": "http://127.0.0.1:80",
	})
	if sudoku["aead_method"] != "chacha20-poly1305" || sudoku["multiplex"] != "auto" {
		t.Fatalf("sudoku %#v", sudoku)
	}
	if _, ok := sudoku["httpmask"]; ok {
		t.Fatalf("httpmask off must strip %#v", sudoku)
	}
	tt := map[string]any{"type": "trusttunnel", "transport": map[string]any{}}
	applyTrustTunnelCustomKnobs(tt, "trusttunnel_h2", map[string]string{
		"mode": "h2", "anti_dpi": "true", "enable_udp": "true",
	})
	tr, _ := tt["transport"].(map[string]any)
	if tr["upstream_protocol"] != "http2" || tr["anti_dpi"] != true || tt["enable_udp"] != true {
		t.Fatalf("trusttunnel %#v", tt)
	}
}
