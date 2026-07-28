# 11 — TLS profiles (controlplane)

## Role

One **active TLS profile** for all TLS-enabled inbounds materialized by controlplane.
Non-TLS presets (e.g. `shadowsocks-tcp`, plain `vless-tcp`) are unchanged.
Reality presets must not use `certificate_provider` (sing-box incompatibility).

## Who issues certificates

| Mode | Issuer | Why |
|------|--------|-----|
| `self_signed` | Controlplane (PEM on disk) | sing-box has **no** self-signed `certificate_provider` |
| `acme_domain` / `acme_ip` | sing-box ACME (`certificate_providers`, tag `with_acme`) | certmagic renewals inside the edge process |

Do **not** use server `tls.insecure` ephemeral keypairs as the default server identity.

## Modes

### `self_signed` (default)

Declarative JSON → ECDSA/RSA key + self-signed cert written to:

`data_dir/controlplane/tls/server.crt` + `server.key`

Inbound TLS: `certificate_path` / `key_path`.  
Subscription outbounds: `tls.insecure: true`.

### `acme_domain`

Emitted config:

```json
{
  "certificate_providers": [{
    "type": "acme",
    "tag": "cp-tls",
    "domain": ["vpn.example.com"],
    "email": "admin@example.com",
    "provider": "letsencrypt",
    "data_directory": "{data_dir}/controlplane/acme"
  }],
  "inbounds": [{
    "tls": {
      "enabled": true,
      "server_name": "vpn.example.com",
      "certificate_provider": "cp-tls"
    }
  }]
}
```

Challenges: HTTP-01 / TLS-ALPN-01 (default) or optional `dns01_challenge`.  
Outbounds: verify enabled (no `insecure`), SNI = domain.

### `acme_ip`

Same as domain, but `domain: ["<public-ip>"]`, provider **must** be `letsencrypt` (shortlived profile). DNS-01 rejected. Challenges must reach the edge.

## API

See [05-api](05-api.md): `GET/PUT /v1/controlplane/tls`, `POST /v1/controlplane/tls/regenerate`.

## Boot

Missing `tls_profile.json` → default `self_signed` derived from `controlplane.public_host` (DNS and/or IP SANs).

## Related

- [ADR 0006](adr/0006-tls-profiles.md)
- sing-box docs: `certificate_providers` / ACME
