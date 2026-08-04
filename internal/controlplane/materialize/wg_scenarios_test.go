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

func sampleUser(name string, index int, pub string) domain.User {
	return domain.User{
		ID: "id-" + name, Name: name, Enabled: true, CreatedAt: time.Now().UTC(),
		Creds: map[string]map[string]any{
			"wg": {
				"private_key":   "PRIV-" + name,
				"public_key":    pub,
				"wg_host_index": index,
			},
		},
	}
}

func TestScenario_PlainNoAWGFields(t *testing.T) {
	t.Parallel()
	hub := domain.WgHub{
		Enabled: true, Profile: domain.WgProfilePlain, Subnet: "10.8.0.0/24", ListenPort: 51820,
		HubPrivateKey: "HUBPRIV", HubPublicKey: "HUBPUB",
		AWG: map[string]any{"jc": 4, "id": "leak.example"}, // leftover should be stripped
	}
	ep, err := BuildWireGuardEndpoint(hub, []domain.User{sampleUser("a", 2, "PUBA")}, "edge")
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"jc", "id", "ip", "ib", "header_protection_key", "i1"} {
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
		Enabled: true, Profile: domain.WgProfileAWG2, Subnet: "10.8.0.0/24", ListenPort: 51820,
		HubPrivateKey: "HUBPRIV", HubPublicKey: "HUBPUB", AWG: bundle,
	}
	ep, err := BuildWireGuardEndpoint(hub, []domain.User{sampleUser("a", 2, "PUBA")}, "edge")
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"jc", "jmin", "s1", "h1", "ip", "ib"} {
		if ep[k] == nil || ep[k] == "" {
			t.Fatalf("missing %s", k)
		}
	}
	if _, ok := ep["id"]; ok {
		t.Fatal("id must come from Reality SNI override, not Bundle")
	}
	for _, k := range []string{"i1", "i2", "i3", "i4", "i5", "header_protection_key"} {
		if _, ok := ep[k]; ok {
			t.Fatalf("unexpected %s", k)
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
		Enabled: true, Profile: domain.WgProfileAWG3, Subnet: "10.8.0.0/24", ListenPort: 51820,
		HubPrivateKey: "HUBPRIV", HubPublicKey: "HUBPUB", AWG: bundle,
	}
	ep, err := BuildWireGuardEndpoint(hub, []domain.User{sampleUser("a", 2, "PUBA")}, "edge")
	if err != nil {
		t.Fatal(err)
	}
	if ep["header_protection_key"] == nil || ep["ip"] == nil {
		t.Fatalf("%v", ep)
	}
}

func TestScenario_AWGProfileWithoutBundleFails(t *testing.T) {
	t.Parallel()
	hub := domain.WgHub{
		Enabled: true, Profile: domain.WgProfileAWG2, Subnet: "10.8.0.0/24", ListenPort: 51820,
		HubPrivateKey: "HUBPRIV", HubPublicKey: "HUBPUB",
	}
	_, err := BuildWireGuardEndpoint(hub, nil, "edge")
	if err == nil || !strings.Contains(err.Error(), "requires generated AWG") {
		t.Fatalf("err=%v", err)
	}
}

