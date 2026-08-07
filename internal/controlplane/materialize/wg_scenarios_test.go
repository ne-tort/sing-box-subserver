//go:build with_controlplane

package materialize

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/wgawg"
)

func wgKey(seed byte) string {
	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = seed
	}
	return domain.EncodeWireGuardKey(raw)
}

func sampleUser(name string, index int, pubSeed byte) domain.User {
	return domain.User{
		ID: "id-" + name, Name: name, Enabled: true, CreatedAt: time.Now().UTC(),
		Creds: map[string]map[string]any{
			"wg": {
				"private_key":   wgKey(pubSeed + 100),
				"public_key":    wgKey(pubSeed),
				"wg_host_index": index,
			},
		},
	}
}

func TestScenario_PlainNoAWGFields(t *testing.T) {
	t.Parallel()
	hub := domain.WgHub{
		Enabled: true, Profile: domain.WgProfilePlain, Subnet: "10.8.0.0/24", ListenPort: 41641,
		HubPrivateKey: wgKey(1), HubPublicKey: wgKey(2),
		AWG2: map[string]any{"jc": 4, "id": "leak.example"}, // leftover should be stripped
	}
	ep, err := BuildWireGuardEndpoint(hub, []domain.User{sampleUser("a", 2, 99)}, "edge")
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"jc", "id", "ip", "ib", "header_protection_key", "i1", "awg2", "awg3", "pathology"} {
		if _, ok := ep[k]; ok {
			t.Fatalf("plain must not emit %s", k)
		}
	}
	if ep["system"] != nil {
		t.Fatal("system must be omitted by default")
	}
}

func TestScenario_AWG2RequiresMasqueradeNoISlots(t *testing.T) {
	t.Parallel()
	bundle, err := wgawg.Bundle(false)
	if err != nil {
		t.Fatal(err)
	}
	hub := domain.WgHub{
		Enabled: true, Profile: domain.WgProfileAWG2, Subnet: "10.8.0.0/24", ListenPort: 41641,
		HubPrivateKey: wgKey(1), HubPublicKey: wgKey(2), AWG2: bundle,
	}
	ep, err := BuildWireGuardEndpoint(hub, []domain.User{sampleUser("a", 2, 99)}, "edge")
	if err != nil {
		t.Fatal(err)
	}
	awg2, _ := ep["awg2"].(map[string]any)
	if awg2 == nil {
		t.Fatalf("missing nested awg2: %#v", ep)
	}
	for _, k := range []string{"jc", "jmin", "s1", "h1", "ip", "ib"} {
		if awg2[k] == nil || awg2[k] == "" {
			t.Fatalf("missing awg2.%s", k)
		}
	}
	if _, ok := awg2["id"]; ok {
		t.Fatal("id must come from Reality SNI override, not Bundle")
	}
	for _, k := range []string{"i1", "i2", "i3", "i4", "i5", "header_protection_key"} {
		if _, ok := awg2[k]; ok {
			t.Fatalf("unexpected awg2.%s", k)
		}
	}
	for _, k := range []string{"jc", "ip", "awg3", "pathology"} {
		if k == "awg3" || k == "pathology" {
			if _, ok := ep[k]; ok {
				t.Fatalf("unexpected %s", k)
			}
			continue
		}
		if _, ok := ep[k]; ok {
			t.Fatalf("flat %s must not be on root", k)
		}
	}
}

func TestScenario_AWG3HasHPAndMasquerade(t *testing.T) {
	t.Parallel()
	bundle, err := wgawg.Bundle(true)
	if err != nil {
		t.Fatal(err)
	}
	hub := domain.WgHub{
		Enabled: true, Profile: domain.WgProfileAWG3, Subnet: "10.8.0.0/24", ListenPort: 41641,
		HubPrivateKey: wgKey(1), HubPublicKey: wgKey(2), AWG3: bundle,
	}
	ep, err := BuildWireGuardEndpoint(hub, []domain.User{sampleUser("a", 2, 99)}, "edge")
	if err != nil {
		t.Fatal(err)
	}
	awg3, _ := ep["awg3"].(map[string]any)
	if awg3 == nil || awg3["header_protection_key"] == nil || awg3["ip"] == nil {
		t.Fatalf("%v", ep)
	}
	if _, ok := ep["header_protection_key"]; ok {
		t.Fatal("HP must be nested under awg3")
	}
}

