# 02 — API

Auth: agent Bearer. Query `lang` (default / fallback: `ru`; также `ru-RU`→`ru`).

| Method | Path | Meaning |
|--------|------|---------|
| GET | `/v1/controlplane/protocols?lang=` | Список протоколов: tag, title, description, status, `invariant_tags` |
| GET | `/v1/controlplane/protocols/{tag}?lang=` | Протокол + краткие инварианты (scores, demux_hints, traits) |
| GET | `/v1/controlplane/presets?lang=&protocol=` | Список инвариантов (со templates); filter по протоколу |
| GET | `/v1/controlplane/presets/{tag}?lang=` | Полный инвариант + templates + `protocol_meta` |

`{tag}` принимает **canonical** (`vless_reality`) или **alias** (`vless-reality-tcp`).

Совместимость list: поле `name` = canonical tag; добавлены `tag`, `short_name`, `scores`, `demux_hints`, `aliases`.

### Example

```bash
curl -fsS -H "Authorization: Bearer $TOKEN" \
  "$BASE/v1/controlplane/protocols?lang=ru"

curl -fsS -H "Authorization: Bearer $TOKEN" \
  "$BASE/v1/controlplane/presets/vless_reality?lang=ru"
```
