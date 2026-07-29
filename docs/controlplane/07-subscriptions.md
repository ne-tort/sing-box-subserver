# 07 — Subscriptions (controlplane)

## Goal

Give each local user a **stable URL** that returns client-facing outbounds derived from
protocol preset outbound templates + that user's credentials + public server address.

This is **not** the agent subscribe/pull feature (`POST /v1/subscribe`).

## URL

```
GET /v1/sub/{token}
```

Selection discovery helper (management API):

```
GET /v1/controlplane/sets/{name}/subscription-tags
```

This endpoint lets UI/operators discover which `variant/tag/profile` values can be
combined in subscription query parameters for a specific inbound set.

| Property | Value |
|----------|-------|
| Auth | Path `token` == user `sub_token` (constant-time compare) |
| Agent Bearer | **Not** required; **not** accepted as substitute |
| TLS (management) | Follows management listener (public bind should use TLS — root NFR-5) |
| TLS (outbounds) | From controlplane TLS profile for regular TLS presets (`self_signed` → `tls.insecure: true`; `acme_*` → verify). Reality presets use sticky Reality assignment (`server_name`, `reality.public_key`, `short_id`) + `utls.enabled=true` |

### Query parameters

| Param | Meaning |
|-------|---------|
| (none) | Outbounds for **union** of presets across all **active** sets; if none active → `409` `cp_no_active_set` |
| `set` | Limit to one active set name |
| `preset` | Repeatable or comma-separated; filter preset names (must be ⊆ selected sets) |
| `flow` | Optional VLESS symmetric flow filter. Accepted values: `none` (empty flow), `xtls-rprx-vision`, `xtls-rprx-vision-udp443` (alias `udp-vision`). Repeatable or comma-separated. If omitted → all enabled flow variants. |
| `network` | Optional outbound network override for VLESS. Accepted values: `tcp`, `udp`. If omitted → default sing-box behavior (both). |
| `variant` | Optional repeatable/comma-separated logical variant filter (`flow-none`, `flow-xtls-rprx-vision`, `flow-udp-vision`). |
| `tag` | Optional repeatable/comma-separated binding/variant query tag filter. |
| `profile` | Optional repeatable/comma-separated outbound-only client profile filter. |
| `format` | Default `sing-box-json`. No other formats in v1 (reserved). |

## Default body (`format=sing-box-json`)

Content-Type: `application/json`

```json
{
  "outbounds": [
    {
      "type": "vless",
      "tag": "cp-out-mixed-443-vless-tcp",
      "server": "203.0.113.10",
      "server_port": 443
    }
  ]
}
```

Each outbound = substitute(`outbound_template`, user.creds[preset], public_host, set listen_port),
then optional variant/profile expansion is applied.
For `vless-reality-tcp`, controlplane additionally injects:
- `tls.server_name` from inbound's sticky Reality assignment,
- `tls.utls.enabled=true`,
- `tls.reality.enabled=true`,
- `tls.reality.public_key` / `tls.reality.short_id`.

## Responses

| Code | Meaning |
|------|---------|
| 200 | Body as above |
| 404 | Unknown token |
| 403 | User exists but ineligible (`cp_user_ineligible`) |
| 409 | No active sets (`cp_no_active_set`) |
| 405 | Method not GET |

## Create-user URL fields

| Field | Example |
|-------|---------|
| `subscription_path` | `/v1/sub/{token}` |
| `subscription_url` | `https://vpn.example.com:8080/v1/sub/{token}` |

Host from `controlplane.public_host` when set, else request `Host`. Port from management listen.

## Rotation

`POST .../users/{id}/rotate-token` → old URL dies immediately.
