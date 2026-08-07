//go:build with_controlplane

package materialize

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/wgawg"
)

func bytes32(seed byte) []byte {
	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = seed
	}
	return raw
}

func wgTestKey(seed byte) string {
	return domain.EncodeWireGuardKey(bytes32(seed))
}

func TestBytesPerSecToMbps(t *testing.T) {
	t.Parallel()
	if BytesPerSecToMbps(0) != 0 {
		t.Fatal("0")
	}
	if BytesPerSecToMbps(1) != 1 {
		t.Fatal("tiny")
	}
	// 1 MiB/s = 8 Mbps
	if BytesPerSecToMbps(1_048_576) != 9 { // ceil(8.388...) = 9
		t.Fatalf("got %d", BytesPerSecToMbps(1_048_576))
	}
}

func TestBuildWireGuardEndpointPlainAndAWG(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	bundle, err := wgawg.Bundle(false)
	if err != nil {
		t.Fatal(err)
	}
	hub := domain.WgHub{
		Enabled:       true,
		Profile:       domain.WgProfileAWG2,
		Subnet:        "10.8.0.0/24",
		ListenPort:    41641,
		HubPrivateKey: wgTestKey(1),
		HubPublicKey:  wgTestKey(2),
		AWG2:          bundle,
	}
	user := domain.User{
		Name: "alice", Enabled: true, CreatedAt: now,
		SpeedUpBytesPerSec:   125_000, // 1 Mbps
		SpeedDownBytesPerSec: 250_000, // 2 Mbps
		Creds: map[string]map[string]any{
			"wg": {
				"private_key":   wgTestKey(3),
				"public_key":    wgTestKey(4),
				"wg_host_index": 7,
			},
		},
	}
	ep, err := BuildWireGuardEndpoint(hub, []domain.User{user}, "edge.example")
	if err != nil {
		t.Fatal(err)
	}
	if ep["type"] != "wireguard" {
		t.Fatalf("%v", ep["type"])
	}
	awg2, _ := ep["awg2"].(map[string]any)
	if awg2 == nil || awg2["ip"] == nil || awg2["jc"] == nil {
		t.Fatalf("nested awg2 missing: %#v", ep)
	}
	if _, ok := awg2["i1"]; ok {
		t.Fatal("i1 must not be set")
	}
	if _, ok := ep["jc"]; ok {
		t.Fatal("flat jc must not be on root")
	}
	peers, _ := ep["peers"].([]any)
	if len(peers) != 1 {
		t.Fatalf("peers=%d", len(peers))
	}
	p0 := peers[0].(map[string]any)
	if p0["ip"] != "10.8.0.7" {
		t.Fatalf("ip=%v", p0["ip"])
	}
	if _, ok := p0["allowed_ips"]; ok {
		t.Fatalf("allowed_ips=%v", p0)
	}
	if ep["subnet"] != "10.8.0.0/24" {
		t.Fatalf("subnet=%v", ep["subnet"])
	}
	if p0["up_mbps"] != 1 {
		t.Fatalf("up_mbps=%v", p0["up_mbps"])
	}
	if p0["down_mbps"] != 2 {
		t.Fatalf("down_mbps=%v", p0["down_mbps"])
	}

	hub.Profile = domain.WgProfilePlain
	ep2, err := BuildWireGuardEndpoint(hub, []domain.User{user}, "edge.example")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ep2["jc"]; ok {
		t.Fatal("plain must strip awg")
	}
}

