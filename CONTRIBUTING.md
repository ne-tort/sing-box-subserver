# Contributing

## Before code

1. Read [docs/00-index.md](docs/00-index.md) and relevant ADRs.
2. Wire-contract changes update `docs/05-api.md` (and OpenAPI when present) in the same PR.
3. Follow [docs/10-repo-culture.md](docs/10-repo-culture.md).

## Setup

```bash
git clone https://github.com/ne-tort/sing-box-subserver.git
cd sing-box-subserver
git submodule update --init third_party/sing-box-lx
git -C third_party/sing-box-lx submodule update --init submodules/wireguard-go
# Skip lx clients/* — not required for the agent.
```

Go toolchain: see `.go-version` / `go.mod`.

## Build / test

```bash
TAGS=$(tr -d '\r\n' < build/tags.server)
go test -tags "$TAGS" ./...
go vet -tags "$TAGS" ./...
go build -tags "$TAGS" -o dist/subserver ./cmd/subserver
./dist/subserver -version
```

On Windows PowerShell:

```powershell
$TAGS = (Get-Content build/tags.server -Raw).Trim()
go build -tags $TAGS -o dist/subserver.exe ./cmd/subserver
```

## Commits

Conventional prefixes: `feat:`, `fix:`, `docs:`, `chore:`, `ci:`, `build:`.

Bump of `third_party/sing-box-lx`: `chore: bump sing-box-lx to <sha>`.

## Do not

- Commit secrets or `agent.local.yaml`.
- Add an in-process self-updater ([ADR 0005](docs/adr/0005-external-updates-only.md)).
- Import s-ui / panel packages ([ADR 0004](docs/adr/0004-independent-of-sui.md)).
