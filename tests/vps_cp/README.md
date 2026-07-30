# Live VPS controlplane tests

## Target

- Host: `163.5.180.181`
- Domain: `wiki.ai-qwerty.ru`
- Mgmt: HTTPS `:8080` (self-signed) — use `CP_INSECURE=1`

## Run

```bash
cd third_party/sing-box-subserver
python -m pip install -r tests/vps_cp/requirements.txt
set CP_BASE=https://163.5.180.181:8080
set CP_TOKEN=vps-cp-token-dev-only
set CP_DOMAIN=wiki.ai-qwerty.ru
set CP_INSECURE=1
python -m pytest tests/vps_cp -v --tb=short
```

Artifacts (subscriptions, status dumps) land in `tests/vps_cp/artifacts/`.

## Suites

| File | Coverage |
|------|----------|
| `test_01_bootstrap_meta` | health, bootstrap caps/flows, status/ready, protocols/presets, demux match meta UX |
| `test_02_dns_route` | GET/PUT dns+route templates |
| `test_03_presets_install` | from-presets, edit set, user, subscription dump |
| `test_04_demux_groups` | substitutions, slot alternatives, install :443, autoport |
| `test_05_tls_acme` | self-signed, cert-manager domain, params.sni, optional IP SAN |
| `test_06_reality` | profiles put + all-rejected |

Client-in-docker phase is separate (uses dumped `sub_*.url.txt`).
