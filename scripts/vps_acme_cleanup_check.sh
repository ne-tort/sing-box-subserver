#!/bin/bash
set -eu
H="Authorization: Bearer vps-cp-token-dev-only"
echo '== status =='
curl -fkSs -H "$H" https://127.0.0.1:8080/v1/status | python3 -m json.tool | head -40
echo '== tls =='
curl -fkSs -H "$H" https://127.0.0.1:8080/v1/controlplane/tls | python3 -m json.tool | head -40
echo '== config snippet =='
curl -fkSs -H "$H" https://127.0.0.1:8080/v1/config | python3 -c 'import json,sys; d=json.load(sys.stdin); print("providers", d.get("certificate_providers"));
ibs=d.get("inbounds",[]);
print("inbounds", [(i.get("tag"), (i.get("tls") or {}).keys()) for i in ibs])'
echo '== force self_signed rematerialize =='
# activate trojan if needed then put self_signed
curl -fkSs -X POST -H "$H" https://127.0.0.1:8080/v1/controlplane/sets/tr1/activate || true
curl -fkSs -X PUT -H "$H" -H "Content-Type: application/json" \
  -d '{"mode":"self_signed","self_signed":{"common_name":"163.5.180.181","dns_sans":["localhost"],"ip_sans":["163.5.180.181"],"key_type":"p256","valid_days":3650}}' \
  https://127.0.0.1:8080/v1/controlplane/tls >/dev/null
curl -fkSs -H "$H" https://127.0.0.1:8080/v1/config | python3 -c 'import json,sys; d=json.load(sys.stdin); print("providers", d.get("certificate_providers"));
print("tls0", [i.get("tls") for i in d.get("inbounds",[]) if i.get("type")=="trojan"])'
echo | openssl s_client -connect 127.0.0.1:8443 -servername 163.5.180.181 2>/dev/null | grep -E 'BEGIN CERTIFICATE|subject=' | head -3
echo CLEAN_CHECK_OK
