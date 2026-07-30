# 01 — Structure

## Layout

```
internal/controlplane/presets/data/
  index.json                 # порядок протоколов
  <protocol>/
    protocol.json            # метаданные протокола + i18n
    <invariant_tag>.json     # инвариант (= файл)
  _schema/                   # описания полей
```

## Naming

| Level | Rule | Example |
|-------|------|---------|
| Protocol tag | = sing-box inbound `type` | `vless`, `hysteria2`, `ssh` |
| Invariant tag | `{abbr}_{features}` snake_case | `vless_reality`, `ss_aes256`, `hy2` |
| Short name | одна строка на мобильном | `VLESS Reality`, `SS AES256` |
| Aliases | старые hyphen-имена | `vless-reality-tcp` → `vless_reality` |

Аббревиатуры: `ss`, `hy`/`hy2`, `trojan`, `vmess`, `tuic`, `anytls`, `socks`, `http`, `ssh`.

## Status

- **stable** — templates обязательны, materialize OK
- **lab** — templates есть, экспериментально (`ssh_password`)
- **planned** — только `protocol.json` (наполнение позже)

## WireGuard

Не inbound-set: singleton hub через `PUT /v1/controlplane/wg` (профили `wg`/`wg_awg2`/`wg_awg3`). Эталон **speed = 10** в методике оценок.
