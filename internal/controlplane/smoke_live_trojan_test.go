//go:build with_controlplane

package controlplane

import (
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ne-tort/sing-box-subserver/internal/box"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/materialize"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/presets"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/smoke"
)

// Live hairpin smoke for Trojan TLS (works with with_controlplane alone on Windows).
// Full ready-preset matrix runs in scripts/cp_trojan_sub_docker.ps1.
func TestLiveSmokeTrojanTLS(t *testing.T) {
	dir := t.TempDir()
	tls := domain.DefaultSelfSigned("127.0.0.1")
	cert := filepath.Join(dir, "controlplane", "ssl", "default", "cert.crt")
	key := filepath.Join(dir, "controlplane", "ssl", "default", "cert.key")
	if _, _, err := writeSelfSignedPair(cert, key, cert+".meta", *tls.SelfSigned, "smoke"); err != nil {
		t.Fatal(err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := uint16(ln.Addr().(*net.TCPAddr).Port)
	_ = ln.Close()

	now := time.Now().UTC()
	set := domain.InboundSet{
		Name: "live-trojan", Listen: "127.0.0.1", ListenPort: port,
		Presets: []string{"trojan_tls"},
	}
	user := domain.User{
		ID: "smoke-live-trojan", Name: smoke.SmokeUserName, Enabled: true,
		CreatedAt: now, UpdatedAt: now, SubToken: "tok-trojan",
		Creds: map[string]map[string]any{
			"trojan_tls": {"password": "trojan-smoke-secret"},
		},
	}

	raw, err := materialize.Build(materialize.Input{
		PublicHost:  "127.0.0.1",
		DataDir:     dir,
		TLS:         tls,
		TLSCertPath: cert,
		TLSKeyPath:  key,
		ActiveSets:  []domain.InboundSet{set},
		Users:       []domain.User{user},
	})
	if err != nil {
		t.Fatal(err)
	}

	eng := box.NewEngine(context.Background())
	live, err := eng.Start(context.Background(), raw)
	if err != nil {
		if strings.Contains(err.Error(), "with_utls") {
			t.Skip(err.Error())
		}
		t.Fatalf("live box start: %v\nconfig=%s", err, string(raw))
	}
	defer func() { _ = live.Close() }()
	time.Sleep(300 * time.Millisecond)

	report, err := smoke.Run(context.Background(), smoke.Input{
		User:       user,
		Sets:       []domain.InboundSet{set},
		PublicHost: "127.0.0.1",
		TLS:        tls,
	}, smoke.Request{TimeoutMs: 3000})
	if err != nil {
		if strings.Contains(err.Error(), "with_utls") {
			t.Skip(err.Error())
		}
		t.Fatal(err)
	}
	if len(report.Results) == 0 {
		t.Fatal("no results")
	}
	var okN int
	for _, r := range report.Results {
		if r.Skipped {
			continue
		}
		if r.OK {
			okN++
			continue
		}
		t.Logf("fail: %+v", r)
	}
	if okN == 0 {
		b, _ := json.MarshalIndent(report, "", "  ")
		t.Fatalf("expected at least one ok probe:\n%s", b)
	}
}

func TestAPITrojanAllReadySchemaAndMaterialize(t *testing.T) {
	t.Parallel()
	ready := []string{
		"trojan_tls", "trojan_reality", "trojan_tls_mux", "trojan_tls_fallback",
		"trojan_ws_tls", "trojan_ws_reality",
		"trojan_grpc_tls", "trojan_grpc_reality",
		"trojan_http_tls", "trojan_http_reality",
		"trojan_httpupgrade_tls", "trojan_httpupgrade_reality",
		"trojan_quic_tls",
		"trojan_custom",
	}
	now := time.Now().UTC()
	tls := domain.DefaultSelfSigned("h.example")
	mkAssign := func(key string) domain.RealityAssignment {
		return domain.RealityAssignment{
			InboundKey: key, SNI: "www.microsoft.com",
			HandshakeServer: "www.microsoft.com", HandshakePort: 443,
			PrivateKeyBase64: "Mzi3RBq4Eb3L-ic-8z9yqV3Xcg7G7xUqKdEH7DKn-1Q",
			PublicKeyBase64:  "jQfCMZZk0RwJQK1qlf0LUFUphdE4jE6JIutIlAzxPVo",
			ShortID:          "aabbccddeeff0011", UpdatedAt: now,
		}
	}
	assignments := map[string]domain.RealityAssignment{}
	for _, tag := range ready {
		assignments["m1/"+tag] = mkAssign("m1/" + tag)
	}

	for i, tag := range ready {
		tag, i := tag, i
		t.Run(tag, func(t *testing.T) {
			t.Parallel()
			pp, err := presets.Get(tag)
			if err != nil {
				t.Fatal(err)
			}
			if !pp.CustomPreset {
				t.Fatal("custom_preset required")
			}
			schema := buildParamsSchemaLang(pp, true, "en")
			for _, key := range []string{"transport", "tls_mode", "multiplex", "ws_max_early_data", "fallback"} {
				if _, ok := schema[key]; !ok {
					t.Fatalf("schema missing %s", key)
				}
			}

			set := domain.InboundSet{
				Name: "m1", Listen: "127.0.0.1", ListenPort: uint16(22000 + i),
				Presets: []string{tag},
			}
			user := domain.User{
				Name: "u1", Enabled: true, CreatedAt: now,
				Creds: map[string]map[string]any{
					tag: {"password": "trojan-test-secret"},
				},
			}
			raw, err := materialize.Build(materialize.Input{
				PublicHost:         "h.example",
				DataDir:            "/data",
				TLS:                tls,
				TLSCertPath:        "/data/c.pem",
				TLSKeyPath:         "/data/k.pem",
				ActiveSets:         []domain.InboundSet{set},
				Users:              []domain.User{user},
				RealityAssignments: assignments,
			})
			if err != nil {
				t.Fatalf("materialize: %v", err)
			}
			var doc map[string]any
			if err := json.Unmarshal(raw, &doc); err != nil {
				t.Fatal(err)
			}
			inbounds, _ := doc["inbounds"].([]any)
			if len(inbounds) == 0 {
				t.Fatal("no inbounds")
			}
			if tag == "trojan_tls_fallback" {
				ib, _ := inbounds[0].(map[string]any)
				fb, _ := ib["fallback"].(map[string]any)
				if fb["server"] != "127.0.0.1" {
					t.Fatalf("fallback missing/wrong: %#v", ib["fallback"])
				}
			}
			body, err := materialize.RenderSubscription(
				user, []domain.InboundSet{set}, "h.example", tls,
				materialize.SubscriptionFilters{}, assignments, nil,
			)
			if err != nil {
				t.Fatal(err)
			}
			var sub map[string]any
			if err := json.Unmarshal(body, &sub); err != nil {
				t.Fatal(err)
			}
			outs, _ := sub["outbounds"].([]any)
			if len(outs) == 0 {
				t.Fatal("empty subscription")
			}
		})
	}
}
