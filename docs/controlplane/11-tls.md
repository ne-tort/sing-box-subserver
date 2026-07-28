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

**Management API + `GET /v1/sub/{token}`** use the **same** profile material over HTTPS (no nginx required):

| Mode | Management cert source |
|------|------------------------|
| `self_signed` | `{data_dir}/controlplane/tls/server.{crt,key}` |
| `acme_*` + ready | certmagic PEMs under `controlplane/acme/certificates/` |
| `acme_*` while obtaining | interim self_signed PEMs (always kept as safety net) |
| ACME obtain/renewal emergency | profile mode forced to `self_signed` (persisted) + rematerialize |

`subscription_url` scheme is always `https://` on CP builds. Clients (panel) should use `agent_tls_insecure` for self_signed / interim certs.

When host `:80` is free, leave challenges at defaults (HTTP-01 + TLS-ALPN).  
If host `:80` is taken, either:

- `disable_http_challenge: true` and publish agent `:443` for TLS-ALPN, or
- set `alternative_http_port` (e.g. `9080`) and forward public `80 → alternative_http_port`.

Same for TLS-ALPN via `alternative_tls_port` when public `:443` cannot bind inside the agent.

Important: when using `alternative_http_port`, do **not** also publish host `:80` to an idle container port — LE will hit empty `:80` (`connection refused`). Prefer **host network** deploy.

### `acme_ip`

Same as domain, but `domain: ["<public-ip>"]`, provider **must** be `letsencrypt` (shortlived profile). DNS-01 rejected. Challenges must reach the edge (TLS-ALPN on :443 works when :80 is busy; HTTP-01 works when :80 is free).

## API

See [05-api](05-api.md): `GET/PUT /v1/controlplane/tls`, `POST /v1/controlplane/tls/regenerate`.

`material_status.ready` — self_signed PEM present, or ACME PEMs found under `controlplane/acme/certificates/` for every domain (certmagic store). Poll after `PUT` until `ready=true` before advertising subscriptions; handshakes may still race for a second while the provider loads the cert into memory.

## Boot

Missing `tls_profile.json` → default `self_signed` derived from `controlplane.public_host` (DNS and/or IP SANs).

## Related

- [ADR 0006](adr/0006-tls-profiles.md)
- sing-box docs: `certificate_providers` / ACME
