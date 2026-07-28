# ADR 0006 — TLS profiles: ACME via sing-box, self-signed outside

## Status

Accepted

## Context

Controlplane needs TLS for protocol inbounds (trojan, future vless-tls). Options:

1. Always generate PEM outside sing-box.
2. Use sing-box `certificate_providers` (ACME / Tailscale / Cloudflare Origin CA).
3. Rely on server `tls.insecure` ephemeral self-signed.

Investigation (sing-box 1.14 + `with_acme`): ACME supports Let's Encrypt domains and bare IPs (`shortlived`). There is **no** self-signed certificate provider type.

## Decision

1. **ACME modes** (`acme_domain`, `acme_ip`) materialize top-level `certificate_providers` with tag `cp-tls`; inbounds set `tls.certificate_provider: "cp-tls"`. Renewal is sing-box/certmagic responsibility. `data_directory` = `{data_dir}/controlplane/acme`.
2. **Self-signed** remains controlplane-managed PEM with a declarative JSON spec (CN, DNS/IP SANs, key type, validity). Materialize uses `certificate_path` / `key_path`.
3. One profile per controlplane instance (all TLS inbounds share it).
4. Reject ephemeral `insecure` as server default.
5. Subscription outbounds: `insecure: true` only for `self_signed`; ACME uses normal verify + SNI.

## Consequences

- Pros: LE renewals stay in-process; IP LE supported; clear API; no inventing ACME outside sing-box.
- Cons: self-signed still custom code; Reality cannot share `certificate_provider`.

## Related

- [11-tls](../11-tls.md)
- Build tag `with_acme` in `build/tags.server`
