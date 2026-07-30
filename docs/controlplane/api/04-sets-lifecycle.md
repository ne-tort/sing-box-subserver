# 04 — Sets lifecycle (from-*, activate, replace)

## Preferred install paths

| Mode | Endpoint | Typical body |
|------|----------|--------------|
| Single inbound(s) | `POST /v1/controlplane/sets/from-presets` | `{ items:[{name,preset,listen_port?}], activate, replace? }` |
| Demux group | `POST /v1/controlplane/sets/from-demux-group` | `{ group, name?, listen_port?, slot_presets?, allow_lab?, activate, replace? }` |

Raw `POST/PUT /v1/controlplane/sets` still exists for power users; wizards should prefer `from-*` (auto port, activate contract, demux BuildInstall).

### Optional `listen_port`

Omit → auto-pick free port (prefers 443 when free). Exhausted → `409` **`cp_port_exhausted`**.

### `replace:true`

If a set with the same name exists: deactivate if active, delete, then create. Without replace → `409` **`cp_name_conflict`**.

### `activate:true`

Must succeed as part of the same request:

- Success → HTTP **201**, `activated: true` (and `activated_sets` for multi presets)
- Failure → HTTP **422**, `error.code` from activate/materialize/claim; extras:
  - `set_persisted: true`
  - `dataplane_unchanged: true` (after rollback on multi)
  - `failed_at`, `rolled_back` (multi from-presets)

**Multi from-presets is all-or-nothing:** if item N fails activate, previously activated items in that request are deactivated (rollback).

Claim ownership:

| Path | Claim fail HTTP |
|------|-----------------|
| from-* activate | `422` `cp_claim_failed` |
| `POST .../sets/{name}/activate` | `409` `cp_claim_failed` |

### DELETE / deactivate

| Case | Code |
|------|------|
| DELETE while active | `409` **`cp_conflict_active`** |
| Deactivate unknown name | `404` `not_found` |
| Deactivate already inactive (set exists) | `200` `{ noop: true }` |

### Response fields after demux install

Install returns `member_ports`, `slot_snis`, `warnings`. Both **`member_ports` and `slot_snis` persist** on the set and appear in list/get after reload.

**Tests:**
- `test_03_presets_install.py` — from-presets + activate + edit
- `test_04_demux_groups.py` — demux install, name conflict, autoport, `slot_snis` persist
- `test_08_matrix_lab_transports.py` — `allow_lab` + replace

**Scenarios:** [02-single-presets](../scenarios/02-single-presets.md), [03-demux-443-dual](../scenarios/03-demux-443-dual.md)
