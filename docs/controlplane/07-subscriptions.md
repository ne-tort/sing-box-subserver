# 07 — Subscriptions (controlplane)

## Goal

Give each local user a **stable URL** that returns client-facing **outbounds** (and optional
WireGuard `endpoints`) derived from protocol preset templates + that user's credentials +
`public_host`.

This is **not** a full runnable sing-box config and **not** the agent subscribe/pull feature
(`POST /v1/subscribe`).

## Merge contract (clients)

`GET /v1/sub/{token}` returns a **fragment**. Clients must wrap it:

```json
{
  "log": { "level": "info" },
  "inbounds": [{ "type": "mixed", "listen": "127.0.0.1", "listen_port": 1080 }],
  "outbounds": [ /* from subscription */ , { "type": "direct", "tag": "direct" }],
  "route": { "final": "<chosen outbound tag>" }
}
```

Optional: merge operator DNS/route from `GET /v1/controlplane/config/dns|route` when building
an operator-side client. End-user mobile apps typically only need outbounds + local mixed inbound.

**Envelope:** subscription body is **raw JSON** (no `{"ok":true,"data":…}` wrapper used by
management API).

**Defaults for official sing-box:** prefer `?variant=flow-none` (or rely on preset
`default_user_variants`). `xtls-rprx-vision-udp443` is lx-only and is **not** a subscription
default.

Scenarios: [scenarios/06-client-remote-e2e.md](scenarios/06-client-remote-e2e.md) ·
API module: [api/05-users-subscription.md](api/05-users-subscription.md).

## URL

```
GET /v1/sub/{token}
```

Selection discovery (management API, Bearer required):

```
GET /v1/controlplane/subscription-tags?active_only=true
GET /v1/controlplane/sets/{name}/subscription-tags
```

| Property | Value |
|----------|-------|
| Auth | Path `token` == user `sub_token` (constant-time compare) |
| Agent Bearer | **Not** required; **not** accepted as substitute |
| TLS (management) | Follows management listener |
| TLS (outbounds) | Self-signed profile → `tls.insecure: true`; ACME → verify. Reality → sticky assignment + `utls` |

### Query parameters

| Param | Meaning |
|-------|---------|
| (none) | Outbounds for **union** of presets across all **active** sets; if none active → `409` `cp_no_active_set` |
| `set` | Limit to one active set name |
| `preset` | Repeatable or comma-separated; filter preset names |
| `flow` | VLESS flow: `none`, `xtls-rprx-vision`, `xtls-rprx-vision-udp443` (alias `udp-vision`) |
| `network` | VLESS outbound `network` override: `tcp` / `udp` (variant path only) |
| `variant` | Logical variant names: `flow-none`, `flow-xtls-rprx-vision`, `flow-udp-vision` |
| `tag` | Exact match against discovery `subscription_tags` tokens (often prefixed) |
| `profile` | Client profile names: `pkt-none`, `pkt-xudp`, … |
| `strict_filters` | Unknown filter value → `400` `cp_invalid_sub_filter`. Default `false` (valid-but-non-matching → **200 + empty `outbounds`**) |
| `format` | Default `sing-box-json` |

### Query map (discovery → URL)

Discovery returns prefixed QueryTags. Use the **matching query param**, not the raw prefix in `tag`:

| Discovery token example | Use |
|-------------------------|-----|
| `variant:flow-none` | `?variant=flow-none` (or `?tag=variant:flow-none`) |
| `flow:none` | `?flow=none` |
| `profile:pkt-xudp` | `?profile=pkt-xudp` |
| `tag:flow-none` | `?tag=tag:flow-none` |

Putting `?tag=flow-none` when the catalog only has `tag:flow-none` yields **200 + empty**
unless `strict_filters=true` (and even then only unknown catalog values error — non-matching
valid combos stay empty 200). Check `meta.matched` when present.

## Response body

Content-Type: `application/json`

```json
{
  "outbounds": [ { "type": "vless", "tag": "cp-out-…", "server": "vpn.example.com", "server_port": 443 } ],
  "meta": { "matched": 1 }
}
```

`endpoints` may appear when WG hub is enabled. Outbounds sorted by `tag`.

Reality outbounds get `tls.server_name` / `reality.public_key` / `short_id` from the sticky
assignment (not from template `{{server}}` alone).

## Responses

| Code | Meaning |
|------|---------|
| 200 | Body as above (`outbounds` may be `[]` when filters match nothing) |
| 404 | Unknown token |
| 403 | Ineligible user (`cp_user_ineligible`) |
| 400 | Invalid filter with `strict_filters=true` (`cp_invalid_sub_filter`) |
| 409 | No active sets (`cp_no_active_set`) |
| 405 | Method not GET |

## Create-user URL fields

| Field | Example |
|-------|---------|
| `subscription_path` | `/v1/sub/{token}` |
| `subscription_url` | `https://vpn.example.com:8080/v1/sub/{token}` |

Host from `controlplane.public_host` (required for real clients). If unset, falls back to
request `Host` (often loopback — **misconfigured** for mobile). Port from
`controlplane.public_port` else management listen (`80`/`443` omitted).

## Rotation

`POST .../users/{id}/rotate-token` → old URL dies immediately.
