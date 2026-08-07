//go:build with_controlplane

package controlplane

import (
	"sort"
	"strings"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/catalogsqlite"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
	cpi18n "github.com/ne-tort/sing-box-subserver/internal/controlplane/presets/i18n"
)

const paramsSchemaVersion = 2

// paramI18nPreset is the locale key namespace for param.* copy.
// Catalogsqlite ready presets reuse the base constructor locale keys.
func paramI18nPreset(pp domain.ProtocolPreset) string {
	if catalogsqlite.Owns(pp.Name) {
		if base := catalogsqlite.KnobProfile(pp.Name); base != "" {
			return base
		}
	}
	return pp.Name
}

// buildParamsSchema returns the thin-client form schema for a preset (lang=en).
func buildParamsSchema(pp domain.ProtocolPreset, detail bool) map[string]any {
	return buildParamsSchemaLang(pp, detail, "en")
}

func buildParamsSchemaLang(pp domain.ProtocolPreset, detail bool, lang string) map[string]any {
	if strings.TrimSpace(lang) == "" {
		lang = "en"
	}
	out := map[string]any{}

	listen := baseListenPortField(lang, detail)
	out["listen_port"] = listen

	keys := collectParamKeys(pp)
	for _, f := range keys {
		if f == "" || f == "listen_port" || f == "sni" || f == "self_signed_sni" || f == "demux_sni" || f == domain.BindingParamRealitySNI ||
			f == "tls_alpn" || f == "tls_min_version" || f == "tls_max_version" ||
			f == "tls_cipher_suites" || f == "tls_curve_preferences" || f == "ech" ||
			f == domain.BindingParamSSLProfile {
			continue
		}
		out[f] = paramFieldSchema(f, pp, lang)
	}

	if supportsSSLProfile(pp) {
		out[domain.BindingParamSSLProfile] = map[string]any{
			"type":        "string",
			"required":    false,
			"title":       i18nPick(lang, "SSL profile", "SSL-профиль"),
			"description": "SSL profile id from GET /v1/controlplane/ssl. Empty = Default self-signed profile.",
			"ui_group":    "tls",
			"ui_order":    90,
			"widget":      "ssl_profile",
			"help": map[string]any{
				"summary":    "Select a server SSL profile (leaf certificate + handshake + optional ECH).",
				"input_hint": "Leave empty for the Default self-signed profile.",
				"format":     "default",
			},
		}
	}
	if domain.BindingUsesReality(pp, nil) {
		out[domain.BindingParamRealitySNI] = map[string]any{
			"type":        "string",
			"required":    false,
			"title":       i18nPick(lang, "Reality SNI", "Reality SNI"),
			"description": "Optional SNI from the server Reality list. Empty = auto-pick.",
			"ui_group":    "tls",
			"ui_order":    91,
			"widget":      "select",
			"help": map[string]any{
				"summary":    "Pin this inbound to a Reality pool SNI, or leave empty for automatic selection.",
				"input_hint": "Leave empty for auto, or pick a hostname from the Reality list.",
				"format":     "www.apple.com",
			},
		}
	}
	if detail {
		out["demux_sni"] = map[string]any{
			"type":        "string",
			"required":    false,
			"title":       i18nPick(lang, "Demux SNI", "Demux SNI"),
			"description": "SNI used for demux match / TLS server_name when installed inside a demux group.",
			"ui_group":    "demux",
			"ui_order":    95,
			"widget":      "text",
			"help": map[string]any{
				"summary":    "SNI label matched by the demux front.",
				"input_hint": "Usually the same domain as the SSL profile.",
				"format":     "example.com",
			},
		}
	}

	out["_schema_version"] = paramsSchemaVersion
	out["_ui_groups"] = uiGroupsForPreset(pp, lang)
	return out
}

