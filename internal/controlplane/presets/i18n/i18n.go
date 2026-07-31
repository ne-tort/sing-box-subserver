//go:build with_controlplane

package i18n

import (
	"strings"
	"sync"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
)

// Locale files live under this package (embedded). Keys use dotted paths:
//
//	protocol.<tag>.title|description
//	preset.<tag>.title|description
//	param.<preset>.<field>.title|description|help.summary|help.input_hint|help.format
//	demux.<tag>.title|description
//	common.<key>
//
// JSON shape per file: flat map[string]string OR nested objects (flattened on load).

var (
	once sync.Once
	byLang map[string]map[string]string // lang → key → text
)

func ensure() {
	once.Do(func() {
		byLang = map[string]map[string]string{}
		for _, l := range domain.CatalogLangs {
			byLang[l] = map[string]string{}
		}
		loadEmbedded()
	})
}

// Get returns a locale string for lang with fallback: lang → en.
// Russian is not a universal fallback; missing copy resolves to English.
func Get(lang, key string) string {
	ensure()
	lang = domain.NormalizeLang(lang)
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	chain := []string{lang, "en"}
	if lang == "en" {
		chain = []string{"en"}
	}
	for _, l := range chain {
		if m := byLang[l]; m != nil {
			if v := strings.TrimSpace(m[key]); v != "" {
				return v
			}
		}
	}
	return ""
}

// Protocol returns protocol.<tag>.<field>.
func Protocol(tag, field, lang string) string {
	return Get(lang, "protocol."+tag+"."+field)
}

// Preset returns preset.<tag>.<field>.
func Preset(tag, field, lang string) string {
	return Get(lang, "preset."+tag+"."+field)
}

// Param returns param.<preset>.<field>.<suffix>, then param.common.<field>.<suffix>.
func Param(preset, field, suffix, lang string) string {
	if t := Get(lang, "param."+preset+"."+field+"."+suffix); t != "" {
		return t
	}
	return Get(lang, "param.common."+field+"."+suffix)
}

// Demux returns demux.<tag>.<field>.
func Demux(tag, field, lang string) string {
	return Get(lang, "demux."+tag+"."+field)
}

// MergeFile merges a JSON object (flat or nested) into lang map.
func MergeFile(lang string, raw []byte) error {
	ensure()
	mergeInto(lang, raw)
	return nil
}

func flatten(prefix string, v any, out map[string]string) {
	switch t := v.(type) {
	case map[string]any:
		for k, child := range t {
			key := k
			if prefix != "" {
				key = prefix + "." + k
			}
			flatten(key, child, out)
		}
	case string:
		if prefix != "" && strings.TrimSpace(t) != "" {
			out[prefix] = strings.TrimSpace(t)
		}
	case []any:
		// ignore lists in locale files for now
	}
}
