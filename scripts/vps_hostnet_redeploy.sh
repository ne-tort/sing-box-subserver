#!/bin/bash
set -eu
docker load -i /tmp/subserver-cp-local.tar
iptables -t nat -D PREROUTING -p tcp --dport 80 -j REDIRECT --to-ports 9080 2>/dev/null || true
docker rm -f subserver-cp 2>/dev/null || true
# keep data if present
mkdir -p /opt/subserver/data
docker run -d --name subserver-cp --restart unless-stopped --network host \
  -v /opt/subserver/agent.yaml:/etc/subserver/agent.yaml:ro \
  -v /opt/subserver/data:/var/lib/subserver \
  subserver-cp:local
sleep 2
curl -fsS http://127.0.0.1:8080/v1/health; echo
H='Authorization: Bearer vps-cp-token-dev-only'
curl -fsS -H "$H" http://127.0.0.1:8080/v1/controlplane/status; echo
curl -fsS -H "$H" http://127.0.0.1:8080/v1/controlplane/tls | python3 -c 'import json,sys; d=json.load(sys.stdin)["data"]; ms=d.get("material_status",{}); print("mode",d.get("mode"),"ready",ms.get("ready"),"reason",ms.get("ready_reason"))'
docker inspect subserver-cp --format 'NetworkMode={{.HostConfig.NetworkMode}}'
echo HOST_NET_OK
