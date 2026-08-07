# Live VPS controlplane tests

## Target

- Host: `163.5.180.181`
- Domain: `wiki.ai-qwerty.ru`
- Mgmt: HTTPS `:8080` (self-signed) — use `CP_INSECURE=1`

## Docs (scenarios ↔ this suite)

Operator/client flows with step lists and success checks:

→ [`docs/controlplane/scenarios/00-index.md`](../../docs/controlplane/scenarios/00-index.md)

API how-to modules:

→ [`docs/controlplane/api/00-index.md`](../../docs/controlplane/api/00-index.md)

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

| File | Coverage | Scenario |
|------|----------|----------|
| `test_01_bootstrap_meta` | health, bootstrap caps/flows, status/ready, protocols/presets, demux match meta UX | [01](../../docs/controlplane/scenarios/01-wizard-bootstrap.md) |
| `test_02_dns_route` | GET/PUT dns+route templates | [08](../../docs/controlplane/scenarios/08-ownership-and-expiry.md) |
| `test_03_presets_install` | from-presets, edit set, user, subscription dump, e2e-reality-tcp | [02](../../docs/controlplane/scenarios/02-single-presets.md), [05](../../docs/controlplane/scenarios/05-reality.md) |
| `test_04_demux_groups` | substitutions, install :443, autoport, slot_snis persist | [03](../../docs/controlplane/scenarios/03-demux-443-dual.md) |
| `test_05_tls_acme` | Default SSL, ACME profile, params.ssl_profile, optional IP | [04](../../docs/controlplane/scenarios/04-tls-acme.md) |
| `test_06_reality` | profiles put + all-rejected | [05](../../docs/controlplane/scenarios/05-reality.md) |
| `test_07_client_remote` | Client docker **on VPS** (`--network host`) | [06](../../docs/controlplane/scenarios/06-client-remote-e2e.md) |
| `test_07_client_docker` | Optional local Docker Desktop (`CP_WIN_DOCKER=1`) | — |
| `test_08_matrix_lab_transports` | allow_lab / WS Reality matrix; gRPC Reality | [07](../../docs/controlplane/scenarios/07-lab-transports.md) |

```powershell
$env:CP_BASE='https://163.5.180.181:8080'
$env:CP_TOKEN='vps-cp-token-dev-only'
$env:CP_INSECURE='1'
$env:CP_SSH_HOST='163.5.180.181'
python -m pytest tests/vps_cp/test_07_client_remote.py -v --tb=short
```

Known gaps: gRPC Reality stays `demux_lab`; last-deactivate idle claim is best-effort. ShadowQUIC / Sudoku / TrustTunnel are in `tags.server.controlplane`.
