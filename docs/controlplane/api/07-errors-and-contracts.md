# 07 — Errors and contracts cheat-sheet

Stable `error.code` values used by wizards. Message text may change; **code + HTTP status** are the contract.

## Install / ports

| HTTP | code | When |
|------|------|------|
| 409 | `cp_name_conflict` | Set name exists and `replace` not set |
| 409 | `cp_port_exhausted` | No free listen/private port |
| 409 | `cp_port_conflict` | Explicit port occupancy (validate) |
| 400 | `cp_invalid_slot` | Bad substitute / `demux_lab` without `allow_lab` |
| 400 | `cp_unknown_preset` | Unknown preset tag |
| 404 | `not_found` | Unknown demux group / set / user |

## Activate / materialize

| HTTP | code | When |
|------|------|------|
| 422 | `cp_claim_failed` | from-* activate cannot claim ownership |
| 409 | `cp_claim_failed` | Explicit `POST …/activate` claim fail |
| 422 | `cp_materialize_failed` / `cp_apply_failed` | Build or Apply failed |
| 422 | extras | `set_persisted`, `dataplane_unchanged`, `failed_at`, `rolled_back` |

## Sets delete / deactivate

| HTTP | code | When |
|------|------|------|
| 409 | `cp_conflict_active` | DELETE while set is active |
| 404 | `not_found` | Deactivate unknown set name |

## Subscription

| HTTP | code | When |
|------|------|------|
| 409 | `cp_no_active_set` | No active sets and WG hub off |
| 403 | `cp_user_ineligible` | Disabled / expired / over traffic |
| 400 | `cp_invalid_sub_filter` | `strict_filters=true` + unknown filter |
| 404 | `not_found` | Unknown token |

## Integrator checklist

1. Read bootstrap `capabilities` before assuming features.
2. After activate: poll `ready.ok` **and** interpret `ready.context`.
3. Treat `activate:true` responses without `activated:true` as failure even if set JSON exists.
4. UI demux picker: `fits_interchange && demux_compat=="full"` (or explicit lab mode with `allow_lab`).
5. Sub clients: merge fragment; check `meta.matched`; prefer `flow-none`.

**Tests that pin contracts:** `test_01` (bootstrap/ready), `test_03`/`test_04` (install), `test_07` (client), `test_08` (lab gate).
