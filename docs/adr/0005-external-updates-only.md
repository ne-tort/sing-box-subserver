# ADR 0005 — External updates only (no in-process self-update)

## Status

Accepted

## Context

Edge agents need upgrades (new agent binary, new sing-box-lx pin). Options: (1) agent downloads and replaces its own binary; (2) external orchestration (systemd/Docker/scripts/panel deploy) replaces the artifact and restarts the process.

## Decision

**No self-update inside the agent process.** Binary/image updates are performed **only by external tooling**:

- systemd hosts: replace file + `systemctl restart subserver` (or equivalent);
- Docker/Compose: new image tag + recreate;
- SSH/bootstrap scripts and mother-panel deploy jobs consume GitHub Releases.

The agent **reports** versions so the control plane can decide when an upgrade is required:

- `agent_version`, `agent_commit` (ldflags)
- `singbox_version` (from lx `constant.Version` / build)
- `singbox_commit` (pinned lx submodule short SHA, ldflags)
- `build_tags`

## Consequences

- Pros: same model for bare metal and containers; no half-written inode mid-traffic; clearer supply chain (CI signs releases); no need for write permission to the running binary.
- Cons: panel cannot “click upgrade” without a separate deploy channel — acceptable; heartbeat/status still drives upgrade UX.
- Out of scope forever for v1: HTTP endpoint that downloads and execs a new binary.
