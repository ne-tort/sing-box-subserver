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

func TestAPITuicAllReadySchemaAndMaterialize(t *testing.T) {
	t.Parallel()
	ready := []string{"tuic", "tuic_0rtt", "tuic_custom"}
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
			for _, key := range []string{"congestion_control", "udp_relay_mode", "zero_rtt"} {
				if _, ok := schema[key]; !ok {
					t.Fatalf("schema missing %s", key)
				}
			}

			set := domain.InboundSet{
				Name: "m1", Listen: "127.0.0.1", ListenPort: uint16(23000 + i),
				Presets: []string{tag},
			}
			user := domain.User{
				Name: "u1", Enabled: true, CreatedAt: now,
				Creds: map[string]map[string]any{
					tag: {
						"uuid":     "11111111-2222-3333-4444-555555555555",
						"password": "tuic-test-secret",
					},
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
			if tag == "tuic_0rtt" {
				if ib["zero_rtt_handshake"] != true {
					t.Fatalf("expected zero_rtt_handshake on inbound: %#v", ib)
				}
			} else if tag == "tuic" {
				if _, ok := ib["zero_rtt_handshake"]; ok {
					t.Fatalf("stock tuic must not set zero_rtt: %#v", ib["zero_rtt_handshake"])
				}
			}
			body, err := materialize.RenderSubscription(
				user, []domain.InboundSet{set}, "h.example", tls,
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
			if ob["udp_relay_mode"] != "native" {
				t.Fatalf("default profile udp_relay_mode=%v", ob["udp_relay_mode"])
			}
			if tag == "tuic_0rtt" && ob["zero_rtt_handshake"] != true {
				t.Fatalf("outbound 0rtt %#v", ob)
			}
		})
	}
}
