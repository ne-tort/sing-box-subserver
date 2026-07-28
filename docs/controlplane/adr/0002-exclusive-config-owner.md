# ADR 0002 — Exclusive config owner with controlplane mode

## Status

Accepted

## Context

Module-level restatement of [root ADR 0008](../../adr/0008-exclusive-config-owner.md).
Controlplane materialize must not race with subscribe or PUT config.

## Decision

1. `config_mode` value **`controlplane`** is a first-class exclusive owner.
2. First activate set → `Claim(controlplane)` → cancel subscribe → Apply `source=controlplane`. Further activates add to `active_sets` and rematerialize.
3. `PUT /v1/config` / successful subscribe enable → deactivate CP (`active_sets` cleared via leave hook; users/sets **remain on disk**).
4. No merging of CP JSON with external desired-config.
5. Implementation uses the same shared owner registry as root status/heartbeat; persist mode only in `config-owner.json`.
6. Many sets may be active on different ports; materialize merges them.

## Consequences

- Pros: predictable standalone vs panel-managed operation.
- Cons: switching modes requires explicit operator action; last-good may still be an old
  CP config when mode becomes `idle` until replaced.
