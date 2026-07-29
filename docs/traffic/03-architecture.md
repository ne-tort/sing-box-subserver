# 03 — Architecture

```mermaid
flowchart TB
  subgraph trafficMod [internal_traffic]
    Hook[box.TrafficHook]
    ST[StatsTracker]
    RL[RateLimitTracker]
    Svc[Service flush]
    Store[(traffic store)]
    Hook --> ST
    Hook --> RL
    ST --> Svc --> Store
  end
  App[app.Run] --> Hook
  Sup[supervisor.Apply] --> Hook
  CP[cpbridge] --> Svc
  API[portal HTTP] --> Svc
```

## Box integration

On `box.Engine.Start`:

1. `singbox.New`
2. `router.AppendTracker` for module trackers (before Start)
3. `box.Start`
4. `Hook.OnBoxStarted`

On stop / Apply swap: flush → `OnBoxStopped` → close instance.

## Package layout

| Path | Role |
|------|------|
| `internal/traffic` | Module entry, Hook, stubs |
| `internal/traffic/domain` | Subject, Sample, Limits |
| `internal/traffic/tracker` | Stats + rate limit trackers |
| `internal/traffic/store` | Cumulative + JSONL series |
| `internal/traffic/service` | Flush, manifest, query |
| `internal/traffic/portal` | HTTP routes |
| `internal/traffic/cpbridge` | CP consumer (`with_traffic && with_controlplane`) |

## Capability matrix

| Mode | Accounting | Shaping |
|------|------------|---------|
| controlplane | subject + inbound/outbound | per-user speed |
| subscribed/direct | auto-discover user + inbound | API limits / inbound caps |
| idle | module idle | n/a |
