# ADR 0007: Panel-owned SSH bootstrap; agent stays control-plane only

- Status: Accepted (design)
- Date: 2026-07-27

## Context

The agent already exposes REST for config apply, subscribe, and credential rotation.
Operators need a panel-driven install: add SSH host → install Docker Compose subserver
(host network, restart policy) → panel creates and stores credentials.

Separately, TLS for client-facing inbounds must not be confused with management-API TLS
or with panel web TLS.

## Decision

1. **Provisioning plane (SSH)** is owned by the **panel / VPN client**, not by the agent.
   One-shot (and rare image upgrade) only. Prefer **prebuilt image pull**
   (`deploy/install-edge-image.sh`, `SUBSERVER_IMAGE`) over remote `git clone` + build.
2. **Control plane** after install is **HTTP Bearer** to the agent. No SSH for routine apply,
   token rotate, box stop/start.
3. **Dataplane certificates** on the edge are owned by **sing-box ACME / TLS profiles**
   materialised into server JSON. As of the embedded `with_controlplane` module, operators
   configure ACME via **`/v1/controlplane/cert-manager`** (and self-signed via `/tls`);
   inbounds opt in with `bindings[].params.sni`. This **supersedes** the earlier “no ACME
   CRUD in agent v1” clause from the first draft of this ADR.
4. **Management listen** defaults to loopback or firewall-restricted bind; optional static
   `tls.cert/key` on the agent. No ACME for **management** API in v1 (self-signed or
   operator-supplied files only).

## Consequences

- Panel / client entities: SSH host, install job, node inventory (s-ui or Flutter module).
- Compose template with `network_mode: host`, `restart: unless-stopped`, pinned **CP** image
  (`:controlplane` / `tags.server.controlplane`).
- Credential bootstrap uses existing agent auth API (`POST /v1/auth/tokens`, bootstrap disable).
- Edge LE/HTTP-01 must terminate on the **node** IP; panel SSH must not own ongoing renewal.

## Alternatives rejected

| Alternative | Why not |
|-------------|---------|
| Agent self-installs via phone-home | Requires pre-existing binary; chicken-egg |
| Permanent SSH from panel for config | Bypasses agent API; worse audit/ops |
| ACME daemon inside subserver for inbounds | Duplicates sing-box certmagic; wrong layer |
| Panel issues LE over SSH for edge inbounds | Challenges hit wrong host; fragile renewals |

## References

- [EDGE_SSH_BOOTSTRAP.md](../../../docs/EDGE_SSH_BOOTSTRAP.md) (mother repo)
- [06-control-plane.md](../06-control-plane.md)
- ADR 0004 (independent of s-ui compile-time), ADR 0005 (external updates only)
