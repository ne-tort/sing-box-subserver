# Scenario 05 — Reality

## Goal

Configure a validated Reality SNI pool, install `vless_reality`, confirm sticky assignment and official-client connectivity.

## Steps

1. Prefer seeding known-good profiles on fresh data:
   ```http
   PUT /v1/controlplane/reality
   { "profiles": [
     { "sni": "www.apple.com", "handshake_server": "www.apple.com", "handshake_port": 443 },
     { "sni": "www.ieee.org", "handshake_server": "www.ieee.org", "handshake_port": 443 }
   ]}
   ```
2. Do **not** use `www.microsoft.com` as dest/SNI for production clients.
3. `POST /sets/from-presets` with `preset: "vless_reality"`, `activate: true`, `replace: true` (suite name `e2e-reality-tcp`).
4. Poll ready; `GET /reality` shows `active_assignments` for `e2e-reality-tcp/vless_reality`.
5. Sub outbounds: Reality public_key + short_id + `server_name`; default variant **flow-none** (empty `flow`).
6. Remote probe: official sing-box docker on VPS → `RESULT OK`.

## Partial / reject paths

- `test_reality_get_and_put_partial` — accepted vs rejected profiles
- `test_reality_all_rejected` — empty effective pool handling

## Tests

| File | Cases |
|------|-------|
| [`test_06_reality.py`](../../../tests/vps_cp/test_06_reality.py) | put / reject |
| [`test_03_presets_install.py`](../../../tests/vps_cp/test_03_presets_install.py) | `test_e2e_reality_tcp_for_remote_suite` |
| [`test_07_client_remote.py`](../../../tests/vps_cp/test_07_client_remote.py) | `e2e-reality-tcp` |

## API refs

[../api/06-tls-acme-reality.md](../api/06-tls-acme-reality.md)
