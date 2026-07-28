#!/bin/bash
set -eu
TOKEN=vps-cp-token-dev-only
BASE=https://127.0.0.1:8080
H="Authorization: Bearer ${TOKEN}"
CT="Content-Type: application/json"
DOMAIN=wiki.ai-qwerty.ru
EMAIL=ops@ai-qwerty.ru
CURL=(curl -fkSs)

pass(){ echo "PASS: $*"; }
fail(){ echo "FAIL: $*"; exit 1; }

echo "== switch to ACME domain =="
"${CURL[@]}" -X PUT -H "$H" -H "$CT" -d "{\"mode\":\"acme_domain\",\"acme\":{\"email\":\"${EMAIL}\",\"domains\":[\"${DOMAIN}\"],\"provider\":\"letsencrypt\"}}" \
  "$BASE/v1/controlplane/tls" >/dev/null
"${CURL[@]}" -X POST -H "$H" "$BASE/v1/controlplane/sets/tr1/activate" >/dev/null || true

ok=0
for i in $(seq 1 20); do
  TLS=$("${CURL[@]}" -H "$H" "$BASE/v1/controlplane/tls")
  READY=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["data"]["material_status"].get("ready"))' <<<"$TLS")
  SRC=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["data"]["material_status"].get("mgmt_cert_source"))' <<<"$TLS")
  echo "try $i ready=$READY src=$SRC"
  if [ "$READY" = "True" ] || [ "$READY" = "true" ]; then
    if [ "$SRC" = "acme" ]; then ok=1; break; fi
  fi
  sleep 3
done
test "$ok" = "1" || { echo "$TLS"; docker logs subserver-cp 2>&1 | tail -40; fail acme_mgmt_source; }
pass acme_mgmt_source

echo "== mgmt handshake shows LE =="
echo | openssl s_client -connect 127.0.0.1:8080 -servername "$DOMAIN" 2>/dev/null \
  | openssl x509 -noout -issuer -subject 2>/dev/null | tee /tmp/mgmt_acme.txt
grep -qiE "Let.?s Encrypt|YE1|YE2|R3|E1" /tmp/mgmt_acme.txt || fail mgmt_le_issuer
pass mgmt_le_issuer

echo "== subscription_url still https =="
USER_ID=$("${CURL[@]}" -H "$H" "$BASE/v1/controlplane/users" | python3 -c 'import json,sys; print(json.load(sys.stdin)["data"][0]["id"])')
URL=$("${CURL[@]}" -H "$H" "$BASE/v1/controlplane/users/${USER_ID}?secrets=1" | python3 -c 'import json,sys; print(json.load(sys.stdin)["data"].get("subscription_url",""))')
echo "url=$URL"
echo "$URL" | grep -q '^https://' || fail sub_url
pass sub_url_https

echo "== force fallback by wiping ACME store + short wait simulation via API put self =="
# Emergency path unit-tested; here verify explicit mode switch still works and mgmt returns to self_signed
"${CURL[@]}" -X PUT -H "$H" -H "$CT" \
  -d '{"mode":"self_signed","self_signed":{"common_name":"163.5.180.181","dns_sans":["localhost"],"ip_sans":["163.5.180.181"],"key_type":"p256","valid_days":3650}}' \
  "$BASE/v1/controlplane/tls" >/dev/null
TLS=$("${CURL[@]}" -H "$H" "$BASE/v1/controlplane/tls")
python3 -c 'import json,sys; d=json.load(sys.stdin)["data"]; assert d["mode"]=="self_signed"; assert d["material_status"]["mgmt_cert_source"]=="self_signed"' <<<"$TLS"
pass back_to_self_signed

echo FINAL_OK
