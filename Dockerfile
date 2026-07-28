FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
COPY third_party/sing-box-lx ./third_party/sing-box-lx
RUN test -f third_party/sing-box-lx/go.mod
COPY . .
# TAGS_FILE selects allowlist (default tags.server; use tags.server.controlplane for CP image).
ARG TAGS_FILE=build/tags.server
RUN TAGS=$(tr -d '\r\n' < "$TAGS_FILE") && \
    test -n "$TAGS" && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -checklinkname=0" -tags "$TAGS" -o /out/subserver ./cmd/subserver

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=build /out/subserver /usr/local/bin/subserver
COPY deploy/agent.example.yaml /etc/subserver/agent.yaml
RUN mkdir -p /var/lib/subserver
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/subserver", "-config", "/etc/subserver/agent.yaml"]
