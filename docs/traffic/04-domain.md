# 04 — Domain

## Subject

| Field | Type | Notes |
|-------|------|-------|
| `subject_id` | string | Consumer-stable id (`cp:user:{uuid}`, `inbound:{tag}`, …) |
| `kinds` | string[] | `dataplane_user`, `controlplane_user`, `inbound_aggregate`, … |
| `dataplane_keys` | string[] | Values of `metadata.User` to aggregate |
| `labels` | map | Free-form (`cp_name`, `user_id`, `set`, …) |

## Series types

| Type | Key | Purpose |
|------|-----|---------|
| `dataplane_user` | raw User | Fallback / discovery |
| `inbound` | inbound tag | Capacity |
| `outbound` | outbound tag | Egress |
| `subject` | subject_id | Billing (aggregated from keys on flush) |

## Sample / counters

Each flush produces deltas `(up, down)` per series key; cumulative totals
persisted in `counters.json`. Direction: **up** = client→server (read on
outbound wrap), **down** = server→client (write), matching s-ui convention.

## Limits

| Field | Unit |
|-------|------|
| `up_bytes_per_sec` | 0 = unlimited |
| `down_bytes_per_sec` | 0 = unlimited |

Applied per **dataplane_key** (not subject_id).

Layers (effective = merge):

| Layer | Source | Notes |
|-------|--------|-------|
| `controlplane` | user `speed_*` via cpbridge | only eligible users |
| `manual` | `PUT /v1/traffic/limits` | ops override; survives rematerialize |
| `effective` | CP ∪ manual | manual wins on key conflict |

## Store layout (`{data_dir}/traffic/`)

| Path | Content |
|------|---------|
| `subjects.json` | Registered manifests |
| `counters.json` | Cumulative totals |
| `series/YYYY-MM-DD.jsonl` | Delta samples `{ts,series_type,key,up,down}` |
| config | flush interval / retention via agent `traffic` section |

## Identity (controlplane)

Materialize emits:

- non-variant: `users[].name = User.Name`
- VLESS variants: `User.Name + "-" + variant.Name`

Bridge registers all keys under `cp:user:{User.ID}`.
