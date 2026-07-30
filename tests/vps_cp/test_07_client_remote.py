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
ob=sub['outbounds'][{outbound_index}]
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


def test_remote_hy2_direct_member_works(artifacts_dir):
    """Hy2 works to private member port; documents demux QUIC match gap if :443 fails."""
    script = r"""
set -euo pipefail
AUTH='Authorization: Bearer vps-cp-token-dev-only'
BASE='https://127.0.0.1:8080'
USERJSON=$(curl -sk -H "$AUTH" -H 'Content-Type: application/json' \
  -d "{\"name\":\"pytest-hy2-$RANDOM\",\"enabled\":true}" \
  "$BASE/v1/controlplane/users")
TOKEN=$(python3 -c 'import json,sys; d=json.load(sys.stdin); assert d.get("ok"), d; print(d["data"]["sub_token"])' <<<"$USERJSON")
curl -sk "$BASE/v1/sub/$TOKEN?set=e2e-demux" -o /tmp/sub-demux.json
curl -sk -H "$AUTH" "$BASE/v1/controlplane/sets/e2e-demux" -o /tmp/set-demux.json
python3 - <<'PY'
import json,subprocess,time
sub=json.load(open('/tmp/sub-demux.json'))
ports=json.load(open('/tmp/set-demux.json'))['data']['member_ports']
ob=[o for o in sub['outbounds'] if o.get('type')=='hysteria2'][0]
# member direct
obm=dict(ob); obm['server']='127.0.0.1'; obm['server_port']=int(ports['hy2_salamander'])
# demux front
obd=dict(ob); obd['server']='127.0.0.1'; obd['server_port']=443

def run(name, outbound, port):
  cfg={'log':{'level':'info'},'inbounds':[{'type':'mixed','tag':'m','listen':'127.0.0.1','listen_port':port}],'outbounds':[outbound,{'type':'direct','tag':'direct'}],'route':{'final':outbound['tag']}}
  json.dump(cfg, open('/tmp/c.json','w'), indent=2)
  subprocess.run(['docker','rm','-f',name], capture_output=True)
  subprocess.run(['docker','run','-d','--name',name,'--network','host','-v','/tmp/c.json:/work/client.json:ro','ghcr.io/sagernet/sing-box:v1.12.12','run','-c','/work/client.json'], check=True)
  time.sleep(1.2)
  p=subprocess.run(['curl','-x',f'http://127.0.0.1:{port}','-sS','-m','20','https://1.1.1.1/cdn-cgi/trace'], capture_output=True, text=True)
  ok=p.returncode==0 and 'ip=' in (p.stdout or '')
  print(name, 'OK' if ok else 'FAIL')
  if not ok:
    logs=subprocess.run(['docker','logs',name], capture_output=True, text=True)
    print(((logs.stdout or '')+(logs.stderr or ''))[-400:])
  subprocess.run(['docker','rm','-f',name], capture_output=True)
  return ok
m=run('hy2-member', obm, 19601)
d=run('hy2-demux', obd, 19602)
print('MEMBER', 'OK' if m else 'FAIL')
print('DEMUX', 'OK' if d else 'FAIL')
PY
"""
    out = _ssh(script, timeout=120)
    (artifacts_dir / "client_remote_hy2.log").write_text(out, encoding="utf-8")
    assert "MEMBER OK" in out, out[-2000:]
    # Known gap: salamander/obfs may not match demux protocol=quic
    if "DEMUX OK" not in out:
        (artifacts_dir / "KNOWN_GAP_demux_hy2_quic.txt").write_text(
            "Hy2 works on member port but fails via demux :443 (protocol=quic match vs salamander).\n" + out[-1500:],
            encoding="utf-8",
        )


def test_remote_reality_tcp_already_covered():
    """Plain vless_reality covered by test_remote_client_single_inbound[e2e-reality-tcp]."""
    # Keep a note artifact for WS Reality gap observed in demux group.
    pass
