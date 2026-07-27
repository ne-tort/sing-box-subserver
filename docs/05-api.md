# 05 — REST API v1

Base path: `/v1`. JSON UTF-8. Errors use a uniform envelope.

## Conventions

### Success envelope (resource endpoints)

```json
{
  "ok": true,
  "data": {}
}
```

### Error envelope

```json
{
  "ok": false,
  "error": {
    "code": "config_invalid",
    "message": "human readable",
    "details": {}
  }
}
```

### Auth

Header: `Authorization: Bearer <token>`

| Endpoint | Auth |
|----------|------|
| `GET /v1/health` | Configurable (`health_public=true` default for LB probes) |
| All others | Required |

**Credential model (panel-friendly):**

1. **Bootstrap** — `token` from agent YAML (install-time). Accepted until disabled.
2. **Managed tokens** — created via API, persisted in `data_dir/credentials.json` (mode 0600). Secret returned **once** on create/rotate.
3. Panel flow: create managed token → store in node inventory → `POST /v1/auth/bootstrap/disable` → later `POST /v1/auth/rotate` with `revoke_others=true`.

| Method | Path | Meaning |
|--------|------|---------|
| GET | `/v1/auth/tokens` | List (no secrets): bootstrap flag + managed ids/names |
| POST | `/v1/auth/tokens` | Create `{name, token?}` → returns `{id, token}` once |
| DELETE | `/v1/auth/tokens/{id}` | Revoke managed (`409` if last credential) |
| POST | `/v1/auth/rotate` | `{name?, revoke_others?}` → new token; optional revoke other managed |
| POST | `/v1/auth/bootstrap/disable` | Stop accepting YAML token (`409` if no managed yet) |

Rotation does **not** require process restart.

### Conditional headers

- `If-Match: "<revision>"` or `If-Match: "sha256:<hex>"` on `PUT /v1/config`
- `ETag` on `GET /v1/config` and status where applicable

## Endpoints

### `GET /v1/health`

Liveness of the **process** (not box).

```json
{ "ok": true, "data": { "status": "alive" } }
```

HTTP 200 if process up.

### `GET /v1/version`

Unauthenticated or token-gated (same as health policy). Compact version probe for control plane upgrade checks:

```json
{
  "ok": true,
  "data": {
    "agent_version": "0.1.0",
    "agent_commit": "abc1234",
    "singbox_version": "1.14.0-lx.17",
    "singbox_commit": "1e9c91e1",
    "build_tags": ["with_wireguard", "with_quic"]
  }
}
```

Same fields are always present on `GET /v1/status`.

### `GET /v1/ready`

Readiness for dataplane: 200 if `Running` (and optionally not `Applying`); 503 otherwise. Used by orchestration that must not route users to a dead node.

### `GET /v1/status`

```json
{
  "ok": true,
  "data": {
    "state": "Running",
    "node_id": "edge-1",
    "agent_version": "0.1.0",
    "agent_commit": "abc1234",
    "singbox_version": "1.14.0-lx.17",
    "singbox_commit": "1e9c91e1",
    "build_tags": ["with_wireguard", "with_quic", "..."],
    "revision": 12,
    "content_sha256": "...",
    "box_started_at": "2026-07-27T00:00:00Z",
    "process_started_at": "2026-07-27T00:00:00Z",
    "last_apply": {
      "at": "...",
      "source": "push",
      "result": "ok",
      "error": null
    },
    "last_error": null,
    "pull": {
      "enabled": true,
      "interval_sec": 60,
      "last_success_at": "...",
      "last_error": null
    }
  }
}
```

### `GET /v1/config`

Returns current **last-good** JSON (or 404 if none).  
Headers: `ETag: "sha256:…"`, `X-Revision: 12`.

Query: `?meta=1` → return meta only without body.

### `PUT /v1/config`

Body: raw sing-box JSON **or** wrapper:

