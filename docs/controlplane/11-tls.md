# 11 — TLS + cert-manager (controlplane)

## Role

- **Self-signed** is always the default TLS material for inbounds without ACME SNI and for management HTTPS safety PEMs.
- **Cert-manager** is a separate ACME pool (`domains[]` + provider settings). Individual TLS inbounds (preset or demux slot) opt in via optional `bindings[].params.sni`.
- Reality never uses cert-manager (`params.sni` is rejected for Reality presets).

## Model

```
SelfSigned PEM  ──(no params.sni)──► TLS inbound
CertManager ACME ──(params.sni ∈ domains)──► certificate_provider: cp-tls
```

### Self-signed (`/v1/controlplane/tls`)

Declarative JSON → ECDSA/RSA key + self-signed cert:

`data_dir/controlplane/tls/server.crt` + `server.key`

Inbound TLS without `params.sni`: `certificate_path` / `key_path`.  
Demux TLS slots without ACME SNI: per-`demux_sni` PEM under `controlplane/tls/slots/`.  
Subscription outbounds: `tls.insecure: true` when not using cert-manager SNI.

### Cert-manager (`/v1/controlplane/cert-manager`)

When `domains` is non-empty, materialize emits:

```json
{
  "certificate_providers": [{
    "type": "acme",
    "tag": "cp-tls",
    "domain": ["vpn.example.com"],
    "email": "admin@example.com",
    "provider": "letsencrypt",
    "data_directory": "{data_dir}/controlplane/acme"
  }]
}
```

Inbound with `params.sni` matching a domain:

```json
"tls": {
  "enabled": true,
  "server_name": "vpn.example.com",
  "certificate_provider": "cp-tls"
}
```

For demux, setting `params.sni` also sets `demux_sni = sni` so ClientHello matches the leaf.

Constraints:

- Domains must not mix DNS names and IPs.
- IP mode: exactly one IP, provider `letsencrypt` only, no `dns01_challenge`.
- Challenges: HTTP-01 / TLS-ALPN-01 (default) or optional `dns01_challenge`.

### Management HTTPS

| Situation | Cert source |
|-----------|-------------|
| Default / no ACME PEMs | `{data_dir}/controlplane/tls/server.{crt,key}` |
| Cert-manager domain with obtained PEM | certmagic under `controlplane/acme/certificates/` |
| ACME still obtaining | interim self_signed PEMs |

`subscription_url` scheme is always `https://` on CP builds. Use `agent_tls_insecure` for self_signed / interim certs.

## Optional `params.sni`

- Name: `sni` (`domain.BindingParamSNI`).
- Allowed only on TLS / `tls_custom` non-Reality presets.
- Must be in cert-manager `domains` or validate returns `cp_invalid_bindings`.
- Not required in preset `param_fields` (optional knob, like `demux_sni`).

## API

| Method | Path | Meaning |
|--------|------|---------|
| GET/PUT | `/v1/controlplane/tls` | Self-signed knobs + `material_status` (no modes / ACME) |
| POST | `/v1/controlplane/tls/regenerate` | Force reissue self-signed PEM |
| GET/PUT | `/v1/controlplane/cert-manager` | Domains, provider settings, per-domain status |

Legacy: old `tls_profile.json` with `mode: acme_*` migrates ACME into `cert_manager.json` on boot and rewrites TLS as self-signed.

## Related

- [ADR 0006](adr/0006-tls-profiles.md)
- [05-api](05-api.md)
- sing-box docs: `certificate_providers` / ACME
