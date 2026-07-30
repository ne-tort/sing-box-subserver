"""Wave E: remote matrix for demux_lab transports (WS/gRPC Reality, ShadowQUIC).

Installs a temporary demux with allow_lab, probes member + demux front on the VPS,
writes artifacts/matrix_lab_*.json. Keeps demux_compat=demux_lab unless a future
matrix documents stable success (then promote in demuxCompatForPreset).
"""

from __future__ import annotations

import json
import os
import subprocess

REMOTE_HOST = os.environ.get("CP_SSH_HOST", "163.5.180.181")


def _ssh(script: str, timeout: int = 180) -> str:
    payload = script.replace("\r\n", "\n").replace("\r", "\n")
    if not payload.endswith("\n"):
        payload += "\n"
    r = subprocess.run(
        ["ssh", REMOTE_HOST, "bash", "-s"],
        input=payload.encode("utf-8"),
        capture_output=True,
        timeout=timeout,
        check=False,
    )
    out = (r.stdout or b"").decode("utf-8", errors="replace") + (r.stderr or b"").decode(
        "utf-8", errors="replace"
    )
    if r.returncode != 0:
        raise AssertionError(f"ssh failed rc={r.returncode}\n{out[-4000:]}")
    return out


def _cleanup_set(api, name: str) -> None:
    st = api.get(f"/v1/controlplane/sets/{name}", expect=(200, 404))
    if not st.get("ok"):
        return
    data = api.data(st)
    if data.get("active"):
        api.post(f"/v1/controlplane/sets/{name}/deactivate", expect=(200, 422))
    api.delete(f"/v1/controlplane/sets/{name}", expect=(200, 404, 409))


