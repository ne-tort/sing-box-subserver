# ADR 0001 — In-process sing-box

## Status

Accepted

## Context

The agent must run sing-box on edge hosts. Options: (1) exec external `sing-box` binary and manage process; (2) embed via Go API (`singbox.New`) like panel monoliths; (3) sidecar container only.

## Decision

Embed sing-box **in-process** through the lx module API, behind `internal/box`.

## Consequences

- Pros: shared metrics/hooks, faster apply, single binary deploy, same approach proven in panels.
- Cons: build-tag coupling; bad library panic can theoretically affect process — mitigated by supervisor recover, careful apply, and process-level systemd `Restart=`.
- External binary remains a future escape hatch, not v1.
