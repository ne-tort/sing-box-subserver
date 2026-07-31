# 09 — Build and CI (controlplane)

## Build tag

| Tag | Effect |
|-----|--------|
| `with_controlplane` | Compile real module, register routes, wire expiry loop |
| absent | Stub `New` returns nil; no `/v1/controlplane` or `/v1/sub/` routes |

See [ADR 0001](adr/0001-optional-build-tag.md).

## Relation to `build/tags.server`

**Default** [`build/tags.server`](../../build/tags.server) does **not** include
`with_controlplane` (opt-out by omission — lightweight default edge agent).

Operators who want the module build:

```bash
TAGS="$(tr -d '\r\n' < build/tags.server),with_controlplane"
go build -tags "$TAGS" -o dist/subserver ./cmd/subserver
```

Demux-backed sets also need `with_demux` (already in default server tags today).

## CI matrix (normative intent)

| Job | Tags | Purpose |
|-----|------|---------|
| existing test/vet/build | `build/tags.server` only | Default agent without CP |
| `test-controlplane` | `tags.server` + `with_controlplane` | `go test` packages under `internal/controlplane/...` + wire smoke |
| optional `build-controlplane-*` | same | Artifact variant (optional release) |

Allowlist policy: either extend CI to accept an explicit second tags file
`build/tags.server.controlplane` (= server + `with_controlplane`) **or** append tag in the
job only. Prefer a **checked-in** `build/tags.server.controlplane` for reproducibility.

## Docker matrix (dev, no live ACME)

```bash
# Linux/macOS
./scripts/cp_matrix_docker.sh

# Windows
./scripts/cp_matrix_docker.ps1
```

Builds a linux binary with `tags.server.controlplane`, runs `docker-compose.cp-smoke.yml`,
and exercises: default `self_signed` (+ IP SAN), cert-manager validation rejects for IP+zerossl/dns01,
shadowsocks + trojan activate, subscription `insecure`, TLS handshake, PEM regenerate + Force reload.

Live Let's Encrypt is intentionally out of scope for this matrix.

## Stubs

Mirror existing optional features (`register_demux.go` / `_stub.go`):

- `module.go` — `//go:build with_controlplane`
- `module_stub.go` — `//go:build !with_controlplane`

App wiring: `if svc != nil { api.Controlplane = svc; go svc.Run(ctx) }`.

## Version / capabilities

`GET /v1/version` / status `build_tags` already lists compile tags; presence of
`with_controlplane` is the capability flag. Optional additive
`capabilities.controlplane: true` later — not required if tags are exposed.

## Size

Expect modest binary growth (templates embed). Log size in CP build job; no hard gate in docs v1.

## Published container images (client / panel contract)

VPN-client and panel SSH bootstrap **must not** build from source on the VPS.
They pull a prebuilt image:

| Ref | Tags file | Audience |
|-----|-----------|----------|
| `ghcr.io/ne-tort/sing-box-subserver:controlplane` | `build/tags.server.controlplane` | **Canonical** for CP clients |
| `ghcr.io/ne-tort/sing-box-subserver:<semver>-controlplane` | same | Pinned releases |
| `docker.io/<org>/sing-box-subserver:controlplane` | same | Optional mirror |
| `:latest` | **ambiguous** — may be `tags.server` without CP | Do **not** use as client default |

Dockerfile:

```bash
# Context = vendor/ (sibling of sing-vmess). Do NOT build with context=sing-box-subserver alone.
cd vendor
docker build -f sing-box-subserver/Dockerfile \
  -t ghcr.io/ne-tort/sing-box-subserver:controlplane \
  --build-arg TAGS_FILE=build/tags.server.controlplane .
```

Windows one-shot (subserver + core + Flutter + push): from repo root
`.\scripts\build_cp_windows.ps1` (see app docs `docs/controlplane-client/09-build-and-ci.md`).

CI **should** publish `:controlplane` (and versioned `-controlplane` tags) on main/release.
Until the publish job exists, local/lab builds must tag explicitly — see
[`deploy/install-edge-image.sh`](../../deploy/install-edge-image.sh) and
[`deploy/docker-compose.yml`](../../deploy/docker-compose.yml).

Compose / install helpers:

- Image-first install: `deploy/install-edge-image.sh` (sets `SUBSERVER_IMAGE`).
- Generic install: `deploy/install-edge.sh` — set `SUBSERVER_IMAGE` for production; empty image = clone+build (dev only).

