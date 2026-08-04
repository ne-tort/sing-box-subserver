# 03 — Catalog (protocols, presets, demux groups)

## Protocols & presets

```http
GET /v1/controlplane/protocols?lang=en
GET /v1/controlplane/presets?protocol={tag}&lang=en
GET /v1/controlplane/presets/{name}?lang=en
```

List/get items include (when present):

- `traits`, `scores`, `demux_hints` (`compatible_with_demux`, `looks_like`, …)
- `cred_fields`, `cred_generators`, `peer_secret_fields`
- `param_fields` — **required** binding param keys (e.g. carrier `room`)
- `params_schema` — map of `{type, required, description, …}` for thin clients (**prefer this**)
- `optional_params` — subset of `params_schema` where `required=false` (compat; does **not** include required keys)
- `default_user_variants`

Thin-client rule: render install forms from `params_schema` only. Never hardcode preset tags or which params are required.

Conditional GET: send `If-None-Match` with prior `ETag` (`"sha256:…"`). Unchanged → **304**. Capability: `catalog_etag`.

UI rule for demux slot pickers:

```text
fits_interchange && demux_compat == "full"
```

`demux_compat`:

| Value | Install on **stable** group |
|-------|-----------------------------|
| `full` | Allowed |
| `demux_lab` | Requires `allow_lab:true` (or group `status=lab`) |
| `demux_unsupported` | Rejected |

Examples: `hy2_salamander` / gecko / `hy1_obfs` → unsupported; TrustTunnel / ShadowQUIC / most transport-Reality → lab; `vless_ws_reality` → **full** (matrix 2026-07-30).

**Tests:** `test_01_bootstrap_meta.py::test_protocols_presets_metadata`  
**Unit:** `internal/controlplane/demuxgroups/compat_test.go`

## Demux groups

```http
GET /v1/controlplane/demux-groups?status=stable&lang=en
GET /v1/controlplane/demux-groups/{tag}
GET /v1/controlplane/demux-groups/{tag}/substitutions?lang=en
```

Bootstrap hint: default filter **stable** (`hints.demux_groups_list`).

Substitutions options carry `demux_compat` + `fits_interchange`. Prefer defaults on stable groups (`dg_443_dual` / Bypasser → `vless_reality` + `hy2`).

TLS multi-slot groups assign unique `demux_sni` per Reality/TLS/`sni_pool` QUIC slot; PreferredALPN only sets inbound `tls.alpn`.

**Tests:** `test_01_bootstrap_meta.py::test_demux_groups_match_meta_ux`, `test_04_demux_groups.py`  
**Scenario:** [../scenarios/03-demux-443-dual.md](../scenarios/03-demux-443-dual.md)
