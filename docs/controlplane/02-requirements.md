# 02 — Requirements (controlplane)

Applies only when the binary is built with `with_controlplane`. Root agent FR/NFR still apply.

## Principles

1. **Lightweight** — stdlib + small code; JSON files; no embedded UI or ORM.
2. **Declarative** — presets and demux templates are data; materialize is pure-ish + Apply.
3. **Testable** — table-driven materialize and eligibility (expiry/limit) tests.
4. **Isolated** — file tree under `internal/controlplane/`; optional link via build tag.
5. **Predictable ownership** — exclusive `config_mode` transitions only ([ADR 0002](adr/0002-exclusive-config-owner.md)).

## Functional requirements

### Users

| ID | Requirement |
|----|-------------|
| FR-U-1 | CRUD local users via management API (agent Bearer). |
| FR-U-2 | On create (and `PUT …/creds`), accept optional operator `creds` overrides; **auto-fill missing** presets/fields from the catalog; **lazy backfill** when the catalog grows. |
| FR-U-3 | Optional `expires_at`; when past, user is **ineligible** (creds omitted from materialize). |
| FR-U-4 | Optional `traffic_limit_bytes` + reset metadata hooks; when over limit (counter supplied by future module or manual flag), user ineligible. |
| FR-U-5 | `enabled=false` → ineligible. |
| FR-U-6 | Each user has a `sub_token` for public subscription URL; rotate invalidates old URL. |
| FR-U-7 | Eligibility change that affects any active set triggers materialize + hot Apply. |

### Protocol presets

| ID | Requirement |
|----|-------------|
| FR-P-1 | Ship named presets as embedded JSON (inbound template + outbound template + traits + description). |
| FR-P-2 | List/get presets via API (read-only in v1). |
| FR-P-3 | Traits must be sufficient for operators to author demux match rules (tcp/udp/h2/h3/… — documented enum in domain). |
| FR-P-4 | Inbound and outbound templates are mirrors aside from listen/server address and secrets placeholders. |
| FR-P-5 | `vless-reality-tcp` preset is supported with per-inbound generated Reality key material and sticky endpoint assignment. |

### Inbound sets / demux

| ID | Requirement |
|----|-------------|
| FR-S-1 | CRUD named inbound sets: listen, ordered preset refs, optional demux template. |
| FR-S-1a | Sets may additionally use logical `bindings[]` (preset + variant/profile/tag policy); old `presets[]` remains valid and is lazily normalized. |
| FR-S-2 | Activate set by name → add to `active_sets`, Claim(controlplane) if needed, materialize + Apply. |
| FR-S-3 | Deactivate set by name → remove from `active_sets`; if empty → Claim(idle); last-good unchanged. |
| FR-S-4 | At most one set may use a given `listen_port` in the store; conflict → `409`. Many sets may be **active** on different ports. |
| FR-S-5 | Activate requiring demux without `with_demux` in binary → `422`. |
| FR-S-6 | Demux rules reference inbound tags produced by materialize for that set (documented tag scheme). |
| FR-S-7 | Demux optional: if omitted, set must reference exactly one preset that binds the listen port directly. |

### Materialize / Apply

| ID | Requirement |
|----|-------------|
| FR-M-1 | Build full server sing-box JSON from active set + eligible users only. |
| FR-M-1a | Materialize performs binding expansion: one logical binding may produce multiple inbound user entries and multiple subscription outbounds by user-symmetric variants. |
| FR-M-2 | Apply only via `supervisor.Apply` with `source=controlplane`. |
| FR-M-3 | Same content SHA → noop (no useless restart). |
| FR-M-4 | Include minimal log/dns/route defaults (not panel policy surface). |
| FR-M-5 | Rebuild triggers: user CRUD affecting eligibility, set activate/deactivate, expiry ticker, future traffic signal, preset catalog change (if any). |

### Subscriptions

| ID | Requirement |
|----|-------------|
| FR-SUB-1 | Public `GET /v1/sub/{token}` without agent Bearer ([ADR 0004](adr/0004-user-token-subscriptions.md)). |
| FR-SUB-2 | Default body: sing-box JSON document with `outbounds` (and minimal wrapper fields as specified). |
| FR-SUB-3 | Optional query filters `preset` / `set` to subset outbounds (union of active sets by default). |
| FR-SUB-3a | Optional query filters `flow` (VLESS symmetric `users.flow`) to subset VLESS outbounds. Accepted values: `none`, `xtls-rprx-vision`. Repeatable or comma-separated. |
| FR-SUB-3b | Optional query filter `network` to override VLESS outbound network. Accepted values: `tcp`, `udp`. If omitted → sing-box default behavior. |
| FR-SUB-4 | Unknown/revoked token → `401`/`404` (normative: `404`). |
| FR-SUB-5 | Ineligible user → `403` or empty policy (normative: `403` with stable code). |

### Reality overrides

| ID | Requirement |
|----|-------------|
| FR-R-1 | `PUT /v1/controlplane/reality` accepts operator profile list: `sni` + optional `handshake_server` / `handshake_port`. |
| FR-R-2 | Profile validation runs on PUT and periodically in runtime (DNS + TCP reachability + CDN heuristics). |
| FR-R-3 | Invalid user override entries are skipped; if the entire user pool is invalid, overrides are cleared and defaults are used silently. |
| FR-R-4 | Each Reality inbound keeps sticky assignment (endpoint + generated key pair + short_id) until endpoint disappears/becomes invalid. |

### Mode isolation

| ID | Requirement |
|----|-------------|
| FR-OWN-1 | Activate cancels subscribe and takes exclusive ownership. |
| FR-OWN-2 | `PUT /v1/config` and successful subscribe enable deactivate CP ownership. |
| FR-OWN-3 | Status/heartbeat report `config_mode=controlplane` from shared owner registry. |

## Non-functional

| ID | Requirement |
|----|-------------|
| NFR-CP-1 | Without tag: no routes, no packages linked into default binary path beyond stubs if any. |
| NFR-CP-2 | Store files mode `0600`; atomic replace writes. |
| NFR-CP-3 | Expiry loop cheap (seconds–minutes tick; no per-packet work). |
| NFR-CP-4 | Management handlers remain responsive during Apply (same as root NFR-1). |

## Non-goals (module v1)

- Web UI, panel import, multi-node mesh.
- Real traffic counter / billing engine (hooks only — [ADR 0005](adr/0005-traffic-hooks-without-accounting.md)).
- Editable custom presets via API (overlays later).
- Clash / share-URI catalog as primary format (URI list may be added later as `?format=`).
- Running multiple demux listens as one “set” spanning ports in v1 (one listen per set).
