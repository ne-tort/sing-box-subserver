//go:build with_controlplane

package i18n

import (
	"embed"
	"encoding/json"
	"io/fs"
	"strings"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
)

//go:embed locales
var localeFS embed.FS

func loadEmbedded() {
	_ = fs.WalkDir(localeFS, "locales", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".json") {
			return nil
		}
		parts := strings.Split(filepathSlash(p), "/")
		if len(parts) < 3 {
			return nil
		}
		lang := parts[1]
		b, err := fs.ReadFile(localeFS, p)
		if err != nil {
			return nil
		}
		mergeInto(lang, b)
		return nil
	})
}

func filepathSlash(p string) string {
	return strings.ReplaceAll(p, "\\", "/")
}

func mergeInto(lang string, raw []byte) {
	lang = domain.NormalizeLang(lang)
	if lang == "" {
		lang = "en"
	}
	var root any
	if err := json.Unmarshal(raw, &root); err != nil {
		return
	}
	if byLang[lang] == nil {
		byLang[lang] = map[string]string{}
	}
	flatten("", root, byLang[lang])
}
