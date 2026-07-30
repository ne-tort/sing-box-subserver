# 06 — TLS, ACME, Reality

## Self-signed TLS profile

```http
GET /v1/controlplane/tls
PUT /v1/controlplane/tls
```

Default material under `data_dir`; management HTTPS and non-Reality TLS inbounds use it.
Subscription outbounds for self-signed paths set `tls.insecure: true`.

**Tests:** `test_05_tls_acme.py::test_tls_self_signed_status`

## Cert-manager (ACME)

```http
PUT /v1/controlplane/cert-manager
{ "email": "…", "domains": ["wiki.example"], "provider": "letsencrypt", … }
```

TLS inbounds that need a public leaf set `bindings[].params.sni` to a domain from `domains`.
Poll `status.ready` / ACME readiness after enable — brief `no certificate available` is normal.

Ops notes (shared VPS): HTTP-01 needs :80; else TLS-ALPN-01 only (`disable_http_challenge: true`).

**Tests:** `test_05_tls_acme.py` (domain put, inbound with `params.sni`, optional IP SAN)  
**Remote client:** `test_07_client_remote.py` param `e2e-acme-tls`  
**Scenario:** [04-tls-acme](../scenarios/04-tls-acme.md)

## Reality profiles

```http
GET /v1/controlplane/reality
PUT /v1/controlplane/reality
{ "profiles": [ { "sni": "www.apple.com", "handshake_server": "www.apple.com", "handshake_port": 443 } ] }
```

Pool is validated (DNS + TCP dial). Prefer curated hosts (**not** `www.microsoft.com` — handshake fails with official clients). Sticky assignments bind `set/preset` → keys + SNI.

On fresh data, seed known-good profiles (apple / ieee / amazon) before Reality installs if dials to some pool members fail with `REALITY: failed to dial dest: invalid address`.

**Tests:** `test_06_reality.py`, Reality TCP/demux in `test_03` / `test_07`  
**Scenario:** [05-reality](../scenarios/05-reality.md)
