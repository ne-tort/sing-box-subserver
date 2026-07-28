#!/bin/bash
set -eu
TOKEN=vps-cp-token-dev-only
BASE=https://127.0.0.1:8080
H="Authorization: Bearer ${TOKEN}"
CT="Content-Type: application/json"
DOMAIN=wiki.ai-qwerty.ru
EMAIL=ops@ai-qwerty.ru
IP=163.5.180.181

pass(){ echo "PASS: $*"; }
fail(){ echo "FAIL: $*"; exit 1; }
note(){ echo "NOTE: $*"; }

echo "== catalogs =="
PRESETS=$(curl -fkSs -H "$H" "$BASE/v1/controlplane/presets")
echo "$PRESETS" | python3 -c 'import json,sys; d=json.load(sys.stdin)["data"]; print("presets",len(d),[p["name"] for p in d])'
echo "$PRESETS" | python3 -c 'import json,sys; d=json.load(sys.stdin)["data"]; assert len(d)>=12'
RECIPES=$(curl -fkSs -H "$H" "$BASE/v1/controlplane/demux-recipes")
echo "$RECIPES" | python3 -c 'import json,sys; d=json.load(sys.stdin)["data"]; print("recipes",len(d),[r["name"] for r in d])'
echo "$RECIPES" | python3 -c 'import json,sys; d=json.load(sys.stdin)["data"]; assert len(d)>=6'
curl -fkSs -H "$H" "$BASE/v1/controlplane/demux-recipes/tls-trojan-plain-vless" | python3 -c 'import json,sys; r=json.load(sys.stdin)["data"]; assert "demux_template" in r'
ST=$(curl -fkSs -H "$H" "$BASE/v1/controlplane/status")
echo "$ST" | python3 -c 'import json,sys; d=json.load(sys.stdin)["data"]; assert d.get("demux_recipes_count",0)>=6; print("status presets",d["presets_count"],"recipes",d["demux_recipes_count"])'
pass catalogs

echo "== reject empty demux match =="
code=$(curl -s -o /tmp/bad_demux.json -w "%{http_code}" -X POST -H "$H" -H "$CT" \
  -d '{"name":"bad-demux","listen":"::","listen_port":19999,"presets":["trojan-tcp","vless-tcp"],"demux_template":{"network":["tcp"],"rules":[{"name":"x","match":{"tls":{}},"action":{"reject":true}}]}}' \
  "$BASE/v1/controlplane/sets")
grep -q cp_invalid_demux /tmp/bad_demux.json || { cat /tmp/bad_demux.json; fail "expected cp_invalid_demux"; }
test "$code" = "400" || fail "expected 400 got $code"
pass demux_validate

echo "== user + trojan set for ACME =="
curl -fkSs -X POST -H "$H" -H "$CT" -d '{"name":"acme-http01"}' "$BASE/v1/controlplane/users" >/tmp/u.json
curl -fkSs -X POST -H "$H" -H "$CT" \
  -d '{"name":"trojan-443","listen":"::","listen_port":443,"presets":["trojan-tcp"]}' \
  "$BASE/v1/controlplane/sets" >/dev/null
curl -fkSs -X POST -H "$H" "$BASE/v1/controlplane/sets/trojan-443/activate" >/dev/null || true
pass set_ready

echo "== ACME domain HTTP-01 (ports 80+443 free) =="
# wipe prior acme cache for clean obtain
docker exec subserver-cp rm -rf /var/lib/subserver/controlplane/acme 2>/dev/null || true
curl -fkSs -X PUT -H "$H" -H "$CT" -d "{\"mode\":\"acme_domain\",\"acme\":{\"email\":\"${EMAIL}\",\"domains\":[\"${DOMAIN}\"],\"provider\":\"letsencrypt\"}}" \
  "$BASE/v1/controlplane/tls" | python3 -c 'import json,sys; d=json.load(sys.stdin)["data"]; print(d.get("mode"), d.get("material_status",{}).get("active_material"))'
# rematerialize by re-activate
curl -fkSs -X POST -H "$H" "$BASE/v1/controlplane/sets/trojan-443/activate" >/tmp/act.json || { cat /tmp/act.json; docker logs subserver-cp 2>&1 | tail -40; fail activate; }
sleep 8
ok=0
for i in 1 2 3 4 5 6 7 8 9 10; do
  if echo | openssl s_client -connect 127.0.0.1:443 -servername "$DOMAIN" 2>/dev/null | openssl x509 -noout -issuer -subject 2>/dev/null | tee /tmp/cert_http01.txt | grep -qiE "Let.?s Encrypt|letsencrypt|YE1|R3|E1"; then
    ok=1; break
  fi
  sleep 3
