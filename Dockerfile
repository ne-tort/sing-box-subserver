# syntax=docker/dockerfile:1
#
# Build context MUST be the parent directory that contains both
# `sing-box-subserver/` and `sing-vmess/` (typically `vendor/`):
#
#   cd vendor
#   DOCKER_BUILDKIT=1 docker build -f sing-box-subserver/Dockerfile \
#     -t ghcr.io/ne-tort/sing-box-subserver:controlplane \
#     --build-arg TAGS_FILE=build/tags.server.controlplane .
#
# Layout inside the build stage:
#   /src/sing-vmess              ← go.mod replace ../sing-vmess
#   /src/sing-box-subserver/...  ← module root
#
# Caching: BuildKit cache mounts for module + build cache; dependency
# download is layered before application source COPY.
#
# Runtime is debian (glibc): with_naive_outbound + with_purego needs
# libcronet.so beside the binary. Alpine/musl has no shared cronet build.

FROM golang:1.26-bookworm AS build
WORKDIR /src

ARG TARGETARCH=amd64

# Local replace targets + module manifests (stable unless deps change).
COPY sing-vmess/ ./sing-vmess/
COPY sing-box-subserver/go.mod sing-box-subserver/go.sum ./sing-box-subserver/
COPY sing-box-subserver/third_party/sing-box-lx ./sing-box-subserver/third_party/sing-box-lx

WORKDIR /src/sing-box-subserver
RUN test -f third_party/sing-box-lx/go.mod

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download

# Application source (invalidates compile layer only).
COPY sing-box-subserver/ ./

# TAGS_FILE is relative to this WORKDIR (default tags.server; use tags.server.controlplane for CP).
ARG TAGS_FILE=build/tags.server
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    TAGS=$(tr -d '\r\n' < "$TAGS_FILE") && \
    test -n "$TAGS" && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w -checklinkname=0" -tags "$TAGS" -o /out/subserver ./cmd/subserver

# Ship glibc libcronet.so for purego naive outbound (smoke ephemeral client-box).
RUN --mount=type=cache,target=/go/pkg/mod \
    case "${TARGETARCH}" in \
      amd64|arm64) arch="${TARGETARCH}" ;; \
      *) echo "unsupported TARGETARCH=${TARGETARCH}" >&2; exit 1 ;; \
    esac && \
    CRONET_DIR=$(go list -m -f '{{.Dir}}' "github.com/sagernet/cronet-go/lib/linux_${arch}") && \
    test -f "${CRONET_DIR}/libcronet.so" && \
    cp "${CRONET_DIR}/libcronet.so" /out/libcronet.so

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && mkdir -p /var/lib/subserver
COPY --from=build /out/subserver /usr/local/bin/subserver
COPY --from=build /out/libcronet.so /usr/local/bin/libcronet.so
COPY --from=build /src/sing-box-subserver/deploy/agent.example.yaml /etc/subserver/agent.yaml
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/subserver", "-config", "/etc/subserver/agent.yaml"]
