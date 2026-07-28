# 08 — Pull / Heartbeat / Subscribe contracts

## Independence

Subserver does **not** know about s-ui. Pull and heartbeat are optional HTTP endpoints
with a documented JSON contract. Any controller may implement them.

`agent.yaml` (and install env that writes it) is **seed only**. After first configure
(REST or successful YAML seed), state lives in `data_dir` and survives restart.
YAML/env must **not** re-enable a REST-disabled pull/heartbeat on process restart.

## Defaults

| Setting | Default |
|---------|---------|
| pull / subscribe interval | 60s |
| pull jitter | 10s (when enabled from YAML) |
| pull timeout | 15s |
| heartbeat interval | 30s |
| heartbeat timeout | 10s |

## Subscribe / Pull (same manager)

Runtime control (persisted):

| Method | Path | Effect |
|--------|------|--------|
| GET | `/v1/subscribe` or `/v1/pull` | status |
| POST/PUT | `/v1/subscribe` or `/v1/pull` | enable + immediate fetch |
| DELETE | `/v1/subscribe` or `/v1/pull` | disable; **Present=true** so YAML cannot reseed |
| POST | `/v1/subscribe/refresh` or `/v1/pull/refresh` | one fetch |

Fetch behavior:

1. Conditional GET (`If-None-Match: "sha256:<current>"`) when known.
2. `304` → no apply.
3. `200` → optional decrypt → **local SHA compare** with current content → skip Apply if equal.
4. Else Apply (`source=subscribe`). Supervisor also no-ops identical SHA when box is running.

### Body formats

- **Plain:** sing-box server JSON.
- **Optional encrypted envelope** (opaque URL / shared hosting):

```json
{
  "alg": "aes-256-gcm",
  "ciphertext": "<base64(nonce||ciphertext||tag)>"
}
```

Key = `SHA-256(agent token)`. Spec flag `decrypt_body: true` requires envelope;
otherwise envelopes are auto-detected.

### Identification (controller side)

Recommended URL shape for multi-tenant controllers:

`/subserver/{node_id}` or `/api/edge/agent/{node_id}/desired-config`

Auth: `Authorization: Bearer <token>` (agent token or managed panel token).
Identity is **path id + bearer**, not “I am s-ui”.

Security options:

1. HTTPS + bearer (baseline).
2. Optional encrypted body so raw logs/CDN cannot read config.
3. Short-lived signed URLs (controller concern; agent just GETs).

## Heartbeat

| Method | Path | Effect |
|--------|------|--------|
| GET | `/v1/heartbeat` | status |
| PUT | `/v1/heartbeat` | configure URL/intervals (`enabled` optional, default true) |
| DELETE | `/v1/heartbeat` | disable; persist Present |

POST body to heartbeat URL (agent → controller) includes:

- identity/versions (`node_id`, agent/sing-box versions, listen)
- box state (`state`, `revision`, `content_sha256`, `box_up`, uptime)
- `pull` / `subscribe` summaries
- `config_mode` — **same enum as** `GET /v1/status`: `idle` \| `subscribed` \| `direct` \| `controlplane`
  ([ADR 0008](adr/0008-exclusive-config-owner.md)). Do **not** emit `direct_or_boot`.
- `inbounds_count` from last-good JSON

`online_users` is **not** reported (no Clash API — ADR 0006). Add later only via a
safe sing-box stats path if one exists.

### Mode vs subscribe schedule

Subscribe enable/disable flips ownership per the transition table in ADR 0008.
Heartbeat must read `config_mode` from the shared owner registry, not re-derive it
from `pull.enabled` alone (that misses `direct` and `controlplane`).

## Bootstrap env (install scripts only)

Go binary does **not** read these. Installer may write them once into `agent.yaml`:

| Env | Meaning |
|-----|---------|
| `SUBSERVER_NODE_ID` / `SUBSERVER_TOKEN` | required for fresh agent.yaml |
| `SUBSERVER_PULL_URL` | optional seed pull URL |
| `SUBSERVER_PULL_INTERVAL_SEC` | default 60 |
| `SUBSERVER_HEARTBEAT_URL` | optional seed heartbeat URL |
| `SUBSERVER_HEARTBEAT_INTERVAL_SEC` | default 30 |
| `SUBSERVER_CONTROLLER_URL` | optional: if set and pull/hb URLs empty, expands default REST paths under that base |

After bootstrap, use REST to change pull/heartbeat. Re-running install with `RESET_MODE=fresh`
wipes data_dir and reseeds YAML.

## Persistence files

| File | Owner |
|------|-------|
| `data_dir/subscribe-state.json` | subscribe/pull (`present`, `enabled`, `spec`) |
| `data_dir/heartbeat-state.json` | heartbeat |
| `data_dir/controlplane/` | optional embedded CP ([controlplane/08-storage](controlplane/08-storage.md)) |
| `agent.yaml` | identity/listen/TLS + **first-boot seed only** for pull/hb |
