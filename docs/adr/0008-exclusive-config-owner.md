# ADR 0008 — Exclusive config owner (`config_mode`)

## Status

Accepted

## Context

Desired server JSON can arrive from several writers: `PUT /v1/config` (direct push),
scheduled subscribe/pull, boot of last-good, and (optional) the embedded
[`controlplane`](../controlplane/00-index.md) module.

Historically `config_mode` was **inferred** differently in places:

- Management status mixed `subscribe.Mode` with a `LastApply.Source == push` override.
- Heartbeat reported `direct_or_boot` — a third vocabulary not used by the API.
- Apply `Source` values (`push` / `pull` / `subscribe` / `boot`) did not map 1:1 to mode.
- Unsubscribe cancelled the schedule but did not define ownership of the running last-good.

That made mode transitions hard to reason about and unsafe once a fourth writer
(`controlplane`) materializes configs locally.

## Decision

1. **One exclusive owner** of the dataplane desired-config writer at a time.
   Normative enum for `config_mode` (status, heartbeat, API responses):

   | Value | Writer |
   |-------|--------|
   | `idle` | No scheduled/remote/local writer owns updates; box may still serve last-good from disk |
   | `subscribed` | Subscribe/pull manager |
   | `direct` | External `PUT /v1/config` |
   | `controlplane` | Optional embedded module (`with_controlplane`) |

2. **Boot is not a mode.** `BootLastGood` may start the box from disk without changing
   owner. After boot, `config_mode` remains whatever was persisted (or `idle` if none).
   Do **not** expose `direct_or_boot`.

3. **Single source of truth.** A small owner registry (package-level helper owned by
   `app` / supervisor wiring) is the only producer of `config_mode` for
   `GET /v1/status`, heartbeat payloads, and mode fields on mutate responses.
   Call sites must not re-derive mode from `LastApply.Source` heuristics.

4. **Transition side-effects are explicit** (full replace ownership, no half-states):

   | Event | New mode | Side effects |
   |-------|----------|--------------|
   | Successful `PUT /v1/config` | `direct` | Cancel subscribe schedule; deactivate controlplane ownership (clear active set apply flag; stop CP materialize loop ownership) |
   | Successful `POST /v1/subscribe` (enabled) | `subscribed` | Deactivate controlplane ownership |
   | Successful subscribe `DELETE` (disable) | `idle` **only if** previous mode was `subscribed` | Schedule off; last-good **unchanged**. If mode was `direct`/`controlplane`, DELETE only disables the schedule and does **not** Claim(idle). |
| Successful controlplane set `activate` (first) | `controlplane` | Cancel subscribe; subsequent materialize Apply uses `source=controlplane` |
| Controlplane deactivate (no active sets left) | `idle` | Stop CP Apply ownership; last-good **unchanged** |

Persist `config_mode` in `data_dir/config-owner.json` (single source). Controlplane `state.json` stores `active_sets[]` only — not a second owner flag.

5. **Last-good disk is shared; writer is not.** Any owner applies a **full** sing-box
   JSON via `supervisor.Apply` (same pipeline). Owners do not merge into each other's
   configs. Persisted Apply meta `source` should align with the owner that wrote
   (`push` ↔ direct, `subscribe` ↔ subscribed, `controlplane` ↔ controlplane, `boot` only for boot path).

6. **Predictability over compatibility shims.** Prefer rejecting conflicting concurrent
   writers (`409`) over silent dual schedules. YAML pull seed remains seed-once into
   subscribe state; it does not invent a separate mode.

## Consequences

- Pros: transparent mode matrix; safe addition of embedded controlplane; identical
  vocabulary in API and heartbeat; easier tests (table-driven transitions).
- Cons: implementation must refactor current heuristic `config_mode` in API/heartbeat
  (docs land first; code follows). Controllers that parsed `direct_or_boot` must use
  `direct` or `idle` + `box_up` / revision instead.

## Related

- [05-api](../05-api.md) — status `config_mode`
- [08-pull-heartbeat](../08-pull-heartbeat.md)
- [controlplane/03-architecture](../controlplane/03-architecture.md)
- [ADR 0003](0003-last-good-config.md) — shared last-good store
- [ADR 0004](0004-independent-of-sui.md) — external CP vs optional embedded module
