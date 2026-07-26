# ADR 0006 — No Clash API on the edge agent

## Status

Accepted

## Context

sing-box-lx ships an experimental Clash Meta–compatible HTTP API (`experimental.clash_api`, build tag `with_clash_api`) for client dashboards (Yacd): Selector switching, live connections/traffic, rules, optional `/ui`.

The edge agent already exposes a management REST surface (`/v1`) with Bearer auth for push/pull config, status, logs, and metrics. Typical server configs are inbounds + WireGuard endpoints without Selector/URLTest groups.

## Decision

**Do not enable or integrate Clash API** in sing-box-subserver v1:

1. Omit `with_clash_api` from [`build/tags.server`](../../build/tags.server).
2. Reject desired configs that set a non-empty `experimental.clash_api.external_controller` at validate/apply time (`422` / typed unsupported).
3. Do not document Clash as a supported ops interface.

Rationale: negligible value on server-only profiles; second control plane with a different auth model (`secret`) and larger attack surface; binary size and tag policy already exclude client/UI features.

## Consequences

- Pros: one management contract; smaller binary; no accidental public Clash controller on edge.
- Cons: operators cannot attach Yacd to the agent dataplane. If that becomes an explicit product need later, revisit as an opt-in build profile (loopback + mandatory secret) — not the default server tag set.

## Related

- [ADR 0002](0002-rest-not-grpc.md) — REST is the control protocol.
- [08-build-and-ci](../08-build-and-ci.md) — slim server tags.
