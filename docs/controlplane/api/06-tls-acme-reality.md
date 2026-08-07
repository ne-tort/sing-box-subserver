# 06 — SSL profiles + Reality

## SSL profiles

```http
GET  /v1/controlplane/ssl
POST /v1/controlplane/ssl
GET|PUT|DELETE /v1/controlplane/ssl/{id}
POST /v1/controlplane/ssl/{id}/regenerate
```

Binding: `params.ssl_profile=<id>` (empty → `default`). Management HTTPS uses the Default profile leaf.
Subscription outbounds set `tls.insecure: true` unless the profile is ACME-ready.

**Tests:** `test_05_tls_acme.py`  
**Remote client:** `test_07_client_remote.py` param `e2e-acme-tls`  
**Scenario:** [04-tls-acme](../scenarios/04-tls-acme.md) · [11-tls](../11-tls.md)

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
