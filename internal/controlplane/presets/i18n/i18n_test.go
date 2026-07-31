//go:build with_controlplane

package i18n_test

import (
	"testing"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/presets"
	cpi18n "github.com/ne-tort/sing-box-subserver/internal/controlplane/presets/i18n"
)

func TestLocaleCoverageForInvariants(t *testing.T) {
	for _, inv := range presets.AllInvariants() {
		tag := inv.Tag
		hasLocale := cpi18n.Preset(tag, "title", "ru") != "" || cpi18n.Preset(tag, "title", "en") != "" ||
			cpi18n.Preset(tag, "description", "ru") != "" || cpi18n.Preset(tag, "description", "en") != ""
		ru, ok := inv.I18n["ru"]
		hasInline := ok && (ru.Title != "" || ru.Description != "")
		if !hasLocale && !hasInline {
			t.Errorf("preset %s: missing locale title/description and inline i18n.ru", tag)
		}
	}
}

func TestProtocolLocaleOrInline(t *testing.T) {
	for _, p := range presets.Protocols() {
		hasLocale := cpi18n.Protocol(p.Tag, "title", "ru") != "" || cpi18n.Protocol(p.Tag, "title", "en") != ""
		ru, ok := p.I18n["ru"]
		hasInline := ok && (ru.Title != "" || ru.Description != "")
		if !hasLocale && !hasInline {
			t.Errorf("protocol %s: missing locale and inline i18n.ru", p.Tag)
		}
	}
}

func TestVlessLocalePresent(t *testing.T) {
	if cpi18n.Protocol("vless", "title", "en") == "" && cpi18n.Protocol("vless", "title", "ru") == "" {
		t.Fatal("expected protocol.vless.title in locale files")
	}
	if cpi18n.Preset("vless_reality", "title", "ru") == "" && cpi18n.Preset("vless_reality", "description", "ru") == "" {
		t.Fatal("expected preset.vless_reality texts in locale files")
	}
}
