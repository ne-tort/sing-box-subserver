# 07 — Observability

## Goals

- Debug apply/rollback without SSH log diving alone.
- Feed panel dashboards and alerts (CPU/RSS, apply failures, box restarts).
- Keep overhead small (ring buffer + prometheus registry; no heavy APM required in v1).

## Logging

### Library

Go `log/slog` with JSON handler to stderr (journald-friendly). Optional file sink via agent config.

### Fields (minimum)

| Field | Use |
|-------|-----|
| `time`, `level`, `msg` | standard |
| `component` | `api`, `supervisor`, `pull`, `box`, … |
| `revision`, `content_sha256` | apply context |
| `source` | `push` / `pull` / `boot` / `restart` |
| `error` | when failing |

### Ring buffer

In-memory lock-free or mutex ring (e.g. 2000 entries) for `GET /v1/logs`.  
Does not replace stderr; duplicates info/warn/error into ring.

### Levels

Agent config `log.level`: `debug|info|warn|error`.  
Box/sing-box log level comes from sing-box JSON `log` section when present.

## Metrics

### Process

| Metric | Type | Notes |
|--------|------|-------|
| `subserver_process_rss_bytes` | gauge | from runtime/memstats + OS RSS if cheap |
| `subserver_process_cpu_percent` | gauge | sampled |
| `subserver_goroutines` | gauge | `runtime.NumGoroutine` |

### Dataplane / apply

| Metric | Type |
|--------|------|
| `subserver_box_up` | gauge 0/1 |
| `subserver_box_uptime_seconds` | gauge |
| `subserver_apply_total` | counter labels `result=ok\|fail`, `source=` |
| `subserver_rollback_total` | counter |
| `subserver_box_restart_total` | counter |
| `subserver_apply_duration_seconds` | histogram |
| `subserver_pull_total` | counter `result=` |
| `subserver_config_revision` | gauge |

### Exposition

- Prometheus text at `GET /v1/metrics`.
- JSON summary for panels that do not scrape Prometheus ([05-api](05-api.md)).

## Health vs ready

| Probe | Meaning |
|-------|---------|
| `/v1/health` | Process alive (management up) |
| `/v1/ready` | Dataplane `Running` (safe to send users) |

Orchestrators that only check health will keep the agent supervisable even when box is down — intentional.

## Tracing

v1: not required. Leave a single `trace_id` field hook in logs if request middleware adds one later.

## Alerting suggestions (panel)

- `box_up == 0` for > 2m
- `apply_fail_total` rate spike
- `box_restart_total` increasing
- RSS step-change after deploys
- Pull `last_error` sticky