func baseListenPortField(lang string, detail bool) map[string]any {
	desc := "Public listen port for single-inbound install. Omit to auto-pick a free port (prefers 443)."
	if detail {
		desc = "Public listen port when installing as a single-inbound set (not demux member). Omit to auto-pick."
	}
	return map[string]any{
		"type":        "uint16",
		"required":    false,
		"title":       i18nPick(lang, "Listen port", "Порт"),
		"description": desc,
		"constraint":  "At most one TCP and one UDP occupant per port.",
		"min":         float64(1),
		"max":         float64(65535),
		"ui_group":    "listen",
		"ui_order":    0,
		"widget":      "port",
		"help": map[string]any{
			"summary":    "Port the inbound listens on publicly.",
			"input_hint": "Leave empty for auto, or set 1–65535.",
			"format":     "443",
		},
	}
}

func collectParamKeys(pp domain.ProtocolPreset) []string {
	seen := map[string]struct{}{}
	var keys []string
	add := func(k string) {
		k = strings.TrimSpace(k)
		if k == "" {
			return
		}
		if _, ok := seen[k]; ok {
			return
		}
		seen[k] = struct{}{}
		keys = append(keys, k)
	}
	for _, f := range pp.ParamFields {
		add(f)
	}
	for _, f := range pp.OptionalParamFields {
		add(f)
	}
	for f := range pp.ParamMeta {
		add(f)
	}
	sort.SliceStable(keys, func(i, j int) bool {
		oi, oj := uiOrder(pp, keys[i]), uiOrder(pp, keys[j])
		if oi != oj {
			return oi < oj
		}
		return keys[i] < keys[j]
	})
	return keys
}

func uiOrder(pp domain.ProtocolPreset, key string) int {
	if m, ok := pp.ParamMeta[key]; ok && m.UiOrder != 0 {
		return m.UiOrder
	}
	for i, f := range pp.ParamFields {
		if f == key {
			return i + 1
		}
	}
	return 100
}

func fieldIsRequired(field string, pp domain.ProtocolPreset) bool {
	if m, ok := pp.ParamMeta[field]; ok && m.Required != nil {
		return *m.Required
	}
	for _, f := range pp.ParamFields {
		if f == field {
			return true
		}
	}
	return false
}

func paramFieldSchema(field string, pp domain.ProtocolPreset, lang string) map[string]any {
	meta, _ := pp.ParamMeta[field]
	m := map[string]any{
		"type":        firstNonEmpty(meta.Type, "string"),
		"required":    fieldIsRequired(field, pp),
		"title":       paramFieldTitle(field, pp, lang),
		"description": paramFieldDescription(field, pp, lang),
		"help":        paramFieldHelp(field, pp, lang),
	}
	m = enrichFieldSchema(m, meta, lang)
	if guides := paramFieldRequiredGuides(field, pp, lang); len(guides) > 0 {
		m["required_guides"] = guides
		m["required_guide"] = guides[0]
	} else if guide := paramFieldRequiredGuide(field, pp, lang); guide != nil {
		m["required_guide"] = guide
	}
	return m
}

func enrichFieldSchema(m map[string]any, meta domain.ParamFieldMeta, lang string) map[string]any {
	if meta.Type != "" {
		m["type"] = meta.Type
	}
	if len(meta.Enum) > 0 {
		m["enum"] = append([]string{}, meta.Enum...)
		labels := map[string]string{}
		for _, v := range meta.Enum {
			if lm, ok := meta.EnumLabels[v]; ok {
				if t := domain.PickLocalized(lm, lang); t != "" {
					labels[v] = t
					continue
				}
			}
			labels[v] = v
		}
		m["enum_labels"] = labels
	}
	if meta.Default != "" {
		m["default"] = meta.Default
	}
	if meta.Placeholder != "" {
		m["placeholder"] = meta.Placeholder
	}
	if meta.Pattern != "" {
		m["pattern"] = meta.Pattern
	}
	if meta.Min != nil {
		m["min"] = *meta.Min
	}
	if meta.Max != nil {
		m["max"] = *meta.Max
	}
	if meta.UiGroup != "" {
		m["ui_group"] = meta.UiGroup
	}
	if meta.UiOrder != 0 {
		m["ui_order"] = meta.UiOrder
	}
	widget := meta.Widget
	if widget == "" {
		switch firstNonEmpty(meta.Type, "string") {
		case "bool":
			widget = "toggle"
		case "enum":
			widget = "select"
		case "uint16":
			widget = "port"
		default:
			if len(meta.Enum) > 0 {
				widget = "select"
			} else {
				widget = "text"
			}
		}
	}
	m["widget"] = widget
	if len(meta.UiActions) > 0 {
		m["ui_actions"] = append([]string{}, meta.UiActions...)
	}
	if len(meta.VisibleWhen) > 0 {
		conds := make([]any, 0, len(meta.VisibleWhen))
		for _, c := range meta.VisibleWhen {
			cm := map[string]any{"key": c.Key}
			if c.Equals != "" {
				cm["equals"] = c.Equals
			}
			if len(c.In) > 0 {
				cm["in"] = append([]string{}, c.In...)
			}
			if c.NotEmpty {
				cm["not_empty"] = true
			}
			conds = append(conds, cm)
		}
		m["visible_when"] = conds
	}
	if len(meta.Requires) > 0 {
		m["requires"] = append([]string{}, meta.Requires...)
	}
	if len(meta.ConflictsWith) > 0 {
		m["conflicts_with"] = append([]string{}, meta.ConflictsWith...)
	}
	return m
}

