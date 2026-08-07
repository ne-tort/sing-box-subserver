# Scenario 04 — SSL / ACME inbound

## Goal

Create an ACME SSL profile for a public domain, install a TLS preset with `params.ssl_profile`, verify client path (and optional IP profile).

## Steps

1. `GET /v1/controlplane/ssl` — Default self-signed ready.
2. `POST /v1/controlplane/ssl` then `PUT /v1/controlplane/ssl/{id}` with `type=acme`, `domain`, `email`, provider `letsencrypt`.
3. Poll profile `status.state` until `ready` (or document soft failure if :80 blocked).
4. `POST /sets/from-presets` with a TLS preset and `"params": { "ssl_profile": "<id>" }`, `activate: true` (name e.g. `e2e-acme-tls`).
5. Poll `ready.ok` (includes SSL ACME readiness when bindings reference ACME profiles).
6. Remote client: `test_07` param `e2e-acme-tls`.

## Success

- Subscription outbound for that set verifies TLS (no `insecure`) when ACME leaf is used.
- Brief handshake failures right after enable are expected until obtain completes.

## Tests

| File | Cases |
|------|-------|
| [`test_05_tls_acme.py`](../../../tests/vps_cp/test_05_tls_acme.py) | Default SSL, ACME domain, inbound ssl_profile, optional IP |
| [`test_07_client_remote.py`](../../../tests/vps_cp/test_07_client_remote.py) | `e2e-acme-tls` |

## API refs

[../api/06-tls-acme-reality.md](../api/06-tls-acme-reality.md) · [../11-tls.md](../11-tls.md)
