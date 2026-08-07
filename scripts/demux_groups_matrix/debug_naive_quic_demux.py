#!/usr/bin/env python3
"""One-shot naive_quic demux debug."""
from __future__ import annotations

import json
import os
import subprocess
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
INV = HERE.parent / "invariant_matrix"
sys.path.insert(0, str(INV))
sys.path.insert(0, str(HERE))

from render import (  # noqa: E402
    DEFAULT_LX_BIN,
    IMAGE,
    docker_path,
    lx_bin_ro_mount,
    lx_stage_cmds,
    require_lx_cronet_for_naive,
)
from render_group import render_group_workdir  # noqa: E402
from run import (  # noqa: E402
    compose_down,
    compose_up,
    container_ip,
    ensure_network,
    restart_iperf,
    rewrite_client_iperf_ip,
    run_cmd,
    wait_server_ready,
)

os.environ.setdefault("IPERF_TIME", "3")
tag = "naive_quic"
lx = Path(os.environ.get("LX_BIN", str(DEFAULT_LX_BIN)))
wd = HERE / "work" / "_probe_naive" / "quic_demux_debug"
require_lx_cronet_for_naive(lx, [tag])
render_group_workdir(
    "dg_443_tls_quic",
    workdir=wd,
    lx_bin=lx,
    image=IMAGE,
    slot_presets={"quic": tag},
)

cj = json.loads((wd / "clients" / tag / "client.json").read_text(encoding="utf-8"))
cj["log"] = {"level": "debug"}
(wd / "clients" / tag / "client.json").write_text(json.dumps(cj, indent=2) + "\n", encoding="utf-8")

compose = wd / "docker-compose.generated.yml"
ensure_network()
compose_down(compose)
compose_up(compose)
wait_server_ready()
restart_iperf()
rewrite_client_iperf_ip(wd, tag, container_ip("inv-iperf"), server_ip=container_ip("inv-server"))

cname = "naiveprobe-qdemux"
run_cmd(["docker", "rm", "-f", cname])
script = f"""
set -e
{lx_stage_cmds()}
/tmp/sing-box run -c /work/client.json >/tmp/box.log 2>&1 &
pid=$!
sleep 2
set +e
iperf3 -c 127.0.0.1 -p 15201 -t 3 -J >/tmp/iperf.json 2>/tmp/iperf.err
rc=$?
set -e
kill $pid 2>/dev/null || true
wait $pid 2>/dev/null || true
echo IPERF_RC=$rc
echo '--- box.log ---'
cat /tmp/box.log || true
"""
proc = subprocess.run(
    [
        "docker",
        "run",
        "--rm",
        "--name",
        cname,
        "--network",
        "invmatrix_net",
        "-v",
        lx_bin_ro_mount(lx),
        "-v",
        f"{docker_path(wd / 'clients' / tag)}:/work:ro",
        IMAGE,
        "bash",
        "-c",
        script,
    ],
    capture_output=True,
    text=True,
    timeout=60,
)
print(proc.stdout)
print(proc.stderr, file=sys.stderr)
print("--- server ---")
print(subprocess.check_output(["docker", "logs", "--tail", "80", "inv-server"], text=True, stderr=subprocess.STDOUT))
compose_down(compose)
