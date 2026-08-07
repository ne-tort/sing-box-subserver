# ADR 0006 — SSL profiles (supersedes cert-manager + tls_profile)

## Status

Superseded by [adr-ssl-profiles](../adr-ssl-profiles.md). Historical dual model (`tls_profile.json` + `cert_manager.json` + `params.sni`) is removed.

## Decision (current)

1. **SSL Profile** is the only certificate unit: self-signed / ACME / ACME-IP, handshake knobs, optional ECH.
2. Binding: `params.ssl_profile=<id>` (empty → `default`). Legacy `sni` / `self_signed_sni` / `tls_*` / `ech` params are rejected.
3. Management HTTPS uses the Default SSL profile leaf.
4. Reality never uses SSL profiles.

## Related

- [11-tls](../11-tls.md)
- [adr-ssl-profiles](../adr-ssl-profiles.md)
