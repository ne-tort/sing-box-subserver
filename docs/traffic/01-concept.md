# 01 — Concept

## Problem

Edge agents need per-user (and per-inbound) byte accounting and bandwidth
shaping without depending on Clash/v2ray experimental APIs ([ADR 0006](../adr/0006-no-clash-api.md)).
s-ui already does this via router `ConnectionTracker`s; subserver ports the same
idea as an **isolated optional module**.

## Layers

1. **Traffic module** (`internal/traffic`) — sing-box-only: counters, rate limits,
   time-series store. No knowledge of CP users / Client model.
2. **Consumers** — controlplane bridge, subscribe/direct ops, management API.
   They register **subjects**, poll usage, set limits, enforce quotas.

## Subjects

A **subject** is a consumer-defined identity with one or more **dataplane keys**
(`metadata.User` values emitted by inbounds). Example:

- subject `cp:user:{id}` → keys `alice`, `alice-flow-none`, `alice-flow-xtls-rprx-vision`
  (VLESS variants share one quota).

Without a manifest, the module still records raw `dataplane_user`, `inbound`,
and `outbound` series.

## Shaping

Per-dataplane-key token bucket (bytes/sec), same semantics as s-ui
`RateLimitTracker` (1 MiB burst). Empty `metadata.User` → no per-user shaping
(inbound aggregate accounting only).

WireGuard peer Mbps / IpcGet path is Phase 3+.

## Modes

| Mode | Accounting | Shaping |
|------|------------|---------|
| controlplane + traffic | per CP user via bridge | per user speed fields |
| subscribed / direct | inbound (+ user if present) | via `/v1/traffic/limits` or inbound caps |
| idle / no traffic tag | n/a | n/a |
