# sing-box-subserver

Lightweight **edge dataplane agent**: one process embeds sing-box and exposes a management REST API. Designed to be deployed on remote servers and controlled by a mother panel (e.g. s-ui), while remaining a **standalone** Go project with no compile-time dependency on the panel.

## Goals

- Thin overlay on sing-box (low RSS/CPU overhead).
- Resilient hot config apply with **last-good** rollback.
- Management plane stays up when the dataplane fails.
- Push (`PUT` config) and scheduled pull from the control plane.
- Slim builds: server inbound tags + WireGuard endpoint only.

## Non-goals

- Web UI, client database, billing, or client share-URI generation.
- Being a second full panel.

## Documentation

Start here: **[docs/00-index.md](docs/00-index.md)**

| Doc | Topic |
|-----|--------|
| [01-concept](docs/01-concept.md) | Product roles and boundaries |
| [02-requirements](docs/02-requirements.md) | FR / NFR |
| [03-architecture](docs/03-architecture.md) | Process and packages |
| [04-lifecycle](docs/04-lifecycle.md) | Apply, crash, rollback |
| [05-api](docs/05-api.md) | REST v1 contract |
| [06-control-plane](docs/06-control-plane.md) | Push / pull / auth |
| [07-observability](docs/07-observability.md) | Logs and metrics |
| [08-build-and-ci](docs/08-build-and-ci.md) | Tags, CI, lx pin |
| [09-repo-layout](docs/09-repo-layout.md) | Go layout |
| [10-repo-culture](docs/10-repo-culture.md) | Commits, cleanliness |
| [adr/](docs/adr/) | ADRs (incl. external updates only) |

## Quick start (skeleton)

```bash
git submodule update --init third_party/sing-box-lx
git -C third_party/sing-box-lx submodule update --init submodules/wireguard-go
TAGS=$(tr -d '\r\n' < build/tags.server)
go build -tags "$TAGS" -o dist/subserver ./cmd/subserver
./dist/subserver -version
```

## Status

Skeleton binary (`-version`) and architecture docs. Supervisor / REST apply pipeline next.

## License

TBD (align with mother project / sing-box-lx as needed).
