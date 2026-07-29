# 04 — Shaping (rate limits)

## Layers

| Layer | Source | Survives rematerialize? |
|-------|--------|-------------------------|
| `controlplane` | CP user `speed_up_bytes_per_sec` / `speed_down_bytes_per_sec` | Rebuilt each materialize |
| `manual` | `PUT /v1/traffic/limits` | **Yes** — CP refresh does not wipe it |
| `effective` | CP ∪ manual; **manual wins** on the same key | — |

```bash
curl -fsS -H "Authorization: Bearer $TOKEN" "$BASE/v1/traffic/limits"
```

## Keys = `metadata.User`

Shaping looks up the **exact** dataplane user string on the connection.

| Mode / protocol | Typical key |
|-----------------|-------------|
| Subscribe/static SS, Trojan, VLESS with `"name":"alice"` | `alice` |
| Controlplane VLESS variants | `alice-flow-none`, `alice-flow-xtls-rprx-vision`, … |
| Controlplane SS (no variants) | `alice` (display name) |

**Controlplane `speed_*`:** bridge expands the limit onto **all** keys for that user
(bare name + variants) — shaping works out of the box.

**Manual PUT:** bare display names that prefix registered subject keys are expanded
(e.g. `"alice"` → `alice`, `alice-flow-none`, …). Exact variant keys are applied as-is
and do not fan out to siblings. Subscribe/static usually use the inbound `users[].name`
directly (no variants).

## Manual limits API

Replaces the **entire** manual map (not a patch merge of one user):

```bash
curl -fsS -X PUT -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "limits": {
      "bob": {"up_bytes_per_sec": 32768, "down_bytes_per_sec": 32768},
      "alice-flow-none": {"up_bytes_per_sec": 1048576, "down_bytes_per_sec": 1048576}
    }
  }' \
  "$BASE/v1/traffic/limits"
```

### Semantics of 0

- Missing key → no manual entry (CP layer may still apply).
- Manual `{0,0}` for a key → **force unlimited** for that key (deletes CP contribution in effective).
- Clear all manual: `{"limits": {}}`.

## Controlplane speed fields

```bash
curl -fsS -X PATCH -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"speed_up_bytes_per_sec":65536,"speed_down_bytes_per_sec":65536}' \
  "$BASE/v1/controlplane/users/$USER_ID"
```

`0` / omit = unlimited for that direction on the CP layer.

## Live unthrottle

Rate wrappers re-read limiters on each Read/Write. Clearing `speed_*` or
manual limits applies to **existing** connections without reconnect.

## Burst

Token bucket burst is clamped roughly to **16–64 KiB** from the configured rate.
Small transfers can still look “instant”; multi‑MiB downloads show the cap.
