#!/usr/bin/env python3
"""Docker+iperf runner for demux groups."""
from __future__ import annotations

import argparse
import json
import os
import sys
import time
from datetime import datetime, timezone
from pathlib import Path

HERE = Path(__file__).resolve().parent
INV = HERE.parent / "invariant_matrix"
sys.path.insert(0, str(INV))
sys.path.insert(0, str(HERE))

from render import DEFAULT_LX_BIN, IMAGE, write_json  # noqa: E402
from render_group import render_group_workdir  # noqa: E402
from run import (  # noqa: E402
    MIN_MBPS,
    ensure_network,
    compose_down,
    compose_up,
    container_ip,
    restart_iperf,
    rewrite_client_iperf_ip,
    run_cmd,
    run_iperf_client,
    wait_server_ready,
)

WORK = HERE / "work"
RESULTS = HERE / "results"

# Priority groups for CI / local gate (expand with --all).
PRIORITY = [
    "dg_443_dual",
    "dg_443_anytls_hy2",
    "dg_443_triple",
    "dg_443_tt_hy2",
    "dg_443_alpn_split",
    "dg_443_plain_tls",
    "dg_443_mieru_hy2",
    "dg_8443_quic_pair",
    "dg_443_modern5",
]

ALL_GROUPS = PRIORITY + [
    "dg_443_sni_stack",
    "dg_443_vless_family",
    "dg_443_stack6",
    "dg_443_quic_pair_sni",
    "dg_443_dense8",
    "dg_443_ssh_hy2",
    "dg_443_reality_sq",
    "dg_443_snell_hy2",
    "dg_443_broad7",
]


def run_group(group: str, *, keep: bool = False, slot_presets: dict[str, str] | None = None) -> list[dict]:
    workdir = WORK / group
    lx_bin = Path(os.environ.get("LX_BIN", str(DEFAULT_LX_BIN)))
    print(f"=== render {group} ===")
    if slot_presets:
        print(f"  slot_presets={slot_presets}")
    info = render_group_workdir(group, workdir=workdir, lx_bin=lx_bin, image=IMAGE, slot_presets=slot_presets)
    compose_path = workdir / "docker-compose.generated.yml"
    results: list[dict] = []
    ensure_network()
    compose_down(compose_path)
    try:
        compose_up(compose_path)
        wait_server_ready()
        iperf_ip = container_ip("inv-iperf")
        server_ip = container_ip("inv-server")
        print(f"[iperf] {iperf_ip} server={server_ip}")
        for cell in info["cells"]:
            tag = cell["tag"]
            restart_iperf()
            rewrite_client_iperf_ip(workdir, tag, iperf_ip, server_ip=server_ip)
            client_dir = workdir / "clients" / tag
            cname = f"dgmatrix-cli-{tag}"[:50]
            run_cmd(["docker", "rm", "-f", cname])
            ok, mbps, out = run_iperf_client(client_dir, lx_bin, cname, udp=False)
            status = "pass" if ok and (mbps or 0) >= MIN_MBPS else ("pass" if ok else "fail")
            if ok and mbps is not None and mbps < MIN_MBPS:
                status = "fail"
            print(f"  [{status}] {group}/{tag} mbps={mbps}")
            results.append(
                {
                    "group": group,
                    "tag": tag,
                    "status": status,
                    "mbps": mbps,
                    "detail": out[-2000:] if not ok else f"mbps={mbps}",
                }
            )
    finally:
        if not keep:
            compose_down(compose_path)
    return results


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--group", action="append", default=[])
    ap.add_argument("--all", action="store_true")
    ap.add_argument("--priority", action="store_true", default=True)
    ap.add_argument("--slot", action="append", default=[], help="slot_id=preset_tag (repeatable)")
    ap.add_argument("--keep", action="store_true")
    args = ap.parse_args()
    groups = list(args.group)
    if args.all:
        groups = list(ALL_GROUPS)
    elif not groups:
        groups = list(PRIORITY)
    slot_presets: dict[str, str] = {}
    for raw in args.slot:
        if "=" not in raw:
            print(f"bad --slot {raw!r}, want id=preset", file=sys.stderr)
            return 2
        k, v = raw.split("=", 1)
        slot_presets[k.strip()] = v.strip()

    RESULTS.mkdir(parents=True, exist_ok=True)
    all_res: list[dict] = []
    for g in groups:
        try:
            all_res.extend(run_group(g, keep=args.keep, slot_presets=slot_presets or None))
        except Exception as e:
            print(f"  [fail] {g} harness: {e}")
            all_res.append({"group": g, "tag": "*", "status": "fail", "mbps": None, "detail": str(e)})

    doc = {
        "ts": datetime.now(timezone.utc).isoformat(),
        "min_mbps": MIN_MBPS,
        "results": all_res,
    }
    write_json(RESULTS / "demux_groups.json", doc)
    passes = sum(1 for r in all_res if r["status"] == "pass")
    fails = sum(1 for r in all_res if r["status"] == "fail")
    print(f"\nSUMMARY pass={passes} fail={fails}")
    lines = [
        "# Demux groups matrix",
        "",
        f"Generated: {doc['ts']}",
        "",
        "| group | tag | status | Mbps |",
        "|-------|-----|--------|------|",
    ]
    for r in all_res:
        mbps = r.get("mbps")
        mbps_s = f"{mbps:.2f}" if isinstance(mbps, (int, float)) else "-"
        lines.append(f"| {r.get('group')} | {r.get('tag')} | {r.get('status')} | {mbps_s} |")
    (RESULTS / "SUMMARY.md").write_text("\n".join(lines) + "\n", encoding="utf-8")
    return 1 if fails else 0


if __name__ == "__main__":
    raise SystemExit(main())
