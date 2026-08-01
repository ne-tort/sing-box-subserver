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
	hy := map[string]any{"type": "hysteria2", "ignore_client_bandwidth": false}
	applyStockIgnoreClientBandwidth(hy, map[string]string{"ignore_client_bandwidth": "true"})
	if hy["ignore_client_bandwidth"] != true {
		t.Fatalf("ignore=%v", hy["ignore_client_bandwidth"])
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
	derp := map[string]any{"type": "derp", "udp": "native", "websocket": false}
	applyDerpCustomKnobs(derp, "derp_tls", map[string]string{"udp": "disabled", "websocket": "true"}, true)
	if derp["udp"] != "disabled" || derp["websocket"] != true {
		t.Fatalf("derp=%#v", derp)
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
