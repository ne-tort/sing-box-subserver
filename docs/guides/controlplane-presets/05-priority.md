# 05 — Priority & invariants roadmap

Каталог: полноценные inbound/outbound шаблоны (ALPN, uTLS chrome, transport knobs, mux limits), не минимальные огрызки.
Опциональные overrides: `bindings[].params` → `{{param.*}}` (дефолты в materialize / `client_notes.*_path_default`).

Прогон Docker+iperf: [06-invariant-matrix.md](06-invariant-matrix.md) (`scripts/invariant_matrix/`).

| # | Protocol | Инварианты | Notes |
|---|----------|------------|-------|
| 1 | vless | tcp/tls/reality + ws/grpc/http/httpupgrade + mux + reality transports + **quic_tls** + **hysteria_tls** + **httpupgrade_reality** | `ws_host`/`ws_path` params; hy = `transport.type=hysteria` + peer `hy_auth` (`with_hysteria_transport`; не `type:hysteria2`) |
| 2 | trojan | tls + transports + reality + mux + fallback + http + **quic_tls** + **httpupgrade_reality** | params как у vless |
| 3 | shadowsocks | AEAD + 2022 (`server:user`) + mux + uot + **2022+mux** | peer PSK |
| 4 | vmess | tcp/tls/reality + transports + reality + tls_mux + **quic_tls** + **httpupgrade_reality** | security profiles |
| 5 | hysteria2 | hy2 + salamander + **gecko** (+compact, +masquerade) + masquerade* + realm | Gecko = `obfs.type=gecko` |
| 5b | **wireguard** | singleton hub: `wg` / `wg_awg2` / `wg_awg3` | PUT `/v1/controlplane/wg`; AWG+masquerade auto; sticky `wg_host_index` |
| 6 | tuic | tuic + 0rtt | heartbeat, udp_relay |
| 7 | anytls | padding + idle | uTLS |
| 8 | shadowtls | v3 + wildcard authed + wildcard all | `handshake_server` param |
| 9 | naive | tls + quic | headers |
| 10 | http/socks/mixed | plaintext/tls/path/uot | |
| 11 | mieru | tcp/udp | optional `traffic_pattern` |
| 12 | hysteria1 | hy1 + obfs | auth_str |
| 13 | snell | v5/v6 | `obfs_host` param; out_json copies host for v5 |
| 14 | ssh | password + uot + pubkey | `server_version` param |
| 15 | shadowquic | jls + 0rtt + **uot** | `jls_addr`/`jls_server_name`; `with_shadowquic` |
| 16 | sudoku | pad + httpmask(legacy) + aes | `fallback`; stream/ws — CDN (не v1) |
| 17 | trusttunnel | h2 + h3 + **auto** | PEM из self_signed **или ACME** |
| 18 | derp | tls + ws + uot | `path` param; curve25519 |
| 19 | carrier | peer/vk + jitsi/telemost/wbstream × shared\|users + **jitsi SEI** | `params.room` |
| 20 | **cloudflared** | token tunnel | `params.token`, inbound_only |

## Skip (не proxy multi-user / не dual-role)

| Тип | Почему |
|-----|--------|
| tun / redirect / tproxy | системный dataplane |
| direct | inject-target, без creds/sub |
| demux | `set.demux_template`, не каталог |
| wireguard | **singleton hub** via `/v1/controlplane/wg` (не inbound-set) |
| xhttp | **client-only** transport (lx SPEC 002) |
| vmess/trojan **over** hysteria transport | тот же lx transport есть; CP-инвариант пока только `vless_hysteria_tls` (SPEC 050) |
| shadowsocksr | outbound-only в lx |
| hysteria-realm **service** | отдельный service; inbound `realm{}` — пресет `hy2_realm` |
| sudoku httpmask stream/ws/poll | lx v1: только `legacy` без CDN tunnel |
| bridge / masque / tor / tailscale | service/endpoint/outbound |

## Params (optional unless `param_fields`)

| Ключ | Где | Default |
|------|-----|---------|
| `jls_addr` / `jls_server_name` | shadowquic | cloudflare |
| `handshake_server` | shadowtls | microsoft.com |
| `ws_host` / `ws_path` | ws transports | host=`PublicHost`; path из notes |
| `hu_*` / `http_*` | httpupgrade / http | аналогично |
| `masquerade_url` | hy2 proxy | cloudflare |
| `masquerade_dir` | hy2 file | **required** |
| `realm_server_url` / `realm_id` | hy2_realm | **required** |
| `fallback` | sudoku | `http://127.0.0.1:80` |
| `obfs_host` | snell v5 | bing.com |
| `path` | derp | `/derp` |
| `server_version` | ssh | OpenSSH_8.9 |
| `traffic_pattern` | mieru | empty (omit semantics) |
| `room` / `token` | carrier / cloudflared | **required** via `param_fields` |

## Generators

| gen | use |
|-----|-----|
| `uuid` / `password` | default |
| `ss2022_16` / `ss2022_32` | SS2022 PSK |
| `curve25519` | DERP |
| `ssh_ed25519` | SSH PEM + authorized_keys |

## Peer / shared_key / shared_auth / no_users

`shared_key` / `shared_auth` / `no_users` → inbound без `users[]`.  
`no_listen` → без `listen`/`listen_port` (SFU/cloudflared).
