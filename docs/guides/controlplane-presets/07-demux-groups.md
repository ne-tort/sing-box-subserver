# Demux groups (controlplane)

Каталог готовых наборов инбаундов + demux (~10 branded групп).

## Зачем

На одном TCP-порту и одном UDP-порту публично может жить только по одному «голому» инбаунду.
Demux-группа занимает **один** публичный порт (tcp/udp по `networks`), а члены слушают случайные частные порты `127.0.0.1:41000–60000`. Demux **форвардит** (`dial`), не inject.

В каталоге только актуальные протоколы (без http/socks/vmess/trojan/hy1/ss).

## API для клиента

См. [`docs/controlplane/05-api.md`](../../controlplane/05-api.md) § Demux groups.

Типичный UX:

1. `GET /demux-groups` — список наборов + scores + `separation_summary` / per-slot `separation_tags`
2. `GET /demux-groups/{tag}` — `match_plan` (порядок first-match: tls.sni → protocol.quic → always)
3. `GET /demux-groups/{tag}/substitutions` — замены по слотам + `interchange_tags` / `fits_interchange`
4. `POST /sets/from-demux-group` `{ group, slot_presets?, listen_port?, activate: true }`  
5. Пользователь + subscription (+ query filters)

## Разведение по TLS / QUIC

| Механизм | Как |
|----------|-----|
| SNI pool | Уникальный `demux_sni` на слот → demux `match.tls.sni` / QUIC `sni` + `server_name` у TLS-инбаунда |
| Per-slot PEM | Для non-Reality TLS CP пишет `controlplane/tls/slots/<sni>.crt` с CN/SAN = demux_sni |
| Reality | `demux_sni` предпочитается при Reality assignment; materialize синхронизирует demux SNI с assignment |
| ALPN | `demux_alpn` / PreferredALPN → inbound `tls.alpn` (demux match = SNI) |
| protocol_only | Один QUIC-catch-all (типично Hy2), когда на порту один QUIC-слот |
| Naive H2+H3 | `naive_quic` на TLS-слоте занимает `protocol=quic`; отдельный QUIC-член пропускается |

Несколько Reality/TLS в одном наборе получают **разные** SNI из пула — demux без проблем держит много TLS на одном порту.

## Branded catalog

| Tag | Brand | Status | Суть |
|-----|-------|--------|------|
| `dg_443_dual` | Bypasser | stable | Reality + Hy2 |
| `dg_443_triple` | DPI Triple | stable | Reality + Naive TLS + Hy2 |
| `dg_443_fullstack` | DPI Killer | stable | Флагман ~5 слотов (Naive как TLS/h2) |
| `dg_443_tls_quic` | HTTPS Mask | stable | Naive TLS + Hy2 |
| `dg_443_exotic` | Oddball | stable | Naive + Hy2 + plain |
| `dg_443_modern5` | Vision Pack | lab | 2×Reality + Naive + 2×QUIC |
| `dg_443_sni_stack` | SNI Lattice | lab | Несколько TLS/Reality по hostname |
| `dg_443_broad7` | Full Arsenal | lab | Максимум слотов |
| `dg_443_quic_storm` | QUIC Storm | lab | Два QUIC |
| `dg_443_reality_sq` | Shadow Lane | lab | Reality + lab QUIC |

**Naive** встроен в наборы как TLS-основа и substitute (`naive_tls` / `naive_quic`). Отдельных однослотовых Naive-групп нет. Выбор `naive_quic` на TLS-слоте включает H3 и забирает QUIC-маршрут. Нужен `libcronet` у клиента и у агента для CP smoke.

## Docker matrix

```bash
cd vendor/sing-box-subserver
python scripts/demux_groups_matrix/run.py --priority   # gate
python scripts/demux_groups_matrix/run.py --all        # extended
python scripts/demux_groups_matrix/run.py --group dg_443_dual
```

### Известные ограничения

- **TrustTunnel** через demux dial — `demux_lab` (substitute only).
- **ShadowQUIC** — `demux_lab`; default Hy2. Salamander obfs не используется в demux.
- Параллельные QUIC: **Hy2 + TUIC** по SNI — ок (Vision Pack / QUIC Storm).
