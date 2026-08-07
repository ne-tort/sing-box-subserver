# 14 — Users sync (import / export / metrics)

Cross-node identity and traffic aggregation for controlplane users. Phase 1 is a
**star** (Flutter hub polls agents). Phase 2 peers reuse the same node APIs plus
block commits for profiles (see [13-commits](13-commits.md)).

## Vocabulary

| Term | Meaning |
|------|---------|
| **Local user** | `sync_mode=local` or empty `sync_id` — not in sync exchange |
| **Syncable user** | Has `sync_id` + `sync_mode` in `{identity,full}` + `sync_enabled` |
| **sync_id** | UUID shared across nodes; local `id` stays node-private |
| **identity mode** | Sync meta only; each node keeps own `sub_token` / `creds` |
| **full mode** | Also clone `sub_token` + `creds` |
| **ingress_bytes** | Local dataplane contribution since `traffic_epoch` (pull source for deltas) |
| **used_bytes** | Display / quota total (`traffic_used_bytes`); hub pushes **global** here |
| **epoch** | Generation counter; bumps on local ingress reset |
| **watermark** | Hub-side last consumed `ingress_bytes` for `(sync_id, node_id, epoch)` |

Local-only users on a node never appear in sync export and do not collide with
syncable users (except unique `name` on that node).

## User fields (additions)

| Field | Type | Notes |
|-------|------|-------|
| `sync_id` | string \| omit | UUID; empty → local-only |
| `sync_mode` | `local` \| `identity` \| `full` | Default `local` |
| `sync_enabled` | bool | Default true when `sync_id` set; false excludes from sync on **this** node |
| `deleted_at` | RFC3339 \| omit | Soft-delete tombstone |
| `origin` | `local` \| `import` \| `sync` | Audit |
| `revision` | uint64 | Profile meta revision (not traffic); import no-op if ≤ stored |
| `traffic_ingress_bytes` | uint64 | Local contribution counter |
| `traffic_epoch` | uint64 | Ingress generation |

Eligibility: soft-deleted (`deleted_at` set) ⇒ ineligible. Quota still uses
`traffic_used_bytes` (global after hub push).

### Soft delete

- `DELETE /users/{id}` → set `deleted_at=now`, `enabled=false` (tombstone kept).
- `DELETE /users/{id}?hard=1` → remove from `users.json` (legacy).
- Tombstones participate in export when `include_deleted=1` so hubs can propagate deletes.

### Sync disable

- Node: `PATCH` `sync_enabled=false` or `sync_mode=local`.
- Hub: own registry flag / server list. Sync runs only if **both** ends allow.

## Metrics (watermark + epoch)

Hub must **never** treat `traffic_used_bytes` as the delta source after pushing
global. Pull uses `ingress_bytes` + `epoch` only.

```
delta = max(0, ingress - watermark)   // same epoch
if epoch > stored.epoch: watermark = 0; take new epoch
global += delta; watermark = ingress
POST apply → set traffic_used_bytes = global on each node (ingress untouched)
```

Local reset / decrease of ingress ⇒ node bumps `traffic_epoch` and zeros ingress
(and traffic store subject). Hub drops watermark for that pair.

Global reset from hub ⇒ `global=0`, apply to nodes, hub watermarks=0; optional
`reset_ingress` to bump epoch on nodes.

## API

| Method | Path | Meaning |
|--------|------|---------|
| GET | `/v1/controlplane/users/export` | Bundle export |
| POST | `/v1/controlplane/users/import` | Bulk upsert by `sync_id` |
| GET | `/v1/controlplane/users/sync/metrics` | Ingress report (includes disabled) |
| POST | `/v1/controlplane/users/sync/metrics` | Apply global used / reset |
| POST | `/v1/controlplane/users/sync/membership` | Bulk enable/disable by `sync_id` |
| POST | `/v1/controlplane/users/{id}/sync` | Per-user sync toggle `{enabled, mode?}` |

### Export

`GET .../users/export?sync=1&mode=identity|full&ids=&include_deleted=0`

```json
{
  "format_version": 1,
  "node_id": "...",
  "exported_at": "...",
  "users": [ /* user objects; secrets only if mode=full */ ]
}
```

- `sync=1`: only `sync_enabled && sync_mode != local && sync_id != ""`.
- `mode=full`: include `sub_token` + `creds` (agent token required, same trust as `?secrets=1`).

### Import

```json
{
  "source": "client",
  "policy": {
    "secrets": "identity|full|keep_local",
    "on_name_conflict": "rename|reject|merge_by_sync_id",
    "apply_tombstones": true
  },
  "users": [
    {
      "sync_id": "...",
      "name": "...",
      "enabled": true,
      "sync_mode": "identity",
      "revision": 3,
      "deleted_at": null,
      "sub_token": "...",
      "creds": {}
    }
  ]
}
```

