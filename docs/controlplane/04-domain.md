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

On user create, generate and persist credentials for **every known protocol preset**
in the embedded catalog (stable until rotated).

**Lazy backfill:** if the catalog later gains new preset names, materialize/activate
(or user read/update paths) generate missing `creds[preset]` keys once and persist —
do not fail old users.

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
| `demux_template` | object \| null | Optional. If set: demux listens on port; protocol inbounds inject-only. If null: **exactly one** preset; that inbound binds `listen`/`listen_port` directly |
| `created_at` / `updated_at` | RFC3339 | |

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

Global `config_mode` is **not** duplicated here — see root [`config-owner.json`](../adr/0008-exclusive-config-owner.md) / `internal/configowner`.

## TLSProfile (`tls_profile.json`)

One active profile for all TLS-capable inbounds. See [11-tls](11-tls.md).

| Field | Type | Notes |
|-------|------|-------|
| `mode` | string | `self_signed` \| `acme_domain` \| `acme_ip` |
| `self_signed` | object \| null | Required for `self_signed`: `common_name`, `dns_sans`, `ip_sans`, `key_type`, `valid_days`, `organization` |
| `acme` | object \| null | Required for `acme_*`: `email`, `domains`, `provider`, challenge flags, optional `dns01_challenge` |

Defaults on first boot: `self_signed` from `controlplane.public_host` (IP → `ip_sans`).

## Tag scheme

For set `S` and preset `P`:

- Inbound tag: `cp-in-{S}-{P}`
- Demux tag: `cp-demux-{S}` (when demux present)
- Outbound tag in subscription: `cp-out-{S}-{P}` (set-qualified to avoid collisions across active sets)

## Invariants (checklist)

1. User create ⇒ creds for all catalog presets; later → lazy backfill.
2. Materialize `users[]` ⇒ only eligible users.
3. First activate while not CP → `Claim(controlplane)` then Apply; further activates rematerialize.
4. Port unique among sets; many active sets OK.
5. Demux present + binary without `with_demux` → activate `422`.
6. Demux null ⇒ `len(presets)==1`.
