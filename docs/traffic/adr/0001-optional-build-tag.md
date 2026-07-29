# ADR 0001 — Optional build tag `with_traffic`

## Status

Accepted

## Context

Traffic accounting and shaping add trackers, disk store, and HTTP surface.
Many deploys (CI default, slim edges) do not need metering.

## Decision

1. Gate the module with Go build tag **`with_traffic`**.
2. Keep it **out** of default [`build/tags.server`](../../../build/tags.server).
3. Provide `!with_traffic` stubs so `app` wires a nil module safely.
4. Ship `build/tags.server.traffic` (+ optional `.controlplane` combo) and a CI test job.
5. Do **not** enable Clash/v2ray stats APIs ([ADR 0006](../../adr/0006-no-clash-api.md)).

## Consequences

- Pros: true opt-out; CP/subscribe/direct work without the tag.
- Cons: two/three artifacts if releases ship traffic-enabled binaries.
