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
- [x] Management API + `/v1/sub` HTTPS from CP TLS profile (self_signed / ACME / interim)
- [x] ACME emergency watchdog → persistent `self_signed` + rematerialize

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
curl -X PUT .../v1/controlplane/tls -d '{
  "mode":"acme_domain",
  "acme":{"email":"ops@example.com","domains":["wiki.ai-qwerty.ru"],"provider":"letsencrypt"}
}'
# poll until material_status.ready == true
```

Helpers: `scripts/vps_acme_http01_catalogs.sh`, `scripts/vps_acme_alt_http.sh`.
