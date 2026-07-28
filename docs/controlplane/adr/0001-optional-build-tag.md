# ADR 0001 — Optional build tag `with_controlplane`

## Status

Accepted

## Context

The embedded controlplane adds users, presets, HTTP routes, and an expiry loop. Many
deployments only need push/pull from an external panel and should keep a slimmer binary
and narrower attack surface (no public `/v1/sub/{token}`).

## Decision

1. Gate the module with Go build tag **`with_controlplane`**.
2. Keep it **out** of default [`build/tags.server`](../../../build/tags.server).
3. Provide `//go:build !with_controlplane` stubs so `app` wires a nil service safely.
4. CI: default jobs unchanged; add an explicit job/tags file with the tag
   ([09-build-and-ci](../09-build-and-ci.md)).

## Consequences

- Pros: true opt-out of link/compile; matches demux/xhttp optional patterns.
- Cons: two artifacts if releases ship both; docs must state which binary includes CP.
