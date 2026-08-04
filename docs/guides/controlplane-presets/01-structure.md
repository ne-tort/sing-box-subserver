# 01 — Structure

## Layout

```
internal/controlplane/catalogsqlite/
  ref/
    <protocol>/
      protocol.json            # метаданные протокола + i18n
      <protocol>_custom.json   # constructor base (schema + templates)
      <invariant_tag>.json     # ready preset (+ param_values)
      variants.json            # user_variants / client_profiles
    demux/
      dg_*.json                # demux groups (seeded from demuxgroups.BuiltinGroups)
  data/
    catalog.sqlite             # embedded runtime blob
  sql/
    schema.sql
```

Regenerate blob after editing `ref/`:

```bash
go run -tags with_controlplane ./cmd/dump-demux-groups   # if demux catalog.go changed
go run -tags with_controlplane ./cmd/gen-catalogsqlite
```

Locales stay under `internal/controlplane/presets/i18n/locales/`.

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
- **planned** / **deferred** — скрыты из materialize-каталога

## WireGuard

Не inbound-set: singleton hub через `PUT /v1/controlplane/wg` (профили `wg`/`wg_awg2`/`wg_awg3`). Эталон **speed = 10** в методике оценок.
