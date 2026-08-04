# 08 — Overhaul checklist (living)

Статусы: `[ ]` / `[~]` / `[x]`. A–F — по плану overhaul.

| Protocol | A catalog | B params | C meta | D custom | E i18n | F demux | Notes |
|----------|-----------|----------|--------|----------|--------|---------|-------|
| vless | [x] | [x] | [x] | [x] | [x] | [x] | **catalogsqlite SoT** (`data/catalog.sqlite`); JSON `presets/data/vless` removed; ready=full constructor schema + explicit `param_values` (no own templates); user_variants/client_profiles in SQLite (`ref/vless/variants.json`); param i18n via base tag; mux/WS early-data via params; opaque sub profiles removed |
| hysteria2 | [x] | [x] | [x] | [x] | [x] | [x] | **catalogsqlite SoT** (`ref/hysteria2`); JSON removed; obfs none/salamander/gecko/gecko_compact; masquerade/realm via params; no variants/profiles |
| wireguard | [x] | [x] | [x] | [x] | [x] | n/a | **catalogsqlite SoT** (`ref/wireguard`); JSON removed; hub PUT mtu/awg; ready share constructor endpoint/outbound + `mtu` param_values |
| trojan | [x] | [x] | [x] | [x] | [x] | [x] | **catalogsqlite SoT** (`ref/trojan`); JSON removed; no variants/profiles; multiplex/ws_max_early_data/fallback via params; alias `trojan-tcp` → `trojan_tls` |
| anytls | [x] | [x] | [x] | [x] | [x] | [x] | **catalogsqlite SoT** (`ref/anytls`); JSON removed; alpn/fingerprint/idle_session via params |
| trusttunnel | [x] | [x] | [x] | [x] | [x] | [x] | **catalogsqlite SoT** (`ref/trusttunnel`, LX `with_trusttunnel`); mode h2/h3→http2/http3; anti_dpi + UDP + fallback + force_http1; **registered in `internal/box`** + docker smoke |
| shadowquic | [x] | [x] | [x] | [x] | [x] | [x] | **catalogsqlite SoT** (`ref/shadowquic`, LX `with_shadowquic`); JLS + congestion/0-RTT/udp_over_stream; ready +`shadowquic_cubic`; **registered in `internal/box`** + docker smoke |
| sudoku | [x] | [x] | [x] | [x] | [x] | [x] | **catalogsqlite SoT** (`ref/sudoku`, LX `with_sudoku`); AEAD/multiplex off\|auto\|on + padding/fallback; ready +`sudoku_mux`; **registered in `internal/box`** + docker smoke |
| mieru | [x] | [x] | [x] | [x] | [x] | [x] | **catalogsqlite SoT** (`ref/mieru`, LX `with_mieru`); transport/multiplexing/mtu/traffic_pattern via params |
| carrier | [x] | [x] | [x] | [x] | [x] | n/a | **catalogsqlite SoT** (`ref/carrier`, LX `with_carrier`); provider/auth_mode/room; SFU ready require `room` |
| vmess | [x] | [x] | [x] | [x] | [x] | [x] | **catalogsqlite SoT** (`data/catalog.sqlite` + `ref/vmess`); JSON `presets/data/vmess` removed; no user_variants; client_profiles sec-*/pkt-xudp in SQLite; multiplex/ws_max_early_data via params; param i18n via `vmess_custom` |
| tuic | [x] | [x] | [x] | [x] | [x] | [x] | **catalogsqlite SoT** (`ref/tuic`); JSON removed; client_profiles udp-native/udp-quic in SQLite; zero_rtt via params; ready `tuic` + `tuic_0rtt` |
| shadowsocks | [x] | [x] | [x] | [x] | [x] | n/a | **catalogsqlite SoT** (`ref/shadowsocks`); JSON removed; method/network/UoT/multiplex via params; ready +`ss_aes128_dual` (tcp,udp); SS2022 key length overrides; no variants/profiles |
| shadowtls | [x] | [x] | [x] | [x] | [x] | n/a | **catalogsqlite SoT** (`ref/shadowtls`); JSON removed; wildcard_sni + strict_mode + fingerprint via params |
| naive | [x] | [x] | [x] | [x] | [x] | [x] | **catalogsqlite SoT** (`ref/naive`); JSON removed; network=tcp/udp → naive_tls / naive_quic; `naive_tls` demux-compatible (Oddball default) |
| snell | [x] | [x] | [x] | [x] | [x] | [x] | **catalogsqlite SoT** (`ref/snell`); JSON removed; obfs_mode/host + version≥6 via params; ready +`snell_v5_tls` |
| ssh | [x] | [x] | [x] | [x] | [x] | [x] | **catalogsqlite SoT** (`ref/ssh`, LX `with_ssh`); auth_mode password/pubkey + versions/UoT; ready cred_fields override |
| derp | [x] | [x] | [x] | [x] | [x] | n/a | **catalogsqlite SoT** (`ref/derp`, LX `with_derp`); path/websocket/udp/fingerprint via params |
| http/socks/mixed | [x] | [x] | [x] | [x] | [x] | n/a | **catalogsqlite SoT** (`ref/socks`,`ref/http`,`ref/mixed`); JSON removed; tls_mode/fingerprint/UoT/outbound_type via params |
| hysteria1 | [x] | [x] | [x] | [x] | [x] | n/a | **catalogsqlite SoT** (`ref/hysteria`); JSON removed; bandwidth + obfs peer via params; legacy |
| cloudflared | [x] | [x] | [x] | [x] | [x] | n/a | **catalogsqlite SoT** (`ref/cloudflared`); JSON removed; status=deferred — hidden from subserver catalog (inbound_only; client outbound TBD) |

