#!/bin/bash
set -euo pipefail
AUTH='Authorization: Bearer vps-cp-token-dev-only'
BASE='https://127.0.0.1:8080'
USERJSON=$(curl -sk -H "$AUTH" -H 'Content-Type: application/json' -d '{"name":"probe-reality-fresh","enabled":true}' "$BASE/v1/controlplane/users")
TOKEN=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["data"]["sub_token"])' <<<"$USERJSON")
curl -sk "$BASE/v1/sub/$TOKEN?set=e2e-demux&preset=vless_ws_reality" -o /tmp/sub-reality.json
python3 <<'PY'
import json,subprocess,time,urllib.request,ssl
sub=json.load(open('/tmp/sub-reality.json'))
print('outbounds', [(o.get('tag'), (o.get('tls') or {}).get('reality')) for o in sub.get('outbounds') or []])
ctx=ssl._create_unverified_context()
req=urllib.request.Request('https://127.0.0.1:8080/v1/controlplane/reality', headers={'Authorization':'Bearer vps-cp-token-dev-only'})
asg=json.load(urllib.request.urlopen(req, context=ctx))['data']['active_assignments']
print('assignments', asg)
ob=[o for o in sub['outbounds'] if 'reality' in o.get('tag','')][0]
print('client short_id', ob['tls']['reality'].get('short_id'))
print('client public_key', ob['tls']['reality'].get('public_key'))
# through demux :443
for mode, server, port in [('demux','127.0.0.1',443), ('member','127.0.0.1',49758)]:
    ob2=dict(ob); ob2['server']=server; ob2['server_port']=port
    cfg={'log':{'level':'info'},'inbounds':[{'type':'mixed','tag':'m','listen':'127.0.0.1','listen_port':19310}],'outbounds':[ob2,{'type':'direct','tag':'direct'}],'route':{'final':ob2['tag']}}
    json.dump(cfg, open('/tmp/client-reality.json','w'), indent=2)
    subprocess.run(['docker','rm','-f','probe-reality'], capture_output=True)
    subprocess.run(['docker','run','-d','--name','probe-reality','--network','host','-v','/tmp/client-reality.json:/work/client.json:ro','ghcr.io/sagernet/sing-box:v1.12.12','run','-c','/work/client.json'], check=True)
    time.sleep(1.2)
    p=subprocess.run(['curl','-x','http://127.0.0.1:19310','-sS','-m','20','https://1.1.1.1/cdn-cgi/trace'], capture_output=True, text=True)
    print('mode', mode, 'rc', p.returncode)
    print(((p.stdout or '')+(p.stderr or ''))[:250])
    if p.returncode!=0:
        logs=subprocess.run(['docker','logs','probe-reality'], capture_output=True, text=True)
        print(((logs.stdout or '')+(logs.stderr or ''))[-500:])
    subprocess.run(['docker','rm','-f','probe-reality'], capture_output=True)
PY
