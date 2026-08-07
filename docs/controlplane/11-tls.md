# 11 — SSL profiles (controlplane)

## Role

**SSL Profile** is the first-class unit for leaf certificates, TLS handshake knobs, and optional ECH.

- Binding param: `params.ssl_profile=<id>` (empty → profile `default`).
- Legacy `params.sni` / `self_signed_sni` / `tls_*` / `ech` are **rejected** (no migrate).
- Reality never uses SSL profiles (`params.ssl_profile` rejected for Reality presets).

## Model

```
SSLProfile (self_signed | acme | acme_ip)
  ├── leaf PEM  → controlplane/ssl/<id>/cert.crt|key
  ├── ACME      → certificate_providers tag cp-ssl-<id>
  │                 data_directory: controlplane/ssl/<id>/acme
  └── ECH       → controlplane/ssl/<id>/ech.key.pem + ech.config.pem
```

Store: `controlplane/ssl_profiles.json`.

### Types

| Type | Identity | Leaf |
|------|----------|------|
| `self_signed` | `domain` (SNI/CN) | Generated PEM under `ssl/<id>/` |
| `acme` | `domain` + `email` | ACME provider `cp-ssl-<id>` |
| `acme_ip` | `ip` + `email` | ACME provider (bare IP) |

Handshake fields on the profile: `alpn`, `min_version`, `max_version`, `cipher_suites`, `curve_preferences`.  
ECH: `ech_enabled`, `ech_sni` (empty → random Reality pool SNI).

### Status

Computed on GET (and after ensure): `ready` | `pending` | `missing` | `expired` | `error` via `x509.ParseCertificate`.

## API

| Method | Path | Behavior |
|--------|------|----------|
| GET | `/v1/controlplane/ssl` | `{profiles, options, free_dns?}` — `options` lists allowed field values |
| POST | `/v1/controlplane/ssl` | `{name}` → draft `self_signed` |
| GET/PUT/DELETE | `/v1/controlplane/ssl/{id}` | CRUD; DELETE refuses if referenced |
| POST | `/v1/controlplane/ssl/{id}/regenerate` | Force reissue: self_signed/ECH rewrite; ACME clears leaf+acme store then rematerialize |

`options` keys: `types`, `providers` (`letsencrypt`; `zerossl` gated until live-tested), `domains` (free-DNS OK hosts), `ips`, `reality_snis`, `alpn`, `min_version`, `max_version`, `cipher_suites`, `curve_preferences`.

Removed (no compat): `/tls`, `/tls/regenerate`, `/cert-manager`, `/cert-manager/ensure-free-dns`, `/ech`.

## Boot

`ensureSSLProfiles` ensures the Default `self_signed` profile exists and PEMs under `controlplane/ssl/<id>/`.
`bootstrapFreeDNSSSL` then:
1. `freedns.Ensure` — sslip / nip / addr.tools (soft-skip per provider)
2. `ensureFreeDNSSSLProfiles` — ACME profiles with **stable ids** (`fd-sslip`, `fd-nip`, `fd-addrtools`, `fd-ip`)

Reuse rules: existing id → update domain/IP only; domain/IP already covered by another profile → no duplicate; orphan `ssl/<id>/` on disk → recreate catalog entry. Errors → quiet skip (no retries).

Hosts are persisted in `free_dns.json` and also listed under `GET /ssl` → `free_dns.hosts` (picker + bootstrap). Stable profile dirs under `ssl/fd-*` are preserved across reinstall so ACME material is reused.

## ACME email

Empty / `auto` → `admin@<sni>` from the Reality pool (stable per profile id). Explicit `email` overrides. API returns `email_auto` + `email_effective`.

TLS/QUIC demux slots auto-assign **unique random SNIs from the Reality pool** (live `reality_config.json`, else defaults). No `.local` synthetics.
Self-signed demux members get dedicated leaves under `controlplane/ssl/_slots/<sni>/` (SlotTLS) so CN matches ClientHello.

## Materialize

1. Resolve profile by `ssl_profile` (default `default`).
2. ACME → `certificate_provider: cp-ssl-<id>`; self-signed → profile PEM paths.
3. Demux + self-signed: override leaf with SlotTLS for `demux_sni`.
4. Merge handshake + ECH from the profile only.
5. Subscription `tls.insecure` when profile is not ACME.
