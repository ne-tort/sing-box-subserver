# 08 — Build and CI

## Binary goals

- One static(ish) Linux binary: `subserver`.
- **Server edge profile** — inbound protocols + WireGuard endpoint (+ AWG tags if required by lx).
- No frontend, no panel SQLite, no clash UI experimentals unless later justified.

## Module graph

```
module github.com/ne-tort/sing-box-subserver

require github.com/sagernet/sing-box  (via replace → sing-box-lx)
```

Pin **sing-box-lx** with `replace` / submodule or Go workspace — same family as s-ui. Document the pin commit in `go.mod` comments.

## Build tags (allowlist)

Default `SERVER_TAGS` (illustrative; finalize at implementation against lx):

```
with_quic,with_wireguard,with_utls,with_reality,with_acme,with_gvisor,with_awg,...
```

Include only what server inbounds need (VLESS/Trojan/SS/Naive/Hy2/TUIC/… as product requires).

**Explicitly exclude from default edge profile** unless a product decision says otherwise:

- client-centric TUN as primary;
- heavy experimental APIs not used for serving;
- unused providers that bloat binary (tor, etc.).

CI job: fail if `go build -tags` drifts from allowlist file `build/tags.server`.

## Local build

```bash
go build -trimpath -ldflags="-s -w" -tags "$(cat build/tags.server)" -o dist/subserver ./cmd/subserver
```

## CI matrix

| Job | Purpose |
|-----|---------|
| `test` | `go test ./...` |
| `vet` | `go vet` |
| `build-linux-amd64` | release artifact |
| `build-linux-arm64` | release artifact |
| `tags-allowlist` | ensure tags file matches documented set |
| `docs-link` | optional markdown link check |

Triggers: PR + tag. Keep CI **lightweight** (no npm, no docker-in-docker unless packaging later).

## Size budget

Record binary size in CI logs. Soft budget after first green build (e.g. alert if +20% unexplained). No hard fail until baseline exists.

## Release

GitHub Releases: `subserver_linux_amd64`, `subserver_linux_arm64`, checksums. systemd unit example under `deploy/subserver.service`.

## Cross-compile

`GOOS=linux GOARCH=amd64|arm64` with CGO carefully: prefer `CGO_ENABLED=0` if lx tags allow; if WireGuard/gvisor force CGO, document and use musl/zig or Debian builders — decide at implementation and update this doc.
