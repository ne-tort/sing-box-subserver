#!/bin/bash
set -eu
TOKEN=vps-cp-token-dev-only
BASE=https://127.0.0.1:8080
H="Authorization: Bearer ${TOKEN}"
CT="Content-Type: application/json"

DEMUX='{
  "name": "mixed-9443",
  "listen": "0.0.0.0",
  "listen_port": 9443,
  "presets": ["trojan-tcp", "vless-tcp"],
  "demux_template": {
    "network": ["tcp"],
    "rules": [
      {
        "name": "tls-to-trojan",
        "match": { "protocol": "tls" },
        "action": { "inbound": { "tag": "{{tag:trojan-tcp}}" } }
      },
      {
        "name": "plain-to-vless",
        "match": { "always": true },
        "action": { "inbound": { "tag": "{{tag:vless-tcp}}" } }
      }
    ]
  }
}'

curl -fkSs -X PUT -H "$H" -H "$CT" -d "$DEMUX" "$BASE/v1/controlplane/sets/mixed-9443"
echo
ACT=$(curl -s -o /tmp/da.json -w "%{http_code}" -X POST -H "$H" "$BASE/v1/controlplane/sets/mixed-9443/activate")
echo "activate=$ACT body=$(cat /tmp/da.json)"
if [ "$ACT" != "200" ]; then
  docker logs subserver-cp 2>&1 | tail -40
  exit 1
fi

curl -fkSs -H "$H" "$BASE/v1/config" | python3 -c 'import json,sys; d=json.load(sys.stdin); print([i.get("tag") for i in d["inbounds"]])'
if echo | openssl s_client -connect 127.0.0.1:9443 -servername wiki.ai-qwerty.ru 2>/dev/null | grep -q 'BEGIN CERTIFICATE'; then
  echo PASS_demux_tls
else
  echo FAIL_demux_tls
  exit 1
fi

BOB_TOK=$(curl -fkSs -H "$H" "$BASE/v1/controlplane/users/37e551523f3d4950?secrets=1" | python3 -c 'import json,sys; print(json.load(sys.stdin)["data"]["sub_token"])')
curl -fkSs "$BASE/v1/sub/$BOB_TOK?set=mixed-9443" | python3 -c 'import json,sys; print([o["type"] for o in json.load(sys.stdin)["outbounds"]])'
echo DEMUX_OK
