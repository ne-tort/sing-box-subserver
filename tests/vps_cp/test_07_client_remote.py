"""Client connectivity probes executed on the VPS (docker --network host).

Windows Docker Desktop NAT is unreliable for these protocols toward the public IP;
probing on-host against 127.0.0.1 / public_host is the normative live check.
"""

from __future__ import annotations

import json
import os
import subprocess
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parent
REMOTE_HOST = os.environ.get("CP_SSH_HOST", "163.5.180.181")


def _ssh(script: str, timeout: int = 180) -> str:
    # Bytes stdin avoids Windows text-mode LF→CRLF translation into bash.
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


def _probe_set_script(set_name: str, port: int, *, rewrite_loopback: bool = True, outbound_index: int = 0) -> str:
    server_py = 'ob["server"] = "127.0.0.1"' if rewrite_loopback else "pass  # keep public_host from subscription"
    safe = set_name.replace("-", "_")
    user_prefix = f"pytest-client-{safe}-{outbound_index}"
    return f"""
set -euo pipefail
AUTH='Authorization: Bearer vps-cp-token-dev-only'
BASE='https://127.0.0.1:8080'
USERJSON=$(python3 - <<'PY'
import json, ssl, random
from urllib import request
ctx = ssl._create_unverified_context()
name = "{user_prefix}-" + str(random.randint(1, 10**9))
body = json.dumps({{"name": name, "enabled": True}}).encode()
req = request.Request(
    "https://127.0.0.1:8080/v1/controlplane/users",
    data=body,
    method="POST",
    headers={{"Authorization": "Bearer vps-cp-token-dev-only", "Content-Type": "application/json"}},
)
print(request.urlopen(req, context=ctx).read().decode())
PY
)
TOKEN=$(python3 -c 'import json,sys; d=json.load(sys.stdin); assert d.get("ok"), d; print(d["data"]["sub_token"])' <<<"$USERJSON")
curl -sk "$BASE/v1/sub/$TOKEN?set={set_name}" -o /tmp/sub-pytest.json
python3 - <<PY
import json
sub=json.load(open('/tmp/sub-pytest.json'))
assert sub.get('outbounds'), sub
# Prefer flow-none / first non-vision vless when multiple variants exist
outs=[o for o in sub['outbounds'] if o.get('type') not in ('direct','block','dns','selector','urltest')]
assert outs, sub
idx={outbound_index}
ob=outs[idx]
if ob.get('type')=='vless' and idx==0:
  for cand in outs:
    flow=(cand.get('flow') or '')
    if cand.get('type')=='vless' and 'vision' not in flow and 'udp443' not in flow:
      ob=cand
      break
ob=dict(ob)
{server_py}
cfg={{
  'log':{{'level':'info'}},
  'inbounds':[{{'type':'mixed','tag':'m','listen':'127.0.0.1','listen_port':{port}}}],
  'outbounds':[ob,{{'type':'direct','tag':'direct'}}],
  'route':{{'final':ob['tag']}},
}}
json.dump(cfg, open('/tmp/client-pytest.json','w'), indent=2)
print('TAG', ob.get('tag'))
print('TYPE', ob.get('type'))
print('PORT', ob.get('server_port'))
PY
docker rm -f pytest-client >/dev/null 2>&1 || true
docker run -d --name pytest-client --network host -v /tmp/client-pytest.json:/work/client.json:ro ghcr.io/sagernet/sing-box:v1.12.12 run -c /work/client.json >/dev/null
sleep 2
if curl -x http://127.0.0.1:{port} -sS -m 25 https://1.1.1.1/cdn-cgi/trace | tee /tmp/pytest-trace.txt | grep -q '^ip='; then
  echo RESULT OK
else
  echo RESULT FAIL
  docker logs pytest-client 2>&1 | tail -30
fi
docker rm -f pytest-client >/dev/null 2>&1 || true
"""


@pytest.mark.parametrize(
    "set_name,expect_ok",
    [
        ("e2e-ss", True),
        ("e2e-acme-tls", True),
        ("e2e-reality-tcp", True),
    ],
)
def test_remote_client_single_inbound(set_name: str, expect_ok: bool, artifacts_dir):
    # skip if set missing
    import ssl
    from urllib import request

    ctx = ssl._create_unverified_context()
    req = request.Request(
        f"https://{REMOTE_HOST}:8080/v1/controlplane/sets/{set_name}",
        headers={"Authorization": "Bearer vps-cp-token-dev-only"},
    )
    try:
        with request.urlopen(req, context=ctx, timeout=30) as r:
            data = json.load(r)["data"]
    except Exception:
        pytest.skip(f"set {set_name} not available")
    if not data.get("active"):
        pytest.skip(f"set {set_name} not active")

    out = _ssh(_probe_set_script(set_name, 19500 + hash(set_name) % 1000))
    (artifacts_dir / f"client_remote_{set_name}.log").write_text(out, encoding="utf-8")
    ok = "RESULT OK" in out
    if expect_ok:
        assert ok, out[-2000:]
    else:
        assert not ok


