//go:build with_controlplane

package controlplane

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/materialize"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/presets"
)

func TestAPIHy2AllReadySchemaAndMaterialize(t *testing.T) {
	t.Parallel()
	ready := []string{
		"hy2", "hy2_salamander", "hy2_gecko", "hy2_gecko_compact",
		"hy2_masquerade", "hy2_gecko_masquerade", "hy2_masquerade_file", "hy2_masquerade_proxy",
		"hy2_realm", "hy2_custom",
	}
	now := time.Now().UTC()
	tls := domain.DefaultSelfSigned("h.example")

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
			for _, key := range []string{"obfs_type", "masquerade_mode", "realm_mode", "up_mbps", "down_mbps"} {
				if _, ok := schema[key]; !ok {
					t.Fatalf("schema missing %s", key)
				}
			}

			set := domain.InboundSet{
				Name: "m1", Listen: "127.0.0.1", ListenPort: uint16(24000 + i),
				Presets: []string{tag},
				PeerSecrets: map[string]string{
					tag + "/obfs_password": "obfs-secret",
					tag + "/realm_token":   "realm-secret",
				},
			}
			params := map[string]string{}
			if tag == "hy2_masquerade_file" || tag == "hy2_custom" {
				params["masquerade_dir"] = "/tmp/hy2-masq"
			}
			if tag == "hy2_realm" {
				params["realm_server_url"] = "https://realm.example.com"
				params["realm_id"] = "realm-test"
			}
			if len(params) > 0 {
				set.Bindings = []domain.SetBinding{{Preset: tag, Params: params}}
				set.Presets = nil
			}
			user := domain.User{
				Name: "u1", Enabled: true, CreatedAt: now,
				Creds: map[string]map[string]any{
					tag: {"password": "hy2-user-secret"},
				},
			}
			raw, err := materialize.Build(materialize.Input{
				PublicHost:  "h.example",
				DataDir:     "/data",
				TLS:         tls,
				TLSCertPath: "/data/c.pem",
				TLSKeyPath:  "/data/k.pem",
				ActiveSets:  []domain.InboundSet{set},
				Users:       []domain.User{user},
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
			ib, _ := inbounds[0].(map[string]any)
			switch tag {
			case "hy2":
				if _, ok := ib["obfs"]; ok {
					t.Fatalf("plain hy2 must strip obfs: %#v", ib["obfs"])
				}
				if _, ok := ib["masquerade"]; ok {
					t.Fatalf("plain hy2 must strip masquerade")
				}
			case "hy2_salamander":
				obfs, _ := ib["obfs"].(map[string]any)
				if obfs["type"] != "salamander" {
					t.Fatalf("salamander %#v", obfs)
				}
			case "hy2_gecko":
				obfs, _ := ib["obfs"].(map[string]any)
				if obfs["type"] != "gecko" {
					t.Fatalf("gecko %#v", obfs)
				}
			case "hy2_gecko_masquerade":
				obfs, _ := ib["obfs"].(map[string]any)
				if obfs["type"] != "gecko" {
					t.Fatalf("gecko %#v", obfs)
				}
				masq, _ := ib["masquerade"].(map[string]any)
				if masq["type"] != "string" {
					t.Fatalf("masquerade %#v", masq)
				}
			case "hy2_gecko_compact":
				obfs, _ := ib["obfs"].(map[string]any)
				if obfs["type"] != "gecko" {
					t.Fatalf("gecko compact %#v", obfs)
				}
				min := obfs["min_packet_size"]
				if min != float64(100) && min != 100 {
					t.Fatalf("compact min=%v", min)
				}
			case "hy2_masquerade":
				masq, _ := ib["masquerade"].(map[string]any)
				if masq["type"] != "string" {
					t.Fatalf("masquerade %#v", masq)
				}
			case "hy2_masquerade_file":
				masq, _ := ib["masquerade"].(map[string]any)
				if masq["type"] != "file" || masq["directory"] != "/tmp/hy2-masq" {
					t.Fatalf("file masquerade %#v", masq)
				}
			case "hy2_masquerade_proxy":
				masq, _ := ib["masquerade"].(map[string]any)
				if masq["type"] != "proxy" {
					t.Fatalf("proxy masquerade %#v", masq)
				}
			case "hy2_realm":
				realm, _ := ib["realm"].(map[string]any)
				if realm["server_url"] != "https://realm.example.com" || realm["realm_id"] != "realm-test" {
					t.Fatalf("realm %#v", realm)
				}
			}
			body, err := materialize.RenderSubscription(
				user, []domain.InboundSet{set}, "h.example", tls, domain.CertManager{},
				materialize.SubscriptionFilters{}, nil, nil,
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
			ob, _ := outs[0].(map[string]any)
			if _, ok := ob["masquerade"]; ok {
				t.Fatal("subscription outbound must not carry masquerade")
			}
		})
	}
}
