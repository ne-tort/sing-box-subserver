#!/usr/bin/env bash
# Traffic + controlplane scenario matrix (quota / shaping / reset).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
TOKEN="smoke-token-not-for-prod"
BASE="${BASE_URL:-https://127.0.0.1:18082}"
COMPOSE="docker-compose.traffic-smoke.yml"

echo "== host-build linux binary (traffic+controlplane tags) =="
mkdir -p dist
TAGS="$(tr -d '\r\n' < build/tags.server.traffic.controlplane)"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath \
  -ldflags="-s -w -checklinkname=0" -tags "$TAGS" \
  -o dist/subserver-traffic-cp-linux ./cmd/subserver
bash scripts/ensure_libcronet.sh amd64

echo "== compose up =="
docker compose -f "$COMPOSE" down -v >/dev/null 2>&1 || true
docker compose -f "$COMPOSE" up -d --build

echo "== wait health =="
ok=0
for i in $(seq 1 60); do
  if curl -fkSs "$BASE/v1/health" >/dev/null 2>&1; then
    ok=1
    break
  fi
  sleep 2
done
if [[ "$ok" != "1" ]]; then
  docker compose -f "$COMPOSE" logs --tail 80
  exit 1
fi

echo "== python traffic scenarios =="
python3 scripts/smoke_traffic_scenarios.py --base "$BASE" --token "$TOKEN" --insecure

echo "== OK traffic+controlplane matrix =="
docker compose -f "$COMPOSE" down -v
