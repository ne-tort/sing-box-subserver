# 01 — Auth and envelopes

## Two audiences

| Surface | Auth | Envelope |
|---------|------|----------|
| `/v1/controlplane/*` | `Authorization: Bearer <agent-token>` | `{ "ok": true, "data": … }` / `{ "ok": false, "error": { "code", "message" } }` |
| `GET /v1/sub/{token}` | Path `token` == user `sub_token` only | **Raw** JSON fragment (`outbounds`, optional `meta`, `endpoints`) — **not** wrapped |

Bearer on `/v1/sub/...` is **ignored / not accepted** as a substitute for the path token.

Discover this from bootstrap:

```http
GET /v1/controlplane/client/bootstrap
```

```json
{
  "client_auth": {
    "management": "Bearer agent token",
    "subscription": "path token only (sub_token); Bearer not accepted"
  },
  "needs_tls_insecure": true,
  "subscription": { "envelope": "raw JSON … — not {ok,data}" }
}
```

**Tests:** `tests/vps_cp/test_01_bootstrap_meta.py::test_bootstrap_capabilities_and_flows`

## Secrets redaction

| Resource | Default | Secrets |
|----------|---------|---------|
| Users list/get | redacted (`has_token`) | `?secrets=1` → `sub_token`, `creds`, URLs |
| Sets list/get | no `peer_secrets` (`has_peer_secrets` flag) | `?secrets=1` → `peer_secrets` |

Bootstrap capability: `sets_secrets_query`.

## Management TLS

CP management listener is HTTPS from the controlplane TLS profile (usually self-signed).
Clients talking to `CP_BASE` over HTTPS typically need TLS verify off (`CP_INSECURE=1` in pytest).
That is **management** TLS — Reality outbounds do **not** use `tls.insecure`.

**Tests:** whole suite uses `verify=False` via `CP_INSECURE`; remote client probes hit `https://127.0.0.1:8080` on the VPS.