func uiGroupsForPreset(pp domain.ProtocolPreset, lang string) []any {
	order := []string{"listen", "core", "transport", "tls", "obfs", "awg", "demux", "advanced"}
	titles := map[string][2]string{
		"listen":    {"Listen", "Слушатель"},
		"core":      {"Core", "Основное"},
		"transport": {"Transport", "Транспорт"},
		"tls":       {"TLS / Reality", "TLS / Reality"},
		"obfs":      {"Obfuscation", "Обфускация"},
		"awg":       {"AmneziaWG", "AmneziaWG"},
		"demux":     {"Demux", "Demux"},
		"advanced":  {"Advanced", "Дополнительно"},
	}
	seen := map[string]struct{}{"listen": {}}
	for _, f := range collectParamKeys(pp) {
		g := "core"
		if m, ok := pp.ParamMeta[f]; ok && m.UiGroup != "" {
			g = m.UiGroup
		}
		seen[g] = struct{}{}
	}
	var out []any
	for _, g := range order {
		if _, ok := seen[g]; !ok {
			continue
		}
		t := titles[g]
		out = append(out, map[string]any{
			"id":    g,
			"title": i18nPick(lang, t[0], t[1]),
		})
	}
	for g := range seen {
		known := false
		for _, o := range order {
			if o == g {
				known = true
				break
			}
		}
		if known {
			continue
		}
		out = append(out, map[string]any{"id": g, "title": g})
	}
	return out
}

func supportsSSLProfile(pp domain.ProtocolPreset) bool {
	if meta, ok := pp.ParamMeta["tls_mode"]; ok && pp.CustomPreset {
		for _, v := range meta.Enum {
			if strings.EqualFold(strings.TrimSpace(v), "tls") {
				return true
			}
		}
		mode, _ := domain.BindingTLSMode(pp, nil)
		return mode == "tls"
	}
	if hasTrait(pp.Traits, "reality") {
		return false
	}
	return hasTrait(pp.Traits, "tls") || hasTrait(pp.Traits, "tls_custom")
}

func presetOptionalParams(pp domain.ProtocolPreset) map[string]any {
	return presetOptionalParamsLang(pp, "en")
}

func presetOptionalParamsLang(pp domain.ProtocolPreset, lang string) map[string]any {
	schema := buildParamsSchemaLang(pp, false, lang)
	out := map[string]any{}
	for k, v := range schema {
		if strings.HasPrefix(k, "_") {
			continue
		}
		m, ok := v.(map[string]any)
		if !ok {
			continue
		}
		if req, _ := m["required"].(bool); req {
			continue
		}
		out[k] = v
	}
	return out
}

func presetOptionalParamsDetail(pp domain.ProtocolPreset) map[string]any {
	return presetOptionalParamsDetailLang(pp, "en")
}

