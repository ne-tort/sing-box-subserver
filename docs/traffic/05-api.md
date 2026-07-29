# 05 — API (`/v1/traffic`)

Auth: agent Bearer. Absent without `with_traffic` → routes not registered (`404`).

| Method | Path | Meaning |
|--------|------|---------|
| GET | `/v1/traffic/status` | enabled, last_flush_at, retention_days, flush_interval_sec |
| GET | `/v1/traffic/subjects` | registered + observed subjects |
| GET | `/v1/traffic/stats` | query: `subject`, `series_type`, `key`, `since` (RFC3339) |
| GET | `/v1/traffic/onlines` | keys with up\|down activity in last flush interval |
| GET | `/v1/traffic/limits` | `{ controlplane, manual, effective }` shaping layers |
| PUT | `/v1/traffic/limits` | replaces **manual** layer only: `{ "limits": { "<dataplane_key>": { "up_bytes_per_sec", "down_bytes_per_sec" } } }` — survives CP rematerialize |
| POST | `/v1/traffic/inject` | **lab only** (`traffic.allow_inject: true`): `{ "user"?, "inbound"?, "up", "down" }` then flush |

`GET /v1/traffic/stats?subject=cp:user:…` returns only that subject's series + dataplane_user keys.

Shaping layers: controlplane `speed_*` → `controlplane` map; PUT → `manual` map; **effective** = CP ∪ manual (manual wins on conflict).

**Keys are raw `metadata.User` strings.** In controlplane + VLESS, live traffic often uses variant keys (`alice-flow-none`). CP `speed_*` and manual PUT of a **bare** display name are expanded onto variant keys; PUT of an exact variant key is not fanned out.

### Status example

```json
{
  "ok": true,
  "data": {
    "enabled": true,
    "flush_interval_sec": 10,
    "retention_days": 30,
    "last_flush_at": "2026-07-29T12:00:00Z",
    "subjects_registered": 3
  }
}
```

Controlplane bridge uses in-process Service calls for hot path; HTTP is for ops.
