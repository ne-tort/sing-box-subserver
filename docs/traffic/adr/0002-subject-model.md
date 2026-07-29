# ADR 0002 — Universal subject model

## Status

Accepted

## Context

s-ui keys stats by `Client.Name` (= `metadata.User`). Subserver has controlplane
users (with VLESS variant name suffixes), pushed configs with arbitrary user
strings, and inbound-only anonymous traffic.

## Decision

1. The traffic module records **dataplane observations** (`inbound`, `outbound`,
   `dataplane_user`) independently of any product identity.
2. Consumers register **subjects** with `subject_id` + `dataplane_keys[]`.
3. Subject cumulative usage = sum of counters for all `dataplane_keys`.
4. Shaping limits are applied per **dataplane_key** (bridge duplicates limits
   across a user's variant keys).
5. Controlplane is a consumer (`cpbridge`), not part of the core module.

## Consequences

- Pros: reusable for CP / subscribe / direct; variant aggregation is explicit.
- Cons: consumers must keep manifests in sync with materialize/config.
