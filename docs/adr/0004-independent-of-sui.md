# ADR 0004 — Independent of s-ui

## Status

Accepted

## Context

The agent will be managed by s-ui, and lives as a submodule under the s-ui monorepo for convenience. That must not create a compile-time or module dependency on panel code.

## Decision

- Separate Go module / repository (`ne-tort/sing-box-subserver`).
- Contracts with the panel: HTTP API, bearer token, pull URL, node identity.
- No imports of `github.com/alireza0/s-ui/...`.
- Docs may mention s-ui as the reference control plane.

## Consequences

- Pros: reusable by other controllers; clearer ownership; cleaner CI.
- Cons: panel integration is a separate change set in s-ui; duplicate types avoided by JSON contracts only.
