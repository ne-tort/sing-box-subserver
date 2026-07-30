#!/usr/bin/env bash
# Panel / VPN-client SSH bootstrap: pull a prebuilt image (no git clone / docker build).
# Wraps deploy/install-edge.sh with a CP-capable default image.
#
# Required:
#   SUBSERVER_NODE_ID, SUBSERVER_TOKEN
# Optional:
#   SUBSERVER_IMAGE         (default: ghcr.io/ne-tort/sing-box-subserver:controlplane)
#   SUBSERVER_PUBLIC_HOST / SUBSERVER_PUBLIC_PORT  (agent controlplane.public_*)
#   SUBSERVER_MGMT_TLS=self_signed|off
#   plus other SUBSERVER_* vars from install-edge.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

export SUBSERVER_IMAGE="${SUBSERVER_IMAGE:-ghcr.io/ne-tort/sing-box-subserver:controlplane}"

if [[ -z "${SUBSERVER_NODE_ID:-}" || -z "${SUBSERVER_TOKEN:-}" ]]; then
  echo "[subserver-install-image] ERROR: set SUBSERVER_NODE_ID and SUBSERVER_TOKEN" >&2
  exit 1
fi

if [[ -z "${SUBSERVER_PUBLIC_HOST:-}" ]]; then
  echo "[subserver-install-image] WARN: SUBSERVER_PUBLIC_HOST unset; subscription_url may be unusable on phones" >&2
fi

exec bash "$SCRIPT_DIR/install-edge.sh"
