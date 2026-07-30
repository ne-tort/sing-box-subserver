# 02 — Bootstrap and ready

## Discovery: `GET /v1/controlplane/client/bootstrap`

One-shot contract for operator wizards (Bearer required). Not an end-user SDK login.

Useful top-level fields:

| Field | Meaning |
|-------|---------|
| `public_host` / `public_port` | Values used when building `subscription_url` |
| `client_auth` | Management vs subscription auth |
| `needs_tls_insecure` | Management TLS is typically self-signed |
| `subscription` | Shape, merge contract, `prefer_variant=flow-none`, query_map hint |
| `capabilities` | Feature flags + activate/claim/replace contracts |
| `install_modes` / `flows` | Ordered HTTP steps for single presets vs demux |
| `hints` | e.g. `demux_groups_list=?status=stable`, `install_requires_demux_compat_full` |

Key capabilities (post semantic-audit):

- `activate_contract` — `activate:true` must yield HTTP 201 + `activated:true`; multi from-presets is all-or-nothing (rollback)
- `claim_on_from_star` → `422 cp_claim_failed`
- `claim_on_activate` → `409 cp_claim_failed`
- `replace_on_from_star` → `replace:true` on from-*
- `ready_context` → `ready.context` ∈ `idle` \| `install_ready` \| `degraded`

**Tests:** `test_01_bootstrap_meta.py::test_bootstrap_capabilities_and_flows`  
**Scenario:** [../scenarios/01-wizard-bootstrap.md](../scenarios/01-wizard-bootstrap.md)

## Poll: `GET /v1/controlplane/status`

After `from-*` with `activate:true`, poll until:

```json
"ready": {
  "ok": true,
  "context": "install_ready",
  "box_up": true,
  "supervisor_state": "Running",
  "active_sets": true,
  "reasons": [],
  "poll": "GET /v1/controlplane/status → ready.ok == true"
}
```

| `ready.context` | Meaning |
|-----------------|---------|
| `idle` | No active sets / WG — not a failed install; dashboard should not treat as outage |
| `install_ready` | Wizard may create users and fetch `/v1/sub` |
| `degraded` | Ownership / materialize / TLS / ACME / dataplane issues — see `reasons` |

`ready.ok=false` with reason `no_active_sets` in idle is expected before first activate.

**Tests:** `test_01_bootstrap_meta.py::test_status_ready_shape`  
**Scenario:** every install scenario polls ready via `api.wait_ready()`.

## Ports probe

```http
GET /v1/controlplane/ports/availability?port=443
```

Returns `can_tcp` / `can_udp` / `can_demux` under policy **one TCP + one UDP occupant per port**.

**Tests:** `test_01_bootstrap_meta.py::test_ports_availability_shape`
