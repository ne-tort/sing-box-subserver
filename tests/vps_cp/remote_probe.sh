#!/bin/bash
set -euo pipefail
AUTH='Authorization: Bearer vps-cp-token-dev-only'
BASE='https://127.0.0.1:8080'

USERJSON=$(curl -sk -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{"name":"probe-local","enabled":true}' "$BASE/v1/controlplane/users")
TOKEN=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["data"]["sub_token"])' <<<"$USERJSON")
echo "token=$TOKEN"

probe_set() {
  local set_name="$1"
  local port="$2"
  curl -sk "$BASE/v1/sub/$TOKEN?set=$set_name" -o /tmp/sub-$set_name.json
  python3 - "$set_name" "$port" <<'PY'
import json,sys
set_name, port = sys.argv[1], int(sys.argv[2])
sub=json.load(open(f"/tmp/sub-{set_name}.json"))
assert sub.get("outbounds"), sub
ob=sub["outbounds"][0]
print("outbound", ob.get("type"), ob.get("tag"), ob.get("server"), ob.get("server_port"))
ob = dict(ob)
ob["server"] = "127.0.0.1"  # local loopback to box
cfg={
  "log":{"level":"debug"},
  "inbounds":[{"type":"mixed","tag":"m","listen":"127.0.0.1","listen_port":port}],
  "outbounds":[ob,{"type":"direct","tag":"direct"}],
  "route":{"final":ob["tag"]},
}
json.dump(cfg, open(f"/tmp/client-{set_name}.json","w"), indent=2)
print("wrote", f"/tmp/client-{set_name}.json")
PY
  docker rm -f "probe-$set_name" >/dev/null 2>&1 || true
  docker run -d --name "probe-$set_name" --network host \
    -v "/tmp/client-$set_name.json:/work/client.json:ro" \
    ghcr.io/sagernet/sing-box:v1.12.12 run -c /work/client.json
  sleep 2
  echo "=== probe $set_name ==="
  if curl -x "http://127.0.0.1:$port" -sS -m 25 https://1.1.1.1/cdn-cgi/trace | head -15; then
    echo "OK $set_name"
  else
    echo "FAIL $set_name"
    docker logs "probe-$set_name" 2>&1 | tail -50
  fi
  docker rm -f "probe-$set_name" >/dev/null 2>&1 || true
}

probe_set e2e-ss 19080
probe_set e2e-demux 19081
probe_set e2e-acme-tls 19082
