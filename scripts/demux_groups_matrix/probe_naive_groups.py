#!/usr/bin/env python3
"""Probe Naive embedded in branded demux groups (defaults + naive_quic claim).

Usage:
  python scripts/demux_groups_matrix/probe_naive_groups.py
"""
from __future__ import annotations

import os
import sys
import time
from pathlib import Path

HERE = Path(__file__).resolve().parent
INV = HERE.parent / "invariant_matrix"
sys.path.insert(0, str(INV))
sys.path.insert(0, str(HERE))

from render import DEFAULT_LX_BIN, IMAGE, require_lx_cronet_for_naive, write_json  # noqa: E402
from render_group import render_group_workdir  # noqa: E402
from run import (  # noqa: E402
    MIN_MBPS,
    compose_down,
    compose_up,
    container_ip,
    ensure_network,
    restart_iperf,
    rewrite_client_iperf_ip,
    run_cmd,
    run_iperf_client,
    wait_server_ready,
)

WORK = HERE / "work" / "_probe_naive_groups"
RESULTS = HERE / "results"

# (group, expected default cells) — Naive is TLS base in these brands.
GROUPS: list[tuple[str, list[str]]] = [
    ("dg_443_tls_quic", ["naive_tls", "hy2"]),
    ("dg_443_triple", ["vless_reality", "naive_tls", "hy2"]),
]

# Extra: TLS slot forced to naive_quic should claim QUIC (no separate hy2 member).
NAIVE_QUIC_CLAIM = ("dg_443_tls_quic", {"tls": "naive_quic"}, ["naive_quic"])


def _probe_tag(workdir: Path, tag: str, lx_bin: Path) -> dict:
    restart_iperf()
    rewrite_client_iperf_ip(
        workdir,
        tag,
        container_ip("inv-iperf"),
        server_ip=container_ip("inv-server"),
    )
    cname = f"ngprobe-{tag}"[:50]
    run_cmd(["docker", "rm", "-f", cname])
    os.environ["IPERF_TIME"] = os.environ.get("IPERF_TIME", "3")
    ok, mbps, out = run_iperf_client(workdir / "clients" / tag, lx_bin, cname, udp=False)
    status = "pass" if ok and (mbps or 0) >= MIN_MBPS else "fail"
    print(f"  [{status}] {tag} mbps={mbps}")
    return {"tag": tag, "status": status, "mbps": mbps, "detail": (out or "")[-400:]}


def probe_group(
    lx_bin: Path,
    group: str,
    expect_tags: list[str],
    slot_presets: dict[str, str] | None = None,
) -> dict:
    print(f"=== {group} defaults {expect_tags} slots={slot_presets or {}} ===")
    require_lx_cronet_for_naive(lx_bin, expect_tags)
    workdir = WORK / group
    info = render_group_workdir(
        group,
        workdir=workdir,
        lx_bin=lx_bin,
        image=IMAGE,
        slot_presets=slot_presets,
    )
    got = [c["tag"] for c in info["cells"]]
    for t in expect_tags:
        if t not in got:
            raise RuntimeError(f"{group}: expected cell {t}, got {got}")
    compose = workdir / "docker-compose.generated.yml"
    ensure_network()
    compose_down(compose)
    cells: list[dict] = []
    try:
        compose_up(compose)
        wait_server_ready()
        for tag in expect_tags:
            cells.append(_probe_tag(workdir, tag, lx_bin))
    finally:
        compose_down(compose)
    return {"group": group, "cells": cells, "slot_presets": slot_presets or {}}


def main() -> int:
    lx = Path(os.environ.get("LX_BIN", str(DEFAULT_LX_BIN)))
    RESULTS.mkdir(parents=True, exist_ok=True)
    rows: list[dict] = []
    t0 = time.time()
    for group, tags in GROUPS:
        rows.append(probe_group(lx, group, tags))
    g, slots, tags = NAIVE_QUIC_CLAIM
    rows.append(probe_group(lx, g, tags, slot_presets=slots))
    write_json(RESULTS / "naive_groups_probe.json", {"elapsed_s": round(time.time() - t0, 1), "rows": rows})
    fails = [c for r in rows for c in r["cells"] if c["status"] != "pass"]
    print(f"done fails={len(fails)}")
    return 1 if fails else 0


if __name__ == "__main__":
    raise SystemExit(main())
