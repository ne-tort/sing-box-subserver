# 06 — Control plane integration

## Independence rule

The agent compiles and runs without s-ui sources. Integration is **wire contracts only**: HTTP + JSON + bearer token + optional pull URL.

Mother panel (s-ui) is the **expected** controller, not a library dependency ([ADR 0004](adr/0004-independent-of-sui.md)).

## Identity

Agent settings:

```yaml
node_id: "edge-ams-1"          # stable, unique in panel inventory
token: "..."                   # shared secret with panel
listen: "127.0.0.1:8080"
data_dir: "/var/lib/subserver"
```

On status/heartbeat the agent reports:

- `node_id`, `agent_version`, `agent_commit`, `singbox_version`, `singbox_commit`, `build_tags`
- management listen address
- current `revision` / `content_sha256` / `state`

Control plane uses version fields to decide whether to **externally** replace the binary/image and restart ([ADR 0005](adr/0005-external-updates-only.md)). The agent never downloads or overwrites itself.

## Push (primary)

Control plane generates **server-side** sing-box JSON for that node and:

`PUT https://node-mgmt/v1/config` with Bearer token.

Recommended panel flow:

1. Save inbounds/endpoints for node.
2. Render server JSON (panel responsibility).
3. PUT to agent; on failure show agent `error` details; do not assume dataplane changed.
4. Refresh node card from `GET /v1/status`.

## Pull (safety net / subscribe)

Optional HTTP GET of server JSON from **any** URL (not s-ui-specific).

```yaml
pull:
  enabled: true
  url: "https://example.com/subserver/edge-ams-1"
  interval_sec: 60
  jitter_sec: 10
  timeout_sec: 15
```

Runtime: `POST|DELETE /v1/subscribe` (alias `/v1/pull`).  
YAML seeds only when `subscribe-state.json` is absent. See [08-pull-heartbeat.md](08-pull-heartbeat.md).

Behavior:

- Timer every `interval_sec ± jitter`.
- `If-None-Match` / `304`, plus **local SHA dedupe** before Apply (no useless core restart).
- Optional encrypted envelope (`aes-256-gcm` + agent token).
- `PUT /v1/config` cancels subscribe so push is not overwritten.

## Heartbeat (optional)

```yaml
heartbeat:
  enabled: true
  url: "https://example.com/hooks/status"
  interval_sec: 30
```

Runtime: `PUT|DELETE /v1/heartbeat`. Same seed-once persistence rule as pull.
Payload: status snapshot + `inbounds_count` (not Clash online users).

## Bootstrap via SSH

One-shot **panel-owned** provisioning (not agent self-update). Design:
[ADR 0007](adr/0007-panel-owned-ssh-bootstrap.md), mother doc `docs/EDGE_SSH_BOOTSTRAP.md`.

Operator checklist (manual until panel InstallJob exists):

1. Install binary + systemd unit (or Compose service with `network_mode: host`, `restart: unless-stopped`).
2. Write agent config (token, node_id, pull URL).
3. Open management port only to panel IP **or** terminate TLS / SSH tunnel / WG management net.
4. Panel registers node inventory row; prefer `POST /v1/auth/tokens` then bootstrap disable.

Ongoing ops: API only.

## Agent binary / image upgrades

Not an agent API. Operator or panel bootstrap:

- **systemd:** replace binary under `ExecStart`, `systemctl restart subserver`.
- **Docker/Compose:** new image tag + recreate container.
- Artifacts from GitHub Releases (checksums); agent only reports versions so the plane knows when upgrade is due.

## Multi-server client subscriptions (panel side)

Not implemented in the agent. Panel should:

- treat each agent as a **publish target** for server config;
- build client subscriptions by merging per-node outbounds/endpoints / share URIs using inventory public hostnames;
- exclude nodes with `ready=false` / heartbeat stale if desired.

## Security

| Control | Requirement |
|---------|-------------|
| Token | High entropy; YAML bootstrap + managed tokens in `data_dir/credentials.json` (0600) |
| Rotation | Hot via `POST /v1/auth/rotate` / CRUD `/v1/auth/tokens` — no agent restart |
| Bind | Default localhost; public bind needs TLS or explicit insecure flag |
| TLS | Recommended for non-localhost (`tls.cert`, `tls.key`) |
| Future | mTLS, allowlist CIDRs |

## Panel API expectations (informative)

For pull, mother should expose something equivalent to:

`GET /api/edge/agent/{node_id}/desired-config` → sing-box JSON + `ETag` (Bearer = node agent token).

`POST /api/edge/agent/{node_id}/hello` → accept status snapshot.

Exact panel routes are owned by s-ui (`internal/edge`).