- Match existing by `sync_id` only. Missing `sync_id` → create as local (`origin=import`).
- Name collisions with a **different** user (local or another `sync_id`) never adopt/overwrite.
  `on_name_conflict=rename|merge_by_sync_id` assigns a deterministic suffix `" {n}"` (`Default` → `Default 2`).
  `reject` returns `409 cp_name_conflict`.
- `revision` ≤ stored ⇒ skip profile fields (idempotent).
- Response includes `users: [{sync_id, local_id, action}]` for hub bindings (`created|updated|skipped|tombstoned`).
- One rematerialize after the batch.

Hub watermarks key by **inventory server id** (not agent `node_id`) so reinstall does not double-count.

### Metrics GET

`GET .../users/sync/metrics?sync_ids=a,b`

```json
{
  "node_id": "...",
  "items": [
    {
      "sync_id": "...",
      "local_id": "...",
      "epoch": 1,
      "ingress_bytes": 1000,
      "used_bytes": 5000,
      "sync_enabled": true,
      "sync_mode": "identity"
    }
  ]
}
```

### Metrics POST (apply)

```json
{
  "items": [
    { "sync_id": "...", "global_used": 5000 },
    { "sync_id": "...", "reset_ingress": true },
    { "sync_id": "...", "reset_global": true }
  ]
}
```

- `global_used` → `traffic_used_bytes` only (does **not** rewrite traffic-store ingress).
- `reset_ingress` → `traffic_epoch++`, `traffic_ingress_bytes=0`, `OnTrafficReset`.
- `reset_global` → `traffic_used_bytes=0` (eligibility may change).

### Membership

```json
{ "enable": ["sync-uuid-1"], "disable": ["sync-uuid-2"] }
```

Sets `sync_enabled` for matching users.

### Per-user sync toggle

`POST /v1/controlplane/users/{id}/sync`

```json
{ "enabled": true, "mode": "identity" }
```

| `enabled` | Effect |
|-----------|--------|
| `true` | Ensure `sync_id` (generate UUID if empty); set `sync_mode` to `mode` or `identity`; `sync_enabled=true`; seed `traffic_ingress_bytes` from prior `traffic_used_bytes` if ingress was 0; bump `revision` |
| `false` | Only `sync_enabled=false`. Keep `sync_id` and `sync_mode`. **Not** a delete |

Response: redacted user including `sync_id` (hub clones into its registry on ON).

Do **not** set `sync_mode=local` to opt out after a `sync_id` exists — that hides the user from metrics ignore detection.

## Hub UI contract

Star topology: Flutter hub owns logical clients; each subserver owns a local row keyed by `sync_id`.

| UI action | Agent | Hub-only |
|-----------|-------|----------|
| Toggle ON on server user item | `POST .../users/{id}/sync` `{enabled:true}` | Upsert `SyncClient` by returned `sync_id`; bind `serverId→localId` |
| Toggle OFF on server | `POST .../sync` `{enabled:false}` or membership disable | Do **not** delete hub client; server may show as ignored |
| Hub checkbox “sync this client” | — | `SyncClient.syncEnabled` |
| Server selector warning (orange) | List/metrics: `sync_id` set + `sync_enabled=false` | Show “server ignores this client” |
| Last sync time / errors / schedule | **Not stored on agent** | Hub: `lastSyncAt`, `lastSyncError`, schedule prefs |
| Sync all / one | metrics pull → push `global_used` | Orchestrator `syncNow` |

Sync runs only when **all** hold: hub `syncEnabled`, node `sync_enabled`, and server ∈ hub `serverIds`.

Default toggle / Hub UI mode: **`full`** (clone `sub_token` + `creds` via hub vault). `identity` remains available for advanced use.

Metrics GET includes users with `sync_id` and mode ≠ `local` **even when `sync_enabled=false`** so the hub can detect ignore.

## Phase 2 (peer) — reserved

- Profile blocks: `user:<sync_id>` in [13-commits](13-commits.md) (desired body or tombstone).
- Gossip heads → fetch bodies → `POST /commits` with `source: "peer:<node_id>"`.
- Metrics stay on this channel (not LWW / not `materialize_sha256` base).
- Scoped sync token (users-sync only) — TBD.

## Flutter hub (Phase 1)

Hub store keeps `SyncClient` (syncId, mode, syncEnabled, serverBindings, per-server
`{epoch,watermark}`, `globalUsed`, plus hub-only `lastSyncAt` / `lastSyncError` / schedule).
Orchestrator: import/export profiles → pull metrics → accumulate deltas → push `global_used`.
Server toggle uses `POST /users/{id}/sync`; ignore detection uses metrics/list `sync_enabled=false`.
