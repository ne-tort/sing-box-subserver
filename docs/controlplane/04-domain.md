# 04 — Domain model (controlplane)

Normative types for implementation and API. Field names are JSON/`snake_case` unless noted.

## User

| Field | Type | Notes |
|-------|------|-------|
| `id` | string | Stable opaque id (ulid/uuid) |
| `name` | string | Display; unique recommended |
| `enabled` | bool | Default true |
| `created_at` / `updated_at` | RFC3339 | |
| `expires_at` | RFC3339 \| null | Optional lifetime end |
| `traffic_limit_bytes` | uint64 \| null | Hook for future accounting |
| `traffic_used_bytes` | uint64 | Default 0; written by future module or admin PATCH |
| `traffic_reset_at` | RFC3339 \| null | Next reset instant (optional) |
| `traffic_reset_period_sec` | uint64 \| null | Optional period after reset |
| `sub_token` | string | Secret; high entropy; URL-safe |
| `creds` | object | Map `preset_name` → protocol-specific secret object |

### Credential map

On user create (and `PUT /users/{id}/creds`), the operator may supply partial
`creds` for known presets/fields. Missing presets and empty fields are
**auto-filled** from the embedded catalog (field-level), so materialize/sub always
see a full map. Values stay stable until rotate or a later PUT.

**Lazy backfill:** if the catalog later gains new preset names or fields,
materialize/activate (or user read/update paths) generate missing keys once and
persist — do not fail old users.

### Eligibility

A user is **eligible** for materialize and subscription iff all hold:

1. `enabled == true`
2. `expires_at` is null or `now < expires_at`
3. `traffic_limit_bytes` is null **or** `traffic_used_bytes < traffic_limit_bytes`

Ineligible users are omitted from inbound `users[]` and receive `403` on sub fetch.

When eligibility flips while `config_mode=controlplane` and any set is active → materialize + Apply.

### Traffic reset (hook semantics)

When `traffic_reset_at <= now`:

- set `traffic_used_bytes = 0`
- advance `traffic_reset_at` by `traffic_reset_period_sec` if period set, else clear reset_at
- if user becomes eligible again → materialize + Apply

Actual increment of `traffic_used_bytes` is **out of scope** (future module).

## ProtocolPreset

| Field | Type | Notes |
|-------|------|-------|
| `name` | string | Stable key (`vless-reality-tcp`, …) |
| `protocol` | string | sing-box inbound type |
| `description` | string | Human text (protocol + variant) |
| `traits` | string[] | See trait vocabulary below |
| `inbound_template` | object | sing-box inbound skeleton with placeholders |
| `outbound_template` | object | Mirror outbound skeleton with placeholders |

### Placeholders (materialize / subscribe) — closed list

String tokens inside JSON templates (no Go-template DSL):

| Token | Replaced with |
|-------|----------------|
| `{{tag}}` | Inbound or outbound tag for this preset in context |
| `{{listen}}` | Set listen address |
| `{{listen_port}}` | Set listen port (decimal) |
| `{{server}}` | `controlplane.public_host` (or request host for URL display) |
| `{{server_port}}` | Advertised port (set `listen_port`) |
| `{{user.name}}` | User display name |
| `{{user.uuid}}` | From `creds[preset].uuid` when present |
| `{{user.password}}` | From `creds[preset].password` when present |
| `{{user.username}}` | From `creds[preset].username` when present |
| `{{tag:PRESET}}` | Inbound tag for preset `PRESET` in the current set (`cp-in-{set}-{preset}`) |

Unknown tokens → materialize/validate error.

### Trait vocabulary (initial)

`tcp`, `udp`, `tls`, `reality`, `xtls`, `h2`, `h3`, `quic`, `grpc`, `ws`, `httpupgrade`, `mux`, `udp_over_tcp`

Used when authoring demux `match` blocks — stored/returned by API; not a runtime DSL beyond that.

## InboundSet

| Field | Type | Notes |
|-------|------|-------|
| `name` | string | Unique |
| `description` | string | Optional |
| `listen` | string | e.g. `::` or `0.0.0.0` |
| `listen_port` | uint16 | Public port |
| `presets` | string[] | Ordered preset names (must exist; ≥1) |
| `bindings` | object[] | Optional logical bindings. Backward-compatible layer over `presets[]` (if absent, each preset becomes one default binding). |
| `demux_template` | object \| null | Optional. If set: demux listens on port; protocol inbounds inject-only. If null: **exactly one** preset; that inbound binds `listen`/`listen_port` directly |
| `created_at` / `updated_at` | RFC3339 | |

### SetBinding (logical)

