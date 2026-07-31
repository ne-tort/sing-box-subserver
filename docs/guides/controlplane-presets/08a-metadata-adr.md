# ADR: метаданные пресетов (scores, описания, schema)

**Status:** accepted  
**Date:** 2026-08-01  
**Context:** overhaul каталога controlplane-пресетов (params_schema v2, locale files, `{protocol}_custom`).

## Решение

### Scores (обязательны для stable/lab)

Шкала 0–10, эталоны — [`03-scores.md`](03-scores.md):

| Ключ | Смысл |
|------|--------|
| `dpi` | Устойчивость к DPI / fingerprinting |
| `speed` | Типичная пропускная способность / latency |
| `mobile` | Пригодность на мобильных сетях (NAT, battery, UDP loss) |
| `setup` | Простота установки с клиента (инверсия сложности) |

Запрещены пустые scores у `status: stable`. Lab может иметь оценки, но они должны быть честными (низкий `setup` для конструкторов).

### Описания

- **Протокол** (3–6 предложений): что это, когда брать, ключевые ограничения.
- **Пресет**: фича, отличие от siblings, компромисс. Без тавтологий.
- `short_name` — пригоден для mobile row.

Стиль: технично, структурно, без словоблудия. Английские термины механизмов (Reality, ALPN, uTLS, mux) допустимы в ru.

### i18n

Источник: `presets/i18n/locales/{lang}/` для языков Hiddify picker:  
`ar`, `en`, `es`, `fa`, `fr`, `id`, `pt-BR`, `ru`, `tr`, `zh-CN`, `zh-TW`.

- `ru` + `en` — эталон смысла (семантический, не дословный).
- Остальные языки заполнены; API fallback: **requested → en**.
- Inline `i18n` в JSON — legacy; новые тексты только в locales.

### params_schema v2

См. `params_schema.go` и [`09-custom-presets.md`](09-custom-presets.md). Capability: `params_schema_v2`.

### Запреты

- Пустые / тавтологичные `description`.
- Stub «Controlplane preset …».
- Новые human paragraphs только в data JSON без locale-ключа.
