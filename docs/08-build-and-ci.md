# 08 — Build and CI

## Binary goals

- One Linux binary: `subserver`.
- **Server edge profile** — inbound protocols + WireGuard endpoint (+ AWG).
- No frontend, no panel SQLite, **no Clash API** ([ADR 0006](adr/0006-no-clash-api.md)).
- **No in-binary updater** ([ADR 0005](adr/0005-external-updates-only.md)); CI publishes artifacts for external deploy.

## Core pin (same family as s-ui)

```
third_party/sing-box-lx  →  https://github.com/ne-tort/sing-box-lx.git
```

Pinned to the **same commit** used by the mother panel when releasing a matching agent. `go.mod`:

```
require github.com/sagernet/sing-box v1.14.0-lx.17
replace github.com/sagernet/sing-box => ./third_party/sing-box-lx
replace github.com/sagernet/wireguard-go => ./third_party/sing-box-lx/submodules/wireguard-go
```

Bump lx with an explicit commit in both submodule pointer and release notes.

## Version injection

ldflags (example):

```
-X github.com/ne-tort/sing-box-subserver/internal/version.AgentVersion=0.1.0
-X github.com/ne-tort/sing-box-subserver/internal/version.AgentCommit=<git-sha>
-X github.com/ne-tort/sing-box-subserver/internal/version.SingBoxCommit=<lx-sha>
-X github.com/sagernet/sing-box/constant.Version=<lx-version-string>
```

Runtime status exposes `agent_version`, `agent_commit`, `singbox_version`, `singbox_commit`, `build_tags` ([05-api](05-api.md)).

## Build tags (allowlist)

File: [`build/tags.server`](../build/tags.server) — single source of truth. CI fails if build uses other tags.

Default server set (slim vs panel): keep wireguard/AWG/quic/utls/server inbounds; **omit** `with_clash_api` ([ADR 0006](adr/0006-no-clash-api.md)) and other client/UI-heavy tags.

CI injects version ldflags (`AgentVersion`, `AgentCommit`, `SingBoxCommit`) on build artifacts. Local builds without `-X` report `0.0.0-dev` / `unknown`.

## Local build

```bash
git submodule update --init third_party/sing-box-lx
git -C third_party/sing-box-lx submodule update --init submodules/wireguard-go
TAGS=$(tr -d '\r\n' < build/tags.server)
go build -trimpath -ldflags="-s -w -checklinkname=0" -tags "$TAGS" -o dist/subserver ./cmd/subserver
./dist/subserver -version
```

## CI matrix

| Job | Purpose |
|-----|---------|
| `test` | `go test ./...` with tags |
| `vet` | `go vet` |
| `build-linux-amd64` | release artifact |
| `build-linux-arm64` | release artifact |
| `tags-allowlist` | ensure workflow uses `build/tags.server` only |

Checkout: init `third_party/sing-box-lx`, then only `submodules/wireguard-go` inside lx (skip `clients/*`). Keep CI lightweight (no npm).

## Release / upgrade path

GitHub Releases: `subserver_linux_amd64`, `subserver_linux_arm64`, checksums.  
Deploy: external script or Compose pulls new asset and restarts — agent is not asked to update itself.

## Size budget

Log binary size in CI. Soft alert on unexplained +20% after baseline exists.
