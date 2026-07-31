# Build context MUST be the parent directory that contains both
# `sing-box-subserver/` and `sing-vmess/` (typically `vendor/`):
#
#   cd vendor
#   docker build -f sing-box-subserver/Dockerfile \
#     -t ghcr.io/ne-tort/sing-box-subserver:controlplane \
#     --build-arg TAGS_FILE=build/tags.server.controlplane .
#
# Layout inside the image build:
#   /src/sing-vmess              ← go.mod replace ../sing-vmess
#   /src/sing-box-subserver/...  ← module root

FROM golang:1.26-alpine AS build
WORKDIR /src
COPY sing-vmess ./sing-vmess
COPY sing-box-subserver/go.mod sing-box-subserver/go.sum ./sing-box-subserver/
COPY sing-box-subserver/third_party/sing-box-lx ./sing-box-subserver/third_party/sing-box-lx
WORKDIR /src/sing-box-subserver
RUN test -f third_party/sing-box-lx/go.mod
COPY sing-box-subserver/ .
# TAGS_FILE is relative to this WORKDIR (default tags.server; use tags.server.controlplane for CP).
ARG TAGS_FILE=build/tags.server
RUN TAGS=$(tr -d '\r\n' < "$TAGS_FILE") && \
    test -n "$TAGS" && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -checklinkname=0" -tags "$TAGS" -o /out/subserver ./cmd/subserver

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=build /out/subserver /usr/local/bin/subserver
COPY --from=build /src/sing-box-subserver/deploy/agent.example.yaml /etc/subserver/agent.yaml
RUN mkdir -p /var/lib/subserver
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/subserver", "-config", "/etc/subserver/agent.yaml"]
