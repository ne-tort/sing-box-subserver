# Scenario 02 — Single presets install

## Goal

Install one TCP preset with auto/explicit port, activate, edit, create user, fetch subscription fragment.

## Steps

1. Cleanup leftover set name if any (deactivate → delete).
2. `GET /v1/controlplane/presets?lang=en` — pick a simple TCP preset (e.g. shadowsocks / trojan / vless non-Reality).
3. ```http
   POST /v1/controlplane/sets/from-presets
   { "items": [{ "name": "e2e-ss", "preset": "<tag>", "listen_port": 18443 }], "activate": true }
   ```
4. Expect **201**, `activated === true`, `activated_sets == ["e2e-ss"]`, set `active === true`.
5. Poll `GET /v1/controlplane/status` until `ready.ok === true` (`api.wait_ready`).
6. `PUT /v1/controlplane/sets/e2e-ss` — edit description; keep bindings/port.
7. `POST /v1/controlplane/users` `{ "name": "…", "enabled": true }` → keep `sub_token` / `subscription_url`.
8. `GET /v1/sub/{token}` (raw) → `outbounds` present; note `meta.matched`.
9. Optional: `GET /v1/controlplane/subscription-tags?active_only=true`.

## Success

- Dataplane owns config (`config_mode=controlplane`).
- Subscription URL points at `public_host` (not accidental loopback when configured).

## Reinstall same name

```json
{ "items": [{ "name": "e2e-ss", "preset": "<tag>" }], "activate": true, "replace": true }
```

Without `replace` → `409 cp_name_conflict`.

## Tests

| File | Cases |
|------|-------|
| [`test_03_presets_install.py`](../../../tests/vps_cp/test_03_presets_install.py) | `test_from_presets_install_activate_user_sub`, `test_sets_list_get_shape_parity` |
| Artifacts | `artifacts/from_presets_install.json`, `sub_presets.json`, `subscription_tags.json` |

## API refs

[../api/04-sets-lifecycle.md](../api/04-sets-lifecycle.md) · [../api/05-users-subscription.md](../api/05-users-subscription.md)
