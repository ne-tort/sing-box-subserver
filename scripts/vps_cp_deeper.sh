#!/bin/bash
set -eu
TOKEN=vps-cp-token-dev-only
BASE=https://127.0.0.1:8080
H="Authorization: Bearer ${TOKEN}"
CT="Content-Type: application/json"

echo '== presets =='
curl -fkSs -H "$H" "$BASE/v1/controlplane/presets" | python3 -c 'import json,sys; d=json.load(sys.stdin)["data"]; print([p["name"] for p in (d if isinstance(d,list) else d.get("presets",[]))])'

echo '== user subscription_url =='
curl -fkSs -H "$H" "$BASE/v1/controlplane/users" | python3 -c 'import json,sys; u=json.load(sys.stdin)["data"]; u=u[0] if isinstance(u,list) else u["users"][0]; print("url=",u.get("subscription_url")); print("keys=",sorted(u.keys()))'

echo '== ownership: PUT config steals from CP =='
CFG='{"log":{"level":"error"},"inbounds":[],"outbounds":[{"type":"direct","tag":"direct"}]}'
curl -fkSs -X PUT -H "$H" -H "$CT" --data-binary "$CFG" "$BASE/v1/config"
echo
curl -fkSs -H "$H" "$BASE/v1/status" | python3 -c 'import json,sys; d=json.load(sys.stdin)["data"]; print("mode",d.get("config_mode"),"box_up",d.get("box_up"))'
curl -fkSs -H "$H" "$BASE/v1/controlplane/status" | head -c 500; echo

echo '== re-activate after steal =='
curl -fkSs -X POST -H "$H" "$BASE/v1/controlplane/sets/tr1/activate"; echo
curl -fkSs -H "$H" "$BASE/v1/status" | python3 -c 'import json,sys; d=json.load(sys.stdin)["data"]; print("mode",d.get("config_mode"),"rev",d.get("revision"))'
if echo | openssl s_client -connect 127.0.0.1:8443 -servername 163.5.180.181 2>/dev/null | grep -q BEGIN; then
  echo PASS handshake after reclaim
else
  echo FAIL handshake after reclaim
  exit 1
fi

echo '== acme_domain dry put =='
code=$(curl -s -o /tmp/acme.json -w "%{http_code}" -X PUT -H "$H" -H "$CT" \
  -d '{"mode":"acme_domain","acme":{"email":"ops@example.com","domains":["vpn.example.com"],"provider":"letsencrypt"}}' \
  "$BASE/v1/controlplane/tls")
echo "http=$code"
head -c 400 /tmp/acme.json; echo
# If apply fails with 422 because ACME challenges, that is expected without public DNS — note it.
if [ "$code" != "200" ] && [ "$code" != "422" ]; then
  echo "unexpected acme put code $code"
  exit 1
fi

echo '== restore self_signed =='
curl -fkSs -X PUT -H "$H" -H "$CT" \
  -d '{"mode":"self_signed","self_signed":{"common_name":"163.5.180.181","dns_sans":["localhost"],"ip_sans":["163.5.180.181"],"key_type":"p256","valid_days":3650}}' \
  "$BASE/v1/controlplane/tls" >/dev/null
echo restored

echo '== deactivate last sets -> idle =='
curl -fkSs -X POST -H "$H" "$BASE/v1/controlplane/sets/tr1/deactivate"; echo
curl -fkSs -X POST -H "$H" "$BASE/v1/controlplane/sets/ss1/deactivate"; echo
curl -fkSs -H "$H" "$BASE/v1/status" | python3 -c 'import json,sys; d=json.load(sys.stdin)["data"]; print("mode",d.get("config_mode"))'

echo DEEPER_OK
