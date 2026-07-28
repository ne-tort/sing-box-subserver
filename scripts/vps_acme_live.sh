#!/bin/bash
set -eu
TOKEN=vps-cp-token-dev-only
BASE=http://127.0.0.1:8080
H="Authorization: Bearer ${TOKEN}"
CT="Content-Type: application/json"
DOMAIN=wiki.ai-qwerty.ru
IP=163.5.180.181
EMAIL=ops@ai-qwerty.ru

pass(){ echo "PASS: $*"; }
fail(){ echo "FAIL: $*"; docker logs subserver-cp 2>&1 | tail -40; exit 1; }
note(){ echo "NOTE: $*"; }

json_get(){ python3 -c 'import json,sys; d=json.load(sys.stdin); print(d'"$1"')'; }

echo "== recreate container with :443 for TLS-ALPN =="
docker rm -f subserver-cp 2>/dev/null || true
# keep data; update agent public_host to domain for SNI/sub
cat > /opt/subserver/agent.yaml <<EOF
node_id: "vps-163-cp-1"
token: "vps-cp-token-dev-only"
listen: "0.0.0.0:8080"
data_dir: "/var/lib/subserver"
insecure_public_bind: true
probe_ms: 100
log:
  level: info
controlplane:
  public_host: "${DOMAIN}"
  public_port: 8080
  expiry_tick_sec: 60
EOF
docker run -d --name subserver-cp --restart unless-stopped --network host \
  -v /opt/subserver/agent.yaml:/etc/subserver/agent.yaml:ro \
  -v /opt/subserver/data:/var/lib/subserver \
  subserver-cp:local
sleep 2
curl -fsS "$BASE/v1/health" >/dev/null
ss -lnt | grep -E ':443|:8443' || true

echo "== ensure user + trojan set active =="
# recreate clean-ish sets if needed
USERS=$(curl -fsS -H "$H" "$BASE/v1/controlplane/users")
USER_ID=$(python3 -c 'import json,sys; u=json.load(sys.stdin)["data"]; print(u[0]["id"] if u else "")' <<<"$USERS")
if [ -z "$UID" ]; then
  U=$(curl -fsS -X POST -H "$H" -H "$CT" "$BASE/v1/controlplane/users" -d '{"name":"alice"}')
  USER_ID=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["data"]["id"])' <<<"$U")
  TOK=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["data"]["sub_token"])' <<<"$U")
  echo "created user $UID"
else
  U=$(curl -fsS -H "$H" "$BASE/v1/controlplane/users/${USER_ID}?secrets=1")
  TOK=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["data"]["sub_token"])' <<<"$U")
  echo "reuse user $UID"
fi
# ensure sets
curl -fsS -H "$H" "$BASE/v1/controlplane/sets/tr1" >/dev/null 2>&1 || \
  curl -fsS -X POST -H "$H" -H "$CT" "$BASE/v1/controlplane/sets" \
    -d '{"name":"tr1","listen":"0.0.0.0","listen_port":8443,"presets":["trojan-tcp"]}' >/dev/null
curl -fsS -X POST -H "$H" "$BASE/v1/controlplane/sets/tr1/activate" >/dev/null || true
curl -fsS -H "$H" "$BASE/v1/controlplane/sets/ss1" >/dev/null 2>&1 || \
  curl -fsS -X POST -H "$H" -H "$CT" "$BASE/v1/controlplane/sets" \
    -d '{"name":"ss1","listen":"0.0.0.0","listen_port":1080,"presets":["shadowsocks-tcp"]}' >/dev/null
curl -fsS -X POST -H "$H" "$BASE/v1/controlplane/sets/ss1/activate" >/dev/null || true

echo "== 1) ACME domain ${DOMAIN} (TLS-ALPN only, :443) =="
code=$(curl -s -o /tmp/acme_dom.json -w "%{http_code}" -X PUT -H "$H" -H "$CT" \
  -d "{\"mode\":\"acme_domain\",\"acme\":{\"email\":\"${EMAIL}\",\"domains\":[\"${DOMAIN}\"],\"provider\":\"letsencrypt\",\"disable_http_challenge\":true,\"disable_tls_alpn_challenge\":false}}" \
  "$BASE/v1/controlplane/tls")
echo "put http=$code"
cat /tmp/acme_dom.json | head -c 500; echo
[ "$code" = "200" ] || fail "acme_domain put"

echo "waiting for LE certificate (up to ~3m)..."
ok=0
for i in $(seq 1 36); do
  sleep 5
  if docker logs subserver-cp 2>&1 | tail -80 | grep -qiE 'certificate obtained|obtained certificate|serving|renewed'; then
    ok=1
    break
  fi
  # also try handshake with verify using system CAs against domain SNI on 8443
  if echo | openssl s_client -connect 127.0.0.1:8443 -servername "$DOMAIN" -verify_return_error 2>/dev/null | grep -q 'Verify return code: 0'; then
    ok=1
    break
  fi
  echo "  try $i..."