func TestScenario_PathologyNested(t *testing.T) {
	t.Parallel()
	bundle, err := wgawg.BundlePathology()
	if err != nil {
		t.Fatal(err)
	}
	hub := domain.WgHub{
		Enabled: true, Profile: domain.WgProfilePathology, Subnet: "10.8.0.0/24", ListenPort: 41641,
		HubPrivateKey: wgKey(1), HubPublicKey: wgKey(2), Pathology: bundle,
	}
	ep, err := BuildWireGuardEndpoint(hub, []domain.User{sampleUser("a", 2, 99)}, "edge")
	if err != nil {
		t.Fatal(err)
	}
	path, _ := ep["pathology"].(map[string]any)
	if path == nil || path["key"] == nil || path["auto"] != true {
		t.Fatalf("%v", ep)
	}
	if _, ok := ep["awg2"]; ok {
		t.Fatal("awg2 mutex")
	}
}

func TestScenario_AWGProfileWithoutBundleFails(t *testing.T) {
	t.Parallel()
	hub := domain.WgHub{
		Enabled: true, Profile: domain.WgProfileAWG2, Subnet: "10.8.0.0/24", ListenPort: 41641,
		HubPrivateKey: wgKey(1), HubPublicKey: wgKey(2),
	}
	_, err := BuildWireGuardEndpoint(hub, nil, "edge")
	if err == nil || !strings.Contains(err.Error(), "requires generated AWG") {
		t.Fatalf("err=%v", err)
	}
}

func TestScenario_StickyIndexSurvivesSubnetChange(t *testing.T) {
	t.Parallel()
	u := sampleUser("a", 7, 99)
	hub := domain.WgHub{
		Enabled: true, Profile: domain.WgProfilePlain, Subnet: "10.8.0.0/24", ListenPort: 41641,
		HubPrivateKey: wgKey(1), HubPublicKey: wgKey(2),
	}
	ep1, err := BuildWireGuardEndpoint(hub, []domain.User{u}, "edge")
	if err != nil {
		t.Fatal(err)
	}
	p1 := ep1["peers"].([]any)[0].(map[string]any)
	if p1["ip"] != "10.8.0.7" {
		t.Fatalf("%v", p1["ip"])
	}
	if _, ok := p1["allowed_ips"]; ok {
		t.Fatalf("allowed_ips must be absent in sugar: %v", p1)
	}
	hub.Subnet = "10.9.0.0/24"
	ep2, err := BuildWireGuardEndpoint(hub, []domain.User{u}, "edge")
	if err != nil {
		t.Fatal(err)
	}
	p2 := ep2["peers"].([]any)[0].(map[string]any)
	if p2["ip"] != "10.9.0.7" {
		t.Fatalf("sticky index must rebase: %v", p2["ip"])
	}
	addrs := ep2["address"].([]any)
	if addrs[0] != "10.9.0.1/32" {
		t.Fatalf("hub addr=%v", addrs)
	}
	if ep2["subnet"] != "10.9.0.0/24" {
		t.Fatalf("subnet=%v", ep2["subnet"])
	}
}

func TestScenario_DuplicateHostIndexRejected(t *testing.T) {
	t.Parallel()
	hub := domain.WgHub{
		Enabled: true, Profile: domain.WgProfilePlain, Subnet: "10.8.0.0/24", ListenPort: 41641,
		HubPrivateKey: wgKey(1), HubPublicKey: wgKey(2),
	}
	_, err := BuildWireGuardEndpoint(hub, []domain.User{
		sampleUser("a", 5, 99),
		sampleUser("b", 5, 100),
	}, "edge")
	if err == nil || !strings.Contains(err.Error(), "cp_wg_peer_conflict") {
		t.Fatalf("err=%v", err)
	}
}

