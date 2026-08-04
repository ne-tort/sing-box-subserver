# 08 — Storage (controlplane)

## Layout

Under agent `data_dir`:

```
data_dir/
  config-owner.json
  controlplane/
    users.json
    sets.json
    state.json
    tls_profile.json       # self_signed knobs only
    cert_manager.json      # ACME domains + provider settings
    config_fragments.json  # optional raw dns/route/outbounds overrides
    rulesets/              # local .srs/.json for route.rule_set (from PUT route rulesets)
    tls/server.crt|key     # default self-signed PEM
    tls/self_signed.meta.json  # fingerprint of SelfSignedSpec (reuse / reissue)
    tls/slots/             # per-demux_sni self-signed PEMs
    acme/                  # certmagic data_directory for cert-manager
```

Presets and demux groups live in the **binary** (embedded `catalogsqlite/data/catalog.sqlite`).
No runtime-editable preset files on disk under `data_dir`.

## File rules

| Rule | Detail |
|------|--------|
| Mode | `0600` files, `0700` directory |
| Write | temp file in same directory + `rename` |
| Encoding | JSON UTF-8 |

## `state.json` (sketch)

```json
{
  "active_sets": ["mixed-443", "hy2-8443"],
  "last_materialize_sha256": "...",
  "last_materialize_at": "..."
}
```

## Restart behavior

1. Load JSON stores.
2. `configowner` loads persisted `config_mode`.
3. If mode is `controlplane` and `active_sets` non-empty → rematerialize (pick up eligibility changes).
4. If mode is `direct`/`subscribed`, do **not** steal ownership; `active_sets` may still be listed but Apply ownership stays with the other writer until operator re-activates (on Claim(controlplane) clear conflict — normative: leaving CP clears `active_sets` via `OnLeaveControlplane` hook).

## Migration

v1: no migrations. Additive JSON fields allowed; unknown fields ignored on read.
