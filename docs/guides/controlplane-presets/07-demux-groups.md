# Demux groups (controlplane)

Каталог готовых наборов инбаундов + demux на одном публичном порту.

## Зачем

На одном TCP-порту и одном UDP-порту публично может жить только по одному «голому» инбаунду.
Demux-группа занимает **один** публичный порт (tcp/udp по `networks`), а члены слушают случайные частные порты `127.0.0.1:41000–60000`. Demux **форвардит** (`dial`), не inject.

В каталоге только актуальные протоколы (без http/socks/vmess/trojan/hy1/ss).

## API для клиента

См. [`docs/controlplane/05-api.md`](../../controlplane/05-api.md) § Demux groups.

Типичный UX:

1. `GET /demux-groups` — список наборов + scores  
2. `GET /demux-groups/{tag}/substitutions` — замены по слотам (роли `tcp_reality` / `tcp_tls` / `tcp_plain` / `quic`) + метаданные пресетов  
3. `POST /sets/from-demux-group` `{ group, slot_presets?, listen_port?, activate: true }`  
4. Пользователь + subscription (+ query filters)

## Разведение по TLS / QUIC

| Механизм | Как |
|----------|-----|
| SNI pool | Уникальный `demux_sni` на слот → demux `match.tls.sni` / QUIC `sni` + `server_name` у TLS-инбаунда |
| Per-slot PEM | Для non-Reality TLS/TrustTunnel CP пишет `controlplane/tls/slots/<sni>.crt` с CN/SAN = demux_sni |
| Reality | `demux_sni` предпочитается при Reality assignment; materialize синхронизирует demux SNI с assignment |
| ALPN | `demux_alpn` / PreferredALPN → inbound `tls.alpn` + опционально demux match |
| protocol_only | Один QUIC-catch-all (типично Hy2), когда на порту один QUIC-слот |

Несколько Reality в одном наборе получают **разные** SNI из валидированного пула.

## Docker matrix

```bash
cd third_party/sing-box-subserver/scripts/demux_groups_matrix
python run.py --priority   # gate
python run.py --all        # extended
python run.py --group dg_443_dual
```

Требует тот же `sui-lx-iperf:local` / `LX_BIN`, что и invariant matrix.

### Docker gate

`--priority`: **pass=24 fail=0** (incl. `dg_443_modern5` ×5).  
`--all` (18 groups): **pass=63 fail=0** (2026-07-30).  
ShadowQUIC: JLS `server_name` syncs with `demux_sni` (materialize + harness); verify with  
`python run.py --group dg_443_reality_sq --slot quic=shadowquic_jls`.

### Каталог (слоты)

| Диапазон | Примеры |
|----------|---------|
| 2–3 | dual, anytls_hy2, snell_hy2, reality_sq, … |
| 5–8 | modern5, sni_stack, vless_family, stack6, broad7(7), dense8 |

### Известные ограничения

- **TrustTunnel** через demux dial — `demux_lab`.
- **ShadowQUIC**: JLS SNI sync с `demux_sni` починен; `protocol_only` substitute **pass** (`--slot quic=shadowquic_jls` ≈1 Gbps). Параллельный SQ+другой QUIC (`sni_pool`) всё ещё `demux_lab`.
- Параллельные QUIC: **Hy2 + TUIC** по SNI — ок.
- **dg_443_dense8** / **broad7** — широкие наборы с уникальными SNI (**broad7** Docker pass=7/7).
- **dg_443_snell_hy2** — Docker pass.
