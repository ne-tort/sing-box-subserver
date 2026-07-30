#!/bin/bash
set -euo pipefail
AUTH='Authorization: Bearer vps-cp-token-dev-only'
BASE='https://127.0.0.1:8080'

USERJSON=$(curl -sk -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{"name":"probe-demux2","enabled":true}' "$BASE/v1/controlplane/users")
TOKEN=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["data"]["sub_token"])' <<<"$USERJSON")
curl -sk "$BASE/v1/sub/$TOKEN?set=e2e-demux" -o /tmp/sub-demux.json
python3 - <<'PY'
import json
sub=json.load(open("/tmp/sub-demux.json"))
print("count", len(sub.get("outbounds") or []))
for i,ob in enumerate(sub["outbounds"]):
    print(i, ob["type"], ob["tag"], ob.get("server_port"))
    cfg={
      "log":{"level":"info"},
      "inbounds":[{"type":"mixed","tag":"m","listen":"127.0.0.1","listen_port":19100+i}],
      "outbounds":[{**ob,"server":"127.0.0.1"},{"type":"direct","tag":"direct"}],
      "route":{"final":ob["tag"]},
    }
    json.dump(cfg, open(f"/tmp/client-demux-{i}.json","w"), indent=2)
PY

for i in 0 1 2; do
  f=/tmp/client-demux-$i.json
  [ -f "$f" ] || continue
  name=probe-demux-$i
  docker rm -f "$name" >/dev/null 2>&1 || true
  docker run -d --name "$name" --network host -v "$f:/work/client.json:ro" \
    ghcr.io/sagernet/sing-box:v1.12.12 run -c /work/client.json >/dev/null
  sleep 1
  port=$((19100+i))
  echo "=== demux outbound $i port $port ==="
  if curl -x "http://127.0.0.1:$port" -sS -m 25 https://1.1.1.1/cdn-cgi/trace | head -8; then
    echo OK
  else
    echo FAIL
    docker logs "$name" 2>&1 | tail -20
  fi
  docker rm -f "$name" >/dev/null 2>&1 || true
done
