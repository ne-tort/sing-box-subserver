# 07 — Subscriptions (controlplane)

## Goal

Give each local user a **stable URL** that returns client-facing outbounds derived from
protocol preset outbound templates + that user's credentials + public server address.

This is **not** the agent subscribe/pull feature (`POST /v1/subscribe`).

## URL

```
GET /v1/sub/{token}
```

| Property | Value |
|----------|-------|
| Auth | Path `token` == user `sub_token` (constant-time compare) |
| Agent Bearer | **Not** required; **not** accepted as substitute |
| TLS (management) | Follows management listener (public bind should use TLS — root NFR-5) |
| TLS (outbounds) | From controlplane TLS profile: `self_signed` → `tls.insecure: true`; `acme_*` → verify + `server_name` from ACME domain/IP |

### Query parameters

| Param | Meaning |
|-------|---------|
| (none) | Outbounds for **union** of presets across all **active** sets; if none active → `409` `cp_no_active_set` |
| `set` | Limit to one active set name |
| `preset` | Repeatable or comma-separated; filter preset names (must be ⊆ selected sets) |
| `format` | Default `sing-box-json`. No other formats in v1 (reserved). |

## Default body (`format=sing-box-json`)

Content-Type: `application/json`

```json
{
  "outbounds": [
    {
      "type": "vless",
      "tag": "cp-out-mixed-443-vless-reality-tcp",
      "server": "203.0.113.10",
      "server_port": 443
    }
  ]
}
```

Each outbound = substitute(`outbound_template`, user.creds[preset], public_host, set listen_port).

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
