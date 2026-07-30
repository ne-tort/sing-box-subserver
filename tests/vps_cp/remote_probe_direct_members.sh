#!/bin/bash
set -euo pipefail
AUTH='Authorization: Bearer vps-cp-token-dev-only'
BASE='https://127.0.0.1:8080'
USERJSON=$(curl -sk -H "$AUTH" -H 'Content-Type: application/json' -d '{"name":"probe-direct-m","enabled":true}' "$BASE/v1/controlplane/users")
TOKEN=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["data"]["sub_token"])' <<<"$USERJSON")
curl -sk "$BASE/v1/sub/$TOKEN?set=e2e-demux" -o /tmp/sub-demux.json
curl -sk -H "$AUTH" "$BASE/v1/controlplane/sets/e2e-demux" -o /tmp/set-demux.json
python3 <<'PY'
import json, subprocess, time
sub=json.load(open("/tmp/sub-demux.json"))
st=json.load(open("/tmp/set-demux.json"))["data"]
ports=st.get("member_ports") or {}
print("member_ports", ports)
by_preset = {
    "hy2_salamander": ports.get("hy2_salamander"),
    "vless_ws_reality": ports.get("vless_ws_reality"),
}
for i, ob in enumerate(sub["outbounds"]):
    tag = ob.get("tag") or ""
    member = None
    for preset, p in by_preset.items():
        if preset.replace("_", "-") in tag or preset in tag or preset.split("_")[0] in tag:
            # match by known fragments
            pass
    if "hy2" in tag:
        member = ports.get("hy2_salamander")
    elif "reality" in tag or "vless_ws" in tag:
        member = ports.get("vless_ws_reality")
    print("try", ob["type"], tag, "member", member)
    if not member:
        continue
    ob2 = dict(ob)
    ob2["server"] = "127.0.0.1"
    ob2["server_port"] = int(member)
    cfg = {
        "log": {"level": "info"},
        "inbounds": [{"type": "mixed", "tag": "m", "listen": "127.0.0.1", "listen_port": 19200 + i}],
        "outbounds": [ob2, {"type": "direct", "tag": "direct"}],
        "route": {"final": ob2["tag"]},
    }
    path = f"/tmp/client-direct-{i}.json"
    json.dump(cfg, open(path, "w"), indent=2)
    name = f"probe-direct-{i}"
    subprocess.run(["docker", "rm", "-f", name], capture_output=True)
    subprocess.run(
        [
            "docker", "run", "-d", "--name", name, "--network", "host",
            "-v", f"{path}:/work/client.json:ro",
            "ghcr.io/sagernet/sing-box:v1.12.12", "run", "-c", "/work/client.json",
        ],
        check=True,
    )
    time.sleep(1.5)
    p = subprocess.run(
        ["curl", "-x", f"http://127.0.0.1:{19200+i}", "-sS", "-m", "25", "https://1.1.1.1/cdn-cgi/trace"],
        capture_output=True,
        text=True,
    )
    print("rc", p.returncode)
    print(((p.stdout or "") + (p.stderr or ""))[:400])
    if p.returncode != 0:
        logs = subprocess.run(["docker", "logs", name], capture_output=True, text=True)
        print(((logs.stdout or "") + (logs.stderr or ""))[-900:])
    subprocess.run(["docker", "rm", "-f", name], capture_output=True)
PY
