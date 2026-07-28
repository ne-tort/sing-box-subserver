# 05 — API (controlplane)

Base: `/v1/controlplane`. JSON envelopes match root [05-api](../05-api.md).

**Auth:** `Authorization: Bearer <agent-token>` on all `/v1/controlplane/*` routes.

**Absent build tag:** routes not registered → `404`.

Public subscription: [07-subscriptions](07-subscriptions.md) (`GET /v1/sub/{token}`, **no** agent Bearer).

---

## Status

| Method | Path | Meaning |
|--------|------|---------|
| GET | `/v1/controlplane/status` | Module status |

```json
{
  "ok": true,
  "data": {
    "config_mode": "controlplane",
    "active_sets": ["mixed-443", "hy2-8443"],
    "users_total": 10,
    "users_eligible": 8,
    "presets_count": 12,
    "sets_count": 2,
    "last_materialize_sha256": "...",
    "last_materialize_at": "..."
  }
}
```

`config_mode` is the **global** owner value from `configowner` (may be `direct` / `subscribed` / `idle` even if CP data exists).

---

## Users

| Method | Path | Meaning |
|--------|------|---------|
| GET | `/v1/controlplane/users` | List (secrets redacted: no raw `sub_token` / password fields; show `has_token`, preset names present) |
| POST | `/v1/controlplane/users` | Create `{name, enabled?, expires_at?, traffic_limit_bytes?, …}` → returns user **with** `sub_token` once + `subscription_path` + `subscription_url` |
| GET | `/v1/controlplane/users/{id}` | Get (redacted; `?secrets=1` may return secrets for ops — default off) |
| PATCH | `/v1/controlplane/users/{id}` | Update mutable fields (not bulk creds replace) |
| DELETE | `/v1/controlplane/users/{id}` | Delete; triggers rematerialize if any set active |
| POST | `/v1/controlplane/users/{id}/rotate-token` | New `sub_token`; returns token once + URLs |
| POST | `/v1/controlplane/users/{id}/rotate-creds` | Regenerate all preset creds (forces rematerialize) |

`subscription_url` = `{scheme}://{public_host|Host}:{mgmt_port}/v1/sub/{token}`.

Errors: `400` validation, `409` name conflict, `404` missing.

---

## TLS profile

| Method | Path | Meaning |
|--------|------|---------|
| GET | `/v1/controlplane/tls` | Current profile + `material_status` (DNS tokens redacted) |
| PUT | `/v1/controlplane/tls` | Upsert `{mode, self_signed?, acme?}`; ensures PEM for self_signed; rematerialize if active |
| POST | `/v1/controlplane/tls/regenerate` | Force reissue self-signed PEM (`400` if mode ≠ `self_signed`) |

Modes: `self_signed` \| `acme_domain` \| `acme_ip`. See [11-tls](11-tls.md).

---

## Presets

| Method | Path | Meaning |
|--------|------|---------|
| GET | `/v1/controlplane/presets` | List name, protocol, traits, description |
| GET | `/v1/controlplane/presets/{name}` | Full preset including templates |

Read-only in v1. `404` if unknown.

---

## Inbound sets

| Method | Path | Meaning |
|--------|------|---------|
| GET | `/v1/controlplane/sets` | List (include `active` bool) |
| POST | `/v1/controlplane/sets` | Create set (`demux_template` optional; port unique) |
| GET | `/v1/controlplane/sets/{name}` | Get |
| PUT | `/v1/controlplane/sets/{name}` | Replace definition (`409` port conflict; if active → rematerialize) |
| DELETE | `/v1/controlplane/sets/{name}` | Delete (`409` if active — deactivate first) |
| POST | `/v1/controlplane/sets/{name}/activate` | Add to `active_sets`; Claim(controlplane); materialize + Apply |
| POST | `/v1/controlplane/sets/{name}/deactivate` | Remove from `active_sets`; rematerialize or Claim(idle) if empty |

### Activate responses

| Code | Meaning |
|------|---------|
| 200 | Applied / noop same SHA |
| 404 | Unknown set |
| 409 | Port conflict / apply in progress / owner race |
| 422 | Missing build tag (e.g. demux), invalid template after substitute |
| 400 | Empty presets / unknown preset ref / demux null but `len(presets)!=1` |

Activate side effects: cancel subscribe; `config_mode=controlplane`.

Deactivate last active set: `config_mode=idle`; does not delete last-good.

---

## Error codes (module)

| code | When |
|------|------|
| `cp_disabled` | Binary without module (if stub returns explicit 404 body) |
| `cp_port_conflict` | Set listen_port collision |
| `cp_not_active` | Deactivate when set not in `active_sets` |
| `cp_no_active_set` | Sub fetch with no active sets |
| `cp_user_ineligible` | Sub fetch for expired/limited user |
| `cp_unknown_preset` | Set references missing preset |
| `unsupported_build_tag` | Same family as root validate 422 |

---

## Interaction with root routes

Documented in root [05-api `config_mode`](../05-api.md#config_mode-normative). CP does not expose a second PUT config; materialize is the only CP writer path.

See also [10-scenarios](10-scenarios.md).
