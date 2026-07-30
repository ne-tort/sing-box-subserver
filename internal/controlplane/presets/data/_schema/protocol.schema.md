# protocol.json fields

| Field | Type | Notes |
|-------|------|-------|
| `schema_version` | int | Currently `1` |
| `tag` | string | Protocol id (= folder name, sing-box type) |
| `singbox_type` | string | Usually equals `tag` |
| `short_name` | string | Mobile-friendly label |
| `status` | string | `stable` \| `lab` \| `planned` |
| `i18n` | map | `ru` required; `en` optional. `{title, description}` |
| `default_cred_fields` | string[] | Hint for new invariants |
| `notes` | map | Free-form (`variants_family`, …) |
