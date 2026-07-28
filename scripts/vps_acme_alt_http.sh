#!/bin/bash
# Live-test ACME HTTP-01 via alternative_http_port + host REDIRECT 80→ALT.
# Host networking: ACME listens on host ALT; DNAT public 80 → ALT.
set -eu
TOKEN=vps-cp-token-dev-only
BASE=http://127.0.0.1:8080
H="Authorization: Bearer ${TOKEN}"
CT="Content-Type: application/json"
DOMAIN=wiki.ai-qwerty.ru
EMAIL=ops@ai-qwerty.ru
IP=163.5.180.181
ALT=9080

pass(){ echo "PASS: $*"; }
fail(){ echo "FAIL: $*"; exit 1; }

cleanup() {
  iptables -t nat -D PREROUTING -p tcp --dport 80 -j REDIRECT --to-ports "$ALT" 2>/dev/null || true
}
trap cleanup EXIT

docker rm -f subserver-cp 2>/dev/null || true
rm -rf /opt/subserver/data/*
mkdir -p /opt/subserver/data

iptables -t nat -C PREROUTING -p tcp --dport 80 -j REDIRECT --to-ports "$ALT" 2>/dev/null || \
  iptables -t nat -A PREROUTING -p tcp --dport 80 -j REDIRECT --to-ports "$ALT"

docker run -d --name subserver-cp --restart unless-stopped --network host \
  -v /opt/subserver/agent.yaml:/etc/subserver/agent.yaml:ro \
  -v /opt/subserver/data:/var/lib/subserver \
  subserver-cp:local
sleep 2
curl -fsS "$BASE/v1/health" >/dev/null

curl -fsS -X POST -H "$H" -H "$CT" -d '{"name":"alt-acme"}' "$BASE/v1/controlplane/users" >/dev/null
curl -fsS -X POST -H "$H" -H "$CT" \
  -d '{"name":"trojan-443","listen":"::","listen_port":443,"presets":["trojan-tcp"]}' \
  "$BASE/v1/controlplane/sets" >/dev/null

curl -fsS -X PUT -H "$H" -H "$CT" -d "{
  \"mode\":\"acme_domain\",
  \"acme\":{
    \"email\":\"${EMAIL}\",
    \"domains\":[\"${DOMAIN}\"],
    \"provider\":\"letsencrypt\",
    \"disable_tls_alpn_challenge\":true,
    \"alternative_http_port\":${ALT}
  }
}" "$BASE/v1/controlplane/tls" >/dev/null

curl -fsS -X POST -H "$H" "$BASE/v1/controlplane/sets/trojan-443/activate" >/tmp/act_alt.json || {
  cat /tmp/act_alt.json; docker logs subserver-cp 2>&1 | tail -40; fail activate
}

ss -lntp | grep ":${ALT} " || true

ok=0
for i in $(seq 1 20); do
  TLS=$(curl -fsS -H "$H" "$BASE/v1/controlplane/tls")
  READY=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["data"]["material_status"].get("ready"))' <<<"$TLS")
  if [ "$READY" = "True" ] || [ "$READY" = "true" ]; then
    if echo | openssl s_client -connect 127.0.0.1:443 -servername "$DOMAIN" 2>/dev/null \
      | openssl x509 -noout -issuer -subject 2>/dev/null \
      | tee /tmp/cert_alt.txt \
      | grep -qiE "Let.?s Encrypt|YE1|R3|E1"; then
      ok=1; break
    fi
  fi
  sleep 3
done
test "$ok" = "1" || { cat /tmp/cert_alt.txt || true; docker logs subserver-cp 2>&1 | tail -80; fail alt_http_port; }
pass alt_http_port
cat /tmp/cert_alt.txt
echo FINAL_OK
