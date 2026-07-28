# Controlplane VPS test backlog / follow-ups

Captured from live tests on `163.5.180.181` (2026-07-28).

## Done on VPS

- [x] self_signed default + trojan handshake + regenerate Force reload
- [x] `acme_domain` for `wiki.ai-qwerty.ru` via **TLS-ALPN-01** (`disable_http_challenge: true`, host `:443` published)
- [x] `acme_ip` for `163.5.180.181` (LE shortlived, SAN=IP, openssl verify OK)
- [x] User create → `subscription_url` / `sub_token` / creds for all presets
- [x] `GET /v1/sub/{token}` + `?set=` / `?preset=` filters; ACME sub without `insecure`
- [x] Ownership steal (`PUT /v1/config` → direct) and reclaim via set activate
- [x] Demux set `trojan-tcp`+`vless-tcp` on `:9443` with `match.protocol=tls`

## Gaps / follow-ups

1. **HTTP-01 ACME** not exercised when host nginx owns `:80` (wiki path). Needs either temporary nginx stop, or proxy `/.well-known/acme-challenge/` into the agent `alternative_http_port`.
2. **ACME obtain race**: first handshakes can log `no certificate available for '<name>'` until certmagic finishes — consider status field / ready gate in API (not implemented).
3. **Demux authoring UX**: empty `"match":{"tls":{}}` → sing-box `empty match`; docs now show `protocol: tls`. Optional: CP validate demux templates before Apply.
4. **`build_tags` / version**: CP image must be built with `tags.server.controlplane`; `tags_controlplane.go` appends the tag at runtime.
5. **Parent sui submodule pin**: panel repo still needs submodule bump to pick these commits (out of this submodule PR unless requested).
6. **Management TLS**: agent still `insecure_public_bind` on `:8080` for labs — production should terminate TLS on management listener (root NFR).

## Ops recipe (ACME domain behind busy :80)

```bash
# publish 443 to container; leave nginx on 80
docker run ... -p 443:443 ...
curl -X PUT .../v1/controlplane/tls -d '{
  "mode":"acme_domain",
  "acme":{
    "email":"ops@example.com",
    "domains":["wiki.ai-qwerty.ru"],
    "provider":"letsencrypt",
    "disable_http_challenge":true
  }
}'
```
