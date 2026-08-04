#!/usr/bin/env bash
# Copy glibc libcronet.so next to host-built linux agent binaries (naive smoke / purego).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
ARCH="${1:-amd64}"
OUT_DIR="${2:-dist}"
mkdir -p "$OUT_DIR"
MOD="github.com/sagernet/cronet-go/lib/linux_${ARCH}"
DIR="$(go list -m -f '{{.Dir}}' "$MOD")"
test -n "$DIR"
test -f "${DIR}/libcronet.so"
cp -f "${DIR}/libcronet.so" "${OUT_DIR}/libcronet.so"
echo "copied ${OUT_DIR}/libcronet.so"
