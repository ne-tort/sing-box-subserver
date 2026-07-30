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
    "presets_count": 15,
    "demux_recipes_count": 9,
    "demux_groups_count": 18,
    "sets_count": 2,
    "demux_in_binary": true,
    "cert_manager": { "enabled": false, "domains": [] },
    "tls_material_status": { "ready": true, "active_material": "self_signed_pem" },
    "ready": {
      "ok": true,
      "box_up": true,
      "supervisor_state": "Running",
      "active_sets": true,
      "reasons": [],
      "poll": "GET /v1/controlplane/status → ready.ok == true"
    },
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

`ready.ok` is the **wizard poll signal** after `activate` / `from-*` with `activate:true`: ownership healthy, active sets present, last materialize without error, TLS ready (ACME), supervisor `Running` + `box_up`.

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

## WireGuard hub

| Method | Path | Meaning |
|--------|------|---------|
| GET | `/v1/controlplane/wg` | Singleton hub status (no private key) |
| PUT | `/v1/controlplane/wg` | Update `{enabled, profile, subnet, listen_port, system, forward_allow, internet_allow, …}`; Claim(controlplane) when enabling |
| POST | `/v1/controlplane/wg/regenerate-awg` | Rotate AWG2/3 + masquerade params |

Profiles: `wg` (plain), `wg_awg2` (AWG2+masquerade), `wg_awg3` (AWG3+masquerade). Subnet default `10.8.0.0/24`.

## TLS (self-signed) + cert-manager

| Method | Path | Meaning |
|--------|------|---------|
| GET | `/v1/controlplane/tls` | Self-signed profile + `material_status` |
| PUT | `/v1/controlplane/tls` | Upsert `{self_signed}`; ensures PEM; rematerialize if active |
| POST | `/v1/controlplane/tls/regenerate` | Force reissue self-signed PEM |
| GET | `/v1/controlplane/cert-manager` | Domains, provider settings, per-domain status |
| PUT | `/v1/controlplane/cert-manager` | Replace ACME settings + domains list |

TLS inbounds may set optional `bindings[].params.sni` (must ∈ cert-manager domains). See [11-tls](11-tls.md).

## Config fragments (dns / route)

| Method | Path | Meaning |
|--------|------|---------|
| GET/PUT | `/v1/controlplane/config/dns` | Raw sing-box `dns` object (default: local server); PUT → `rematerialized` if CP owns dataplane |
| GET/PUT | `/v1/controlplane/config/route` | Raw sing-box `route` object (default: `final=direct`, `rules=[]`); same rematerialize contract |

---

## Reality profiles

| Method | Path | Meaning |
|--------|------|---------|
| GET | `/v1/controlplane/reality` | `user_overrides`, `effective_profiles`, `default_profiles`, `using_user_overrides`, `active_assignments` |
| PUT | `/v1/controlplane/reality` | Replace-all profiles; response includes `accepted` / `rejected[{sni,reason}]` |

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
- Invalid entries are listed in `rejected` and omitted from the stored pool.

---

## Protocols & presets

File catalog under `internal/controlplane/presets/data/`. Operator how-to:
[`docs/guides/controlplane-presets/`](../guides/controlplane-presets/00-index.md).

Query `lang` (default/fallback `ru`).

| Method | Path | Meaning |
|--------|------|---------|
| GET | `/v1/controlplane/protocols?lang=` | Protocol list + `invariant_tags` |
| GET | `/v1/controlplane/protocols/{tag}?lang=` | Protocol meta + invariant summaries |
| GET | `/v1/controlplane/presets?lang=&protocol=` | Invariant list (short; filter by protocol) |
| GET | `/v1/controlplane/presets/{tag}?lang=` | Full invariant + templates + `protocol_meta` |

`{tag}` accepts canonical (`vless_reality`) or legacy alias (`vless-reality-tcp`).
Read-only. `404` if unknown / no templates.

List compat fields: `name` (=tag), `protocol`, `description`, `traits`; plus `tag`, `short_name`, `scores`, `demux_hints`, `aliases`, `cred_fields`, `param_fields`, `networks`, `optional_params.listen_port`.
Full templates remain on `GET /presets/{tag}`.

---

## Demux groups (catalog)

First-class installable demux bundles (modern protocols only). Prefer these over raw `demux-recipes` for client UX.

| Method | Path | Meaning |
|--------|------|---------|
| GET | `/v1/controlplane/demux-groups?lang=` | List groups: tag, scores, slots (+ match tags), `separation_summary` |
| GET | `/v1/controlplane/demux-groups/{tag}?lang=` | Full group + `match_plan` + enriched slots |
| GET | `/v1/controlplane/demux-groups/{tag}/substitutions` | Slot picker: presets + `separation_tags` / `interchange_tags` / `fits_interchange` |
| POST | `/v1/controlplane/sets/from-demux-group` | Install set from group (`group`, `name?`, `listen_port?`, `slot_presets?`, `slot_sni?`, `activate?`) |
| POST | `/v1/controlplane/sets/from-presets` | Batch install single-inbound sets with port policy |

### Match metadata (client)

Derived from slot `role` + `match_hint` (same first-match order as demux rule builder). Capability: `demux_group_match_meta`, vocab: `demux_match_tag_vocab`.

Per slot:

