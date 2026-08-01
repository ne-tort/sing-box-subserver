# 08 — Overhaul checklist (living)

Статусы: `[ ]` / `[~]` / `[x]`. A–F — по плану overhaul.

| Protocol | A catalog | B params | C meta | D custom | E i18n | F demux | Notes |
|----------|-----------|----------|--------|----------|--------|---------|-------|
| vless | [x] | [x] | [x] | [x] | [x] | [x] | эталон; materialize knobs transport/tls_mode |
| hysteria2 | [x] | [x] | [x] | [x] | [x] | [x] | bandwidth + ignore_client_bandwidth knobs; first_bytes |
| wireguard | [x] | [x] | [x] | [x] | [x] | n/a | hub PUT mtu/awg; i1–i5 rejected; wg_custom schema-only |
| trojan | [x] | [x] | [x] | [x] | [x] | [x] | constructor = transport + tls_mode |
| anytls | [x] | [x] | [x] | [x] | [x] | [x] | fingerprint/ALPN/idle_session |
| trusttunnel | [x] | [x] | [x] | [x] | [x] | [x] | mode + anti_dpi + fallback |
| shadowquic | [x] | [x] | [x] | [x] | [x] | [x] | JLS + stock congestion/0-RTT knobs |
| sudoku | [x] | [x] | [x] | [x] | [x] | [x] | AEAD/multiplex/padding/fallback |
| mieru | [x] | [x] | [x] | [x] | [x] | [x] | stock MTU + pattern; custom multiplexing |
| carrier | [x] | [x] | [x] | [x] | [x] | n/a | carrier_custom: provider/token/transport knobs |
| vmess | [x] | [x] | [x] | [x] | [x] | [~] | demux defaults = VLESS; vmess_* substitute, не primary |
| tuic | [x] | [x] | [x] | [x] | [x] | [x] | stock congestion / udp_relay / 0-RTT knobs |
| shadowsocks | [x] | [x] | [x] | [x] | [x] | n/a | method + network + UoT |
| shadowtls | [x] | [x] | [x] | [x] | [x] | n/a | stock strict_mode + handshake + uTLS |
| naive | [x] | [x] | [x] | [x] | [x] | n/a | network tcp/udp + ALPN switch |
| snell | [x] | [x] | [x] | [x] | [x] | [x] | obfs_mode + obfs_host |
| ssh | [x] | [x] | [x] | [x] | [x] | [x] | server_version + client_version |
| derp | [x] | [x] | [x] | [x] | [x] | n/a | path + websocket knobs |
| http/socks/mixed | [x] | [x] | [x] | [x] | [x] | n/a | tls_mode / UoT / outbound_type |
| hysteria1 | [x] | [x] | [x] | [x] | [x] | n/a | bandwidth knobs + obfs; legacy |
| cloudflared | [x] | [x] | [x] | [x] | [x] | n/a | token + PQ/HA на stock+custom |

**Materialize:** stock knobs для hy2 bandwidth/ignore, shadowquic cc/0rtt, shadowtls strict, mieru mtu, derp websocket, cloudflared PQ/HA, tuic, carrier provider map. Customs через `custom_knobs.go`.

**WG hub:** `PUT /v1/controlplane/wg` — `mtu`, `jc|jmin|jmax` или `awg{}`; `i1–i5` отвергаются. `wg_custom` — schema каталога, не from-presets.

**i18n:** locale prune; ru+en приоритет; 11 langs; API fallback → en. Pass3/5: META_UNUSED=0, empty first_bytes stable=0.

**Примечание:** Docker/invariant smoke matrix end-to-end по всем тегам не прогонялся — unit tests controlplane закрыты.

## Foundation

- [x] params_schema v2
- [x] locales: 11 языков Hiddify; fallback → en; prune orphans
- [x] ADR + custom arch + demux fullstack
- [x] materialize wiring для custom constructors
- [x] WG hub AWG overrides
- [x] `generate_all_catalog_locales.py` / `rewrite_priority_locales.py`
- [x] Pass5: stock optional params + demux first_bytes

## Scripts

```bash
python scripts/generate_all_catalog_locales.py
go test -tags with_controlplane ./internal/controlplane/...
```
