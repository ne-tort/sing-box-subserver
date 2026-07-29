# 03 — Accounting (tracking)

## Series

Live trackers accumulate bytes; every `flush_interval_sec` they flush into:

| `series_type` | `key` | Meaning |
|---------------|-------|---------|
| `dataplane_user` | `metadata.User` | Per-user on the wire |
| `inbound` | inbound tag | Aggregate per inbound |
| `outbound` | outbound tag | Aggregate per outbound |
| `subject` | subject id (e.g. `cp:user:…`) | Sum of subject's dataplane keys |

**Up** = bytes from client (upload). **Down** = bytes to client (download).

## Subjects

- **Controlplane**: bridge registers `cp:user:{id}` with one or more dataplane keys
  (display name + VLESS variant suffixes).
- **Subscribe / static**: consumer `auto` discovers observed users and inbounds after flush.
  Cleared while a controlplane manifest is active (avoids double subjects).

```bash
curl -fsS -H "Authorization: Bearer $TOKEN" \
  "$BASE/v1/traffic/subjects"
```

## Stats

```bash
# one CP user
curl -fsS -H "Authorization: Bearer $TOKEN" \
  "$BASE/v1/traffic/stats?subject=cp:user:$USER_ID"

# raw dataplane user
curl -fsS -H "Authorization: Bearer $TOKEN" \
  "$BASE/v1/traffic/stats?series_type=dataplane_user&key=alice"
```

Optional `since=` (RFC3339) filters JSONL series.

## Onlines

`GET /v1/traffic/onlines` — dataplane keys with **non-zero up|down in the last flush window**,
not a live session list. Active TCP sessions: `conns_active` on `/v1/traffic/status`.

## Inject (lab only)

Requires `traffic.allow_inject: true`.

```bash
curl -fsS -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"user":"alice","up":1000,"down":2000}' \
  "$BASE/v1/traffic/inject"
```

Also accepts `"inbound":"<tag>"`. Always followed by an immediate flush.
