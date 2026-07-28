#!/bin/bash
set -eu
H='Authorization: Bearer vps-cp-token-dev-only'
curl -fsS -H "$H" http://127.0.0.1:8080/v1/controlplane/status; echo
curl -fsS -H "$H" http://127.0.0.1:8080/v1/controlplane/presets | python3 -c 'import json,sys; print("presets",len(json.load(sys.stdin)["data"]))'
curl -fsS -H "$H" http://127.0.0.1:8080/v1/controlplane/demux-recipes | python3 -c 'import json,sys; print("recipes",len(json.load(sys.stdin)["data"]))'
systemctl is-enabled nginx || true
docker ps --format '{{.Names}} {{.Status}}'
