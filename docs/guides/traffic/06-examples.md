# 06 — Examples

Replace `$BASE`, `$TOKEN`, ids/names as needed. HTTPS agents may need `-k` for
self-signed management TLS.

---

## A. Minimal agent with traffic (static VLESS)

**agent.yaml**

```yaml
node_id: "lab-edge"
token: "lab-token"
listen: "0.0.0.0:8080"
data_dir: "/var/lib/subserver"
insecure_public_bind: true
traffic:
  flush_interval_sec: 5
  retention_days: 7
```

**Put config** (`users[].name` = shaping keys):

```bash
curl -fsS -X PUT -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d @vless-multi.json \
  "$BASE/v1/config"
```

**Shape bob @ 32 KiB/s, unlimited alice:**

```bash
curl -fsS -X PUT -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "limits": {
      "bob": {"up_bytes_per_sec": 32768, "down_bytes_per_sec": 32768}
    }
  }' \
  "$BASE/v1/traffic/limits"
```

---

## B. Subscribe stub panel

```bash
curl -fsS -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "url": "http://mock-panel:8080/vless-multi.json",
    "interval_sec": 60,
    "jitter_sec": 0,
    "timeout_sec": 10
  }' \
  "$BASE/v1/subscribe"
```

Then same `PUT /v1/traffic/limits` as above. Cancel: `DELETE /v1/subscribe`.

---

## C. Controlplane multi-user

```bash
# create + speed + quota
curl -fsS -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"alice"}' "$BASE/v1/controlplane/users"

curl -fsS -X PATCH -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "speed_down_bytes_per_sec": 65536,
    "speed_up_bytes_per_sec": 65536,
    "traffic_limit_bytes": 50000000
  }' \
  "$BASE/v1/controlplane/users/$ALICE_ID"

# activate a VLESS set (once)
curl -fsS -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"vless1","listen":"0.0.0.0","listen_port":8443,"presets":["vless-tcp"]}' \
  "$BASE/v1/controlplane/sets"
curl -fsS -X POST -H "Authorization: Bearer $TOKEN" \
  "$BASE/v1/controlplane/sets/vless1/activate"

# inspect effective shaping (includes alice-flow-* keys)
curl -fsS -H "Authorization: Bearer $TOKEN" "$BASE/v1/traffic/limits"
```

**Ops override one VLESS variant without wiping CP layer for others:**

```bash
curl -fsS -X PUT -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "limits": {
      "alice-flow-none": {"up_bytes_per_sec": 4096, "down_bytes_per_sec": 4096}
    }
  }' \
  "$BASE/v1/traffic/limits"
```

---

## D. Inspect after real traffic

```bash
curl -fsS -H "Authorization: Bearer $TOKEN" "$BASE/v1/traffic/status"
curl -fsS -H "Authorization: Bearer $TOKEN" "$BASE/v1/traffic/onlines"
curl -fsS -H "Authorization: Bearer $TOKEN" \
  "$BASE/v1/traffic/stats?subject=cp:user:$ALICE_ID"
```

---

## E. Lab inject (never production)

```yaml
traffic:
  allow_inject: true
```

```bash
curl -fsS -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"user":"alice","up":800,"down":800}' \
  "$BASE/v1/traffic/inject"
```
