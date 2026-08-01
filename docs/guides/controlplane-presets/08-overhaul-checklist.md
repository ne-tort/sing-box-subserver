# 08 — Overhaul checklist (living)

Статусы: `[ ]` / `[~]` / `[x]`. A–F — по плану overhaul.

| Protocol | A catalog | B params | C meta | D custom | E i18n | F demux | Notes |
|----------|-----------|----------|--------|----------|--------|---------|-------|
| vless | [x] | [x] | [x] | [x] | [x] | [x] | эталон; materialize knobs transport/tls_mode |
| hysteria2 | [x] | [x] | [x] | [x] | [x] | [x] | hy2_custom: obfs/bandwidth/masquerade wiring |
| wireguard | [x] | [x] | [x] | [x] | [x] | n/a | hub PUT + jc/jmin/jmax overrides; i1–i5 removed |
| trojan | [x] | [x] | [x] | [x] | [x] | [x] | constructor = transport + tls_mode |
| anytls | [x] | [x] | [x] | [x] | [x] | [x] | fingerprint/ALPN/idle_session |
| trusttunnel | [x] | [x] | [x] | [x] | [x] | [x] | mode + anti_dpi + fallback |
| shadowquic | [x] | [x] | [x] | [x] | [x] | [x] | JLS params |
| sudoku | [x] | [x] | [x] | [x] | [x] | [x] | AEAD/multiplex/padding/fallback |
| mieru | [x] | [x] | [x] | [x] | [x] | [x] | transport/multiplexing/MTU/pattern |
| carrier | [x] | [x] | [x] | [x] | [x] | n/a | required_guide room |
| tuic | [x] | [x] | [x] | [x] | [x] | [x] | congestion / udp_relay / 0-RTT |
| vmess | [x] | [x] | [x] | [x] | [x] | [~] | constructor parity with vless (no Vision) |
| shadowsocks | [x] | [x] | [x] | [x] | [x] | n/a | method + network + UoT |
| shadowtls | [x] | [x] | [x] | [x] | [x] | n/a | handshake + strict + wildcard + uTLS |
| naive | [x] | [x] | [x] | [x] | [x] | n/a | network tcp/udp + ALPN switch |
| snell | [x] | [x] | [x] | [x] | [x] | [x] | obfs_mode + obfs_host |
| ssh | [x] | [x] | [x] | [x] | [x] | [x] | server_version + client_version |
| derp | [x] | [x] | [x] | [x] | [x] | n/a | path/websocket/udp/fingerprint |
| http/socks/mixed | [x] | [x] | [x] | [x] | [x] | n/a | tls_mode / UoT / outbound_type |
| hysteria1 | [x] | [x] | [x] | [x] | [x] | n/a | bandwidth + obfs; legacy |
| cloudflared | [x] | [x] | [x] | [x] | [x] | n/a | token + protocol/PQ/HA |

**Materialize (custom):** `tls_mode` через `domain.BindingUsesReality` / `BindingTLSMode`. Knobs: hy2 / vless-like / SS / socks / http|mixed / TT / tuic / naive / lite customs.

**WG hub:** `PUT /v1/controlplane/wg` принимает `jc|jmin|jmax` или `awg{}` поверх generate; `i1–i5` отвергаются. `wg_custom` — schema каталога, не from-presets.

**i18n:** locale files prune по протоколу/param_meta (убран кросс-мусор). ru+en приоритет; остальные 9 langs заполнены; API fallback → en.

**Примечание:** Docker/invariant smoke matrix end-to-end по всем тегам не прогонялся — unit tests controlplane закрыты.

## Foundation

- [x] params_schema v2
- [x] locales: 11 языков Hiddify; fallback → en; prune orphans
- [x] ADR + custom arch + demux fullstack
- [x] materialize wiring для custom constructors
- [x] WG hub AWG overrides
- [x] `generate_all_catalog_locales.py` / `rewrite_priority_locales.py`

## Scripts

```bash
python scripts/generate_all_catalog_locales.py
go test -tags with_controlplane ./internal/controlplane/...
```
