# Deploy helpers

| File | Role |
|------|------|
| [`docker-compose.yml`](docker-compose.yml) | Canonical host-network service; default image `:controlplane` |
| [`install-edge-image.sh`](install-edge-image.sh) | **Preferred** panel/client bootstrap: pull image only |
| [`install-edge.sh`](install-edge.sh) | Low-level installer; set `SUBSERVER_IMAGE` or (dev) clone+build |
| [`agent.example.yaml`](agent.example.yaml) | Example agent config (see `controlplane.public_host`) |
| [`subserver.service`](subserver.service) | Optional systemd unit for non-Docker binary installs |

## Client / panel quick start

```bash
export SUBSERVER_NODE_ID="edge-1"
export SUBSERVER_TOKEN="$(openssl rand -hex 32)"
export SUBSERVER_PUBLIC_HOST="203.0.113.10"   # or DNS name
export SUBSERVER_MGMT_TLS=self_signed
# optional override:
# export SUBSERVER_IMAGE=ghcr.io/ne-tort/sing-box-subserver:controlplane
sudo -E bash deploy/install-edge-image.sh
```

Image must include `with_controlplane` — see [docs/controlplane/09-build-and-ci.md](../docs/controlplane/09-build-and-ci.md).
