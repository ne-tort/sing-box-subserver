# 08 — Overhaul checklist (living)

Статусы: `[ ]` / `[~]` / `[x]`. A–F — по плану overhaul.

| Protocol | A catalog | B params | C meta | D custom | E i18n | F demux | Notes |
|----------|-----------|----------|--------|----------|--------|---------|-------|
| vless | [x] | [x] | [x] | [x] | [x] | [x] | эталон schema v2 + locales |
| hysteria2 | [x] | [x] | [x] | [x] | [x] | [x] | obfs/masquerade tradeoffs |
| wireguard | [x] | [x] | [x] | [x] | [x] | n/a | AWG knobs |
| trojan | [x] | [x] | [x] | [x] | [x] | [x] | transport params + fingerprint |
| anytls | [x] | [x] | [x] | [x] | [x] | [x] | demux TLS |
| trusttunnel | [x] | [x] | [x] | [x] | [x] | [x] | h2/h3/auto |
| shadowquic | [x] | [x] | [x] | [x] | [x] | [x] | JLS params |
| sudoku | [x] | [x] | [x] | [x] | [x] | [x] | plain demux |
| mieru | [x] | [x] | [x] | [x] | [x] | [x] | plain demux |
| carrier | [x] | [x] | [x] | [x] | [x] | n/a | required_guide room |
| tuic | [x] | [x] | [x] | [x] | [x] | [x] | QUIC substitute |
| vmess | [x] | [x] | [x] | [x] | [x] | n/a | transport params |
| shadowsocks | [x] | [x] | [x] | [x] | [x] | n/a | AEAD/2022 |
| shadowtls | [x] | [x] | [x] | [x] | [x] | n/a | handshake_server |
| naive | [x] | [x] | [x] | [x] | [x] | n/a | tls/quic |
| snell | [x] | [x] | [x] | [x] | [x] | [x] | plain |
| ssh | [x] | [x] | [x] | [x] | [x] | [x] | plain |
| derp | [x] | [x] | [x] | [x] | [x] | n/a | niche |
| http/socks/mixed | [x] | [x] | [x] | [x] | [x] | n/a | utility |
| hysteria1 | [x] | [x] | [x] | [x] | [x] | n/a | legacy |
| cloudflared | [x] | [x] | [x] | [x] | [x] | n/a | token |

**Примечание:** пункт A «Docker/invariant smoke» для всех тегов не прогонялся end-to-end в этой итерации — каталог/schema/locales закрыты; matrix — по мере CI.

## Foundation

- [x] params_schema v2
- [x] locales: 11 языков Hiddify; fallback → en
- [x] ADR + custom arch + demux fullstack
- [x] `inject_optional_param_meta.py` / `generate_all_catalog_locales.py` / `patch_stub_descriptions.py`

## Scripts

```bash
python scripts/inject_optional_param_meta.py
python scripts/patch_stub_descriptions.py
python scripts/generate_all_catalog_locales.py
go test -tags with_controlplane ./internal/controlplane/...
```
