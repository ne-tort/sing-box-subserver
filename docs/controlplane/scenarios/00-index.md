# Scenarios — controlplane operator / client flows

Each scenario lists **goal**, **HTTP steps**, **success checks**, and **pytest anchors**
under [`tests/vps_cp/`](../../../tests/vps_cp/).

| # | Scenario | Primary tests |
|---|----------|---------------|
| [01](01-wizard-bootstrap.md) | Discover capabilities + ready shape | `test_01_bootstrap_meta` |
| [02](02-single-presets.md) | from-presets → user → sub | `test_03_presets_install` |
| [03](03-demux-443-dual.md) | Stable demux on :443 | `test_04_demux_groups`, `test_07` demux |
| [04](04-tls-acme.md) | Cert-manager + params.sni | `test_05_tls_acme`, `test_07` acme |
| [05](05-reality.md) | Reality pool + TCP inbound | `test_06_reality`, `test_03` e2e-reality-tcp, `test_07` |
| [06](06-client-remote-e2e.md) | Official sing-box client on VPS | `test_07_client_remote` |
| [07](07-lab-transports.md) | allow_lab WS/gRPC Reality | `test_08_matrix_lab_transports` |
| [08](08-ownership-and-expiry.md) | Expiry, panel handoff, last deactivate | docs + unit (partial live) |

## How to run the live suite

See [`tests/vps_cp/README.md`](../../../tests/vps_cp/README.md).

```powershell
$env:CP_BASE='https://163.5.180.181:8080'
$env:CP_TOKEN='vps-cp-token-dev-only'
$env:CP_DOMAIN='wiki.ai-qwerty.ru'
$env:CP_INSECURE='1'
$env:CP_SSH_HOST='163.5.180.181'
python -m pytest tests/vps_cp -v --tb=short -k "not client_docker"
```

Artifacts (status dumps, sub JSON, probe logs): `tests/vps_cp/artifacts/`.

## Related API modules

[../api/00-index.md](../api/00-index.md) · endpoint catalog [../05-api.md](../05-api.md)
