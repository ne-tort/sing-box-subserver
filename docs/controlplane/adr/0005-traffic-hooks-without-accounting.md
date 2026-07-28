# ADR 0005 — Traffic hooks without accounting

## Status

Accepted

## Context

Local users need expiry and future traffic limits so a later accounting module can
attach without redesigning user records or materialize. Real counters, probes, and
billing are not available yet.

## Decision

1. User fields: `expires_at`, `traffic_limit_bytes`, `traffic_used_bytes`,
   `traffic_reset_at`, `traffic_reset_period_sec` (all optional except used defaulting to 0).
2. Eligibility omits ineligible users from materialize and rejects sub fetch.
3. Expiry/reset ticker in controlplane performs rematerialize + Apply when eligibility
   changes.
4. **No** packet/byte metering in this module; `traffic_used_bytes` is only updated by
   future module or explicit admin PATCH.
5. When used ≥ limit, behave as expired for dataplane membership until reset/clear.

## Consequences

- Pros: stable extension point; hotreload behavior defined early.
- Cons: without the future module, traffic limits only work if something updates
  `traffic_used_bytes` (or operators PATCH it).
