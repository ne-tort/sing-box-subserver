# 02 — Requirements

## Principles (normative)

1. **Resource frugality** — minimize agent RSS and CPU beyond sing-box; prefer stdlib / small libs; no embedded UI.
2. **Resilience** — management plane outlives dataplane; apply never panics the process; rollback to last-good on failed apply or post-swap crash within probe window.
3. **Operability** — every important state is visible via REST (status, last error, revisions, metrics, recent logs).
4. **Control-plane friendly** — Bearer auth, push apply, configurable pull interval, stable node identity and capability reporting.
5. **Server build profile** — compile only inbound protocols + WireGuard endpoint tags needed for server edge.

## Functional requirements

### Config and dataplane

| ID | Requirement |
|----|-------------|
| FR-CFG-1 | Accept full server-side sing-box JSON (inbounds, endpoints, route, dns, log, … as produced by the control plane). |
| FR-CFG-2 | `validate` without applying. |
| FR-CFG-3 | Hot apply with last-good retention (see [04-lifecycle](04-lifecycle.md)). |
| FR-CFG-4 | Persist last-good and staged artifacts on disk under a configurable data dir. |
| FR-CFG-5 | Support conditional apply via revision / content hash (`If-Match` / body revision). |
| FR-CFG-6 | Detect unexpected box death and attempt restart from last-good with exponential backoff + jitter. |

### Management API

| ID | Requirement |
|----|-------------|
| FR-API-1 | REST JSON API under `/v1` (see [05-api](05-api.md)). |
| FR-API-2 | Auth via `Authorization: Bearer <token>` on all mutating and sensitive GETs (health may be open or token-gated by config). |
| FR-API-3 | Expose process + box status, last error, revisions, uptime. |
| FR-API-4 | Expose recent structured logs (ring buffer) and optional file log path. |
| FR-API-5 | Expose metrics: Prometheus text and a compact JSON summary (CPU%, RSS, goroutines, apply counters). |

### Control plane integration

| ID | Requirement |
|----|-------------|
| FR-CP-1 | Push: `PUT /v1/config` applies desired JSON. |
| FR-CP-2 | Pull: configurable URL + interval `N` (+ jitter); fetch desired config when mother is available. |
| FR-CP-3 | Pull failures must not stop or restart a healthy box. |
| FR-CP-4 | Report `node_id`, agent version, sing-box version / build tags, listen bind of management API. |
| FR-CP-5 | Optional heartbeat/register URL to mother (POST status snapshot). |

### Ops

| ID | Requirement |
|----|-------------|
| FR-OPS-1 | Single binary; systemd-friendly (foreground, SIGTERM graceful shutdown). |
| FR-OPS-2 | Config file for agent settings (listen, token, data dir, pull, TLS) separate from sing-box JSON. |
| FR-OPS-3 | Graceful shutdown: stop box, flush logs, exit 0 on SIGTERM. |

## Non-functional requirements

| ID | Requirement |
|----|-------------|
| NFR-1 | Management handlers remain responsive while apply is in progress (apply serialized; status readable). |
| NFR-2 | Apply of invalid JSON returns `4xx` with structured error; process stays up. |
| NFR-3 | Failed `New`/`Start` of staged box leaves previous instance running. |
| NFR-4 | Agent idle overhead target: small vs sing-box idle (document measured RSS in CI notes; no hard fail until baseline exists). |
| NFR-5 | Default listen for management: `127.0.0.1` unless TLS is configured; public bind requires TLS **or** explicit `insecure_public_bind=true` (ops footgun, logged at start). |
| NFR-6 | Structured logs (JSON lines); levels configurable. |
| NFR-7 | No panic across API/supervisor boundaries; recover at top-level HTTP middleware and supervisor loop. |

## Non-goals (v1)

- Web admin UI, multi-user RBAC beyond single bearer token.
- SQLite / client CRUD / subscription generation for end users.
- Multiple sing-box instances in one process.
- gRPC as primary control protocol ([ADR 0002](adr/0002-rest-not-grpc.md)).
- Embedding control-plane business logic.

## SLOs (initial, soft)

| Signal | Target |
|--------|--------|
| Status API availability while process up | ~100% (independent of box) |
| Successful apply of valid config | Completes with Running or explicit error; no silent half-state |
| Rollback on failed apply | Previous revision remains active |
| Pull storm | Interval + jitter; 304/ETag short-circuit |

## Compliance notes for mother panels

Control plane MUST send **server** configs only (no client TUN-as-product). Agent MAY reject configs that require build tags not present in the binary (clear error listing missing features).