func presetOptionalParamsDetailLang(pp domain.ProtocolPreset, lang string) map[string]any {
	schema := buildParamsSchemaLang(pp, true, lang)
	out := map[string]any{}
	for k, v := range schema {
		if strings.HasPrefix(k, "_") {
			continue
		}
		m, ok := v.(map[string]any)
		if !ok {
			continue
		}
		if req, _ := m["required"].(bool); req {
			continue
		}
		out[k] = v
	}
	return out
}

func paramFieldTitle(field string, pp domain.ProtocolPreset, lang string) string {
	if t := cpi18n.Param(paramI18nPreset(pp), field, "title", lang); t != "" {
		return t
	}
	if m, ok := pp.ParamMeta[field]; ok {
		if t := domain.PickLocalized(m.Title, lang); t != "" {
			return t
		}
	}
	switch field {
	case "room":
		return i18nPick(lang, "Room URL", "URL комнаты")
	case "token":
		return i18nPick(lang, "Tunnel token", "Токен туннеля")
	case "masquerade_dir":
		return i18nPick(lang, "Masquerade directory", "Каталог masquerade")
	case "realm_server_url":
		return i18nPick(lang, "Realm server URL", "URL realm-сервера")
	case "realm_id":
		return i18nPick(lang, "Realm ID", "ID realm")
	case "ws_host", "hu_host", "http_host":
		return "HTTP Host"
	case "ws_path", "hu_path", "http_path":
		return i18nPick(lang, "Path", "Путь")
	case "grpc_service_name":
		return i18nPick(lang, "gRPC service", "gRPC сервис")
	case "sni":
		return "SNI"
	case "transport":
		return i18nPick(lang, "Transport", "Транспорт")
	case "security":
		return i18nPick(lang, "Security", "Безопасность")
	case "flow":
		return "Flow"
	case "packet_encoding":
		return i18nPick(lang, "Packet encoding", "Packet encoding")
	case "fingerprint":
		return i18nPick(lang, "uTLS fingerprint", "uTLS fingerprint")
	default:
		return field
	}
}

func paramFieldDescription(field string, pp domain.ProtocolPreset, lang string) string {
	if t := cpi18n.Param(paramI18nPreset(pp), field, "description", lang); t != "" {
		return t
	}
	if m, ok := pp.ParamMeta[field]; ok {
		if d := domain.PickLocalized(m.Description, lang); d != "" {
			return d
		}
	}
	switch field {
	case "room":
		return i18nPick(lang, "SFU/room URL for Carrier underlay.", "URL SFU/комнаты для Carrier underlay.")
	case "token":
		return i18nPick(lang, "Tunnel/provider token.", "Токен туннеля/провайдера.")
	case "masquerade_dir":
		return i18nPick(lang, "On-disk root for Hy2 file masquerade.", "Каталог на диске для Hy2 file-masquerade.")
	case "realm_server_url":
		return i18nPick(lang, "Hysteria realm control URL.", "URL control-plane Hysteria realm.")
	case "realm_id":
		return i18nPick(lang, "Realm identifier.", "Идентификатор realm.")
	case "ws_host", "hu_host", "http_host", "transport_host":
		if hasTrait(pp.Traits, "reality") {
			return i18nPick(lang, "Host header; materialize often aligns to Reality SNI.", "Host header; materialize часто выравнивает под Reality SNI.")
		}
		return i18nPick(lang, "HTTP Host / :authority for the transport.", "HTTP Host / :authority транспорта.")
	case "ws_path", "hu_path", "http_path", "transport_path":
		return i18nPick(lang, "HTTP path after TLS.", "HTTP path после TLS.")
	case "grpc_service_name", "service_name":
		return i18nPick(lang, "gRPC Gun service_name.", "gRPC Gun service_name.")
	case "sni":
		return i18nPick(lang, "TLS server_name / ACME domain (not Reality).", "TLS server_name / домен ACME (не Reality).")
	case "up_mbps":
		return i18nPick(lang, "Upload bandwidth cap (Mbps).", "Потолок upload (Mbps).")
	case "down_mbps":
		return i18nPick(lang, "Download bandwidth cap (Mbps).", "Потолок download (Mbps).")
	case "mtu":
		return i18nPick(lang, "Interface MTU.", "MTU интерфейса.")
	case "flow":
		return i18nPick(lang, "XTLS Vision flow (TCP+TLS/Reality only).", "XTLS Vision (только TCP+TLS/Reality).")
	case "fingerprint":
		return i18nPick(lang, "uTLS ClientHello fingerprint.", "uTLS fingerprint ClientHello.")
	case "packet_encoding":
		return i18nPick(lang, "VLESS outbound UDP encoding.", "Кодирование UDP VLESS outbound.")
	case "alpn":
		return i18nPick(lang, "Comma-separated ALPN list.", "ALPN через запятую.")
	case "transport":
		return i18nPick(lang, "VLESS/sing-box transport.type.", "transport.type VLESS/sing-box.")
	case "tls_mode":
		return i18nPick(lang, "none | tls | reality.", "none | tls | reality.")
	default:
		return i18nPick(lang, "Preset parameter.", "Параметр пресета.")
	}
}

