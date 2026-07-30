# Scenario 04 — TLS / ACME inbound

## Goal

Enable cert-manager for a public domain, install a TLS preset with `params.sni`, verify client path (and optional IP SAN).

## Steps

1. `GET /v1/controlplane/tls` — self-signed ready.
2. `PUT /v1/controlplane/cert-manager` with `email`, `domains: [$CP_DOMAIN]`, provider `letsencrypt` (challenge mode appropriate for the VPS).
3. Poll until cert material / status reports domain ready (suite dumps `cert_manager_*.json`).
4. `POST /sets/from-presets` with a TLS preset and `"params": { "sni": "<domain>" }`, `activate: true` (name e.g. `e2e-acme-tls`).
5. Poll `ready.ok` (includes ACME readiness when bindings use `sni`).
6. Remote client: `test_07` param `e2e-acme-tls`.

## Success

- Subscription outbound for that set verifies TLS (no `insecure`) when ACME leaf is used.
- Brief handshake failures right after enable are expected until obtain completes.

## Tests

| File | Cases |
|------|-------|
| [`test_05_tls_acme.py`](../../../tests/vps_cp/test_05_tls_acme.py) | self-signed, domain put, inbound sni, IP SAN optional |
| [`test_07_client_remote.py`](../../../tests/vps_cp/test_07_client_remote.py) | `e2e-acme-tls` |

## API refs

[../api/06-tls-acme-reality.md](../api/06-tls-acme-reality.md) · [../11-tls.md](../11-tls.md)
