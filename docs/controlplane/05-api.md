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
| GET | `/v1/controlplane/status/details` | Extended status with active set breakdown, owner transition log, supervisor snapshot |

```json
{
  "ok": true,
  "data": {
    "config_mode": "controlplane",
    "active_sets": ["mixed-443", "hy2-8443"],
    "users_total": 10,
    "users_eligible": 8,
    "presets_count": 13,
    "demux_recipes_count": 9,
    "sets_count": 2,
    "demux_in_binary": true,
    "tls_mode": "self_signed",
    "tls_material_status": { "ready": true, "active_material": "self_signed_pem" },
    "materialize_status": {
      "last_success_at": "2026-07-29T10:00:00Z",
      "last_attempt_at": "2026-07-29T10:00:00Z",
      "last_apply_noop": false
    },
    "owner_transitions_recent": [
      { "from": "idle", "to": "controlplane", "reason": "activate", "trigger": "s1", "success": true }
    ],
    "ownership_health": {
      "status": "ok",
      "issues": []
    },
    "reality": {
      "using_user_overrides": false,
      "effective_profiles": [{ "sni": "www.microsoft.com", "handshake_server": "www.microsoft.com", "handshake_port": 443 }],
      "active_assignments": [{ "inbound_key": "rset/vless-reality-tcp", "sni": "www.microsoft.com" }]
    },
    "last_materialize_sha256": "...",
    "last_materialize_at": "..."
  }
}
```

`GET /status/details` additionally returns:
- `owner_transitions` (full recent log, up to 20 entries),
- `active_set_details` (per active set: bindings, inbound tags, enabled variants/profiles),
- `supervisor` snapshot (`state`, `revision`, `content_sha256`, `last_apply`).

`ownership_health` summarizes config-owner vs active-set consistency:
- `status`: `ok` | `degraded`,
- `issues`: e.g. `controlplane_mode_without_active_sets`, `active_sets_without_controlplane_ownership`, `last_materialize_failed`, `never_materialized`.

On bootstrap, if `config_mode=controlplane` but `active_sets` is empty (orphan after crash), ownership is rolled back to `idle` with reason `boot_reconcile_orphan`.
If `config_mode` is not `controlplane` but `active_sets` is non-empty, stale entries are cleared on bootstrap.

`config_mode` is the **global** owner value from `configowner` (may be `direct` / `subscribed` / `idle` even if CP data exists).
`demux_in_binary` reflects compile-time `with_demux`. `tls_material_status` mirrors `GET /tls` readiness (ops polling).
`reality` reflects validated profile pool and active Reality inbound assignments.

---

## Users

| Method | Path | Meaning |
|--------|------|---------|
| GET | `/v1/controlplane/users` | List (secrets redacted: no raw `sub_token` / password fields; show `has_token`, preset names present) |
| POST | `/v1/controlplane/users` | Create `{name, enabled?, expires_at?, traffic_limit_bytes?, traffic_reset_at?, traffic_reset_period_sec?, speed_up_bytes_per_sec?, speed_down_bytes_per_sec?, creds?, …}` → returns user **with** `sub_token` + `creds` once + `subscription_path` + `subscription_url` |
| GET | `/v1/controlplane/users/{id}` | Get (redacted; `?secrets=1` returns `sub_token`, `creds`, `subscription_path`, `subscription_url`) |
| PATCH | `/v1/controlplane/users/{id}` | Update mutable fields including traffic hooks and `speed_*` shaping (not bulk creds replace); rename unique → else `409` |
| DELETE | `/v1/controlplane/users/{id}` | Delete; triggers rematerialize if any set active |
| PUT | `/v1/controlplane/users/{id}/creds` | Merge operator `creds` into user; auto-fill missing presets/fields; rematerialize; returns secrets once |
| POST | `/v1/controlplane/users/{id}/rotate-token` | New `sub_token`; returns token once + URLs |
| POST | `/v1/controlplane/users/{id}/rotate-creds` | Wipe + regenerate all preset creds (forces rematerialize) |

`subscription_url` = `{scheme}://{public_host|Host}:{mgmt_port}/v1/sub/{token}`.

Optional `creds` on create / PUT body:

```json
{
  "creds": {
    "vless-tcp": { "uuid": "11111111-2222-3333-4444-555555555555" },
    "trojan-tcp": { "password": "operator-secret" },
    "socks": { "username": "u1", "password": "p1" }
  }
}
```

Merge is per-preset field map (supplied fields overwrite; unspecified fields kept). Then field-level auto-fill completes the catalog. PATCH never accepts bulk creds.

Errors: `400` validation / `cp_invalid_creds` (unknown preset/field, empty or non-string values), `409` name conflict, `404` missing.

---

## TLS profile

| Method | Path | Meaning |
|--------|------|---------|
| GET | `/v1/controlplane/tls` | Current profile + `material_status` (DNS tokens redacted) |
| PUT | `/v1/controlplane/tls` | Upsert `{mode, self_signed?, acme?}`; ensures PEM for self_signed; rematerialize if active |
| POST | `/v1/controlplane/tls/regenerate` | Force reissue self-signed PEM (`400` if mode ≠ `self_signed`) |

Modes: `self_signed` \| `acme_domain` \| `acme_ip`. See [11-tls](11-tls.md).

---

## Reality profiles