func paramFieldHelp(field string, pp domain.ProtocolPreset, lang string) map[string]any {
	// Locale files win (thin-client source of truth for copy).
	// Ready presets share constructor param copy under the base tag (e.g. vless_custom).
	i18nPreset := paramI18nPreset(pp)
	if s := cpi18n.Param(i18nPreset, field, "help.summary", lang); s != "" {
		return map[string]any{
			"summary":    s,
			"input_hint": cpi18n.Param(i18nPreset, field, "help.input_hint", lang),
			"format":     cpi18n.Param(i18nPreset, field, "help.format", lang),
		}
	}
	if m, ok := pp.ParamMeta[field]; ok && m.Help != nil {
		h := m.Help
		out := map[string]any{
			"summary":    domain.PickLocalized(h.Summary, lang),
			"input_hint": domain.PickLocalized(h.InputHint, lang),
			"format":     h.Format,
		}
		if out["summary"] != "" || out["input_hint"] != "" || out["format"] != "" {
			return out
		}
	}
	switch field {
	case "room":
		return map[string]any{
			"summary":    i18nPick(lang, "Full SFU/room URL — Carrier joins that call as underlay.", "Полный URL комнаты SFU — Carrier использует звонок как underlay."),
			"input_hint": i18nPick(lang, "Create a room, copy the link.", "Создайте комнату, скопируйте ссылку."),
			"format":     "https://meet.jit.si/MyRoom",
		}
	case "token":
		return map[string]any{
			"summary":    i18nPick(lang, "Provider tunnel token (opaque string).", "Токен туннеля провайдера (opaque-строка)."),
			"input_hint": i18nPick(lang, "Dashboard → create tunnel → copy token.", "Кабинет → создать туннель → скопировать token."),
			"format":     "eyJ...",
		}
	case "ws_path", "hu_path", "http_path", "transport_path":
		return map[string]any{
			"summary":    i18nPick(lang, "HTTP path after TLS. Match CDN/proxy rules; stock defaults are easy DPI fingerprints.", "HTTP path после TLS. Совпадайте с CDN/прокси; стоковые path легко ловятся DPI."),
			"input_hint": i18nPick(lang, "Path starting with /", "Путь с /"),
			"format":     "/api/v1/connect",
		}
	case "ws_host", "hu_host", "http_host", "transport_host":
		return map[string]any{
			"summary":    i18nPick(lang, "Host/:authority must match the front (often = SNI).", "Host/:authority — как ждёт фронт (часто = SNI)."),
			"input_hint": i18nPick(lang, "Hostname without scheme", "Хост без схемы"),
			"format":     "cdn.example.com",
		}
	case "masquerade_dir":
		return map[string]any{
			"summary":    i18nPick(lang, "Static root for Hy2 file masquerade on auth failure.", "Корень статики Hy2 file-masquerade при неверном auth."),
			"input_hint": i18nPick(lang, "Absolute path on the VPS", "Абсолютный путь на VPS"),
			"format":     "/var/www/html",
		}
	case "realm_server_url":
		return map[string]any{
			"summary":    i18nPick(lang, "Hysteria realm control URL.", "URL control-plane Hysteria realm."),
			"input_hint": i18nPick(lang, "https URL of realm server", "https URL realm-сервера"),
			"format":     "https://realm.example.com",
		}
	case "realm_id":
		return map[string]any{
			"summary":    i18nPick(lang, "Realm id from the operator.", "Идентификатор realm у оператора."),
			"input_hint": i18nPick(lang, "Opaque realm id", "Opaque id"),
			"format":     "my-realm",
		}
	default:
		return map[string]any{
			"summary":    paramFieldDescription(field, pp, lang),
			"input_hint": "",
			"format":     "",
		}
	}
}