func TestScenario_StickyIndexSurvivesSubnetChange(t *testing.T) {
	t.Parallel()
	u := sampleUser("a", 7, "PUBA")
	hub := domain.WgHub{
		Enabled: true, Profile: domain.WgProfilePlain, Subnet: "10.8.0.0/24", ListenPort: 51820,
		HubPrivateKey: "HUBPRIV", HubPublicKey: "HUBPUB",
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
	if addrs[0] != "10.9.0.1" {
		t.Fatalf("hub addr=%v", addrs)
	}
	if ep2["subnet"] != "10.9.0.0/24" {
		t.Fatalf("subnet=%v", ep2["subnet"])
	}
}

func TestScenario_DuplicateHostIndexRejected(t *testing.T) {
	t.Parallel()
	hub := domain.WgHub{
		Enabled: true, Profile: domain.WgProfilePlain, Subnet: "10.8.0.0/24", ListenPort: 51820,
		HubPrivateKey: "HUBPRIV", HubPublicKey: "HUBPUB",
	}
	_, err := BuildWireGuardEndpoint(hub, []domain.User{
		sampleUser("a", 5, "PUBA"),
		sampleUser("b", 5, "PUBB"),
	}, "edge")
	if err == nil || !strings.Contains(err.Error(), "cp_wg_peer_conflict") {
		t.Fatalf("err=%v", err)
	}
}

func TestScenario_DuplicatePublicKeyRejected(t *testing.T) {
	t.Parallel()
	hub := domain.WgHub{
		Enabled: true, Profile: domain.WgProfilePlain, Subnet: "10.8.0.0/24", ListenPort: 51820,
		HubPrivateKey: "HUBPRIV", HubPublicKey: "HUBPUB",
	}
	_, err := BuildWireGuardEndpoint(hub, []domain.User{
		sampleUser("a", 2, "SAME"),
		sampleUser("b", 3, "SAME"),
	}, "edge")
	if err == nil || !strings.Contains(err.Error(), "public_key") {
		t.Fatalf("err=%v", err)
	}
}

func TestScenario_SystemOptInAndPeerRelay(t *testing.T) {
	t.Parallel()
	hub := domain.WgHub{
		Enabled: true, Profile: domain.WgProfilePlain, Subnet: "10.8.0.0/24", ListenPort: 51820,
		System: true, Name: "wg0", PeerRelay: true,
		HubPrivateKey: "HUBPRIV", HubPublicKey: "HUBPUB",
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
		Enabled: true, Profile: domain.WgProfilePlain, Subnet: "10.8.0.0/24", ListenPort: 51820,
		HubPublicKey: "HUBPUB",
	}
	u := sampleUser("a", 3, "PUBA")
	ep, err := RenderWireGuardClientEndpoint(u, hub, "edge.example")
	if err != nil {
		t.Fatal(err)
	}
	addrs := ep["address"].([]any)
	if len(addrs) != 1 || addrs[0] != "10.8.0.3" {
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
	exit := sampleUser("exit", 4, "PUBE")
	other := sampleUser("a", 2, "PUBA")
	hub := domain.WgHub{
		Enabled: true, Profile: domain.WgProfilePlain, Subnet: "10.8.0.0/24", ListenPort: 51820,
		HubPrivateKey: "HUBPRIV", HubPublicKey: "HUBPUB",
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
		Enabled: true, Profile: domain.WgProfilePlain, Subnet: "10.8.0.0/24", ListenPort: 51820,
		HubPrivateKey: "HUBPRIV", HubPublicKey: "HUBPUB",
	}
	u := sampleUser("a", 2, "PUBA")
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
	ep, err := BuildWireGuardEndpoint(hub, []domain.User{sampleUser("a", 2, "PUBA")}, "edge")
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
		Enabled: true, Profile: domain.WgProfilePlain, Subnet: "10.8.0.0/24", ListenPort: 51820,
		HubPrivateKey: "HUBPRIV", HubPublicKey: "HUBPUB",
	}
	u := sampleUser("a", 1, "PUBA") // .1 reserved for hub
	_, err := BuildWireGuardEndpoint(hub, []domain.User{u}, "edge")
	if err == nil {
		t.Fatal("index 1 must be rejected")
	}
}

func TestScenario_CredsUnderAwgKeyOnly(t *testing.T) {
	t.Parallel()
	hub := domain.WgHub{
		Enabled: true, Profile: domain.WgProfileAWG2, Subnet: "10.8.0.0/24", ListenPort: 51820,
		HubPrivateKey: "HUBPRIV", HubPublicKey: "HUBPUB",
	}
	bundle, err := wgawg.Bundle(false)
	if err != nil {
		t.Fatal(err)
	}
	hub.AWG = bundle
	u := domain.User{
		Name: "a", Enabled: true, CreatedAt: time.Now().UTC(),
		Creds: map[string]map[string]any{
			"wg_awg2": {
				"private_key":   "PRIV",
				"public_key":    "PUBA",
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
