#!/usr/bin/env python3
"""Fast A/B for NaiveProxy: direct preset vs demux slot (no full priority matrix).

Usage:
  python scripts/demux_groups_matrix/probe_naive.py
  python scripts/demux_groups_matrix/probe_naive.py --skip-direct
  python scripts/demux_groups_matrix/probe_naive.py --skip-demux
"""
from __future__ import annotations

import argparse
import json
import os
import sys
import time
from pathlib import Path

HERE = Path(__file__).resolve().parent
SUBSERVER = HERE.parents[1]
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
from render import render_protocol_workdir  # noqa: E402
from run import write_compose as inv_write_compose  # noqa: E402


WORK = HERE / "work" / "_probe_naive"
RESULTS = HERE / "results"


def _probe_cell(workdir: Path, tag: str, lx_bin: Path) -> dict:
    restart_iperf()
    iperf_ip = container_ip("inv-iperf")
    server_ip = container_ip("inv-server")
    rewrite_client_iperf_ip(workdir, tag, iperf_ip, server_ip=server_ip)
    client_dir = workdir / "clients" / tag
    cname = f"naiveprobe-{tag}"[:50]
    run_cmd(["docker", "rm", "-f", cname])
    # Short iperf window for probe.
    os.environ["IPERF_TIME"] = os.environ.get("IPERF_TIME", "3")
    ok, mbps, out = run_iperf_client(client_dir, lx_bin, cname, udp=False)
    status = "pass" if ok and (mbps or 0) >= MIN_MBPS else "fail"
    # Dump client tls knobs for diagnosis.
    client = json.loads((client_dir / "client.json").read_text(encoding="utf-8"))
    ob = next((o for o in client.get("outbounds") or [] if o.get("type") == "naive"), {})
    tls = ob.get("tls") if isinstance(ob.get("tls"), dict) else {}
    dns_hosts = {}
    for srv in (client.get("dns") or {}).get("servers") or []:
        if isinstance(srv, dict) and srv.get("type") == "hosts":
            dns_hosts = srv.get("predefined") or {}
    print(f"  [{status}] {tag} mbps={mbps} insecure={tls.get('insecure')!r} sni={tls.get('server_name')!r}")
    print(f"         hosts={list(dns_hosts.keys())} cert={'certificate' in tls}")
    return {
        "tag": tag,
        "status": status,
        "mbps": mbps,
        "insecure": tls.get("insecure"),
        "server_name": tls.get("server_name"),
        "hosts": list(dns_hosts.keys()),
        "detail": (out or "")[-800:],
    }


def run_direct(lx_bin: Path, tag: str = "naive_tls") -> dict:
    print(f"=== A: direct {tag} (no demux) ===")
    require_lx_cronet_for_naive(lx_bin, [tag])
    workdir = WORK / f"direct_{tag}"
    cells = render_protocol_workdir(tags=[tag], workdir=workdir, lx_bin=lx_bin, image=IMAGE)
    _ = cells
    compose = inv_write_compose(workdir, lx_bin)
    ensure_network()
    compose_down(compose)
    try:
        compose_up(compose)
        wait_server_ready()
        return {"mode": "direct", **_probe_cell(workdir, tag, lx_bin)}
    finally:
        compose_down(compose)


def run_demux(
    lx_bin: Path,
    *,
    group: str = "dg_443_tls_quic",
    slot: str = "tls",
    preset: str = "naive_tls",
) -> dict:
    print(f"=== B: demux {group} slot {slot}={preset} ===")
    require_lx_cronet_for_naive(lx_bin, [preset])
    workdir = WORK / f"demux_{slot}_{preset}"
    info = render_group_workdir(
        group,
        workdir=workdir,
        lx_bin=lx_bin,
        image=IMAGE,
        slot_presets={slot: preset},
    )
    compose = workdir / "docker-compose.generated.yml"
    ensure_network()
    compose_down(compose)
    try:
        compose_up(compose)
        wait_server_ready()
        tags = [c["tag"] for c in info["cells"]]
        tag = preset if preset in tags else tags[0]
        return {"mode": "demux", "group": group, "slot": slot, **_probe_cell(workdir, tag, lx_bin)}
    finally:
        compose_down(compose)


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--skip-direct", action="store_true")
    ap.add_argument("--skip-demux", action="store_true")
    ap.add_argument("--skip-quic", action="store_true", help="skip naive_quic direct+demux probes")
    ap.add_argument("--group", default="dg_443_tls_quic", help="demux group for B")
    args = ap.parse_args()
    lx_bin = Path(os.environ.get("LX_BIN", str(DEFAULT_LX_BIN)))
    RESULTS.mkdir(parents=True, exist_ok=True)
    rows: list[dict] = []
    t0 = time.time()
    if not args.skip_direct:
        rows.append(run_direct(lx_bin, "naive_tls"))
    if not args.skip_demux:
        rows.append(run_demux(lx_bin, group=args.group, slot="tls", preset="naive_tls"))
    if not args.skip_quic:
        if not args.skip_direct:
            rows.append(run_direct(lx_bin, "naive_quic"))
        if not args.skip_demux:
            rows.append(run_demux(lx_bin, group=args.group, slot="quic", preset="naive_quic"))
    doc = {"elapsed_s": round(time.time() - t0, 1), "min_mbps": MIN_MBPS, "results": rows}
    write_json(RESULTS / "naive_probe.json", doc)
    fails = sum(1 for r in rows if r.get("status") != "pass")
    print(f"\nSUMMARY fail={fails} elapsed={doc['elapsed_s']}s -> {RESULTS / 'naive_probe.json'}")
    return 1 if fails else 0


if __name__ == "__main__":
    raise SystemExit(main())
