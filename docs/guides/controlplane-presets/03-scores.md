# 03 — Scores

Субъективные оценки 0–10 в JSON инварианта. Не метрики рантайма.

## DPI (`scores.dpi`)

Эталон: **`vless_reality` = 10**.

Ориентиры:

| Диапазон | Типично |
|----------|---------|
| 1–2 | plaintext socks/http, raw SS |
| 3–4 | plaintext VLESS/VMess |
| 5–7 | classic TLS (trojan, vless_tls) |
| 8–10 | Reality / сильный fingerprint mimic |

## Speed (`scores.speed`)

Эталон: **WireGuard = 10** (не является inbound-пресетом каталога).

Ориентиры: Hy2/TUIC ближе к верху на хорошей UDP-сети; TCP+TLS ниже; lab SSH скромнее.

## Mobile / setup (optional)

- `mobile` — батарея, NAT, UDP reliability
- `setup` — простота выдачи клиенту / онбординга

Пересматривайте оценки при добавлении инвариантов; фиксируйте в git вместе с JSON.
