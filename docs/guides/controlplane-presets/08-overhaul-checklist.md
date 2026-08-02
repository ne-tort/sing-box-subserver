# 08 — Overhaul checklist (living)

Статусы: `[ ]` / `[~]` / `[x]`. A–F — по плану overhaul.

| Protocol | A catalog | B params | C meta | D custom | E i18n | F demux | Notes |
|----------|-----------|----------|--------|----------|--------|---------|-------|
| vless | [x] | [x] | [x] | [x] | [x] | [x] | **catalogsqlite SoT** (`data/vless.sqlite`); JSON `presets/data/vless` removed; ready=full constructor schema; param i18n via base tag; mux/WS early-data via params |
| hysteria2 | [x] | [x] | [x] | [x] | [x] | [x] | bandwidth + ignore_client_bandwidth; first_bytes |
| wireguard | [x] | [x] | [x] | [x] | [x] | n/a | hub PUT mtu/awg/s/h + masquerade or manual i1–i5; wg_custom schema-only |
| trojan | [x] | [x] | [x] | [x] | [x] | [x] | constructor = transport + tls_mode |
| anytls | [x] | [x] | [x] | [x] | [x] | [x] | stock ALPN/fingerprint; idle = anytls_idle |
| trusttunnel | [x] | [x] | [x] | [x] | [x] | [x] | mode h2/h3→http2/http3; anti_dpi + UDP + fallback + force_http1 |
| shadowquic | [x] | [x] | [x] | [x] | [x] | [x] | JLS + congestion/0-RTT/udp_over_stream |
| sudoku | [x] | [x] | [x] | [x] | [x] | [x] | AEAD/multiplex/padding/fallback |
| mieru | [x] | [x] | [x] | [x] | [x] | [x] | stock MTU + pattern; custom multiplexing |
| carrier | [x] | [x] | [x] | [x] | [x] | n/a | carrier_custom: provider/token/transport |
| vmess | [x] | [x] | [x] | [x] | [x] | [~] | demux defaults = VLESS; vmess_* substitute |
| tuic | [x] | [x] | [x] | [x] | [x] | [x] | stock congestion / udp_relay / 0-RTT |
| shadowsocks | [x] | [x] | [x] | [x] | [x] | n/a | method + network + UoT (identity presets) |
| shadowtls | [x] | [x] | [x] | [x] | [x] | n/a | strict_mode + fingerprint; wildcard = отдельные пресеты |
| naive | [x] | [x] | [x] | [x] | [x] | n/a | network = naive_tls / naive_quic |
| snell | [x] | [x] | [x] | [x] | [x] | [x] | obfs_mode + obfs_host |
| ssh | [x] | [x] | [x] | [x] | [x] | [x] | server_version + client_version раздельно |
| derp | [x] | [x] | [x] | [x] | [x] | n/a | path + websocket; udp native\|uot (не disabled) |
| http/socks/mixed | [x] | [x] | [x] | [x] | [x] | n/a | fingerprint на TLS; tls_mode/UoT = custom/identity |
| hysteria1 | [x] | [x] | [x] | [x] | [x] | n/a | bandwidth knobs + obfs; legacy |
| cloudflared | [x] | [x] | [x] | [x] | [x] | n/a | status=deferred — hidden from subserver catalog (inbound_only; client outbound TBD) |

**Materialize:** stock knobs + `applyStockUTLSFingerprint`; customs в `custom_knobs.go`. flow/packet_encoding: `none` снимает поле.

**WG hub:** `PUT /v1/controlplane/wg` — `mtu`, `awg{}` (`jc/jmin/jmax`, `s1–s4`, `h1–h4`, masquerade `id/ip/ib` or manual `i1–i5`), optional `masquerade_mode`/`masquerade_url`/`manual_init`. `wg_custom` — schema каталога, не from-presets.

**i18n:** locale prune; ru+en приоритет; 11 langs; API fallback → en (`i18n.Get`, `ResolveI18n`/`PickLocalized`: lang→en). Pass3–11: META_UNUSED=0 (knobs), empty enum=0, demux refs; EN purity; TT h2→http2; DERP udp native|uot; SQ udp_over_stream.

**Protocol gate:** каталог A–F закрыт; оставшиеся custom-only — identity/constructor (не пробелы). UI клиента — следующий этап после вашего OK.

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
