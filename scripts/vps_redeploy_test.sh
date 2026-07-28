#!/bin/bash
set -eu
sed -i 's/\r$//' /tmp/vps_cp_smoke.sh /tmp/vps_cp_deeper.sh || true
docker rm -f subserver-cp 2>/dev/null || true
rm -rf /opt/subserver/data/*
mkdir -p /opt/subserver/data
docker run -d --name subserver-cp --restart unless-stopped --network host \
  -v /opt/subserver/agent.yaml:/etc/subserver/agent.yaml:ro \
  -v /opt/subserver/data:/var/lib/subserver \
  subserver-cp:local
sleep 2
bash /tmp/vps_cp_smoke.sh
echo '----'
bash /tmp/vps_cp_deeper.sh
echo '----'
python3 - <<'PY'
import json,urllib.request
req=urllib.request.Request('https://127.0.0.1:8080/v1/version', headers={'Authorization':'Bearer vps-cp-token-dev-only'})
d=json.load(urllib.request.urlopen(req))['data']
print('tags_has_cp', 'with_controlplane' in d['build_tags'])
print('version', d['agent_version'])
# secrets URL check
req2=urllib.request.Request('https://127.0.0.1:8080/v1/controlplane/users', headers={'Authorization':'Bearer vps-cp-token-dev-only'})
users=json.load(urllib.request.urlopen(req2))['data']
uid=users[0]['id']
req3=urllib.request.Request(f'https://127.0.0.1:8080/v1/controlplane/users/{uid}?secrets=1', headers={'Authorization':'Bearer vps-cp-token-dev-only'})
u=json.load(urllib.request.urlopen(req3))['data']
print('secrets_has_url', bool(u.get('subscription_url')))
print('url', u.get('subscription_url'))
PY
echo FINAL_OK
