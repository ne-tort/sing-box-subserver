# Scenario 03 — Demux group on :443 (stable dual)

## Goal

Install catalog group `dg_443_dual` (defaults: `vless_reality` + `hy2`) on public 443, verify subscription, prove member ports **and** demux front work with official sing-box.

Cost: 1 demux + 2 members (see [09-demux-cost.md](../../guides/controlplane-presets/09-demux-cost.md)). Groups SoT = catalogsqlite `ref/demux`.

## Steps

1. `GET /v1/controlplane/demux-groups?status=stable&lang=en`
2. `GET /v1/controlplane/demux-groups/dg_443_dual/substitutions?lang=en`
   - For each option: salamander must not be `full`+fits; prefer `demux_compat=full` for defaults.
3. Free :443 if another active set holds it (deactivate).
4. ```http
   POST /v1/controlplane/sets/from-demux-group
   {
     "group": "dg_443_dual",
     "name": "e2e-demux",
     "listen_port": 443,
     "slot_presets": { "<slot_id>": "<default_preset>", … },
     "activate": true
   }
   ```
5. Expect 201, `activated`, `member_ports`, `slot_snis`.
6. Reinstall same name without cleanup → `409 cp_name_conflict`.
7. Poll ready; `GET /sets/e2e-demux` → **`member_ports` and `slot_snis` still present**.
8. Create user; `GET /v1/sub/{token}` → ≥2 outbounds; VLESS must not advertise `udp443` by default; `meta.matched == len(outbounds)`.
9. Client probes (on VPS): Hy2 + Reality against **member** ports and against **443**.

## Autoport variant

Omit `listen_port` → server picks a free port (`test_demux_omit_listen_port_auto_pick`).

## Success

- Dual stack live: QUIC member+demux OK, Reality member+demux OK (seed Reality pool first on fresh data).

## Tests

| File | Cases |
|------|-------|
| [`test_04_demux_groups.py`](../../../tests/vps_cp/test_04_demux_groups.py) | install, conflict, persist, sub shape |
| [`test_07_client_remote.py`](../../../tests/vps_cp/test_07_client_remote.py) | `test_remote_demux_defaults_member_and_front` |
| Artifacts | `demux_install.json`, `client_remote_demux.log` |

## API refs

[../api/03-catalog.md](../api/03-catalog.md) · [../api/04-sets-lifecycle.md](../api/04-sets-lifecycle.md)
