# ADR 0004 — User-token subscription URLs

## Status

Accepted

## Context

End clients need to fetch outbounds without holding the agent management Bearer (which
can rotate credentials, stop the box, etc.).

## Decision

1. Public endpoint `GET /v1/sub/{token}` authenticated solely by the path token.
2. Default response format: sing-box JSON with an `outbounds` array.
3. Token is per-user (`sub_token`), rotatable; unknown token → `404`.
4. Ineligible user → `403` with `cp_user_ineligible`.
5. v1 requires an active inbound set to render (`cp_no_active_set` otherwise).

## Consequences

- Pros: simple client UX; clear auth split from ops Bearer.
- Cons: management listener must be reachable (and preferably TLS) by clients; token
  leakage equals subscription access until rotate.
