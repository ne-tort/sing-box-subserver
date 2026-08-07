# 13 — Block commits (async apply)

Lightweight desired-state commits for controlplane configure. One commit = one rematerialize.
Content hashes per block are the sync primitive for a future multi-subserver mesh.

## Vocabulary

| Term | Meaning |
|------|---------|
| **Block** | Atomic desired-state unit (`demux`, `preset:<tag>`, `ssl:<id>`, `reality`, `wg`, `dns`, `route`, `outbounds`) |
| **Head** | Current `sha256` of a block’s canonical JSON on the agent |
| **Commit** | Set of block bodies + meta; accepted quickly, applied once in the background |
| **Materialize SHA** | Existing `last_materialize_sha256` after successful dataplane Apply |

Not a git DAG: no parent chain, no CRDT. Last-writer-wins with optional `base` If-Match. Ring of ~32 recent commits for poll/debug.

## Block IDs

| ID | Body (canonical) |
|----|------------------|
| `demux` | Demux install intent (`group`, `name`, `listen_port`, `slot_presets`, …) or `{ "deleted": true }` |
| `preset:<tag>` | Single inbound set (`name`, `preset`, `listen_port`, `params`, variants) or tombstone |
| `ssl:<id>` | SSL profile put body or tombstone |
| `reality` | `{ "profiles": [...] }` |
| `wg` | WG hub put body |
| `dns` / `route` / `outbounds` | Fragment put body or `{ "deleted": true }` |
| `user:<sync_id>` | **Reserved (Phase 1.5+):** syncable user profile body or tombstone — see [14-users-sync](14-users-sync.md). Not applied by commits yet; use users import API. |

Traffic/metrics are **not** commit blocks (watermark channel, not LWW).

**Content hash:** SHA-256 of canonical JSON (`encoding/json` map key sort). Client may send `sha256`; server recomputes and rejects mismatch (`400 bad_block_hash`).

## Storage

Under `data_dir/controlplane/`:

```
heads.json
commits/<id>.json
commits/_meta.json   # { "pending_id"?, "recent": ["id", ...] }
```

`heads.json`:

```json
{
  "blocks": {
    "demux": { "sha256": "...", "updated_at": "..." }
  },
  "materialize_sha256": "..."
}
```

## API

| Method | Path | Meaning |
|--------|------|---------|
| GET | `/v1/controlplane/heads` | Block heads + `materialize_sha256` + `pending_commit_id?` |
| POST | `/v1/controlplane/commits` | Accept dirty blocks → **202** |
| GET | `/v1/controlplane/commits/{id}` | Commit status |
| GET | `/v1/controlplane/commits?limit=20` | Recent commits |

### POST `/commits`

```json
{
  "source": "client",
  "base": {
    "materialize_sha256": "optional",
    "blocks": { "demux": "optional-head-sha" }
  },
  "blocks": {
    "demux": { "sha256": "optional", "body": { } }
  }
}
```

Algorithm:

1. Validate + recompute block hashes.
2. If `base` present and mismatches current heads / materialize → **409** `commit_conflict` + current heads.
3. If another commit is `pending`/`applying` → **409** `apply_in_progress`.
4. Persist block bodies into existing stores (sets, fragments, ssl, …); update heads.
5. Write commit `accepted`, set `pending_id`, respond **202** `{ id, status, block_shas }`.
6. Background: one `rematerializeForce`; then `applied` or `failed` (last-good box kept on fail; desired store remains).

Statuses: `accepted` → `applying` → `applied` | `failed` | `conflict`.

### Status projection

`GET /v1/controlplane/status` includes:

```json
"commit": {
  "pending_id": null,
  "heads_digest": "sha256 of sorted block id:sha pairs",
  "materialize_sha256": "..."
}
```

## Relation to legacy writes

`PUT` / `from-presets` / `from-demux-group` remain for CLI/bootstrap. Configure UI Send uses **only** commits.

## Future multi-subserver

1. Gossip `GET /heads`.
2. Fetch missing block bodies by content sha (endpoint TBD).
3. `POST /commits` with `source: "peer:<id>"` and `base` = local heads.
4. Conflict policy (LWW / primary / UI merge) — not defined here.
5. User profiles: `user:<sync_id>` blocks; metrics via [14-users-sync](14-users-sync.md) peer exchange (not materialize base).
