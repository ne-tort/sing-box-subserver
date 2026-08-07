# 08 — Storage (controlplane)

## Layout

Under agent `data_dir`:

```
data_dir/
  config-owner.json
  controlplane/
    users.json             # users + sync/tombstone/ingress fields (14-users-sync)
    sets.json
    state.json
    ssl_profiles.json      # SSL profiles (self_signed | acme | acme_ip)
    ssl/<id>/              # leaf PEM, optional ACME store, optional ECH
      cert.crt|cert.key
      acme/                # certmagic data_directory for this profile
      ech.key.pem|ech.config.pem
    ssl/_slots/<sni>/      # demux per-SNI self-signed leaves (CN=demux_sni)
    free_dns.json          # sslip/nip/addr.tools state; drives bootstrap SSL profiles fd-*
    config_fragments.json  # optional raw dns/route/outbounds overrides
    rulesets/              # local .srs/.json for route.rule_set
    reality_config.json
    heads.json             # block content heads (see 13-commits)
    commits/<id>.json      # apply commit records
    commits/_meta.json     # pending_id + recent ids
```

Presets and demux groups live in the **binary** (embedded `catalogsqlite/data/catalog.sqlite`).
No runtime-editable preset files on disk under `data_dir`.

Block commits: [13-commits.md](13-commits.md).

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
