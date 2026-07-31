//go:build with_controlplane

package presets

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"sync"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
	cpi18n "github.com/ne-tort/sing-box-subserver/internal/controlplane/presets/i18n"
)

//go:embed data/index.json data/*/protocol.json data/*/*.json
var dataFS embed.FS

var (
	loadOnce     sync.Once
	loadErr      error
	protocols    []domain.ProtocolMeta
	protocolBy   map[string]domain.ProtocolMeta
	invariants   []domain.InvariantPreset
	invariantBy  map[string]domain.InvariantPreset // tag + aliases
	canonicalTag map[string]string                 // any name → canonical tag
)

func ensureLoaded() {
	loadOnce.Do(func() {
		loadErr = loadCatalog(dataFS)
	})
}

func loadCatalog(fsys fs.FS) error {
	raw, err := fs.ReadFile(fsys, "data/index.json")
	if err != nil {
		return fmt.Errorf("presets index: %w", err)
	}
	var idx struct {
		SchemaVersion int      `json:"schema_version"`
		Protocols     []string `json:"protocols"`
	}
	if err := json.Unmarshal(raw, &idx); err != nil {
		return fmt.Errorf("presets index json: %w", err)
	}
	if idx.SchemaVersion != 1 {
		return fmt.Errorf("presets index unsupported schema_version %d", idx.SchemaVersion)
	}

	protocolBy = make(map[string]domain.ProtocolMeta)
	invariantBy = make(map[string]domain.InvariantPreset)
	canonicalTag = make(map[string]string)
	protocols = nil
	invariants = nil

	for _, ptag := range idx.Protocols {
		prow, err := fs.ReadFile(fsys, path.Join("data", ptag, "protocol.json"))
		if err != nil {
			return fmt.Errorf("protocol %s: %w", ptag, err)
		}
		var meta domain.ProtocolMeta
		if err := json.Unmarshal(prow, &meta); err != nil {
			return fmt.Errorf("protocol %s json: %w", ptag, err)
		}
		if meta.SchemaVersion != 1 {
			return fmt.Errorf("protocol %s: bad schema_version", ptag)
		}
		if meta.Tag == "" {
			meta.Tag = ptag
		}
		if meta.Tag != ptag {
			return fmt.Errorf("protocol folder %s != tag %s", ptag, meta.Tag)
		}
		if meta.SingBoxType == "" {
			meta.SingBoxType = meta.Tag
		}
		ru, ok := meta.I18n["ru"]
		if !ok || (ru.Title == "" && ru.Description == "") {
			if cpi18n.Protocol(ptag, "title", "ru") == "" && cpi18n.Protocol(ptag, "title", "en") == "" {
				return fmt.Errorf("protocol %s: i18n.ru required (or locale protocol.%s.title)", ptag, ptag)
			}
		}

		entries, err := fs.ReadDir(fsys, path.Join("data", ptag))
		if err != nil {
			return err
		}
		var invTags []string
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || name == "protocol.json" || !strings.HasSuffix(name, ".json") {
				continue
			}
			tag := strings.TrimSuffix(name, ".json")
			b, err := fs.ReadFile(fsys, path.Join("data", ptag, name))
			if err != nil {
				return err
			}
			var inv domain.InvariantPreset
			if err := json.Unmarshal(b, &inv); err != nil {
				return fmt.Errorf("%s/%s: %w", ptag, name, err)
			}
			if inv.SchemaVersion != 1 {
				return fmt.Errorf("%s: bad schema_version", tag)
			}
			if inv.Tag != tag {
				return fmt.Errorf("%s: filename tag mismatch %q", name, inv.Tag)
			}
			if inv.Protocol != ptag {
				return fmt.Errorf("%s: protocol %q != folder %s", tag, inv.Protocol, ptag)
			}
			iru, ok := inv.I18n["ru"]
			if !ok || (iru.Title == "" && iru.Description == "") {
				// Locale files may supply texts; allow empty inline i18n when locales exist.
				if cpi18n.Preset(tag, "title", "ru") == "" && cpi18n.Preset(tag, "title", "en") == "" &&
					cpi18n.Preset(tag, "description", "ru") == "" && cpi18n.Preset(tag, "description", "en") == "" {
					return fmt.Errorf("%s: i18n.ru required (or locale preset.%s.title)", tag, tag)
				}
			}
			st := inv.Status
			if st == "" {
				st = "stable"
			}
			if st == "stable" || st == "lab" {
				endpoint := false
				inboundOnly := false
				for _, tr := range inv.Traits {
					if tr == "endpoint" {
						endpoint = true
					}
					if tr == "inbound_only" {
						inboundOnly = true
					}
				}
				if endpoint {
					if len(inv.EndpointTemplate) == 0 {
						return fmt.Errorf("%s: endpoint_template required for trait endpoint", tag)
					}
					if len(inv.OutboundTemplate) == 0 {
						return fmt.Errorf("%s: outbound_template required for endpoint preset", tag)
					}
					if len(inv.CredFields) == 0 {
						return fmt.Errorf("%s: cred_fields required", tag)
					}
				} else {
					if len(inv.InboundTemplate) == 0 {
						return fmt.Errorf("%s: inbound_template required for status %s", tag, st)
					}
					if !inboundOnly && len(inv.OutboundTemplate) == 0 {
						return fmt.Errorf("%s: outbound_template required for status %s", tag, st)
					}
					if !inboundOnly && len(inv.CredFields) == 0 {
						return fmt.Errorf("%s: cred_fields required", tag)
					}
				}
			}
			if _, exists := invariantBy[inv.Tag]; exists {
				return fmt.Errorf("duplicate invariant tag %q", inv.Tag)
			}
			if err := registerInvariant(inv); err != nil {
				return err
			}
			invTags = append(invTags, inv.Tag)
			invariants = append(invariants, inv)
		}
		sort.Strings(invTags)
		meta.InvariantTags = invTags
		protocols = append(protocols, meta)
		protocolBy[meta.Tag] = meta
	}
	return nil
}

func registerInvariant(inv domain.InvariantPreset) error {
	invariantBy[inv.Tag] = inv
	canonicalTag[inv.Tag] = inv.Tag
	for _, a := range inv.Aliases {
		if a == "" {
			continue
		}
		if other, ok := canonicalTag[a]; ok && other != inv.Tag {
			return fmt.Errorf("alias %q maps to both %s and %s", a, other, inv.Tag)
		}
		canonicalTag[a] = inv.Tag
		invariantBy[a] = inv
	}
	return nil
}
