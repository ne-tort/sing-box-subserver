# invariant JSON fields

Filename must equal `tag` + `.json`.

| Field | Type | Notes |
|-------|------|-------|
| `schema_version` | int | `1` |
| `tag` | string | Canonical snake_case id |
| `protocol` | string | Parent protocol tag |
| `aliases` | string[] | Legacy hyphen names |
| `short_name` | string | ≤ ~18 chars for mobile row |
| `status` | string | `stable` \| `lab` \| `planned` |
| `i18n` | map | `ru` required |
| `traits` | string[] | `tcp`/`udp`/`quic`/`tls`/`reality`/… |
| `demux_hints` | object | Pre-TLS / first-packet hints for demux grouping |
| `demux_hints.network` | string[] | `tcp` / `udp` |
| `demux_hints.looks_like` | string | `tls_clienthello` \| `quic` \| `ssh_banner` \| `http` \| `raw_tcp` |
| `demux_hints.compatible_with_demux` | bool | Safe as demux peer (manual review still required) |
| `scores.dpi` | 0–10 | Benchmark: `vless_reality` = 10 |
| `scores.speed` | 0–10 | Benchmark: WireGuard = 10 (not an inbound preset) |
| `scores.mobile` / `scores.setup` | 0–10 | Optional |
| `requirements` | object | `tls_profile`, `reality_assignment`, `udp`, `quic`, `build_tag` |
| `cred_fields` | string[] | Keys under `user.creds[tag]` |
| `cred_generators` | map | Optional per-field generator: `uuid`, `password`, `ss2022_16`, `ss2022_32` |
| `peer_secret_fields` | map | Set-level secrets (`set.peer_secrets["tag/field"]`); placeholders `{{peer.field}}` |
| `param_fields` | string[] | Required keys in `bindings[].params` (e.g. carrier `room`) |
| `client_notes.ws_path_default` / `hu_path_default` / `http_path_default` | string | Per-preset default for `{{param.ws_path}}` etc. |
| `default_user_variants` | string[] | When binding omits `enabled_user_variants` (e.g. transports → `["flow-none"]`) |
| `default_client_profiles` | string[] | When binding omits `enabled_client_profiles` |
| `inbound_template` / `outbound_template` | object | Required for `stable`/`lab` |
| `client_notes` | map | Optional free-form |

Placeholders in templates: `{{tag}}`, `{{listen}}`, `{{listen_port}}`, `{{server}}`, `{{user.*}}`, `{{peer.*}}`, `{{param.*}}` — see design `docs/controlplane/04-domain.md`.

User variants (symmetric inbound users) and client profiles (outbound-only) live in `domain/variants.go`; presets only select defaults via the fields above.
