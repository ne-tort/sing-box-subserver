#!/usr/bin/env bash
# Smoke scenarios against docker-compose.smoke.yml
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
TOKEN="smoke-token-not-for-prod"
BASE="${BASE_URL:-http://127.0.0.1:18080}"

echo "== compose up =="
docker compose -f docker-compose.smoke.yml up -d --build

echo "== wait health =="
for i in $(seq 1 60); do
  if curl -fsS "$BASE/v1/health" >/dev/null; then
    break
  fi
  sleep 1
done
curl -fsS "$BASE/v1/health" | tee /tmp/subserver-health.json
grep -q alive /tmp/subserver-health.json

echo "== version =="
curl -fsS "$BASE/v1/version" | tee /tmp/subserver-version.json

echo "== unauthorized status =="
code=$(curl -s -o /dev/null -w "%{http_code}" "$BASE/v1/status" || true)
test "$code" = "401"

echo "== put minimal config =="
CFG='{"log":{"level":"error"},"inbounds":[],"outbounds":[{"type":"direct","tag":"direct"}]}'
curl -fsS -X PUT "$BASE/v1/config" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "$CFG" | tee /tmp/subserver-put.json
grep -q '"ok":true' /tmp/subserver-put.json

echo "== ready =="
curl -fsS -H "Authorization: Bearer $TOKEN" "$BASE/v1/ready" | tee /tmp/subserver-ready.json

echo "== reject clash_api =="
CLASH='{"experimental":{"clash_api":{"external_controller":"0.0.0.0:9090"}},"outbounds":[{"type":"direct","tag":"d"}]}'
code=$(curl -s -o /tmp/subserver-clash.json -w "%{http_code}" -X POST "$BASE/v1/validate" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d "$CLASH")
test "$code" = "422"

echo "== box stop/start =="
curl -fsS -X POST -H "Authorization: Bearer $TOKEN" "$BASE/v1/box/stop" >/dev/null
code=$(curl -s -o /dev/null -w "%{http_code}" -H "Authorization: Bearer $TOKEN" "$BASE/v1/ready")
test "$code" = "503"
curl -fsS -X POST -H "Authorization: Bearer $TOKEN" "$BASE/v1/box/start" >/dev/null
curl -fsS -H "Authorization: Bearer $TOKEN" "$BASE/v1/ready" >/dev/null

echo "== metrics =="
curl -fsS -H "Authorization: Bearer $TOKEN" "$BASE/v1/metrics?format=json" | tee /tmp/subserver-metrics.json

echo "== OK smoke =="
docker compose -f docker-compose.smoke.yml down
