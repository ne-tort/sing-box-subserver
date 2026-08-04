# 09 — Demux cost model (1 public demux + N members)

## Model

One demux install produces:

| Component | Count |
|-----------|-------|
| Public demux inbound | 1 (listen on `suggested_port`, usually :443) |
| Member inbounds | **N** = enabled slots (dial target `127.0.0.1:41000–60000`) |
| Client outbounds (subscription) | ≈ N (one per member preset) |

So **listeners ≈ 1 + N**. Example: `dg_443_broad7` (Full Arsenal) → 7 members → **8 listeners**.

Per member slot also allocates:

- Protocol state (TLS/Reality material, users[], QUIC stacks for Hy2/TUIC/ShadowQUIC)
- Private port from the demux pool (exhaustion → `cp_port_exhausted`)
- SNI / cert / Reality assignment when `match_hint=sni_pool`

QUIC slots cost more CPU (congestion + crypto) than plain TCP (mieru/sudoku/snell/ssh).

## Catalog capacity ladder (stable → lab)

| Group | Brand | N (defaults) | Role |
|-------|-------|--------------|------|
| `dg_443_dual` | Bypasser | 2 | Stable baseline |
| `dg_443_tls_quic` / `dg_443_triple` | HTTPS Mask / DPI Triple | 2–3 | Stable mid |
| `dg_443_fullstack` | DPI Killer | 5 | Stable flagship |
| `dg_443_exotic` | Oddball | 4 | Stable exotic |
| `dg_443_modern5` / `dg_443_sni_stack` | Vision Pack / SNI Lattice | 5 | Lab multi-SNI |
| `dg_443_broad7` | Full Arsenal | 7 | Lab max density |
| `dg_443_quic_storm` | QUIC Storm | 2 | Lab UDP-only |

SoT: demux groups live in **catalogsqlite** (`ref/demux/*.json` → `demux_groups` / `demux_slots`). Slot presets must `Owns()` ready tags.

## Order-of-magnitude (not SLA)

Without claiming product SLAs, expect on a small VPS (2 vCPU / 2–4 GiB):

- Idle RSS grows roughly with N×(TLS session caches + QUIC buffers); Full Arsenal is noticeably heavier than Bypasser.
- Demux match itself is cheap vs protocol crypto; load is dominated by active flows on QUIC members.
- Prefer dual/triple/fullstack for production; treat broad7 as lab / high-end edges.

## Probe

```bash
# optional docker stats probe (order-of-magnitude)
pwsh scripts/demux_cost_probe.ps1
# or:
# python scripts/demux_groups_matrix/run.py --group dg_443_fullstack --group dg_443_broad7 --defaults-only --keep
# then: docker stats --no-stream inv-server
```

Unit coverage for shape (1 demux + N dials, no inject):  
`go test -tags with_controlplane ./internal/controlplane/demuxgroups/ -run TestBuildInstallAllGroupsDefaults`.
