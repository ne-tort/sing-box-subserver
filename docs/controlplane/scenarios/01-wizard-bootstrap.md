# Scenario 01 — Wizard bootstrap

## Goal

Operator UI / SDK learns what the binary supports and how to poll readiness **before** installing sets.

## Steps

1. `GET /v1/health` → `status=alive`
2. `GET /v1/controlplane/client/bootstrap?lang=en`
3. Assert capabilities include at least: `protocols`, `presets`, `demux_groups`, `optional_listen_port`, `ready_poll`, `activate_contract`, `replace_on_from_star`
4. Assert `subscription.prefer_variant == "flow-none"`, `public_host` present, `flows[]` have `method`+`path`
5. `GET /v1/controlplane/status` → `ready` has `ok`, `reasons`, `poll`, **`context`** ∈ {`idle`,`install_ready`,`degraded`}
6. `GET /v1/controlplane/ports/availability?port=443`

## Success

- Bootstrap is enough to render install mode chooser (single vs demux vs wg).
- Idle node: `ready.context=idle` (do not alert as outage solely on `ready.ok=false` + `no_active_sets`).

## Tests

| File | Cases |
|------|-------|
| [`test_01_bootstrap_meta.py`](../../../tests/vps_cp/test_01_bootstrap_meta.py) | `test_health`, `test_bootstrap_capabilities_and_flows`, `test_status_ready_shape`, `test_ports_availability_shape` |

## API refs

[../api/01-auth-and-envelopes.md](../api/01-auth-and-envelopes.md) · [../api/02-bootstrap-and-ready.md](../api/02-bootstrap-and-ready.md)
