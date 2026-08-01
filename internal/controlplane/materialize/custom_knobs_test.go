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