```json
{
  "config": { },
  "source": "push",
  "revision_hint": "optional"
}
```

Raw body treated as config for simplicity (Content-Type `application/json`).

**Side effect:** successful PUT sets `config_mode=direct` and **cancels** any active subscription so the scheduled fetch cannot overwrite the pushed config.

Responses:

| Code | Meaning |
|------|---------|
| 200 | Applied or no-op same hash |
| 400 | Validate / decode error |
| 401 | Auth |
| 409 | Apply in progress or If-Match mismatch |
| 422 | Unsupported feature / missing build tag |
| 500 | Unexpected supervisor error (process still up) |

### Subscription / pull (`/v1/subscribe`, alias `/v1/pull`)

Runtime control of **remote JSON URL** (any HTTP that returns server sing-box JSON). Not s-ui-specific. Not a client share-URI generator.

| Method | Path | Meaning |
|--------|------|---------|
| GET | `/v1/subscribe` or `/v1/pull` | Status (`configured`, `enabled`, `last_noop`, …) |
| POST | `/v1/subscribe` | Enable + immediate fetch `{url, interval_sec, jitter_sec, timeout_sec, headers?, decrypt_body?}` |
| PUT | `/v1/pull` | Same as POST subscribe |
| DELETE | `/v1/subscribe` or `/v1/pull` | Disable; persists so YAML cannot re-seed on restart |
| POST | `/v1/subscribe/refresh` or `/v1/pull/refresh` | Force one fetch (`409` if idle) |

**Schedule:** in-process `time.Timer` + jitter — not robfig/cron / crontab.  
**Dedupe:** `304` and local SHA compare before Apply (no useless core restart).  
**Seed vs runtime:** YAML `pull:` seeds only when `subscribe-state.json` is absent. See [08-pull-heartbeat.md](08-pull-heartbeat.md).

Mode matrix:

| Event | Result |
|-------|--------|
| Idle (no subscribe) | Wait for PUT or POST subscribe |
| POST subscribe | `config_mode=subscribed`; overwrites prior direct config on successful fetch |
| PUT config | `config_mode=direct`; subscription cancelled |
| refresh | Re-fetch URL; Apply if body changed (`304` / same SHA = no-op) |

### Heartbeat (`/v1/heartbeat`)

Optional POST of status JSON to any URL. Seed-once from YAML; REST owns afterwards.

| Method | Path | Meaning |
|--------|------|---------|
| GET | `/v1/heartbeat` | Status |
| PUT | `/v1/heartbeat` | `{url, interval_sec?, timeout_sec?, headers?, enabled?}` |
| DELETE | `/v1/heartbeat` | Disable; persist |

### `POST /v1/validate`

Same body as PUT; never mutates dataplane. Returns parse/tag errors.

### `GET /v1/logs`

Query: `since` (RFC3339 or seq id), `level`, `limit` (default 200, max 2000).

```json
{
  "ok": true,
  "data": {
    "next": "seq-123",
    "entries": [
      { "ts": "...", "level": "error", "msg": "...", "fields": {} }
    ]
  }
}
```

### `GET /v1/metrics`

- Default / `?format=prometheus` → Prometheus text exposition.
- `?format=json` → summary:

```json
{
  "ok": true,
  "data": {
    "process": { "cpu_percent": 1.2, "rss_bytes": 67108864, "goroutines": 42 },
    "box": { "uptime_sec": 3600, "state": "Running" },
    "apply_total": 10,
    "apply_fail_total": 1,
    "rollback_total": 1,
    "box_restart_total": 0
  }
}
```

### `POST /v1/box/stop` / `POST /v1/box/start` (optional v1)

Stop/start last-good without changing revision. Default: include for ops; start uses last-good only.

## Versioning

Breaking changes → `/v2`. Additive fields allowed in v1.

## OpenAPI

Ship `openapi/openapi.yaml` alongside implementation (not required for this docs phase, but contract above is normative until then).
