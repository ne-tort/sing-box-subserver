# Scenario 07 — Lab transports (allow_lab)

## Goal

Install demux substitutes that are `demux_compat=demux_lab` / promoted variants; document matrix outcomes for UI policy.

## Policy (current)

| Preset | demux_compat | Notes |
|--------|--------------|-------|
| `vless_ws_reality` | **full** | Matrix 2026-07-30: member + demux front OK |
| `vless_grpc_reality` | demux_lab | Install with `allow_lab:true` |
| `shadowquic_*` | demux_lab | Needs `with_shadowquic` — **included** in `tags.server.controlplane` (+ agent box register) |

Stable group install **without** `allow_lab` must reject remaining lab presets (`cp_invalid_slot`).

## Steps (WS Reality + Hy2)

1. `GET …/dg_443_dual/substitutions` — assert `vless_ws_reality` is `full`, `shadowquic_jls` is `demux_lab`.
2. `POST /sets/from-demux-group` with `allow_lab: true` (still fine), `listen_port: 8443`, slot presets WS Reality + Hy2, `replace: true`.
3. Wait ready; SSH probe member + demux listen port for both outbounds.
4. Dump `artifacts/matrix_lab_summary.json`.

## Steps (gRPC Reality)

Same pattern on port 8444 with `vless_grpc_reality` + default QUIC; assert install accepts only with `allow_lab`.

## Tests

| File | Cases |
|------|-------|
| [`test_08_matrix_lab_transports.py`](../../../tests/vps_cp/test_08_matrix_lab_transports.py) | WS+Hy2 probe, gRPC install |
| Unit | `demuxgroups/compat_test.go` |

## API refs

[../api/03-catalog.md](../api/03-catalog.md) · [../api/04-sets-lifecycle.md](../api/04-sets-lifecycle.md)