**Materialize:** stock knobs + `applyStockUTLSFingerprint`; customs в `custom_knobs.go`. flow/packet_encoding: `none` снимает поле.

**WG hub:** `PUT /v1/controlplane/wg` — `mtu`, `awg{}` (`jc/jmin/jmax`, `s1–s4`, `h1–h4`, masquerade `id/ip/ib` or manual `i1–i5`), optional `masquerade_mode`/`masquerade_url`/`manual_init`. `wg_custom` — schema каталога, не from-presets.

**i18n:** locale prune; ru+en приоритет; 11 langs; API fallback → en (`i18n.Get`, `ResolveI18n`/`PickLocalized`: lang→en). Pass3–11: META_UNUSED=0 (knobs), empty enum=0, demux refs; EN purity; TT h2→http2; DERP udp native|uot; SQ udp_over_stream.

**Protocol gate:** каталог A–F закрыт; **весь** matrix в **catalogsqlite SoT** (`ref/` → `data/catalog.sqlite`). JSON `presets/data` и `demux-recipes` удалены. **Demux groups** (~10 branded; Naive встроен в TLS-слоты, без одиночных Naive-наборов): `ref/demux` → `demux_groups`/`demux_slots`; cost: [09-demux-cost.md](09-demux-cost.md). UI клиента — следующий этап после вашего OK.

**Примечание:** `cp_matrix_docker` API smoke прогнан (TLS self_signed, SS+Trojan set, sub insecure, cert regenerate). Полный invariant matrix по всем тегам — отдельно / опционально.

## Foundation

- [x] params_schema v2
- [x] locales: 11 языков Hiddify; fallback → en; prune orphans
- [x] ADR + custom arch + demux fullstack
- [x] materialize wiring для custom + stock knobs
- [x] WG hub AWG overrides
- [x] `generate_all_catalog_locales.py` / `rewrite_priority_locales.py`
- [x] Pass5–11: stock params, help, fingerprint, enum none, demux refs, EN purity, wire mappings (TT/DERP/SQ)
- [x] Pass9–11: structural EN; smoke; first_bytes; TT/DERP/SQ capability fixes

## Scripts

```bash
python scripts/generate_all_catalog_locales.py
go test -tags with_controlplane ./internal/controlplane/...
```