done
test "$ok" = "1" || { cat /tmp/cert_http01.txt || true; docker logs subserver-cp 2>&1 | tail -60; fail http01_cert; }
pass http01_domain
cat /tmp/cert_http01.txt

echo "== switch self_signed then ACME IP HTTP-01 =="
curl -fkSs -X PUT -H "$H" -H "$CT" \
  -d "{\"mode\":\"self_signed\",\"self_signed\":{\"common_name\":\"${IP}\",\"dns_sans\":[\"localhost\"],\"ip_sans\":[\"${IP}\"],\"key_type\":\"p256\",\"valid_days\":3650}}" \
  "$BASE/v1/controlplane/tls" >/dev/null
docker exec subserver-cp rm -rf /var/lib/subserver/controlplane/acme 2>/dev/null || true
curl -fkSs -X PUT -H "$H" -H "$CT" \
  -d "{\"mode\":\"acme_ip\",\"acme\":{\"email\":\"${EMAIL}\",\"domains\":[\"${IP}\"],\"provider\":\"letsencrypt\"}}" \
  "$BASE/v1/controlplane/tls" >/dev/null
curl -fkSs -X POST -H "$H" "$BASE/v1/controlplane/sets/trojan-443/activate" >/dev/null || true
sleep 8
ok=0
for i in 1 2 3 4 5 6 7 8 9 10; do
  if echo | openssl s_client -connect 127.0.0.1:443 -servername "$IP" 2>/dev/null | openssl x509 -noout -issuer -subject 2>/dev/null | tee /tmp/cert_ip.txt | grep -qiE "Let.?s Encrypt|letsencrypt|YE1|R3|E1"; then
    ok=1; break
  fi
  sleep 3
done
test "$ok" = "1" || { cat /tmp/cert_ip.txt || true; docker logs subserver-cp 2>&1 | tail -60; fail http01_ip; }
pass http01_ip
cat /tmp/cert_ip.txt

echo "== alternative_http_port path =="
# reset to self_signed and clear acme
curl -fkSs -X PUT -H "$H" -H "$CT" \
  -d "{\"mode\":\"self_signed\",\"self_signed\":{\"common_name\":\"${DOMAIN}\",\"dns_sans\":[\"${DOMAIN}\",\"localhost\"],\"ip_sans\":[\"${IP}\"],\"key_type\":\"p256\",\"valid_days\":3650}}" \
  "$BASE/v1/controlplane/tls" >/dev/null
docker exec subserver-cp rm -rf /var/lib/subserver/controlplane/acme 2>/dev/null || true
ALT=9080
# host DNAT: public 80 -> container published ALT
iptables -t nat -C PREROUTING -p tcp --dport 80 -j REDIRECT --to-ports "$ALT" 2>/dev/null || \
  iptables -t nat -A PREROUTING -p tcp --dport 80 -j REDIRECT --to-ports "$ALT"

curl -fkSs -X PUT -H "$H" -H "$CT" -d "{\"mode\":\"acme_domain\",\"acme\":{\"email\":\"${EMAIL}\",\"domains\":[\"${DOMAIN}\"],\"provider\":\"letsencrypt\",\"disable_tls_alpn_challenge\":true,\"alternative_http_port\":${ALT}}}" \
  "$BASE/v1/controlplane/tls" >/dev/null
curl -fkSs -X POST -H "$H" "$BASE/v1/controlplane/sets/trojan-443/activate" >/tmp/act2.json || { cat /tmp/act2.json; docker logs subserver-cp 2>&1 | tail -80; fail alt_activate; }
sleep 10
ok=0
for i in 1 2 3 4 5 6 7 8 9 10 11 12; do
  if echo | openssl s_client -connect 127.0.0.1:443 -servername "$DOMAIN" 2>/dev/null | openssl x509 -noout -issuer -subject 2>/dev/null | tee /tmp/cert_alt.txt | grep -qiE "Let.?s Encrypt|letsencrypt|YE1|R3|E1"; then
    ok=1; break
  fi
  sleep 3
done
iptables -t nat -D PREROUTING -p tcp --dport 80 -j REDIRECT --to-ports "$ALT" 2>/dev/null || true
test "$ok" = "1" || { cat /tmp/cert_alt.txt || true; docker logs subserver-cp 2>&1 | tail -80; fail alt_http_port; }
pass alt_http_port
cat /tmp/cert_alt.txt

echo FINAL_OK