func TestScenario_DuplicatePublicKeyRejected(t *testing.T) {
	t.Parallel()
	hub := domain.WgHub{
		Enabled: true, Profile: domain.WgProfilePlain, Subnet: "10.8.0.0/24", ListenPort: 41641,
		HubPrivateKey: wgKey(1), HubPublicKey: wgKey(2),
	}
	_, err := BuildWireGuardEndpoint(hub, []domain.User{
		sampleUser("a", 2, 97),
		sampleUser("b", 3, 97),
	}, "edge")
	if err == nil || !strings.Contains(err.Error(), "public_key") {
		t.Fatalf("err=%v", err)
	}
}

func TestScenario_SystemOptInAndPeerRelay(t *testing.T) {
	t.Parallel()
	hub := domain.WgHub{
		Enabled: true, Profile: domain.WgProfilePlain, Subnet: "10.8.0.0/24", ListenPort: 41641,
		System: true, Name: "wg0", PeerRelay: true,
		HubPrivateKey: wgKey(1), HubPublicKey: wgKey(2),
	}
	ep, err := BuildWireGuardEndpoint(hub, nil, "edge")
	if err != nil {
		t.Fatal(err)
	}
	if ep["system"] != true || ep["name"] != "wg0" {
		t.Fatalf("%v", ep)
	}
	if ep["peer_relay"] != true {
		t.Fatalf("peer_relay missing: %v", ep)
	}
	hubOff := hub
	hubOff.PeerRelay = false
	epOff, err := BuildWireGuardEndpoint(hubOff, nil, "edge")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := epOff["peer_relay"]; ok {
		t.Fatalf("peer_relay should be omitted when false: %v", epOff)
	}
}

func TestScenario_ClientInternetAllowRoutes(t *testing.T) {
	t.Parallel()
	hub := domain.WgHub{
		Enabled: true, Profile: domain.WgProfilePlain, Subnet: "10.8.0.0/24", ListenPort: 41641,
		HubPublicKey: wgKey(2),
	}
	u := sampleUser("a", 3, 99)
	ep, err := RenderWireGuardClientEndpoint(u, hub, "edge.example")
	if err != nil {
		t.Fatal(err)
	}
	addrs := ep["address"].([]any)
	if len(addrs) != 1 || addrs[0] != "10.8.0.3/32" {
		t.Fatalf("client iface addr=%v", addrs)
	}
	if ep["subnet"] != "10.8.0.0/24" {
		t.Fatalf("subnet=%v", ep["subnet"])
	}
	if ep["use_exit_node"] != true {
		t.Fatalf("want use_exit_node: %v", ep)
	}
	peer := ep["peers"].([]any)[0].(map[string]any)
	if _, ok := peer["allowed_ips"]; ok {
		t.Fatalf("allowed_ips must be absent: %v", peer)
	}
	off := false
	hub.InternetAllow = &off
	ep2, err := RenderWireGuardClientEndpoint(u, hub, "edge.example")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ep2["use_exit_node"]; ok {
		t.Fatalf("overlay-only must omit use_exit_node: %v", ep2)
	}
}

func TestScenario_ExitNodeSugar(t *testing.T) {
	t.Parallel()
	exit := sampleUser("exit", 4, 103)
	other := sampleUser("a", 2, 99)
	hub := domain.WgHub{
		Enabled: true, Profile: domain.WgProfilePlain, Subnet: "10.8.0.0/24", ListenPort: 41641,
		HubPrivateKey: wgKey(1), HubPublicKey: wgKey(2),
		ExitUserID: exit.ID,
	}
	ep, err := BuildWireGuardEndpoint(hub, []domain.User{other, exit}, "edge")
	if err != nil {
		t.Fatal(err)
	}
	if ep["peer_relay"] != true {
		t.Fatalf("exit forces peer_relay: %v", ep)
	}
	var exitPeer map[string]any
	for _, raw := range ep["peers"].([]any) {
		p := raw.(map[string]any)
		if p["ip"] == "10.8.0.4" {
			exitPeer = p
		}
	}
	if exitPeer == nil || exitPeer["exit_node"] != true {
		t.Fatalf("exit peer=%v", exitPeer)
	}
	clientExit, err := RenderWireGuardClientEndpoint(exit, hub, "edge.example")
	if err != nil {
		t.Fatal(err)
	}
	if clientExit["advertise_exit_node"] != true {
		t.Fatalf("%v", clientExit)
	}
	if _, ok := clientExit["use_exit_node"]; ok {
		t.Fatalf("exit client must not use_exit_node: %v", clientExit)
	}
	clientOther, err := RenderWireGuardClientEndpoint(other, hub, "edge.example")
	if err != nil {
		t.Fatal(err)
	}
	if clientOther["use_exit_node"] != true {
		t.Fatalf("%v", clientOther)
	}
}