| Method | Path | Meaning |
|--------|------|---------|
| GET | `/v1/controlplane/reality` | Returns `user_overrides`, `effective_profiles`, `using_user_overrides`, `active_assignments` |
| PUT | `/v1/controlplane/reality` | Replaces user profile list; validates profiles; if validated user pool is empty, user overrides are cleared and defaults are used |

PUT body:

```json
{
  "profiles": [
    { "sni": "www.microsoft.com" },
    { "sni": "localhost", "handshake_server": "localhost", "handshake_port": 443 }
  ]
}
```

Rules:
- `sni` required, domain only.
- `handshake_server` default = `sni`.
- `handshake_port` default = `443`.
- Validation filters unusable profiles (DNS/TCP/CDN heuristics).

---

## Presets

| Method | Path | Meaning |
|--------|------|---------|
| GET | `/v1/controlplane/presets` | List name, protocol, traits, description |
| GET | `/v1/controlplane/presets/{name}` | Full preset including templates |

Read-only in v1. `404` if unknown.

---

## Demux recipes

Named demux_template skeletons (separate from protocol presets). Operators copy `demux_template` + `required_presets` into a set.

| Method | Path | Meaning |
|--------|------|---------|
| GET | `/v1/controlplane/demux-recipes` | List name, description, required_presets, suggested_port |
| GET | `/v1/controlplane/demux-recipes/{name}` | Full recipe including `demux_template` |

`404` if unknown. Creating a set with empty `"match":{"tls":{}}` → `400` `cp_invalid_demux`.

---

## Inbound sets

| Method | Path | Meaning |
|--------|------|---------|
| GET | `/v1/controlplane/sets` | List (include `active` bool) |
| POST | `/v1/controlplane/sets` | Create set (`demux_template` optional; port unique) |
| GET | `/v1/controlplane/sets/{name}` | Get |
| GET | `/v1/controlplane/sets/{name}/subscription-tags` | List available subscription selection tags/variants/profiles per inbound binding |
| GET | `/v1/controlplane/subscription-tags` | Aggregate subscription-tags across sets (`?active_only=true` default; `false` lists all sets) |
| PUT | `/v1/controlplane/sets/{name}` | Replace definition (`409` port conflict; if active → rematerialize) |
| DELETE | `/v1/controlplane/sets/{name}` | Delete (`409` if active — deactivate first) |
| POST | `/v1/controlplane/sets/{name}/activate` | Add to `active_sets`; Claim(controlplane); materialize + Apply |
| POST | `/v1/controlplane/sets/{name}/deactivate` | Remove from `active_sets` (idempotent if already inactive); rematerialize or Claim(idle) if empty |

### Activate responses

| Code | Meaning |
|------|---------|
| 200 | Applied / noop same SHA |
| 404 | Unknown set |
| 409 | Port conflict / Claim persist failure / apply in progress |
| 422 | Missing build tag (e.g. demux → `unsupported_build_tag`), invalid template after substitute |
| 400 | Empty presets / unknown preset ref / demux null but `len(presets)!=1` |

`POST/PUT /sets` compatibility:
- old shape: `presets[]` only;
- new optional shape: `bindings[]` where each binding has:
  - `preset` (required),
  - `subscription_tags[]` (optional),
  - `enabled_user_variants[]` (optional),
  - `enabled_client_profiles[]` (optional),
  - `credential_instance_policy` (optional).
- if only `presets[]` is sent, server lazily normalizes to default bindings.

`GET /sets/{name}/subscription-tags` response model:
- returns per-binding `inbound_tag`, `preset`, `protocol`,
- returns discoverable `subscription_tags`, `enabled_user_variants`, `enabled_client_profiles`,
- this endpoint is for subscription UI/client selection only; server inbounds already keep all required user entries by variant policy.

`GET /subscription-tags` returns `{ "sets": [ { "set", "active", "bindings": [...] }, ... ] }`.
With `active_only=true` (default), only currently active sets are included.

Activate side effects: cancel subscribe; `config_mode=controlplane`. On **first** activate failure after Claim, ownership rolls back to `idle` and `active_sets` are restored.

Deactivate last active set: `config_mode=idle`; does not delete last-good.

---

## Error codes (module)

| code | When |
|------|------|
| `cp_disabled` | Binary without module (if stub returns explicit 404 body) |
| `cp_port_conflict` | Set listen_port collision |
| `cp_not_active` | _(removed)_ deactivate is idempotent |
| `cp_no_active_set` | Sub fetch with no active sets |
| `cp_user_ineligible` | Sub fetch for expired/limited user |
| `cp_unknown_preset` | Set references missing preset |
| `cp_invalid_creds` | Manual creds: unknown preset/field or empty/non-string value |
| `cp_claim_failed` | `configowner.Claim(controlplane)` failed during activate |
| `cp_materialize_failed` | Materialize build/validate failed before Apply |
| `cp_apply_failed` | Supervisor Apply failed after materialize |
| `cp_invalid_sub_filter` | Subscription query filter unknown/disallowed when `strict_filters=true` |
| `unsupported_build_tag` | Same family as root validate 422 |

---

## Interaction with root routes

Documented in root [05-api `config_mode`](../05-api.md#config_mode-normative). CP does not expose a second PUT config; materialize is the only CP writer path.

See also [10-scenarios](10-scenarios.md).