done
docker logs subserver-cp 2>&1 | tail -50
CFG=$(curl -fsS -H "$H" "$BASE/v1/config")
echo "$CFG" | python3 -c 'import json,sys; d=json.load(sys.stdin); print("providers", d.get("certificate_providers"));
print("trojan_tls", [i.get("tls") for i in d.get("inbounds",[]) if i.get("type")=="trojan"])'
if ! echo "$CFG" | grep -q '"certificate_provider":"cp-tls"'; then
  fail "config missing certificate_provider"
fi
# verify handshake: LE cert should verify
if echo | openssl s_client -connect 127.0.0.1:8443 -servername "$DOMAIN" -verify_return_error 2>&1 | tee /tmp/ssl_dom.txt | grep -q 'Verify return code: 0'; then
  pass "domain ACME verify openssl"
else
  note "openssl verify failed — dumping"
  cat /tmp/ssl_dom.txt | tail -30
  fail "domain ACME cert not trusted yet"
fi
# subject should be domain
echo | openssl s_client -connect 127.0.0.1:8443 -servername "$DOMAIN" 2>/dev/null | openssl x509 -noout -subject -issuer || true

SUB=$(curl -fsS "$BASE/v1/sub/$TOK")
echo "$SUB" | python3 -c 'import json,sys; d=json.load(sys.stdin); obs=d["outbounds"];
t=[o for o in obs if o.get("type")=="trojan"][0]; tls=t.get("tls") or {};
print("sub_server", t.get("server")); print("sni", tls.get("server_name")); print("insecure", tls.get("insecure"));
assert tls.get("insecure") in (None, False), tls
assert tls.get("server_name")=="'"$DOMAIN"'", tls'
pass "subscription ACME no insecure + SNI domain"

echo "== 2) ACME bare IP ${IP} (TLS-ALPN only) =="
# switch public_host conceptually via profile domains; keep agent host or update
code=$(curl -s -o /tmp/acme_ip.json -w "%{http_code}" -X PUT -H "$H" -H "$CT" \
  -d "{\"mode\":\"acme_ip\",\"acme\":{\"email\":\"${EMAIL}\",\"domains\":[\"${IP}\"],\"provider\":\"letsencrypt\",\"disable_http_challenge\":true}}" \
  "$BASE/v1/controlplane/tls")
echo "put http=$code"
cat /tmp/acme_ip.json | head -c 500; echo
[ "$code" = "200" ] || fail "acme_ip put"

ok=0
for i in $(seq 1 36); do
  sleep 5
  if echo | openssl s_client -connect 127.0.0.1:8443 -servername "$IP" -verify_return_error 2>/dev/null | grep -q 'Verify return code: 0'; then
    ok=1
    break
  fi
  echo "  ip try $i..."
done
docker logs subserver-cp 2>&1 | tail -40
CFG=$(curl -fsS -H "$H" "$BASE/v1/config")
echo "$CFG" | python3 -c 'import json,sys; d=json.load(sys.stdin); print("providers", d.get("certificate_providers"))'
if echo | openssl s_client -connect 127.0.0.1:8443 -servername "$IP" -verify_return_error 2>&1 | tee /tmp/ssl_ip.txt | grep -q 'Verify return code: 0'; then
  pass "IP ACME verify openssl"
else
  note "IP ACME verify failed"
  cat /tmp/ssl_ip.txt | tail -40
  # LE IP certs are shortlived; document if blocked
  fail "acme_ip cert not trusted"
fi
echo | openssl s_client -connect 127.0.0.1:8443 -servername "$IP" 2>/dev/null | openssl x509 -noout -subject -issuer -ext subjectAltName || true

SUB=$(curl -fsS "$BASE/v1/sub/$TOK")
echo "$SUB" | python3 -c 'import json,sys; d=json.load(sys.stdin); t=[o for o in d["outbounds"] if o.get("type")=="trojan"][0]; tls=t.get("tls") or {};
print(tls); assert tls.get("insecure") in (None, False); assert tls.get("server_name")=="'"$IP"'"'
pass "subscription ACME IP no insecure"

echo "== restore self_signed for demux/lab =="
curl -fsS -X PUT -H "$H" -H "$CT" \
  -d "{\"mode\":\"self_signed\",\"self_signed\":{\"common_name\":\"${DOMAIN}\",\"dns_sans\":[\"${DOMAIN}\",\"localhost\"],\"ip_sans\":[\"${IP}\"],\"key_type\":\"p256\",\"valid_days\":3650}}" \
  "$BASE/v1/controlplane/tls" >/dev/null
pass "restored self_signed"

echo ACME_FLOW_DONE
