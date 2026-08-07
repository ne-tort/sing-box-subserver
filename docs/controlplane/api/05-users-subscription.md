# 05 — Users and subscription

Full public contract: [../07-subscriptions.md](../07-subscriptions.md).

## Create user

```http
POST /v1/controlplane/users
Authorization: Bearer …
Content-Type: application/json

{ "name": "alice", "enabled": true }
```

Returns (once): `sub_token`, `subscription_path`, `subscription_url`, generated `creds`.

Cross-node sync / import / export / metrics: [../14-users-sync.md](../14-users-sync.md).

HTTP status may be **200** (create) while sets use **201** — clients should check `ok` + body, not only status.

If no active set / WG hub: later `GET /v1/sub/{token}` → `409` `cp_no_active_set`. Wizard order: **ready first**, then user, then sub.

**Tests:** `test_03_presets_install.py`, `test_04_demux_groups.py`

## Fetch subscription

```http
GET /v1/sub/{token}
GET /v1/sub/{token}?set=e2e-demux&variant=flow-none
```

Body example:

```json
{
  "outbounds": [ { "type": "vless", "tag": "cp-out-…", "server": "…", "server_port": 443 } ],
  "meta": { "matched": 1 }
}
```

| Rule | Detail |
|------|--------|
| Defaults | Empty enabled variants → **SubscriptionDefault** only (`flow-none` for Reality) |
| Official sing-box | Avoid `xtls-rprx-vision-udp443` (lx-only) |
| Empty filters | HTTP 200 + `outbounds: []` + `meta.matched: 0` unless `strict_filters=true` |
| Query map | Discovery `variant:flow-none` → `?variant=flow-none` (**not** `?tag=flow-none`) |

Discover filters:

```http
GET /v1/controlplane/subscription-tags?active_only=true
```

**Tests:**
- unfiltered demux sub has ≥2 outbounds, no `udp443` — `test_04_demux_groups.py`
- remote docker client wraps fragment — `test_07_client_remote.py`

**Scenario:** [06-client-remote-e2e](../scenarios/06-client-remote-e2e.md)
