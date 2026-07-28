#!/bin/bash
set -eu
docker load -i /tmp/subserver-cp-local.tar
iptables -t nat -D PREROUTING -p tcp --dport 80 -j REDIRECT --to-ports 9080 2>/dev/null || true
docker rm -f subserver-cp 2>/dev/null || true
rm -rf /opt/subserver/data/*
mkdir -p /opt/subserver/data
docker run -d --name subserver-cp --restart unless-stopped --network host \
  -v /opt/subserver/agent.yaml:/etc/subserver/agent.yaml:ro \
  -v /opt/subserver/data:/var/lib/subserver \
  subserver-cp:local
sleep 3
curl -fkSs https://127.0.0.1:8080/v1/health; echo
docker inspect subserver-cp --format 'NetworkMode={{.HostConfig.NetworkMode}}'
echo HOST_NET_OK
