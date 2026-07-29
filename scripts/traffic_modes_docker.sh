#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
COMPOSE="docker-compose.traffic-modes.yml"

echo "== host-build linux binaries =="
mkdir -p dist testdata/docker/client
TAGS_CP="$(tr -d '\r\n' < build/tags.server.traffic.controlplane)"
TAGS_T="$(tr -d '\r\n' < build/tags.server.traffic)"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath \
  -ldflags="-s -w -checklinkname=0" -tags "$TAGS_CP" \
  -o dist/subserver-traffic-cp-linux ./cmd/subserver
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath \
  -ldflags="-s -w -checklinkname=0" -tags "$TAGS_T" \
  -o dist/subserver-traffic-linux ./cmd/subserver

echo "== compose up =="
docker compose -f "$COMPOSE" down -v >/dev/null 2>&1 || true
docker compose -f "$COMPOSE" up -d --build

echo "== wait health =="
ok=0
for i in $(seq 1 60); do
  if curl -fkSs https://127.0.0.1:18082/v1/health >/dev/null 2>&1 \
     && curl -fsS http://127.0.0.1:18083/v1/health >/dev/null 2>&1; then
    ok=1; break
  fi
  sleep 2
done
if [[ "$ok" != "1" ]]; then
  docker compose -f "$COMPOSE" logs --tail 80
  exit 1
fi

EXTRA=()
[[ "${SKIP_IPERF:-}" == "1" ]] && EXTRA+=(--skip-iperf)
python3 scripts/smoke_traffic_modes.py --insecure "${EXTRA[@]}"

echo "== OK traffic modes matrix =="
[[ "${KEEP_UP:-}" == "1" ]] || docker compose -f "$COMPOSE" down -v
