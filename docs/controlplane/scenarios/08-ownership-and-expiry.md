# Scenario 08 — Ownership, expiry, handoff

These flows are specified in API/docs; live coverage is thinner than install e2e.

## User expiry

1. `PATCH /v1/controlplane/users/{id}` `{ "expires_at": "<past>" }`
2. After tick / rematerialize: user omitted from inbound users.
3. `GET /v1/sub/{token}` → `403` `cp_user_ineligible`

**Coverage:** unit / eligibility tests under `internal/controlplane/` (`service_eligibility_test.go`).

## Panel / subscribe takes over

1. While CP active, `PUT /v1/config` (panel JSON) → `config_mode=direct`; CP `active_sets` cleared; users/sets files remain.
2. Or `POST /v1/subscribe` → `config_mode=subscribed`.
3. Later `POST …/sets/{name}/activate` → Claim(controlplane) again.

**Coverage:** ownership / claim unit tests; see root ADR 0008.

## Deactivate last set

1. Deactivate every active set → ownership best-effort `idle`.
2. Last-good listeners may still serve previous JSON until another owner Applies.
3. `ready.context` returns to `idle`; ports/availability reflect CP sets file, not necessarily lingering OS listeners.

## DNS / route templates

`GET/PUT /v1/controlplane/config/dns|route` — non-owner may get `200` with `rematerialized:false`.

**Tests:** `test_02_dns_route.py`

## API refs

[../api/07-errors-and-contracts.md](../api/07-errors-and-contracts.md) · [../03-architecture.md](../03-architecture.md)
