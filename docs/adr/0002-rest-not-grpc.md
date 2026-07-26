# ADR 0002 — REST, not gRPC

## Status

Accepted

## Context

Control plane (s-ui) and operators need to apply config, read status, and scrape metrics. REST and gRPC were considered.

## Decision

**REST/JSON over HTTP(S)** is the primary control protocol for v1. Optional Prometheus text for metrics. gRPC is deferred.

## Consequences

- Pros: curl-friendly, easy TLS termination, matches panel habits, low tooling cost.
- Cons: weaker native streaming; logs use poll/ring instead of bidirectional stream.
- Revisit gRPC only if continuous high-frequency streams become a product requirement.
