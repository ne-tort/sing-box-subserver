#!/bin/bash
set -eu
BASE="${BASE:-https://127.0.0.1:8080}"
TOKEN="${TOKEN:-vps-cp-token-dev-only}"
AUTH=(-H "Authorization: Bearer ${TOKEN}" -H "Content-Type: application/json")
HOST_PUBLIC="${HOST_PUBLIC:-163.5.180.181}"
CURL=(curl -fkSs)

pass() { echo "PASS: $*"; }
fail() { echo "FAIL: $*"; exit 1; }
note() { echo "NOTE: $*"; }

echo "== health =="
"${CURL[@]}" "$BASE/v1/health" | grep -q alive || fail health
pass health

echo "== version =="
VER=$("${CURL[@]}" -H "Authorization: Bearer ${TOKEN}" "$BASE/v1/version")
echo "$VER" | head -c 400; echo
echo "$VER" | grep -q with_controlplane || note "build_tags missing with_controlplane (version package gap)"

echo "== status =="
ST=$("${CURL[@]}" -H "Authorization: Bearer ${TOKEN}" "$BASE/v1/status")
echo "$ST" | head -c 500; echo
MODE=$(python3 -c 'import json,sys; d=json.load(sys.stdin); print((d.get("data") or d).get("config_mode"))' <<<"$ST")
echo "config_mode=$MODE"

echo "== TLS default =="
TLS=$("${CURL[@]}" -H "Authorization: Bearer ${TOKEN}" "$BASE/v1/controlplane/tls")
echo "$TLS" | head -c 800; echo
echo "$TLS" | grep -q '"mode":"self_signed"' || fail tls mode
echo "$TLS" | grep -q '"mgmt_https":true' || fail mgmt_https
echo "$TLS" | python3 -c 'import json,sys; d=json.load(sys.stdin)["data"]["material_status"]; assert d.get("ready") is True; print("mgmt_cert_source", d.get("mgmt_cert_source"))' || fail tls ready
echo "$TLS" | grep -q "$HOST_PUBLIC" || fail public_host in tls profile
pass tls_default

echo "== reject acme_ip+zerossl =="
code=$(curl -ks -o /tmp/tls_bad.json -w "%{http_code}" -X PUT "${AUTH[@]}" "$BASE/v1/controlplane/tls" \
  -d '{"mode":"acme_ip","acme":{"email":"a@b.c","domains":["'"$HOST_PUBLIC"'"],"provider":"zerossl"}}')
test "$code" = "400" || fail "expected 400 got $code $(cat /tmp/tls_bad.json)"
pass validate rejects zerossl+ip

echo "== user + ss + trojan =="
USER_JSON=$("${CURL[@]}" -X POST "${AUTH[@]}" "$BASE/v1/controlplane/users" -d '{"name":"alice"}')
TOK=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["data"]["sub_token"])' <<<"$USER_JSON")
test -n "$TOK" || fail sub_token
SUB_URL=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["data"].get("subscription_url",""))' <<<"$USER_JSON")
echo "subscription_url=$SUB_URL"
echo "$SUB_URL" | grep -q '^https://' || fail subscription_url must be https
"${CURL[@]}" -X POST "${AUTH[@]}" "$BASE/v1/controlplane/sets" \
  -d '{"name":"ss1","listen":"0.0.0.0","listen_port":1080,"presets":["shadowsocks-tcp"]}' >/dev/null
ACT=$("${CURL[@]}" -X POST -H "Authorization: Bearer ${TOKEN}" "$BASE/v1/controlplane/sets/ss1/activate")
echo "$ACT" | grep -q controlplane || fail activate ss
"${CURL[@]}" -X POST "${AUTH[@]}" "$BASE/v1/controlplane/sets" \
  -d '{"name":"tr1","listen":"0.0.0.0","listen_port":8443,"presets":["trojan-tcp"]}' >/dev/null
ACT2=$("${CURL[@]}" -X POST -H "Authorization: Bearer ${TOKEN}" "$BASE/v1/controlplane/sets/tr1/activate")
echo "$ACT2" | head -c 300; echo
echo "$ACT2" | grep -q '"ok":true' || fail "activate trojan: $ACT2"
pass activate sets

echo "== config materialize =="
CFG=$("${CURL[@]}" -H "Authorization: Bearer ${TOKEN}" "$BASE/v1/config")
echo "$CFG" | grep -q certificate_path || fail certificate_path
echo "$CFG" | grep -q controlplane/tls/server || fail cert path
! echo "$CFG" | grep -q certificate_provider || fail unexpected provider
pass config paths

echo "== subscription =="
SUB=$("${CURL[@]}" "$BASE/v1/sub/$TOK")
echo "$SUB" | grep -q '"insecure":true' || fail sub insecure
echo "$SUB" | grep -q trojan || fail sub trojan outbound
pass sub insecure self_signed

echo "== TLS handshake dataplane =="
if command -v openssl >/dev/null; then
  echo | openssl s_client -connect 127.0.0.1:8443 -servername "$HOST_PUBLIC" 2>/dev/null | grep -q 'BEGIN CERTIFICATE' \
    || fail openssl handshake
  pass openssl handshake
else
  note "openssl missing"
fi

echo "== TLS handshake management API =="
echo | openssl s_client -connect 127.0.0.1:8080 -servername "$HOST_PUBLIC" 2>/dev/null | grep -q 'BEGIN CERTIFICATE' \
  || fail mgmt openssl handshake
pass mgmt openssl handshake

echo "== regenerate Force reload =="
FP1=$(sha256sum /opt/subserver/data/controlplane/tls/server.crt)
REV1=$("${CURL[@]}" -H "Authorization: Bearer ${TOKEN}" "$BASE/v1/status" | python3 -c 'import json,sys; d=json.load(sys.stdin); print((d.get("data") or d)["revision"])')
"${CURL[@]}" -X POST -H "Authorization: Bearer ${TOKEN}" "$BASE/v1/controlplane/tls/regenerate" >/dev/null
FP2=$(sha256sum /opt/subserver/data/controlplane/tls/server.crt)
test "$FP1" != "$FP2" || fail cert unchanged
REV2=$("${CURL[@]}" -H "Authorization: Bearer ${TOKEN}" "$BASE/v1/status" | python3 -c 'import json,sys; d=json.load(sys.stdin); print((d.get("data") or d)["revision"])')
test "$REV2" -gt "$REV1" || fail "revision $REV1 -> $REV2"
echo | openssl s_client -connect 127.0.0.1:8443 -servername "$HOST_PUBLIC" 2>/dev/null | grep -q 'BEGIN CERTIFICATE' \
  || fail handshake after regen
pass regenerate reload

echo "== external reachability from public IP =="
curl -fkSs --connect-timeout 5 "https://${HOST_PUBLIC}:8080/v1/health" | grep -q alive \
  || fail public health
pass public health

echo "ALL OK"
