# ADR 0003 — JSON files under `data_dir/controlplane`

## Status

Accepted

## Context

Need durable users/sets/state with minimal dependencies, easy backup/replace, and
transparency for operators debugging edge nodes.

## Decision

1. Store module state as JSON files in `data_dir/controlplane/`
   (`users.json`, `sets.json`, `state.json`).
2. Atomic write via temp + rename; file mode `0600`.
3. Preset catalog embedded in the binary (not in data_dir) for v1.
4. No SQLite/Bolt in v1.

## Consequences

- Pros: inspectable, scriptable, aligns with agent configstore style.
- Cons: concurrent writers must be serialized in-process (API mutex); large user counts
  may need a different store later (migrate via versioned docs).
