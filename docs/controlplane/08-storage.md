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
    tls_profile.json       # TLS mode + self_signed/acme specs
    tls/server.crt|key     # only for mode=self_signed (active material)
    tls/self_signed.meta.json  # fingerprint of SelfSignedSpec (reuse / reissue)
    acme/                  # certmagic data_directory for ACME modes
```

Presets live in the **binary** (`internal/controlplane/presets` embed). No preset files
in data_dir for v1.

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