func localizeGuideMeta(g domain.ParamGuideMeta, lang string) map[string]any {
	steps := make([]any, 0, len(g.Steps))
	for _, s := range g.Steps {
		step := map[string]any{"text": domain.PickLocalized(s.Text, lang)}
		if u := strings.TrimSpace(s.URL); u != "" {
			step["url"] = u
		}
		steps = append(steps, step)
	}
	out := map[string]any{
		"title": domain.PickLocalized(g.Title, lang),
		"steps": steps,
	}
	if len(g.VisibleWhen) > 0 {
		conds := make([]any, 0, len(g.VisibleWhen))
		for _, c := range g.VisibleWhen {
			cm := map[string]any{"key": c.Key}
			if c.Equals != "" {
				cm["equals"] = c.Equals
			}
			if len(c.In) > 0 {
				cm["in"] = append([]string{}, c.In...)
			}
			if c.NotEmpty {
				cm["not_empty"] = true
			}
			conds = append(conds, cm)
		}
		out["visible_when"] = conds
	}
	return out
}

func paramFieldRequiredGuides(field string, pp domain.ProtocolPreset, lang string) []map[string]any {
	m, ok := pp.ParamMeta[field]
	if !ok || len(m.RequiredGuides) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(m.RequiredGuides))
	for _, g := range m.RequiredGuides {
		if len(g.Steps) == 0 {
			continue
		}
		out = append(out, localizeGuideMeta(g, lang))
	}
	return out
}