def test_remote_demux_defaults_member_and_front(artifacts_dir):
    """dg_443_dual defaults (vless_reality + hy2): member ports and demux :443 must both work."""
    script = r"""
set -euo pipefail
AUTH='Authorization: Bearer vps-cp-token-dev-only'
BASE='https://127.0.0.1:8080'
USERJSON=$(curl -sk -H "$AUTH" -H 'Content-Type: application/json' \
  -d "{\"name\":\"pytest-demux-$RANDOM\",\"enabled\":true}" \
  "$BASE/v1/controlplane/users")
TOKEN=$(python3 -c 'import json,sys; d=json.load(sys.stdin); assert d.get("ok"), d; print(d["data"]["sub_token"])' <<<"$USERJSON")
curl -sk "$BASE/v1/sub/$TOKEN?set=e2e-demux" -o /tmp/sub-demux.json
curl -sk -H "$AUTH" "$BASE/v1/controlplane/sets/e2e-demux" -o /tmp/set-demux.json
python3 - <<'PY'
import json,subprocess,time
sub=json.load(open('/tmp/sub-demux.json'))
setd=json.load(open('/tmp/set-demux.json'))['data']
ports=setd.get('member_ports') or {}
presets=setd.get('presets') or []
print('PRESETS', presets)
print('PORTS', ports)
assert 'hy2_salamander' not in ports, ports
assert 'hy2' in ports, ports
assert any('vless_reality' == k or k == 'vless_reality' for k in ports) or 'vless_reality' in ports

def pick_vless():
  for o in sub['outbounds']:
    if o.get('type')!='vless': continue
    flow=o.get('flow') or ''
    if 'vision' in flow or 'udp443' in flow: continue
    return o
  raise SystemExit('no flow-none vless')

def pick_quic():
  for o in sub['outbounds']:
    if o.get('type') in ('hysteria2','tuic'):
      return o
  raise SystemExit('no quic outbound')

def run(name, outbound, port):
  cfg={'log':{'level':'info'},'inbounds':[{'type':'mixed','tag':'m','listen':'127.0.0.1','listen_port':port}],'outbounds':[outbound,{'type':'direct','tag':'direct'}],'route':{'final':outbound['tag']}}
  json.dump(cfg, open('/tmp/c.json','w'), indent=2)
  subprocess.run(['docker','rm','-f',name], capture_output=True)
  subprocess.run(['docker','run','-d','--name',name,'--network','host','-v','/tmp/c.json:/work/client.json:ro','ghcr.io/sagernet/sing-box:v1.12.12','run','-c','/work/client.json'], check=True)
  time.sleep(1.5)
  p=subprocess.run(['curl','-x',f'http://127.0.0.1:{port}','-sS','-m','20','https://1.1.1.1/cdn-cgi/trace'], capture_output=True, text=True)
  ok=p.returncode==0 and 'ip=' in (p.stdout or '')
  print(name, 'OK' if ok else 'FAIL')
  if not ok:
    logs=subprocess.run(['docker','logs',name], capture_output=True, text=True)
    print(((logs.stdout or '')+(logs.stderr or ''))[-500:])
  subprocess.run(['docker','rm','-f',name], capture_output=True)
  return ok

quic=pick_quic(); vl=pick_vless()
quic_key=next(k for k in ports if k in ('hy2','tuic') or k.startswith('hy2') or k.startswith('tuic'))
vl_key=next(k for k in ports if 'reality' in k)
quic_port=int(ports[quic_key]); vl_port=int(ports[vl_key])

obm=dict(quic); obm['server']='127.0.0.1'; obm['server_port']=quic_port
obd=dict(quic); obd['server']='127.0.0.1'; obd['server_port']=443
vlm=dict(vl); vlm['server']='127.0.0.1'; vlm['server_port']=vl_port
vld=dict(vl); vld['server']='127.0.0.1'; vld['server_port']=443
results={
  'quic_member': run('quic-member', obm, 19601),
  'quic_demux': run('quic-demux', obd, 19602),
  'reality_member': run('vl-member', vlm, 19603),
  'reality_demux': run('vl-demux', vld, 19604),
}
for k,v in results.items():
  print(k.upper(), 'OK' if v else 'FAIL')
assert all(results.values()), results
PY
"""
    out = _ssh(script, timeout=180)
    (artifacts_dir / "client_remote_demux.log").write_text(out, encoding="utf-8")
    assert "QUIC_MEMBER OK" in out, out[-2500:]
    assert "QUIC_DEMUX OK" in out, out[-2500:]
    assert "REALITY_MEMBER OK" in out, out[-2500:]
    assert "REALITY_DEMUX OK" in out, out[-2500:]