| Field | Type | Notes |
|-------|------|-------|
| `preset` | string | Required preset key |
| `subscription_tags` | string[] | Optional query tags for subscription selection |
| `enabled_user_variants` | string[] | Optional allowed user-symmetric variants for this binding |
| `enabled_client_profiles` | string[] | Optional outbound-only profile keys for this binding |
| `credential_instance_policy` | string | Optional future policy hint |

### Variant metadata (v1)

First-class metadata classifies fields/scopes:
- `user_symmetric`: appears in inbound user entry **and** outbound.
- `peer_symmetric`: preset/runtime-level symmetric state (e.g. Reality assignment).
- `outbound_only`: only outbound/client side.

Initial v1 user variants for `vless`:
- `flow-none` → `creds[preset].uuid`, outbound/inbound `flow=""`
- `flow-xtls-rprx-vision` → `creds[preset].uuid_xtls`, `flow="xtls-rprx-vision"`
- `flow-udp-vision` → `creds[preset].uuid_udp`, `flow="xtls-rprx-vision-udp443"`

Resolver: `domain.UserVariantsForProtocol(protocol, binding)` — single entry point for materialize, subscription catalog, and discovery APIs. Empty or unknown `enabled_user_variants` falls back to the full protocol catalog.

### Port exclusivity

No two **stored** sets may share the same `listen_port` (create/update → `409` `cp_port_conflict`).

### Multiple active sets

Runtime `state.json` holds `active_sets: string[]` (names).

- Many sets may be **active** at once (typical panel: several ports live).
- Activate adds the name (idempotent); requires `config_mode=controlplane` (Claim on first activate).
- Deactivate removes the name; if the list becomes empty → `Claim(idle)`.
- Materialize **merges** all active sets into one server JSON.

## Runtime state (`state.json`)

| Field | Type | Notes |
|-------|------|-------|
| `active_sets` | string[] | |
| `last_materialize_sha256` | string | |
| `last_materialize_at` | RFC3339 \| null | |
| `materialize` | object \| null | Last materialize attempt: `last_success_at`, `last_attempt_at`, `last_error`, `last_error_code`, `last_apply_noop` |
| `owner_transitions` | array | Recent ownership changes (max 20): `from`, `to`, `at`, `reason`, `trigger`, `success`, `error` |

Global `config_mode` is **not** duplicated here — see root [`config-owner.json`](../adr/0008-exclusive-config-owner.md) / `internal/configowner`.

Boot reconciliation (see [05-api](05-api.md)): orphan `controlplane` without `active_sets` → `idle`; stale `active_sets` while not `controlplane` → cleared.

## TLSProfile (`tls_profile.json`)

One active profile for all TLS-capable inbounds. See [11-tls](11-tls.md).

| Field | Type | Notes |
|-------|------|-------|
| `mode` | string | `self_signed` \| `acme_domain` \| `acme_ip` |
| `self_signed` | object \| null | Required for `self_signed`: `common_name`, `dns_sans`, `ip_sans`, `key_type`, `valid_days`, `organization` |
| `acme` | object \| null | Required for `acme_*`: `email`, `domains`, `provider`, challenge flags, optional `dns01_challenge` |

Defaults on first boot: `self_signed` from `controlplane.public_host` (IP → `ip_sans`).

## Reality config (`reality_config.json`)

| Field | Type | Notes |
|-------|------|-------|
| `user_profiles` | `RealityEndpoint[]` | Operator overrides from `PUT /reality` |
| `effective_profiles` | `RealityEndpoint[]` | Validated pool currently used by runtime |
| `using_user_overrides` | bool | `true` only when validated user pool is non-empty |
| `updated_at` | RFC3339 \| null | Last validation/update time |

### RealityEndpoint

| Field | Type | Notes |
|-------|------|-------|
| `sni` | string | Required domain |
| `handshake_server` | string | Optional; default = `sni` |
| `handshake_port` | uint16 | Optional; default = `443` |

## Reality assignments (`reality_assignments.json`)

Map `inbound_key` (`{set}/{preset}`) → assignment object:

- `sni`, `handshake_server`, `handshake_port` (chosen endpoint)
- `private_key_base64`, `public_key_base64`, `short_id` (generated per inbound)
- `updated_at`

## Tag scheme

For set `S` and preset `P`:

- Inbound tag: `cp-in-{S}-{P}`
- Demux tag: `cp-demux-{S}` (when demux present)
- Outbound tag in subscription: `cp-out-{S}-{P}` (set-qualified to avoid collisions across active sets)

## Invariants (checklist)

1. User create / PUT creds ⇒ merge overrides then field-level auto-fill for all catalog presets; later → lazy backfill.
2. Materialize `users[]` ⇒ only eligible users.
3. First activate while not CP → `Claim(controlplane)` then Apply; further activates rematerialize.
4. Port unique among sets; many active sets OK.
5. Demux present + binary without `with_demux` → activate `422`.
6. Demux null ⇒ exactly one effective preset binding.
