#!/bin/bash
set -euo pipefail
python3 <<'PY'
import json, subprocess, time, ssl
from urllib import request

ctx = ssl._create_unverified_context()

def api(method, path, body=None):
    data = None if body is None else json.dumps(body).encode()
    req = request.Request(
        f"https://127.0.0.1:8080{path}",
        data=data,
        method=method,
        headers={"Authorization": "Bearer vps-cp-token-dev-only", "Content-Type": "application/json"},
    )
    with request.urlopen(req, context=ctx) as r:
        return json.load(r)

u = api("POST", "/v1/controlplane/users", {"name": "probe-reality4", "enabled": True})["data"]
tok = u["sub_token"]
with request.urlopen(request.Request(f"https://127.0.0.1:8080/v1/sub/{tok}?set=e2e-demux&preset=vless_ws_reality"), context=ctx) as r:
    sub = json.load(r)
ob = sub["outbounds"][0]
print("base transport", ob.get("transport"))

variants = []
# A: as-is member
a = dict(ob); a["server"]="127.0.0.1"; a["server_port"]=49758
variants.append(("as_is_member", a))
# B: strip early data
b = dict(ob); b["server"]="127.0.0.1"; b["server_port"]=49758
tr=dict(b.get("transport") or {}); tr.pop("max_early_data", None); tr.pop("early_data_header_name", None)
b["transport"]=tr
variants.append(("no_early_member", b))
# C: Host = SNI
c = dict(b); tr=dict(c["transport"]); tr["headers"]={"Host":["www.microsoft.com"]}; c["transport"]=tr
variants.append(("host_sni_member", c))
# D: through demux
d = dict(b); d["server"]="127.0.0.1"; d["server_port"]=443
variants.append(("no_early_demux", d))

for name, outbound in variants:
    cfg = {
        "log": {"level": "info"},
        "inbounds": [{"type": "mixed", "tag": "m", "listen": "127.0.0.1", "listen_port": 19330}],
        "outbounds": [outbound, {"type": "direct", "tag": "direct"}],
        "route": {"final": outbound["tag"]},
    }
    json.dump(cfg, open("/tmp/client-rv.json", "w"), indent=2)
    subprocess.run(["docker", "rm", "-f", "probe-rv"], capture_output=True)
    subprocess.run([
        "docker", "run", "-d", "--name", "probe-rv", "--network", "host",
        "-v", "/tmp/client-rv.json:/work/client.json:ro",
        "ghcr.io/sagernet/sing-box:v1.12.12", "run", "-c", "/work/client.json",
    ], check=True)
    time.sleep(1.0)
    p = subprocess.run(["curl", "-x", "http://127.0.0.1:19330", "-sS", "-m", "12", "https://1.1.1.1/cdn-cgi/trace"], capture_output=True, text=True)
    ok = p.returncode == 0 and "ip=" in (p.stdout or "")
    print(name, "OK" if ok else "FAIL", ((p.stdout or "") + (p.stderr or ""))[:120].replace("\n", " "))
    if not ok:
        logs = subprocess.run(["docker", "logs", "probe-rv"], capture_output=True, text=True)
        err = ((logs.stdout or "") + (logs.stderr or ""))
        for line in err.splitlines()[-3:]:
            print(" ", line)
    subprocess.run(["docker", "rm", "-f", "probe-rv"], capture_output=True)
PY
