# ADR 0006 — TLS self-signed + separate cert-manager

## Status

Accepted (supersedes mode-based ACME on TLS profile)

## Context

Controlplane needs TLS for protocol inbounds (trojan, vless-tls, demux slots). Options:

1. Always generate PEM outside sing-box.
2. Use sing-box `certificate_providers` (ACME).
3. Rely on server `tls.insecure` ephemeral self-signed.

Investigation (sing-box + `with_acme`): ACME supports Let's Encrypt domains and bare IPs. There is **no** self-signed certificate provider type. Mixing “how to issue default PEM” with “ACME for all TLS inbounds” conflicted with demux per-SNI self-signed and Reality.

## Decision

1. **Always** maintain self-signed PEMs (`tls_profile.json` / `controlplane/tls/`). Default for inbounds without ACME SNI and for management HTTPS safety.
2. **Cert-manager** (`cert_manager.json`, `GET/PUT /cert-manager`) owns ACME domains + settings. Materialize emits `certificate_providers` tag `cp-tls` when domains are non-empty.
3. TLS inbounds opt in with optional `bindings[].params.sni` ∈ cert-manager domains → `certificate_provider: cp-tls`. Demux syncs `demux_sni = sni`.
4. Reality never uses cert-manager.
5. Subscription outbounds: `insecure: true` unless outbound uses a cert-manager SNI.

## Consequences

- Pros: LE renewals stay in-process; per-inbound ACME selection; demux/Reality unchanged conceptually.
- Cons: operators must set both cert-manager domains and `params.sni`.

## Related

- [11-tls](../11-tls.md)
- Build tag `with_acme` in `build/tags.server`
