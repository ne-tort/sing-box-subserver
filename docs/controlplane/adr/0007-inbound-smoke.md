# ADR 0007 — Inbound smoke via ephemeral client-box

## Status

Accepted

## Context

Operators need a one-shot check that active inbound sets accept traffic with the same credentials/outbounds that clients get from subscriptions. Options considered:

1. **Always-on proxy outbounds** in the live server config — idle sockets are cheap, but without Clash API there is no on-demand delay endpoint; urltest-interval would wake the box continuously.
2. **Inject outbounds into live dataplane for the test, then rematerialize without them** — `Supervisor.Apply` restarts the whole box (Close+Start), drops user sessions, and races activate/TLS/WG (`ErrConflict`).
3. **Ephemeral client-box** — second in-process sing-box started only for the smoke run; live dataplane untouched.

Clash API remains forbidden ([root ADR 0006](../../adr/0006-no-clash-api.md)).

## Decision

1. Expose `POST /v1/controlplane/smoke` (mgmt auth). One run at a time (`409` if busy).
2. Probe credentials: prefer an enabled non-system user already present on live inbounds (no Apply). Reserved `__cp_smoke__` is created only when no such user exists, then rematerialize once so its UUID is accepted.
3. Use `RenderSubscription(probeUser, activeSets, …)` so outbounds match client SoT.
4. Rewrite outbound `server` → `127.0.0.1` (hairpin); keep TLS/Reality SNI and `server_port` from the active set.
5. Start an ephemeral box with a SOCKS/mixed inbound per outbound + route; probe HTTP(S) URLs with short timeout and URL fallbacks (incl. IP-literal); then Close the box.
6. Skip presets that cannot hairpin (`inbound_only`, carrier/cloudflared, WireGuard, missing outbound template).

## Consequences

- Idle load on the VPS outside a run: **0**.
- Live inbounds keep accepting external traffic during the test (ephemeral box is separate).
- Smoke does **not** inject proxy outbounds into the live config. Rematerialize happens only when probing requires a newly created `__cp_smoke__` user (or refreshed probe-user creds).
- Flutter/debug UI calls the API; this is not the device-side `Core.UrlTest`.
- Last completed report is persisted on the agent as `data_dir/controlplane/smoke_last.json` (atomic overwrite, no TTL/history). Clients read `GET /v1/controlplane/smoke/last` and optional `smoke` fields on `GET /sets`.

## Related

- Package `internal/controlplane/smoke`
- Subscription materialize: [07-subscriptions](../07-subscriptions.md)
