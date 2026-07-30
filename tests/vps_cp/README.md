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

| `test_07_client_remote` | Client docker **on VPS** (`--network host`): presets, ACME, Reality TCP, Hy2 member |
| `test_07_client_docker` | Optional local Docker Desktop (`CP_WIN_DOCKER=1`) |

```powershell
$env:CP_BASE='https://163.5.180.181:8080'
$env:CP_TOKEN='vps-cp-token-dev-only'
$env:CP_INSECURE='1'
$env:CP_SSH_HOST='163.5.180.181'
python -m pytest tests/vps_cp/test_07_client_remote.py -v --tb=short
```

Known gaps (artifacts `KNOWN_GAP_*`): demux `:443` → Hy2 (salamander vs `protocol=quic`); demux WS Reality member path.
