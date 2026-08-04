# 06 — Invariant matrix (Docker + iperf)

Чеклист прогона CP-инвариантов через harness
[`scripts/invariant_matrix/`](../../../scripts/invariant_matrix/).

## Как запускать

```bash
cd third_party/sing-box-subserver/scripts/invariant_matrix
python run.py --stage 1
python run.py --protocol vless --stage 1
python run.py --all-manifests
python run.py --list --stage 1
```

Env: `MIN_MBPS` (default `0.5`), `IPERF_TIME` (default `5`), `LX_BIN`, `INVMATRIX_IMAGE` (`sui-lx-iperf:local`).

Результаты: `results/<protocol>.json`, `results/SUMMARY.md`. Колонка **status** ниже обновляется best-effort при прогоне.

## Topology

`iperf3 -s` + lx **server** (все cells = отдельные inbounds) + per-cell lx **client**
(`direct` inbound override → iperf IP:5201 → proxy outbound).

Сеть: `invmatrix_net`. Бинарь: `.tools/lx-client/sing-box` (linux) **и** рядом `libcronet.so` для naive outbound (`python ../ensure_lx_client.py`). Image `sui-lx-iperf:local`.

Naive inbound на агенте — pure-Go. Naive outbound (`with_naive_outbound`+`with_purego`) нужен клиенту и агенту: CP smoke крутит ephemeral client-box на агенте. В образе агента рядом с бинарём лежит `libcronet.so` (glibc; runtime = debian).

## Status legend

| status | meaning |
|--------|---------|
| pending | ещё не гоняли |
| pass | iperf ≥ MIN_MBPS (или reuse_cmd ok) |
| fail | handshake/iperf/reuse failed |
| skip | сознательный skip (нет токена / внешняя зависимость) |

---

## Stage 1 — Core

| protocol | tag | stage | status |
|----------|-----|-------|--------|
| vless | `vless_tls` | 1 | pass |
| vless | `vless_reality` | 1 | pass |
| vless | `vless_ws_tls` | 1 | pass |
| vless | `vless_grpc_tls` | 1 | pass |
| trojan | `trojan_tls` | 1 | pass |
| trojan | `trojan_reality` | 1 | pass |
| vmess | `vmess_tls` | 1 | pass |
| vmess | `vmess_reality` | 1 | pass |
| shadowsocks | `ss_aes128` | 1 | pass |
| shadowsocks | `ss_2022_aes128` | 1 | pass |
| hysteria2 | `hy2` | 1 | pass |
| hysteria2 | `hy2_salamander` | 1 | pass |
| tuic | `tuic` | 1 | pass |
| anytls | `anytls` | 1 | pass |

## Stage 2 — Transports / mux / QUIC / Hy

| protocol | tag | stage | status |
|----------|-----|-------|--------|
| vless | `vless_ws_reality` | 2 | pass |
| vless | `vless_grpc_reality` | 2 | pass |
| vless | `vless_http_tls` | 2 | pass |
| vless | `vless_http_reality` | 2 | pass |
| vless | `vless_httpupgrade_tls` | 2 | pass |
| vless | `vless_httpupgrade_reality` | 2 | pass |
| vless | `vless_tls_mux` | 2 | pass |
| vless | `vless_quic_tls` | 2 | pass |
| vless | `vless_hysteria_tls` | 2 | pass |
| vless | `vless_tcp` | 2 | pass |
| trojan | `trojan_ws_tls` | 2 | pass |
| trojan | `trojan_ws_reality` | 2 | pass |
| trojan | `trojan_grpc_tls` | 2 | pass |
| trojan | `trojan_grpc_reality` | 2 | pass |
| trojan | `trojan_http_tls` | 2 | pass |
| trojan | `trojan_http_reality` | 2 | pass |
| trojan | `trojan_httpupgrade_tls` | 2 | pass |
| trojan | `trojan_httpupgrade_reality` | 2 | pass |
| trojan | `trojan_tls_mux` | 2 | pass |
| trojan | `trojan_quic_tls` | 2 | pass |
| trojan | `trojan_tls_fallback` | 2 | pass |
| vmess | `vmess_ws_tls` | 2 | pass |
| vmess | `vmess_ws_reality` | 2 | pass |
| vmess | `vmess_grpc_tls` | 2 | pass |
| vmess | `vmess_grpc_reality` | 2 | pass |
| vmess | `vmess_http_tls` | 2 | pass |
| vmess | `vmess_http_reality` | 2 | pass |
| vmess | `vmess_httpupgrade_tls` | 2 | pass |
| vmess | `vmess_httpupgrade_reality` | 2 | pass |
| vmess | `vmess_tls_mux` | 2 | pass |
| vmess | `vmess_quic_tls` | 2 | pass |
| vmess | `vmess_tcp` | 2 | pass |
| shadowsocks | `ss_aes128_mux` | 2 | pass |
| shadowsocks | `ss_aes128_uot` | 2 | pass |
| shadowsocks | `ss_aes256` | 2 | pass |
| shadowsocks | `ss_chacha20` | 2 | pass |
| shadowsocks | `ss_2022_aes256` | 2 | pass |
| shadowsocks | `ss_2022_chacha` | 2 | pass |
| shadowsocks | `ss_2022_aes128_mux` | 2 | pass |
| hysteria2 | `hy2_gecko` | 2 | pass |
| hysteria2 | `hy2_gecko_compact` | 2 | pass |
| hysteria2 | `hy2_gecko_masquerade` | 2 | pass |
| hysteria2 | `hy2_masquerade` | 2 | pass |
| hysteria2 | `hy2_masquerade_proxy` | 2 | pass |
| hysteria2 | `hy2_masquerade_file` | 2 | pass |
| tuic | `tuic_0rtt` | 2 | pass |
| anytls | `anytls_idle` | 2 | pass |

