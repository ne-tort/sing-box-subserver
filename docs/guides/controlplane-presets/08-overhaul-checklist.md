# 08 — Overhaul checklist (living)

Статусы: `[ ]` / `[~]` / `[x]`. A–F — по плану overhaul.

| Protocol | A catalog | B params | C meta | D custom | E i18n | F demux | Notes |
|----------|-----------|----------|--------|----------|--------|---------|-------|
| vless | [x] | [x] | [x] | [x] | [x] | [x] | эталон; materialize knobs для transport/tls_mode |
| hysteria2 | [x] | [x] | [x] | [x] | [x] | [x] | hy2_custom: obfs/bandwidth/masquerade wiring |
| wireguard | [x] | [x] | [x] | [~] | [x] | n/a | AWG knobs в meta; hub apply через WG API |
| trojan | [x] | [x] | [x] | [x] | [x] | [x] | constructor = transport + tls_mode |
| anytls | [x] | [x] | [x] | [x] | [x] | [x] | custom: fingerprint/ALPN/idle_session |
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

**Materialize (custom):** `tls_mode` для конструкторов учитывается через `domain.BindingUsesReality` / `BindingTLSMode` (не статический trait `reality`). Knobs: hy2 / vless-like transport / SS / socks UoT / http|mixed / TT / tuic 0-RTT / naive network.

**Примечание:** Docker/invariant smoke matrix по всем тегам end-to-end в этой итерации не прогонялся — каталог/schema/locales/materialize unit tests закрыты; matrix — по мере CI.

## Foundation

- [x] params_schema v2
- [x] locales: 11 языков Hiddify; fallback → en
- [x] ADR + custom arch + demux fullstack
- [x] materialize wiring для custom constructors
- [x] `inject_optional_param_meta.py` / `generate_all_catalog_locales.py` / `strengthen_weak_help.py`

## Scripts

```bash
python scripts/inject_optional_param_meta.py
python scripts/generate_all_catalog_locales.py
python scripts/strengthen_weak_help.py
go test -tags with_controlplane ./internal/controlplane/...
```