func TestScenario_SpeedMapsToPeerMbps(t *testing.T) {
	t.Parallel()
	hub := domain.WgHub{
		Enabled: true, Profile: domain.WgProfilePlain, Subnet: "10.8.0.0/24", ListenPort: 41641,
		HubPrivateKey: wgKey(1), HubPublicKey: wgKey(2),
	}
	u := sampleUser("a", 2, 99)
	u.SpeedUpBytesPerSec = 125_000   // 1 Mbps
	u.SpeedDownBytesPerSec = 1_000_000 // 8 Mbps
	ep, err := BuildWireGuardEndpoint(hub, []domain.User{u}, "edge")
	if err != nil {
		t.Fatal(err)
	}
	p := ep["peers"].([]any)[0].(map[string]any)
	if p["up_mbps"] != 1 || p["down_mbps"] != 8 {
		t.Fatalf("%v", p)
	}
}

func TestScenario_DisabledHubOmitsEndpoint(t *testing.T) {
	t.Parallel()
	hub := domain.WgHub{Enabled: false, Profile: domain.WgProfilePlain, Subnet: "10.8.0.0/24"}
	ep, err := BuildWireGuardEndpoint(hub, []domain.User{sampleUser("a", 2, 99)}, "edge")
	if err != nil || ep != nil {
		t.Fatalf("ep=%v err=%v", ep, err)
	}
	raw, err := Build(Input{
		PublicHost: "h", DataDir: "/d", TLS: domain.DefaultSelfSigned("h"), WgHub: &hub,
	})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	_ = json.Unmarshal(raw, &doc)
	if _, ok := doc["endpoints"]; ok {
		t.Fatal("disabled hub must not emit endpoints")
	}
}

func TestScenario_InvalidHostIndexRejected(t *testing.T) {
	t.Parallel()
	hub := domain.WgHub{
		Enabled: true, Profile: domain.WgProfilePlain, Subnet: "10.8.0.0/24", ListenPort: 41641,
		HubPrivateKey: wgKey(1), HubPublicKey: wgKey(2),
	}
	u := sampleUser("a", 1, 99) // .1 reserved for hub
	_, err := BuildWireGuardEndpoint(hub, []domain.User{u}, "edge")
	if err == nil {
		t.Fatal("index 1 must be rejected")
	}
}

func TestScenario_CredsUnderAwgKeyOnly(t *testing.T) {
	t.Parallel()
	hub := domain.WgHub{
		Enabled: true, Profile: domain.WgProfileAWG2, Subnet: "10.8.0.0/24", ListenPort: 41641,
		HubPrivateKey: wgKey(1), HubPublicKey: wgKey(2),
	}
	bundle, err := wgawg.Bundle(false)
	if err != nil {
		t.Fatal(err)
	}
	hub.AWG2 = bundle
	u := domain.User{
		Name: "a", Enabled: true, CreatedAt: time.Now().UTC(),
		Creds: map[string]map[string]any{
			"wg_awg2": {
				"private_key":   wgKey(50),
				"public_key":    wgKey(51),
				"wg_host_index": 9,
			},
		},
	}
	ep, err := BuildWireGuardEndpoint(hub, []domain.User{u}, "edge")
	if err != nil {
		t.Fatal(err)
	}
	p := ep["peers"].([]any)[0].(map[string]any)
	if p["ip"] != "10.8.0.9" {
		t.Fatalf("%v", p)
	}
	if _, ok := p["allowed_ips"]; ok {
		t.Fatalf("allowed_ips forbidden: %v", p)
	}
	client, err := RenderWireGuardClientEndpoint(u, hub, "edge.example")
	if err != nil || client == nil {
		t.Fatalf("client=%v err=%v", client, err)
	}
}
