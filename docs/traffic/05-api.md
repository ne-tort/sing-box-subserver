# 05 — API (`/v1/traffic`)

Auth: agent Bearer. Absent without `with_traffic` → routes not registered (`404`).

| Method | Path | Meaning |
|--------|------|---------|
| GET | `/v1/traffic/status` | enabled, last_flush_at, retention_days, flush_interval_sec |
| GET | `/v1/traffic/subjects` | registered + observed subjects |
| GET | `/v1/traffic/stats` | query: `subject`, `series_type`, `key`, `since` (RFC3339), `granularity` |
| GET | `/v1/traffic/onlines` | keys with upload activity in last flush interval |
| PUT | `/v1/traffic/limits` | body `{ "limits": { "<dataplane_key>": { "up_bytes_per_sec", "down_bytes_per_sec" } } }` |

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
