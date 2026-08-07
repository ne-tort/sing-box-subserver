# Controlplane VPS test backlog / follow-ups

Captured from live tests on `163.5.180.181` (2026-07-28).

## Done on VPS

- [x] Default SSL profile self_signed + trojan handshake + regenerate
- [x] SSL ACME domain via TLS-ALPN-01 and HTTP-01 (nginx stopped)
- [x] SSL ACME IP (`acme_ip`)
- [x] `alternative_http_port` + host REDIRECT (host network)
- [x] presets catalog + demux-groups API + `cp_invalid_demux`
- [x] Deploy default = **host network** (`deploy/docker-compose.yml`, install-edge, VPS scripts)
- [x] SSL profile `status.state` from x509 / certmagic PEM presence
- [x] Management API + `/v1/sub` HTTPS (Default SSL profile)
- [x] ACME watchdog logs obtain stalls; mgmt uses Default SSL leaf

## Gaps / follow-ups

1. **ACME in-memory race**: even after PEM on disk, first handshakes can briefly fail until certmagic loads — clients should retry.
2. **Panel AgentURL**: new installs use `https://` + `agent_tls_insecure` (self_signed). Existing nodes may still have `http://` — migrate/probe.
3. **ACME obtain grace** is 5m / lost grace 2m — tune for production if needed.
4. **Protocol preset semantics phase**: pause here before expanding cross-protocol preset variants. Next stage should define per-protocol semantic contracts (inbound/outbound symmetry rules, variant scopes, client profile constraints) and only then widen variant catalogs beyond current VLESS flow model.
5. **Ownership/observability hardening**: done — owner transition log, `materialize_status`, `ownership_health`, boot orphan/stale reconcile, strict subscription filters, aggregate `/subscription-tags`, unified `UserVariantsForProtocol`. Deferred (needs per-protocol contracts first): variant catalogs beyond VLESS; ClientProfile outbound override runtime (profiles today are subscription selection tags only).
6. **`TestRun_PullDisabledKeepsServing`**: fixed — was not flaky timing; with `with_controlplane` mgmt is HTTPS and the test previously probed plain HTTP (always failed after 8s). Now probes scheme from build tags.
7. **Traffic module (`with_traffic`)**: core trackers + store + CP bridge + `/v1/traffic/*` + ConnTracker kick on ineligible landed. Still TODO: WG IpcGet path, production retention tuning under load, real SS-client throttle smoke.

## Ops notes

Default deploy (host network):

```bash
# see deploy/docker-compose.yml / install-edge.sh
docker compose up -d   # network_mode: host — no -p flags
```

HTTP-01 with free :80 (host network binds 80/443 directly):

```bash
# create/update ACME SSL profile, then bind with params.ssl_profile
curl -X POST .../v1/controlplane/ssl -d '{"name":"prod"}'
curl -X PUT .../v1/controlplane/ssl/{id} -d '{
  "type":"acme","domain":"wiki.ai-qwerty.ru","email":"ops@example.com","provider":"letsencrypt"
}'
# poll GET /ssl/{id} until status.state == ready
```

Helpers: `scripts/vps_acme_http01_catalogs.sh`, `scripts/vps_acme_alt_http.sh`.
