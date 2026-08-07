#!/usr/bin/env python3
"""One-shot naive_quic direct debug."""
from __future__ import annotations

import json
import os
import subprocess
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
INV = HERE.parent / "invariant_matrix"
sys.path.insert(0, str(INV))

from render import (  # noqa: E402
    DEFAULT_LX_BIN,
    IMAGE,
    docker_path,
    lx_bin_ro_mount,
    lx_stage_cmds,
    render_protocol_workdir,
    require_lx_cronet_for_naive,
)
from run import (  # noqa: E402
    compose_down,
    compose_up,
    container_ip,
    ensure_network,
    restart_iperf,
    rewrite_client_iperf_ip,
    run_cmd,
    wait_server_ready,
    write_compose,
)

os.environ.setdefault("IPERF_TIME", "3")
tag = "naive_quic"
lx = Path(os.environ.get("LX_BIN", str(DEFAULT_LX_BIN)))
wd = HERE / "work" / "_probe_naive" / "quic_direct_debug"
require_lx_cronet_for_naive(lx, [tag])
render_protocol_workdir(tags=[tag], workdir=wd, lx_bin=lx, image=IMAGE)

sj = json.loads((wd / "server.json").read_text(encoding="utf-8"))
print("SERVER_INBOUND", json.dumps(sj["inbounds"][0], indent=2)[:1200])
cj = json.loads((wd / "clients" / tag / "client.json").read_text(encoding="utf-8"))
ob = next(o for o in cj["outbounds"] if o.get("type") == "naive")
print("CLIENT_OB", json.dumps(ob, indent=2)[:1200])
cj["log"] = {"level": "debug"}
(wd / "clients" / tag / "client.json").write_text(json.dumps(cj, indent=2) + "\n", encoding="utf-8")

compose = write_compose(wd, lx)
ensure_network()
compose_down(compose)
compose_up(compose)
wait_server_ready()
restart_iperf()
rewrite_client_iperf_ip(wd, tag, container_ip("inv-iperf"), server_ip=container_ip("inv-server"))

cname = "naiveprobe-quic-dbg"
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
        "docker", "run", "--rm", "--name", cname, "--network", "invmatrix_net",
        "-v", lx_bin_ro_mount(lx),
        "-v", f"{docker_path(wd / 'clients' / tag)}:/work:ro",
        IMAGE, "bash", "-c", script,
    ],
    capture_output=True, text=True, timeout=60,
)
print(proc.stdout)
print(proc.stderr, file=sys.stderr)
print("--- server ---")
print(subprocess.check_output(["docker", "logs", "--tail", "60", "inv-server"], text=True, stderr=subprocess.STDOUT))
compose_down(compose)
