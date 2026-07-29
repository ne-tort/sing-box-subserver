# Operator guides

Practical how-tos for running sing-box-subserver. These are **operator-facing**:
enable features, configure modes, copy-paste examples.

They are **not** the design/implementation ladder (FR/ADR/architecture). For that, see
[`docs/00-index.md`](../00-index.md) and module trees under `docs/controlplane/`,
`docs/traffic/`.

```mermaid
flowchart LR
  idx[guides_index]
  traffic[traffic_guides]
  idx --> traffic
```

| Guide set | Audience | Design docs (separate) |
|-----------|----------|------------------------|
| [traffic/](traffic/00-index.md) | Ops enabling accounting, shaping, quotas | [`docs/traffic/`](../traffic/00-index.md) |