func TestBuildIncludesEndpoints(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	bundle, _ := wgawg.Bundle(true)
	hub := domain.WgHub{
		Enabled: true, Profile: domain.WgProfileAWG3, Subnet: "10.9.0.0/24", ListenPort: 51821,
		HubPrivateKey: wgTestKey(1),
		HubPublicKey:  wgTestKey(2),
		AWG3:          bundle,
	}
	user := domain.User{
		Name: "u", Enabled: true, CreatedAt: now,
		Creds: map[string]map[string]any{
			"wg": {"private_key": wgTestKey(3), "public_key": wgTestKey(4), "wg_host_index": float64(2)},
		},
	}
	raw, err := Build(Input{
		PublicHost: "h.example",
		DataDir:    "/data",
		TLS:        domain.DefaultSelfSigned("h.example"),
		WgHub:      &hub,
		Users:      []domain.User{user},
	})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	eps, _ := doc["endpoints"].([]any)
	if len(eps) != 1 {
		t.Fatalf("endpoints=%d", len(eps))
	}
	ep := eps[0].(map[string]any)
	awg3, _ := ep["awg3"].(map[string]any)
	if awg3 == nil || awg3["header_protection_key"] == nil {
		t.Fatal("awg3 HP missing")
	}
	addrs, _ := ep["address"].([]any)
	if len(addrs) != 1 || addrs[0] != "10.9.0.1/32" {
		t.Fatalf("address=%v", addrs)
	}
	if ep["subnet"] != "10.9.0.0/24" {
		t.Fatalf("subnet=%v", ep["subnet"])
	}
}

func TestRenderWireGuardClientSubscription(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	hub := domain.WgHub{
		Enabled: true, Profile: domain.WgProfilePlain, Subnet: "10.8.0.0/24", ListenPort: 41641,
		HubPublicKey: wgTestKey(2),
	}
	user := domain.User{
		Name: "u", Enabled: true, CreatedAt: now,
		Creds: map[string]map[string]any{
			"wg": {
				"private_key":   wgTestKey(3),
				"public_key":    wgTestKey(4),
				"wg_host_index": 3,
			},
		},
	}
	body, err := RenderSubscription(user, nil, "edge.example", domain.DefaultSelfSigned("edge.example"), SubscriptionFilters{}, nil, &hub)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	_ = json.Unmarshal(body, &doc)
	eps, _ := doc["endpoints"].([]any)
	if len(eps) != 1 {
		t.Fatalf("endpoints=%d body=%s", len(eps), body)
	}
	ep := eps[0].(map[string]any)
	addrs, _ := ep["address"].([]any)
	if len(addrs) != 1 || addrs[0] != "10.8.0.3/32" {
		t.Fatalf("local=%v want 10.8.0.3/32", addrs)
	}
	if ep["use_exit_node"] != true {
		t.Fatalf("use_exit_node=%v", ep["use_exit_node"])
	}
	peers := ep["peers"].([]any)
	p0 := peers[0].(map[string]any)
	if p0["public_key"] != wgTestKey(2) || p0["port"] != float64(41641) {
		t.Fatalf("%v", p0)
	}
	if _, ok := p0["allowed_ips"]; ok {
		t.Fatalf("allowed_ips=%v", p0)
	}
}

func TestRenderWireGuardClientPathologyKeyFromCreds(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	hubKey := "hubPathologyKeyAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	userKey := "userPathologyKeyAAAAAAAAAAAAAAAAAAAAAAAAAA="
	hub := domain.WgHub{
		Enabled: true, Profile: domain.WgProfilePathology, Subnet: "10.8.0.0/24", ListenPort: 41641,
		HubPrivateKey: wgTestKey(1), HubPublicKey: wgTestKey(2),
		Pathology: map[string]any{"enabled": true, "auto": true, "key": hubKey},
	}
	user := domain.User{
		Name: "u", Enabled: true, CreatedAt: now,
		Creds: map[string]map[string]any{
			"wg": {
				"private_key":   wgTestKey(3),
				"public_key":    wgTestKey(4),
				"wg_host_index": 2,
				"pathology_key": userKey,
			},
		},
	}
	ep, err := RenderWireGuardClientEndpoint(user, hub, "edge.example")
	if err != nil {
		t.Fatal(err)
	}
	path, _ := ep["pathology"].(map[string]any)
	if path == nil {
		t.Fatal("missing pathology")
	}
	if path["key"] != userKey {
		t.Fatalf("want creds pathology_key, got %v (hub was %v)", path["key"], hubKey)
	}
	addrs, _ := ep["address"].([]any)
	if len(addrs) != 1 || addrs[0] != "10.8.0.2/32" {
		t.Fatalf("address=%v", addrs)
	}
}