func paramFieldRequiredGuide(field string, pp domain.ProtocolPreset, lang string) map[string]any {
	if m, ok := pp.ParamMeta[field]; ok && m.RequiredGuide != nil && len(m.RequiredGuide.Steps) > 0 {
		return localizeGuideMeta(*m.RequiredGuide, lang)
	}
	switch field {
	case "room":
		if hasTrait(pp.Traits, "telemost") {
			return map[string]any{
				"title": i18nPick(lang, "How to get a Telemost room URL", "Как получить URL комнаты Телемост"),
				"steps": []any{
					map[string]any{"text": i18nPick(lang, "Open Yandex Telemost", "Откройте Яндекс Телемост"), "url": "https://telemost.yandex.ru"},
					map[string]any{"text": i18nPick(lang, "Create a video-call room", "Создайте комнату видеозвонка")},
					map[string]any{"text": i18nPick(lang, "Copy the room link and paste it into «Room URL»", "Скопируйте ссылку комнаты и вставьте в «URL комнаты»")},
					map[string]any{"text": i18nPick(lang, "Example format: https://telemost.yandex.ru/j/…", "Пример формата: https://telemost.yandex.ru/j/…")},
				},
			}
		}
		if hasTrait(pp.Traits, "wbstream") {
			return map[string]any{
				"title": i18nPick(lang, "How to get a WB Stream room URL", "Как получить URL комнаты WB Stream"),
				"steps": []any{
					map[string]any{"text": i18nPick(lang, "Open the WB Stream room / broadcast page", "Откройте страницу комнаты / трансляции WB Stream")},
					map[string]any{"text": i18nPick(lang, "Copy the full room URL from the browser address bar", "Скопируйте полный URL комнаты из адресной строки браузера")},
					map[string]any{"text": i18nPick(lang, "Paste it into «Room URL»", "Вставьте его в «URL комнаты»")},
				},
			}
		}
		return map[string]any{
			"title": i18nPick(lang, "How to get a room URL", "Как получить URL комнаты"),
			"steps": []any{
				map[string]any{"text": i18nPick(lang, "Open Jitsi Meet", "Откройте Jitsi Meet"), "url": "https://meet.jit.si"},
				map[string]any{"text": i18nPick(lang, "Create a video-call room", "Создайте комнату видеозвонка")},
				map[string]any{"text": i18nPick(lang, "Copy the room link and paste it into «Room URL»", "Скопируйте ссылку комнаты и вставьте в «URL комнаты»")},
				map[string]any{"text": i18nPick(lang, "Example format: https://meet.jit.si/MyRoom", "Пример формата: https://meet.jit.si/MyRoom")},
			},
		}
	case "token":
		return map[string]any{
			"title": i18nPick(lang, "How to get a tunnel token", "Как получить токен туннеля"),
			"steps": []any{
				map[string]any{"text": i18nPick(lang, "Open Cloudflare Zero Trust", "Откройте Cloudflare Zero Trust"), "url": "https://one.dash.cloudflare.com"},
				map[string]any{"text": i18nPick(lang, "Go to Networks → Tunnels", "Перейдите в Networks → Tunnels")},
				map[string]any{"text": i18nPick(lang, "Create a Cloudflare Tunnel", "Создайте Cloudflare Tunnel")},
				map[string]any{"text": i18nPick(lang, "Copy the tunnel token", "Скопируйте токен туннеля")},
				map[string]any{"text": i18nPick(lang, "Paste it into «Tunnel token»", "Вставьте его в «Токен туннеля»")},
			},
		}
	case "masquerade_dir":
		return map[string]any{
			"title": i18nPick(lang, "How to set masquerade directory", "Как указать каталог masquerade"),
			"steps": []any{
				map[string]any{"text": i18nPick(lang, "On the VPS, pick a directory with static files (HTML/CSS/images)", "На VPS выберите каталог со статическими файлами (HTML/CSS/изображения)")},
				map[string]any{"text": i18nPick(lang, "Ensure the subserver process can read that path", "Убедитесь, что процесс subserver может читать этот путь")},
				map[string]any{"text": i18nPick(lang, "Paste the absolute path into «Masquerade directory»", "Вставьте абсолютный путь в «Каталог masquerade»")},
				map[string]any{"text": i18nPick(lang, "Example: /var/www/html", "Пример: /var/www/html")},
			},
		}
	case "realm_server_url":
		return map[string]any{
			"title": i18nPick(lang, "How to set realm server URL", "Как указать URL realm-сервера"),
			"steps": []any{
				map[string]any{"text": i18nPick(lang, "Open your Hysteria realm control-plane dashboard", "Откройте панель управления Hysteria realm")},
				map[string]any{"text": i18nPick(lang, "Copy the base HTTPS URL of the realm server", "Скопируйте базовый HTTPS URL realm-сервера")},
				map[string]any{"text": i18nPick(lang, "Paste it into «Realm server URL» (no trailing junk path)", "Вставьте его в «URL realm-сервера» (без лишнего хвоста пути)")},
				map[string]any{"text": i18nPick(lang, "Example: https://realm.example.com", "Пример: https://realm.example.com")},
			},
		}
	case "realm_id":
		return map[string]any{
			"title": i18nPick(lang, "How to set realm ID", "Как указать ID realm"),
			"steps": []any{
				map[string]any{"text": i18nPick(lang, "In the realm dashboard, open the target realm", "В панели realm откройте нужный realm")},
				map[string]any{"text": i18nPick(lang, "Copy the realm id / identifier", "Скопируйте id / идентификатор realm")},
				map[string]any{"text": i18nPick(lang, "Paste it into «Realm ID»", "Вставьте его в «ID realm»")},
			},
		}
	default:
		return nil
	}
}

func i18nPick(lang, en, ru string) string {
	if strings.HasPrefix(strings.ToLower(lang), "ru") {
		return ru
	}
	return en
}

func hasTrait(traits []string, want string) bool {
	for _, t := range traits {
		if t == want {
			return true
		}
	}
	return false
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
