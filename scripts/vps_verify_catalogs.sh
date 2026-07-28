#!/bin/bash
set -eu
H='Authorization: Bearer vps-cp-token-dev-only'
BASE=https://127.0.0.1:8080
curl -fkSs -H "$H" "$BASE/v1/controlplane/status"; echo
curl -fkSs -H "$H" "$BASE/v1/controlplane/presets" | python3 -c 'import json,sys; print("presets",len(json.load(sys.stdin)["data"]))'
curl -fkSs -H "$H" "$BASE/v1/controlplane/demux-recipes" | python3 -c 'import json,sys; print("recipes",len(json.load(sys.stdin)["data"]))'
curl -fkSs -H "$H" "$BASE/v1/controlplane/tls" | python3 -c 'import json,sys; d=json.load(sys.stdin)["data"]; ms=d["material_status"]; print("mode",d["mode"],"ready",ms.get("ready"),"mgmt",ms.get("mgmt_https"),"src",ms.get("mgmt_cert_source"))'
systemctl is-enabled nginx || true
docker ps --format '{{.Names}} {{.Status}}'
