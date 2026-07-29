# 05 — Quotas and expiry (controlplane)

These controls live on **controlplane users**. Traffic module meters bytes;
controlplane enforces eligibility (omit from materialize + reject `/v1/sub/{token}`).

## Traffic quota

| Field | Role |
|-------|------|
| `traffic_limit_bytes` | Hard cap on **up+down** (`null` / omit = unlimited) |
| `traffic_used_bytes` | Cumulative used (bridge sync + admin PATCH) |
| `traffic_reset_at` / `traffic_reset_period_sec` | Periodic reset → used=0 |

When `used >= limit`, user becomes **ineligible**:

1. Live sessions kicked (`CloseConnByUsers` on dataplane keys).
2. Rematerialize omits credentials.
3. Subscription fetch returns **403**.

```bash
# set 1 GiB quota
curl -fsS -X PATCH -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"traffic_limit_bytes":1073741824}' \
  "$BASE/v1/controlplane/users/$USER_ID"

# admin correction / restore eligibility
curl -fsS -X PATCH -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"traffic_used_bytes":0}' \
  "$BASE/v1/controlplane/users/$USER_ID"
```

Bridge maps PATCH into the traffic store (discard live + absolute) so the next
flush cannot resurrect counters.

## Expiry

```bash
curl -fsS -X PATCH -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"expires_at":"2026-01-01T00:00:00Z"}' \
  "$BASE/v1/controlplane/users/$USER_ID"

# clear
curl -fsS -X PATCH -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"expires_at":null}' \
  "$BASE/v1/controlplane/users/$USER_ID"
```

Ticker (`controlplane.expiry_tick_sec`) rematerializes when eligibility flips.

## Disable

`enabled: false` — same ineligible path (kick + omit + 403 sub).

## Subscribe / static

No built-in traffic quota or expiry in the traffic module. Enforce on the panel
or regenerate the pulled/static JSON without the user.