| Field | Meaning |
|-------|---------|
| `separation_tags` | How this slot is distinguished from siblings (`tcp`, `tls`, `sni`, `udp`, `quic`, …) |
| `interchange_tags` | Class shared by substitutes (safe swap within the slot) |
| `match_shape` | Rule shape: `tls.sni` \| `tls.alpn` \| `protocol.quic` \| `protocol.quic+sni` \| `always` |
| `match_priority` | Lower = earlier in demux first-match |

Group-level: `separation_summary` (union), `match_plan` (ordered steps with notes) on GET by tag.

Example `dg_443_triple` plan order: Reality `tls.sni` → TLS `tls.sni` → Hy2 `protocol.quic`.

Substitutions options also expose `looks_like`, `demux_hints`, `fits_interchange` (trait drift guard vs slot class).

### Client install flow

1. `GET /protocols` → pick protocol folder  
2. `GET /presets?protocol=` → pick tags (+ `optional_params.listen_port`)  
3. **or** `GET /demux-groups` → `GET .../substitutions` → choose slot replacements  
4. `POST /sets/from-demux-group` or `/sets/from-presets` with `activate: true`  
   - Response: `activated` is always a **boolean**; `from-presets` also returns `activated_sets: string[]`.  
   - `listen_port` may be omitted → agent auto-picks a free port (prefers 443).  
5. Poll `GET /status` until `ready.ok === true`  
6. Create user → `subscription_url` (+ query filters from `/subscription-tags`)

### Port policy

- Single-inbound sets: at most **one TCP and one UDP** occupant per public `listen_port` (e.g. Reality TCP + Hy2 UDP on `:443` is allowed; two TCP on `:443` → `409 cp_port_conflict`).
- Demux groups occupy networks declared in the group (`tcp`/`udp`) on one public port; members bind **random private** ports `41000–60000` on `127.0.0.1`.
- Demux actions use **`dial` forward** to member ports (not inject).

### `from-demux-group` body

```json
{
  "group": "dg_443_dual",
  "name": "edge-443",
  "listen_port": 443,
  "slot_presets": { "tcp": "vless_ws_reality", "quic": "tuic" },
  "activate": true
}
```

Response includes `set`, `member_ports`, `slot_snis`, `warnings`. Reality slots get unique SNIs aligned with demux match / Reality assignment.

### Client bootstrap & ports

| Method | Path | Meaning |
|--------|------|---------|
| GET | `/v1/controlplane/client/bootstrap?lang=` | Flows + capabilities + counts for mobile/desktop UX |
| GET | `/v1/controlplane/ports/availability?port=` | Free TCP/UDP on a port; `can_demux` |

---

## Demux recipes

Named demux_template skeletons (legacy/manual; separate from demux-groups). Operators copy `demux_template` + `required_presets` into a set.

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
| POST | `/v1/controlplane/sets` | Create set (`201`; `demux_template` optional; port unique) |
| GET | `/v1/controlplane/sets/{name}` | Get (same shape as list item, includes `active`) |
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
  - `credential_instance_policy` (optional),
  - `params` (optional map; **required keys** declared by preset `param_fields`, e.g. carrier SFU `room` URL; demux members may carry `demux_sni`).
- Port uniqueness is **per L4 network** (TCP/UDP), not the whole port number.
- if only `presets[]` is sent, server lazily normalizes to default bindings.
- missing required `params` → `400` `cp_invalid_bindings`.

`GET /sets/{name}/subscription-tags` response model:
- returns per-binding `inbound_tag`, `preset`, `protocol`,
- returns discoverable `subscription_tags`, `enabled_user_variants`, `enabled_client_profiles`,
- returns `params` / `param_fields` (carrier room URL etc.),
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
| `cp_invalid_bindings` | Missing/invalid binding params (e.g. `params.sni`) |
| `cp_invalid_preset` | Preset not allowed in this context (e.g. WG via sets) |
| `cp_invalid_demux` | Demux template invalid |
| `cp_invalid_demux_group` | from-demux-group install request invalid |
| `cp_invalid_slot` | Slot preset not allowed / duplicate across slots |
| `cp_invalid_set` | Generic set validation failure |
| `cp_invalid_config` | DNS/route fragment validation failed |
| `cp_invalid_cert_manager` | Cert-manager settings invalid |
| `cp_materialize_failed` | Materialize build/validate failed before Apply |
| `cp_apply_failed` | Supervisor Apply failed after materialize |
| `cp_invalid_sub_filter` | Subscription query filter unknown/disallowed when `strict_filters=true` |
| `unsupported_build_tag` | Same family as root validate 422 |

`422` on activate / PUT active set / TLS / cert-manager / dns / route / deactivate rematerialize uses `cp_materialize_failed` or `cp_apply_failed` (not the legacy `config_invalid` alias).

### Follow-ups (не блокируют клиентский happy-path)

| Item | Why later |
|------|-----------|
| `materializeErrorCode` эвристика по тексту ошибки | Достаточно для UI; точнее — typed errors из supervisor Apply |
| Reality `PUT` всегда 200 + `accepted`/`rejected` | Клиент обязан смотреть `rejected[]`; смена на 400 при all-rejected — breaking |
| Soft-fail `from-*` + `activate:true` → HTTP 201 + `activated:false` | Уже контракт bootstrap; не превращать в 422 без версии API |
| `peer_secrets` в GET set | Нужны оператору; для mobile UI — не светить без нужды (отдельный redaction flag) |

---

## Interaction with root routes

Documented in root [05-api `config_mode`](../05-api.md#config_mode-normative). CP does not expose a second PUT config; materialize is the only CP writer path.

See also [10-scenarios](10-scenarios.md).
