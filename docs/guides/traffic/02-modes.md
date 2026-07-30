# 02 — Modes and who owns config

Traffic metering attaches to whatever dataplane config is running. Config ownership
is exclusive (subscribe **or** direct PUT **or** controlplane) — see agent ADR
on exclusive config owner.

| Mode | How config arrives | Who sets shaping | Who sets quota / expiry |
|------|--------------------|------------------|-------------------------|
| **controlplane** | Materialize from local users/sets | CP user `speed_*` (+ optional manual PUT) | CP `traffic_limit_bytes`, `expires_at` |
| **subscribe** | Pull JSON from panel URL | Manual `PUT /v1/traffic/limits` | Not in traffic module (panel policy) |
| **static / direct** | `PUT /v1/config` | Manual `PUT /v1/traffic/limits` | Not in traffic module |

## Controlplane

1. Build with `with_traffic` **and** `with_controlplane`.
2. Create users / activate sets as usual.
3. Set per-user `speed_up_bytes_per_sec` / `speed_down_bytes_per_sec` via PATCH.
4. Optional: ops override with `PUT /v1/traffic/limits` (exact dataplane keys — [04](04-shaping.md)).
5. Bridge registers subjects `cp:user:{id}` and syncs `traffic_used_bytes`.

## Subscribe

1. Build with `with_traffic` (CP optional/off).
2. `POST /v1/subscribe` with panel URL returning sing-box JSON.
3. Users in inbound `users[].name` become `metadata.User` → use those names in limits.
4. `DELETE /v1/subscribe` cancels ownership.

Stub panel for labs: `testdata/docker/mock_panel.py` + `configs/vless-multi.json`.

## Static

1. `PUT /v1/config` with full sing-box JSON (claims direct ownership; cancels subscribe).
2. Same manual limits / stats API as subscribe.

## What traffic does **not** change

- It does not choose the config owner.
- It does not invent usernames: keys come from inbound `metadata.User`.
- WireGuard peer shaping: CP `speed_*` → materialize `peers[].up_mbps/down_mbps` (integer Mbps).
