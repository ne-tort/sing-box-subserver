# Preset catalog data layout

Embedded via `go:embed` from `internal/controlplane/presets/data/`.

## Layout

```
data/
  index.json           # ordered protocol tags
  _schema/             # human schemas (this folder)
  <protocol>/
    protocol.json      # protocol metadata + i18n
    <invariant_tag>.json
```

## Naming

- Protocol tag = sing-box inbound `type` (`vless`, `hysteria2`, `ssh`).
- Invariant tag = `{abbr}_{features}` snake_case (`vless_reality`, `ss_aes256`, `hy2`).
- `aliases` keep old hyphen IDs (`vless-reality-tcp`) resolvable.

## Status

| status | Meaning |
|--------|---------|
| `stable` | Templates required; safe for materialize |
| `lab` | Templates present; experimental |
| `planned` | `protocol.json` only — fill later |

## Adding an invariant

1. Study sing-box inbound/outbound options for the protocol.
2. Copy a sibling JSON; set `tag`, `aliases`, `i18n.ru`, `traits`, `demux_hints`, `scores`, templates.
3. Run `go test ./internal/controlplane/presets/ -tags with_controlplane`.
4. See operator guide: `docs/guides/controlplane-presets/04-adding-invariant.md`.
