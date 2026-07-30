# Scenario 06 — Client remote e2e (official sing-box)

## Goal

Prove subscription fragments work with **ghcr.io/sagernet/sing-box** run on the VPS (`--network host`), not from a Windows Docker NAT path.

## Pattern

1. Ensure target set exists and is **active** (created by earlier suite steps).
2. SSH to `$CP_SSH_HOST`; create user via management API; `GET /v1/sub/{token}?set=<name>`.
3. Pick outbound (prefer VLESS without vision/udp443).
4. Write minimal client JSON: mixed inbound + outbound + `route.final`.
5. `docker run --network host … sing-box run -c …`
6. `curl -x http://127.0.0.1:<mixed> https://1.1.1.1/cdn-cgi/trace` → line `ip=…`

## Covered sets

| Set | Expect |
|-----|--------|
| `e2e-ss` | OK |
| `e2e-acme-tls` | OK (when ACME suite created it) |
| `e2e-reality-tcp` | OK |
| `e2e-demux` | member + :443 for Reality and Hy2 |

## Tests

| File | Cases |
|------|-------|
| [`test_07_client_remote.py`](../../../tests/vps_cp/test_07_client_remote.py) | parametrized single inbound + `test_remote_demux_defaults_member_and_front` |
| Optional local Win Docker | `test_07_client_docker.py` (`CP_WIN_DOCKER=1`) — not normative |

## API refs

[../api/05-users-subscription.md](../api/05-users-subscription.md) · [../07-subscriptions.md](../07-subscriptions.md)
