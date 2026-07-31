# 02 — API

Auth: agent Bearer.

### Language (`lang`)

Resolved strings for thin clients. Order:

1. Query `?lang=` (BCP-47 / ISO: `ru`, `en`, `pt-BR`, `zh-CN`, `zh-TW`, `ru-RU`→`ru`, …)
2. Header `X-Lang`
3. Header `Accept-Language` (first tag)
4. Default: `ru`

**Catalog locales (Hiddify picker):** `ar`, `en`, `es`, `fa`, `fr`, `id`, `pt-BR`, `ru`, `tr`, `zh-CN`, `zh-TW`.

**Fallback:** requested locale → **`en`**. Missing keys never fall through to Russian.

| Method | Path | Meaning |
|--------|------|---------|
| GET | `/v1/controlplane/protocols?lang=` | Список протоколов: tag, title, description, status, `invariant_tags` |
| GET | `/v1/controlplane/protocols/{tag}?lang=` | Протокол + краткие инварианты (scores, demux_hints, traits) |
| GET | `/v1/controlplane/presets?lang=&protocol=` | Список инвариантов; filter по протоколу |
| GET | `/v1/controlplane/presets/{tag}?lang=` | Полный инвариант + templates + `protocol_meta` + `params_schema` |

`params_schema` (v2): каждое поле уже с `title`/`description`/`help`/`required_guide` на выбранном языке. Клиент не локализует серверные инструкции.

`{tag}` принимает **canonical** (`vless_reality`) или **alias** (`vless-reality-tcp`).

Совместимость list: `name` = canonical tag; также `tag`, `short_name`, `scores`, `demux_hints`, `aliases`, `custom_preset`.

### Example

```bash
curl -fsS -H "Authorization: Bearer $TOKEN" \
  "$BASE/v1/controlplane/protocols?lang=ru"

curl -fsS -H "Authorization: Bearer $TOKEN" \
  -H "Accept-Language: en-US,en;q=0.9" \
  "$BASE/v1/controlplane/presets/vless_reality"

curl -fsS -H "Authorization: Bearer $TOKEN" \
  -H "X-Lang: zh-CN" \
  "$BASE/v1/controlplane/presets?protocol=trojan"
```
