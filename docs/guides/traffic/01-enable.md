# 01 — Enable traffic module

## Build

Traffic is **opt-in**. Default `build/tags.server` does **not** include it.

| Goal | Tags file (typical) |
|------|---------------------|
| Traffic only (subscribe / static) | `build/tags.server.traffic` |
| Traffic + controlplane | `build/tags.server.traffic.controlplane` |

```bash
TAGS=$(tr -d '\r\n' < build/tags.server.traffic.controlplane)
go build -tags "$TAGS" -o dist/subserver ./cmd/subserver
```

Without the tag, `/v1/traffic/*` returns **404** (routes not registered).

## Agent YAML

```yaml
node_id: "edge-1"
token: "<agent-bearer>"
listen: "0.0.0.0:8080"
data_dir: "/var/lib/subserver"

traffic:
  flush_interval_sec: 10   # how often live counters hit disk / onlines window
  retention_days: 30       # JSONL series retention
  allow_inject: false      # NEVER true in production (lab/smoke only)
```

Store layout under `data_dir/traffic/`: cumulative counters, subjects, daily JSONL.

## Verify

```bash
curl -fsS -H "Authorization: Bearer $TOKEN" \
  https://127.0.0.1:8080/v1/traffic/status
```

Expect `"enabled": true`, `flush_interval_sec`, `retention_days`.

## Related

- Modes: [02-modes.md](02-modes.md)
- Design: [`docs/traffic/adr/0001`](../../traffic/adr/0001-optional-build-tag.md)
