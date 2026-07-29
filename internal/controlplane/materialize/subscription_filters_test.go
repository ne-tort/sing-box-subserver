//go:build with_controlplane

package materialize

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
)

func TestSubscriptionCatalogValidateStrict(t *testing.T) {
	t.Parallel()
	set := domain.InboundSet{
		Name: "s1", ListenPort: 443,
		Bindings: []domain.SetBinding{
			{
				Preset:              "vless-tcp",
				SubscriptionTags:    []string{"mobile"},
				EnabledUserVariants: []string{"flow-none"},
			},
		},
	}
	cat := BuildSubscriptionCatalog([]domain.InboundSet{set})
	if err := cat.Validate(SubscriptionFilters{Variants: []string{"flow-none"}}); err != nil {
		t.Fatalf("expected ok: %v", err)
	}
	if err := cat.Validate(SubscriptionFilters{Variants: []string{"flow-nope"}}); err == nil {
		t.Fatal("expected unknown variant error")
	}
	if err := cat.Validate(SubscriptionFilters{Tags: []string{"mobile"}}); err != nil {
		t.Fatalf("tag mobile: %v", err)
	}
	if err := cat.Validate(SubscriptionFilters{Tags: []string{"missing"}}); err == nil {
		t.Fatal("expected unknown tag error")
	}
}

func TestRenderSubscriptionStrictAndSorted(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	set := domain.InboundSet{
		Name: "mix", Listen: "0.0.0.0", ListenPort: 443,
		Presets: []string{"trojan-tcp", "vless-tcp"},
		Bindings: []domain.SetBinding{
			{Preset: "trojan-tcp"},
			{Preset: "vless-tcp"},
		},
	}
	user := domain.User{
		Name: "u1", Enabled: true, CreatedAt: now,
		Creds: map[string]map[string]any{
			"trojan-tcp": {"password": "t"},
			"vless-tcp": {
				"uuid":      "11111111-2222-3333-4444-555555555555",
				"uuid_xtls": "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
				"uuid_udp":  "bbbbbbbb-cccc-dddd-eeee-ffff-000000000000",
			},
		},
	}
	body, err := RenderSubscription(user, []domain.InboundSet{set}, "h.example", domain.DefaultSelfSigned("h.example"), SubscriptionFilters{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatal(err)
	}
	outs, _ := doc["outbounds"].([]any)
	if len(outs) < 2 {
		t.Fatalf("outbounds=%d", len(outs))
	}
	prev := ""
	for _, o := range outs {
		ob, _ := o.(map[string]any)
		tag := ob["tag"].(string)
		if tag < prev {
			t.Fatalf("out of order: %s after %s", tag, prev)
		}
		prev = tag
	}

	_, err = RenderSubscription(user, []domain.InboundSet{set}, "h.example", domain.DefaultSelfSigned("h.example"),
		SubscriptionFilters{StrictFilters: true, Presets: []string{"no-such"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "cp_invalid_sub_filter") {
		t.Fatalf("strict preset err=%v", err)
	}
}
