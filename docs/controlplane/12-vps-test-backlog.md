# Controlplane VPS test backlog / follow-ups

Captured from live tests on `163.5.180.181` (2026-07-28).

## Done on VPS

- [x] self_signed default + trojan handshake + regenerate Force reload
- [x] `acme_domain` via TLS-ALPN-01 and HTTP-01 (nginx stopped)
- [x] `acme_ip` LE shortlived
- [x] `alternative_http_port` + host REDIRECT (host network)
- [x] presets catalog (13) + demux-recipes API (9) + `cp_invalid_demux`
- [x] Deploy default = **host network** (`deploy/docker-compose.yml`, install-edge, VPS scripts)
- [x] `material_status.ready` from certmagic PEM presence (ACME obtain gate)

## Gaps / follow-ups

1. **ACME in-memory race**: even after PEM on disk, first handshakes can briefly fail until certmagic loads — clients should retry; optional TLS dial probe can be added later.
2. **Parent sui submodule pin**: bump submodule SHA after this push.
3. **Management TLS in production**:
   - Lab default remains `insecure_public_bind` + `http://` AgentURL (panel bootstrap).
   - Real scenarios:
     1. `SUBSERVER_MGMT_TLS=self_signed` on install → agent `tls.cert/key`, panel `https://` + skip-verify (or pin CA).
     2. Bind management to `127.0.0.1:8080` and reach via SSH tunnel / private overlay.
     3. Terminate TLS on host reverse-proxy (Caddy/nginx) to loopback agent.
   - Full LE for management API is out of scope (ADR 0007); dataplane certs stay on CP TLS profiles.

## Ops notes

Default deploy (host network):

```bash
# see deploy/docker-compose.yml / install-edge.sh
docker compose up -d   # network_mode: host — no -p flags
```

HTTP-01 with free :80 (host network binds 80/443 directly):

```bash
curl -X PUT .../v1/controlplane/tls -d '{
  "mode":"acme_domain",
  "acme":{"email":"ops@example.com","domains":["wiki.ai-qwerty.ru"],"provider":"letsencrypt"}
}'
# poll until material_status.ready == true
```

Helpers: `scripts/vps_acme_http01_catalogs.sh`, `scripts/vps_acme_alt_http.sh`.