## Stage 3 — Dual-role (CP render + iperf; lx-docker reused where noted)

| protocol | tag | stage | status | notes |
|----------|-----|-------|--------|-------|
| shadowquic | `shadowquic_jls` | 3 | pass | render+iperf |
| shadowquic | `shadowquic_0rtt` | 3 | pass |  |
| shadowquic | `shadowquic_uot` | 3 | pass |  |
| sudoku | `sudoku_pad` | 3 | pass | multiplex=on unsupported → off in `sudoku_aes` |
| sudoku | `sudoku_httpmask` | 3 | pass |  |
| sudoku | `sudoku_aes` | 3 | pass |  |
| trusttunnel | `trusttunnel_h2` | 3 | pass | custom TLS PEM |
| trusttunnel | `trusttunnel_h3` | 3 | pass | http3; anti_dpi=false |
| trusttunnel | `trusttunnel_auto` | 3 | pass |  |
| derp | `derp_tls` | 3 | pass | curve25519 wg-keypair; no `stun: true` |
| derp | `derp_ws` | 3 | pass |  |
| derp | `derp_uot` | 3 | pass |  |
| mieru | `mieru_tcp` | 3 | pass |  |
| mieru | `mieru_udp` | 3 | pass | UDP underlay needs dial IP (not Docker DNS) |

## Stage 4 — WG + carrier

| protocol | tag | stage | status | notes |
|----------|-----|-------|--------|-------|
| wireguard | `wg` | 4 | pass | reuse `awg-matrix-docker` |
| wireguard | `wg_awg2` | 4 | pass |  |
| wireguard | `wg_awg3` | 4 | pass |  |
| carrier | `carrier_peer_shared` | 4 | pass | reuse `smoke-failfast.ps1` (peer+vk) |
| carrier | `carrier_peer_users` | 4 | pass | mapped via smoke |
| carrier | `carrier_jitsi_shared` | 4 | pass | mapped via smoke |

## Stage 5 — Niche

| protocol | tag | stage | status | notes |
|----------|-----|-------|--------|-------|
| hysteria | `hy1` | 5 | pass |  |
| hysteria | `hy1_obfs` | 5 | pass |  |
| hysteria2 | `hy2_realm` | 5 | skip | needs external realm `server_url` |
| snell | `snell_v5` | 5 | pass | inbound has no `obfs_host` |
| snell | `snell_v6` | 5 | pass |  |
| ssh | `ssh_password` | 5 | pass |  |
| ssh | `ssh_uot` | 5 | pass |  |
| ssh | `ssh_pubkey` | 5 | pass | host `ssh-keygen` ed25519 |
| naive | `naive_tls` | 5 | pass | client kit: `sing-box` + `libcronet.so` (см. `scripts/ensure_lx_client.py`) |
| naive | `naive_quic` | 5 | pass | same; Cronet нужен только outbound/клиенту |
| shadowtls | `shadowtls_v3` | 5 | skip | standalone no destination (needs detour) |
| shadowtls | `shadowtls_v3_wildcard` | 5 | skip | same |
| shadowtls | `shadowtls_v3_wildcard_all` | 5 | skip | same |
| http | `http` | 5 | pass | no `path: /` on outbound |
| http | `http_tls` | 5 | pass |  |
| http | `http_tls_path` | 5 | pass |  |
| socks | `socks` | 5 | pass |  |
| socks | `socks_uot` | 5 | pass |  |
| mixed | `mixed_auth` | 5 | pass |  |
| mixed | `mixed_tls` | 5 | pass |  |
| cloudflared | `cloudflared_token` | 5 | skip | no CF token |

## Skip (не в матрице как proxy cells)

См. [05-priority.md](05-priority.md) — tun/redirect/tproxy/direct/demux/xhttp/SSR/…
