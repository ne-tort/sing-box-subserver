//go:build with_controlplane

package domain_test

import (
	"strings"
	"testing"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
	cpi18n "github.com/ne-tort/sing-box-subserver/internal/controlplane/presets/i18n"
)

func TestNormalizeLangRegional(t *testing.T) {
	cases := map[string]string{
		"":      "ru",
		"ru-RU": "ru",
		"en_US": "en",
		"pt-BR": "pt-BR",
		"pt_br": "pt-BR",
		"zh-CN": "zh-CN",
		"zh-TW": "zh-TW",
		"zh":    "zh-CN",
		"fa-IR": "fa",
		"tr":    "tr",
	}
	for in, want := range cases {
		if got := domain.NormalizeLang(in); got != want {
			t.Fatalf("NormalizeLang(%q)=%q want %q", in, got, want)
		}
	}
}

func TestI18nFallbackToEnglish(t *testing.T) {
	en := cpi18n.Protocol("vless", "description", "en")
	if en == "" {
		t.Fatal("en protocol.vless.description empty")
	}
	got := cpi18n.Protocol("vless", "description", "xx")
	if got != en {
		t.Fatalf("unknown lang fallback: got %q want en %q", got, en)
	}
}

func TestCatalogLangsMatchHiddify(t *testing.T) {
	need := map[string]bool{
		"ar": true, "en": true, "es": true, "fa": true, "fr": true, "id": true,
		"pt-BR": true, "ru": true, "tr": true, "zh-CN": true, "zh-TW": true,
	}
	for _, l := range domain.CatalogLangs {
		delete(need, l)
	}
	if len(need) != 0 {
		t.Fatalf("missing catalog langs: %v", need)
	}
}

func TestNoEnglishStubDescriptions(t *testing.T) {
	for _, tag := range []string{"vmess_reality", "tuic", "shadowquic_jls", "carrier_jitsi_shared", "ss_aes128"} {
		d := cpi18n.Preset(tag, "description", "en")
		if d == "" {
			t.Fatalf("%s: empty en description", tag)
		}
		if len(d) < 24 {
			t.Fatalf("%s: weak en description %q", tag, d)
		}
		if strings.Contains(strings.ToLower(d), "controlplane preset") {
			t.Fatalf("%s: stub description %q", tag, d)
		}
	}
}