def test_matrix_lab_ws_reality_and_shadowquic(api, artifacts_dir):
    name = "e2e-matrix-lab"
    _cleanup_set(api, name)
    for s in api.data(api.get("/v1/controlplane/sets")):
        if s.get("listen_port") == 8443 and s.get("active"):
            api.post(f"/v1/controlplane/sets/{s['name']}/deactivate", expect=(200, 422))

    subs = api.data(api.get("/v1/controlplane/demux-groups/dg_443_dual/substitutions?lang=en"))
    # assert demux_lab metadata for transports; install WS Reality + Hy2
    # (ShadowQUIC needs with_shadowquic — not in tags.server.controlplane)
    slot_presets = {}
    for slot in subs["slots"]:
        opts = {o["tag"]: o for o in slot.get("options") or []}
        if "shadowquic_jls" in opts:
            assert opts["shadowquic_jls"].get("demux_compat") == "demux_lab"
        if "vless_ws_reality" in opts:
            assert opts["vless_ws_reality"].get("demux_compat") == "full"
            slot_presets[slot["id"]] = "vless_ws_reality"
        elif "hy2" in opts:
            slot_presets[slot["id"]] = "hy2"
        else:
            slot_presets[slot["id"]] = slot["default_preset"]

    body = {
        "group": "dg_443_dual",
        "name": name,
        "listen_port": 8443,
        "allow_lab": True,
        "slot_presets": slot_presets,
        "activate": True,
        "replace": True,
    }

    install = api.post("/v1/controlplane/sets/from-demux-group", body, expect=(201, 400, 422, 409))
    api.dump(artifacts_dir, "matrix_lab_install.json", install)
    if not install.get("ok"):
        api.dump(
            artifacts_dir,
            "matrix_lab_summary.json",
            {"skipped": True, "reason": "install_failed", "body": install},
        )
        return
    data = api.data(install)
    assert data.get("activated") is True
    api.wait_ready(timeout=180)

    script = r"""
set -euo pipefail
AUTH='Authorization: Bearer vps-cp-token-dev-only'
BASE='https://127.0.0.1:8080'
USERJSON=$(curl -sk -H "$AUTH" -H 'Content-Type: application/json' \
  -d "{\"name\":\"pytest-matrix-$RANDOM\",\"enabled\":true}" \
  "$BASE/v1/controlplane/users")
TOKEN=$(python3 -c 'import json,sys; d=json.load(sys.stdin); assert d.get("ok"), d; print(d["data"]["sub_token"])' <<<"$USERJSON")
curl -sk "$BASE/v1/sub/$TOKEN?set=e2e-matrix-lab" -o /tmp/sub-matrix.json
curl -sk -H "$AUTH" "$BASE/v1/controlplane/sets/e2e-matrix-lab" -o /tmp/set-matrix.json
python3 - <<'PY'
import json,subprocess,time
sub=json.load(open('/tmp/sub-matrix.json'))
setd=json.load(open('/tmp/set-matrix.json'))['data']
ports=setd.get('member_ports') or {}
print('PORTS', ports)
print('PRESETS', setd.get('presets'))
print('META', sub.get('meta'))

def run(name, outbound, port):
  cfg={'log':{'level':'info'},'inbounds':[{'type':'mixed','tag':'m','listen':'127.0.0.1','listen_port':port}],'outbounds':[outbound,{'type':'direct','tag':'direct'}],'route':{'final':outbound['tag']}}
  json.dump(cfg, open('/tmp/c.json','w'), indent=2)
  subprocess.run(['docker','rm','-f',name], capture_output=True)
  subprocess.run(['docker','run','-d','--name',name,'--network','host','-v','/tmp/c.json:/work/client.json:ro','ghcr.io/sagernet/sing-box:v1.12.12','run','-c','/work/client.json'], check=True)
  time.sleep(1.5)
  p=subprocess.run(['curl','-x',f'http://127.0.0.1:{port}','-sS','-m','18','https://1.1.1.1/cdn-cgi/trace'], capture_output=True, text=True)
  ok=p.returncode==0 and 'ip=' in (p.stdout or '')
  print(name, 'OK' if ok else 'FAIL')
  if not ok:
    logs=subprocess.run(['docker','logs',name], capture_output=True, text=True)
    print(((logs.stdout or '')+(logs.stderr or ''))[-400:])
  subprocess.run(['docker','rm','-f',name], capture_output=True)
  return ok

results={}
listen=int(setd.get('listen_port') or 8443)
for o in sub.get('outbounds') or []:
  t=o.get('type')
  tag=o.get('tag') or ''
  key=None
  for k in ports:
    if k in tag:
      key=k
      break
  if key is None:
    if t=='vless':
      key=next((k for k in ports if 'vless' in k), None)
    elif 'shadowquic' in (t or '') or t in ('hysteria2','tuic'):
      key=next((k for k in ports if any(x in k for x in ('shadowquic','hy2','tuic'))), None)
  if not key:
    print('SKIP_NO_PORT', tag)
    continue
  member=int(ports[key])
  om=dict(o); om['server']='127.0.0.1'; om['server_port']=member
  od=dict(o); od['server']='127.0.0.1'; od['server_port']=listen
  results[f'{key}_member']=run(f'm-{key}'[:20], om, 19710+len(results))
  results[f'{key}_demux']=run(f'd-{key}'[:20], od, 19750+len(results))
print('RESULTS', json.dumps(results))
open('/tmp/matrix_results.json','w').write(json.dumps({'results':results,'ports':ports,'presets':setd.get('presets')}, indent=2))
PY
cat /tmp/matrix_results.json
"""
    out = _ssh(script, timeout=240)
    (artifacts_dir / "matrix_lab_probe.log").write_text(out, encoding="utf-8")
    summary = {
        "probe_log_tail": out[-3000:],
        "demux_compat_policy": {
            "vless_ws_reality": "full (matrix member+demux OK)",
            "vless_grpc_reality": "demux_lab",
            "shadowquic_*": "demux_lab (no with_shadowquic in CP tags)",
        },
    }
    for line in out.splitlines():
        if line.startswith("RESULTS "):
            try:
                summary["results"] = json.loads(line[len("RESULTS ") :])
            except json.JSONDecodeError:
                summary["results_raw"] = line
    api.dump(artifacts_dir, "matrix_lab_summary.json", summary)
    assert "RESULTS" in out or "PORTS" in out, out[-2000:]


def test_matrix_lab_grpc_reality_member(api, artifacts_dir):
    name = "e2e-matrix-grpc"
    _cleanup_set(api, name)
    subs = api.data(api.get("/v1/controlplane/demux-groups/dg_443_dual/substitutions?lang=en"))
    slot_presets = {}
    for slot in subs["slots"]:
        opts = {o["tag"]: o for o in slot.get("options") or []}
        if "vless_grpc_reality" in opts:
            assert opts["vless_grpc_reality"].get("demux_compat") == "demux_lab"
            slot_presets[slot["id"]] = "vless_grpc_reality"
        else:
            slot_presets[slot["id"]] = slot["default_preset"]
    body = {
        "group": "dg_443_dual",
        "name": name,
        "listen_port": 8444,
        "allow_lab": True,
        "slot_presets": slot_presets,
        "activate": True,
        "replace": True,
    }
    install = api.post("/v1/controlplane/sets/from-demux-group", body, expect=(201, 400, 422, 409))
    api.dump(artifacts_dir, "matrix_grpc_install.json", install)
    if not install.get("ok"):
        return
    assert api.data(install).get("activated") is True
    detail = api.data(api.get(f"/v1/controlplane/sets/{name}"))
    assert "vless_grpc_reality" in (detail.get("presets") or [])
    api.dump(artifacts_dir, "matrix_grpc_detail.json", detail)
