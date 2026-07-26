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

## Pull (safety net)

Agent settings:

```yaml
pull:
  enabled: true
  url: "https://panel.example/api/nodes/edge-ams-1/desired-config"
  interval_sec: 60          # N, user-configurable
  jitter_sec: 10
  timeout_sec: 15
  headers:
    Authorization: "Bearer <panel-token>"  # or agent-specific
```

Behavior:

- Timer fires every `interval_sec ± jitter`.
- Conditional GET with `If-None-Match` / `If-None-Match: "sha256:…"` when known.
- `304` → no apply.
- `200` → supervisor `Apply` with `source=pull`.
- Transport errors → log + metric; **do not** restart box.
- Overlap with push → single mutex; loser gets consistent state via status.

Pull exists so a missed push (panel restart, network blip) self-heals within N seconds.

## Heartbeat (optional)

```yaml
heartbeat:
  enabled: true
  url: "https://panel.example/api/nodes/edge-ams-1/hello"
  interval_sec: 30
```

POST JSON snapshot of `GET /v1/status` data. Panel marks node online/offline for multi-server subscription health.

## Bootstrap via SSH

One-time (out of band):

1. Install binary + systemd unit (or Compose service).
2. Write agent config (token, node_id, pull URL).
3. Open management port only to panel IP **or** terminate TLS / SSH tunnel / WG management net.
4. Panel registers node inventory row.

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
| Token | High entropy; file 0600; rotation supported by restart or hot-reload of agentcfg |
| Bind | Default localhost; public bind needs TLS or explicit insecure flag |
| TLS | Recommended for non-localhost (`tls.cert`, `tls.key`) |
| Future | mTLS, allowlist CIDRs |

## Panel API expectations (informative)

For pull, mother should expose something equivalent to:

`GET /api/nodes/{node_id}/desired-config` → sing-box JSON + `ETag`.

Exact panel routes are owned by s-ui integration work (out of this repo’s v1 code scope; contract only).
