# 10 — Repository culture

## Docs-first contracts

- Wire contracts (REST, agent YAML, lifecycle) live in `docs/` **before** or **with** implementation.
- Changing a public API field requires updating `docs/05-api.md` (and OpenAPI when present) in the same PR.

## Cleanliness

- No secrets in git (tokens, TLS keys, `agent.local.yaml`).
- No large binaries in git; releases go to GitHub Releases / container registry.
- `gofmt` + `go vet` clean on CI.
- Prefer small PRs: docs, skeleton, feature slices.

## Commits

- Conventional style: `feat:`, `fix:`, `docs:`, `chore:`, `ci:`, `build:`.
- Submodule bumps of `third_party/sing-box-lx` are explicit (`chore: bump sing-box-lx to <sha>`).

## Package rules

See [09-repo-layout](09-repo-layout.md). Summary:

- `internal/box` must not import `api` / `pull`.
- Handlers call `supervisor` only.
- No imports from s-ui / panel modules ([ADR 0004](adr/0004-independent-of-sui.md)).

## Updates

Agent does **not** self-update ([ADR 0005](adr/0005-external-updates-only.md)). Deploy scripts and container recreate own upgrades.

## CI expectations

- Submodule checkout: lx + `wireguard-go` only (no client apps).
- `go test ./...` and `go build` with `build/tags.server`.
- Matrix: linux/amd64, linux/arm64.
- Tag allowlist: CI reads only `build/tags.server`.

## Review bar

- New dependency: justify size/security.
- New build tag: update `build/tags.server` (and/or `tags.server.controlplane`) + docs.
- Status/API fields: keep mother-panel compatibility in mind (additive JSON preferred).
- Optional controlplane changes: update `docs/controlplane/` in the same PR as the contract change.
