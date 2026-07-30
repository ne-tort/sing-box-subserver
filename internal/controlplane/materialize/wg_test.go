//go:build with_controlplane

package materialize

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/wgawg"
)

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
		ListenPort:    51820,
		HubPrivateKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		HubPublicKey:  "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB",
		AWG:           bundle,
	}
	user := domain.User{
		Name: "alice", Enabled: true, CreatedAt: now,
		SpeedUpBytesPerSec:   125_000, // 1 Mbps
		SpeedDownBytesPerSec: 250_000, // 2 Mbps
		Creds: map[string]map[string]any{
			"wg": {
				"private_key":   "CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC",
				"public_key":    "DDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDD",
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
	if ep["ip"] == nil || ep["jc"] == nil {
		t.Fatalf("awg fields missing: %#v", ep)
	}
	if _, ok := ep["i1"]; ok {
		t.Fatal("i1 must not be set")
	}
	peers, _ := ep["peers"].([]any)
	if len(peers) != 1 {
		t.Fatalf("peers=%d", len(peers))
	}
	p0 := peers[0].(map[string]any)
	ips, _ := p0["allowed_ips"].([]any)
	if len(ips) != 1 || ips[0] != "10.8.0.7/32" {
		t.Fatalf("allowed_ips=%v", ips)
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
		HubPrivateKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		HubPublicKey:  "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB",
		AWG:           bundle,
	}
	user := domain.User{
		Name: "u", Enabled: true, CreatedAt: now,
		Creds: map[string]map[string]any{
			"wg": {"private_key": "CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC", "public_key": "DDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDD", "wg_host_index": float64(2)},
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
	if ep["header_protection_key"] == nil {
		t.Fatal("awg3 HP missing")
	}
	addrs, _ := ep["address"].([]any)
	if len(addrs) != 1 || addrs[0] != "10.9.0.1/24" {
		t.Fatalf("address=%v", addrs)
	}
}

func TestRenderWireGuardClientSubscription(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	hub := domain.WgHub{
		Enabled: true, Profile: domain.WgProfilePlain, Subnet: "10.8.0.0/24", ListenPort: 51820,
		HubPublicKey: "HUBPUB",
	}
	user := domain.User{
		Name: "u", Enabled: true, CreatedAt: now,
		Creds: map[string]map[string]any{
			"wg": {"private_key": "PRIV", "public_key": "PUB", "wg_host_index": 3},
		},
	}
	body, err := RenderSubscription(user, nil, "edge.example", domain.DefaultSelfSigned("edge.example"), domain.CertManager{}, SubscriptionFilters{}, nil, &hub)
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
	if addrs[0] != "10.8.0.3/32" {
		t.Fatalf("local=%v", addrs)
	}
	peers := ep["peers"].([]any)
	p0 := peers[0].(map[string]any)
	if p0["public_key"] != "HUBPUB" || p0["port"] != float64(51820) {
		t.Fatalf("%v", p0)
	}
}
