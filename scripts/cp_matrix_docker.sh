#!/usr/bin/env bash
# Controlplane scenario matrix (self_signed only — no live ACME).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
TOKEN="smoke-token-not-for-prod"
BASE="${BASE_URL:-https://127.0.0.1:18081}"
COMPOSE="docker-compose.cp-smoke.yml"
auth() { echo -n "-H" "Authorization: Bearer ${TOKEN}" "-H" "Content-Type: application/json"; }

echo "== host-build linux binary (controlplane tags) =="
mkdir -p dist
TAGS="$(tr -d '\r\n' < build/tags.server.controlplane)"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w -checklinkname=0" -tags "$TAGS" -o dist/subserver-cp-linux ./cmd/subserver

echo "== compose up (runtime image + prebuilt binary) =="
docker compose -f "$COMPOSE" down -v >/dev/null 2>&1 || true
docker compose -f "$COMPOSE" up -d --build

echo "== wait health =="
for i in $(seq 1 120); do
  if curl -fkSs "$BASE/v1/health" >/dev/null; then
    break
  fi
  sleep 1
done
curl -fkSs "$BASE/v1/health" >/dev/null

echo "== case: default TLS self_signed + IP SAN =="
TLS=$(curl -fkSs -H "Authorization: Bearer ${TOKEN}" "$BASE/v1/controlplane/tls")
echo "$TLS" | grep -q '"mode":"self_signed"'
echo "$TLS" | grep -q '203.0.113.10'
echo "$TLS" | grep -q '"self_signed_cert_present":true'

echo "== case: validate rejects (no live ACME) =="
code=$(curl -s -o /dev/null -w "%{http_code}" -X PUT \
  -H "Authorization: Bearer ${TOKEN}" -H "Content-Type: application/json" \
  "$BASE/v1/controlplane/tls" \
  -d '{"mode":"acme_ip","acme":{"email":"a@b.c","domains":["203.0.113.10"],"provider":"zerossl"}}')
test "$code" = "400"
code=$(curl -s -o /dev/null -w "%{http_code}" -X PUT \
  -H "Authorization: Bearer ${TOKEN}" -H "Content-Type: application/json" \
  "$BASE/v1/controlplane/tls" \
  -d '{"mode":"acme_ip","acme":{"email":"a@b.c","domains":["203.0.113.10"],"provider":"letsencrypt","dns01_challenge":{"provider":"cloudflare","api_token":"x"}}}')
test "$code" = "400"

echo "== case: user + shadowsocks set =="
USER_JSON=$(curl -fkSs -X POST \
  -H "Authorization: Bearer ${TOKEN}" -H "Content-Type: application/json" \
  "$BASE/v1/controlplane/users" -d '{"name":"alice"}')
TOK=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["data"]["sub_token"])' <<<"$USER_JSON")
curl -fkSs -X POST \
  -H "Authorization: Bearer ${TOKEN}" -H "Content-Type: application/json" \
  "$BASE/v1/controlplane/sets" \
  -d '{"name":"ss1","listen":"0.0.0.0","listen_port":1080,"presets":["shadowsocks-tcp"]}' >/dev/null
ACT=$(curl -fkSs -X POST -H "Authorization: Bearer ${TOKEN}" "$BASE/v1/controlplane/sets/ss1/activate")
echo "$ACT" | grep -q '"config_mode":"controlplane"'

echo "== case: trojan-tcp + PEM paths in live config =="
curl -fkSs -X POST \
  -H "Authorization: Bearer ${TOKEN}" -H "Content-Type: application/json" \
  "$BASE/v1/controlplane/sets" \
  -d '{"name":"tr1","listen":"0.0.0.0","listen_port":8443,"presets":["trojan-tcp"]}' >/dev/null
curl -fkSs -X POST -H "Authorization: Bearer ${TOKEN}" "$BASE/v1/controlplane/sets/tr1/activate" >/dev/null
CFG=$(curl -fkSs -H "Authorization: Bearer ${TOKEN}" "$BASE/v1/config")
echo "$CFG" | grep -q certificate_path
echo "$CFG" | grep -q 'controlplane/tls/server'
! echo "$CFG" | grep -q certificate_provider

echo "== case: subscription insecure for self_signed =="
SUB=$(curl -fkSs "$BASE/v1/sub/$TOK")
echo "$SUB" | grep -q '"insecure":true'

echo "== case: TLS handshake to mapped trojan port =="
# Prefer openssl on host if present; otherwise skip live handshake (config+regen still covered).
if command -v openssl >/dev/null 2>&1; then
  echo | openssl s_client -connect 127.0.0.1:18443 -servername 203.0.113.10 2>/dev/null | grep -q 'BEGIN CERTIFICATE'
else
  echo "(skip openssl: not installed on host)"
fi

echo "== case: regenerate reloads PEM =="
FP1=$(docker exec subserver-cp-smoke sha256sum /var/lib/subserver/controlplane/tls/server.crt)
REV1=$(curl -fkSs -H "Authorization: Bearer ${TOKEN}" "$BASE/v1/status" | python3 -c 'import json,sys; d=json.load(sys.stdin); print((d.get("data") or d)["revision"])')
curl -fkSs -X POST -H "Authorization: Bearer ${TOKEN}" "$BASE/v1/controlplane/tls/regenerate" >/dev/null
FP2=$(docker exec subserver-cp-smoke sha256sum /var/lib/subserver/controlplane/tls/server.crt)
test "$FP1" != "$FP2"
REV2=$(curl -fkSs -H "Authorization: Bearer ${TOKEN}" "$BASE/v1/status" | python3 -c 'import json,sys; d=json.load(sys.stdin); print((d.get("data") or d)["revision"])')
test "$REV2" -gt "$REV1"
if command -v openssl >/dev/null 2>&1; then
  echo | openssl s_client -connect 127.0.0.1:18443 -servername 203.0.113.10 2>/dev/null | grep -q 'BEGIN CERTIFICATE'
fi

echo "== OK controlplane matrix =="
docker compose -f "$COMPOSE" down -v
