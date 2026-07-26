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

Responses:

| Code | Meaning |
|------|---------|
| 200 | Applied or no-op same hash |
| 400 | Validate / decode error |
| 401 | Auth |
| 409 | Apply in progress or If-Match mismatch |
| 422 | Unsupported feature / missing build tag |
| 500 | Unexpected supervisor error (process still up) |

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
