#!/usr/bin/env bash
set -euo pipefail

REMOTE_DIR="${SUBSERVER_REMOTE_DIR:-/opt/subserver}"
REPO_URL="${SUBSERVER_REPO_URL:-https://github.com/ne-tort/sing-box-subserver.git}"
REPO_REF="${SUBSERVER_REPO_REF:-main}"
IMAGE="${SUBSERVER_IMAGE:-}"
MGMT_LISTEN="${SUBSERVER_LISTEN:-0.0.0.0:8080}"
NODE_ID="${SUBSERVER_NODE_ID:-}"
TOKEN="${SUBSERVER_TOKEN:-}"
CONTROLLER_URL="${SUBSERVER_CONTROLLER_URL:-}"
CONTROLLER_URL="${CONTROLLER_URL%/}"
RESET_MODE="${SUBSERVER_RESET_MODE:-fresh}" # fresh|inplace
PRESERVE_DATA="${SUBSERVER_PRESERVE_DATA:-0}" # 1 keeps data_dir contents

log() { echo "[subserver-install] $*"; }

ensure_docker() {
  if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
    return 0
  fi
  if [[ -f /etc/os-release ]]; then . /etc/os-release; fi
  case "${ID:-}" in
    ubuntu|debian)
      export DEBIAN_FRONTEND=noninteractive
      apt-get update -y
      apt-get install -y ca-certificates curl gnupg git
      install -m 0755 -d /etc/apt/keyrings
      if [[ ! -f /etc/apt/keyrings/docker.asc ]]; then
        curl -fsSL "https://download.docker.com/linux/${ID}/gpg" -o /etc/apt/keyrings/docker.asc
        chmod a+r /etc/apt/keyrings/docker.asc
      fi
      echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/${ID} ${VERSION_CODENAME} stable" > /etc/apt/sources.list.d/docker.list
      apt-get update -y
      apt-get install -y docker-ce docker-ce-cli containerd.io docker-compose-plugin
      ;;
    *)
      log "ERROR: install Docker manually for distro ${ID:-unknown}"
      exit 2
      ;;
  esac
  systemctl enable --now docker || true
}

cleanup_old_install() {
  if [[ "$RESET_MODE" != "fresh" ]]; then
    log "RESET_MODE=inplace: keep existing install artifacts"
    return 0
  fi
  log "RESET_MODE=fresh: cleaning old subserver artifacts"

  for name in "subserver" "sui-subserver" "subserver-smoke" "subserver-cp"; do
    if docker ps -a --format '{{.Names}}' | grep -Fxq "$name"; then
      docker rm -f "$name" >/dev/null 2>&1 || true
    fi
  done

  docker ps -a --format '{{.ID}} {{.Image}} {{.Names}}' \
    | while read -r cid image cname; do
      if [[ "$image" == *"sing-box-subserver"* ]]; then
        docker rm -f "$cid" >/dev/null 2>&1 || true
      fi
    done

  if [[ -f "$REMOTE_DIR/docker-compose.yml" ]]; then
    (cd "$REMOTE_DIR" && docker compose down -v --remove-orphans) || true
  fi

  rm -rf "$REMOTE_DIR/src" || true
  if [[ "$PRESERVE_DATA" != "1" ]]; then
    rm -rf "$REMOTE_DIR/data" || true
    rm -rf "/var/lib/subserver" || true
  fi
  rm -f "$REMOTE_DIR/agent.yaml" "$REMOTE_DIR/docker-compose.yml" || true
}

write_install_files() {
  mkdir -p "$REMOTE_DIR/data"
  if [[ -z "$NODE_ID" || -z "$TOKEN" ]]; then
    log "ERROR: set SUBSERVER_NODE_ID and SUBSERVER_TOKEN"
    exit 1
  fi

  PULL_URL="${SUBSERVER_PULL_URL:-}"
  HB_URL="${SUBSERVER_HEARTBEAT_URL:-}"
  PULL_INTERVAL="${SUBSERVER_PULL_INTERVAL_SEC:-60}"
  HB_INTERVAL="${SUBSERVER_HEARTBEAT_INTERVAL_SEC:-30}"
  # Optional: if controller base URL set, append default pull/hb paths when URLs empty.
  if [[ -n "$CONTROLLER_URL" ]]; then
    if [[ -z "$PULL_URL" ]]; then
      PULL_URL="$CONTROLLER_URL/api/edge/agent/$NODE_ID/desired-config"
    fi
    if [[ -z "$HB_URL" ]]; then
      HB_URL="$CONTROLLER_URL/api/edge/agent/$NODE_ID/hello"
    fi
  fi

  MGMT_TLS_MODE="${SUBSERVER_MGMT_TLS:-off}" # off|self_signed
  INSECURE_BIND=true
  TLS_YAML=""
  if [[ "$MGMT_TLS_MODE" == "self_signed" ]]; then
    command -v openssl >/dev/null 2>&1 || { apt-get update -y && apt-get install -y openssl || true; }
    mkdir -p "$REMOTE_DIR/tls"
    HOST_CN="${SUBSERVER_MGMT_TLS_CN:-}"
    if [[ -z "$HOST_CN" ]]; then
      HOST_CN="$(hostname -f 2>/dev/null || hostname || echo localhost)"
    fi
    openssl req -x509 -newkey rsa:2048 -nodes \
      -keyout "$REMOTE_DIR/tls/mgmt.key" \
      -out "$REMOTE_DIR/tls/mgmt.crt" \
      -days 825 \
      -subj "/CN=${HOST_CN}" \
      -addext "subjectAltName=DNS:${HOST_CN},DNS:localhost,IP:127.0.0.1" \
      >/dev/null 2>&1 || openssl req -x509 -newkey rsa:2048 -nodes \
      -keyout "$REMOTE_DIR/tls/mgmt.key" \
      -out "$REMOTE_DIR/tls/mgmt.crt" \
      -days 825 \
      -subj "/CN=${HOST_CN}"
    chmod 600 "$REMOTE_DIR/tls/mgmt.key" "$REMOTE_DIR/tls/mgmt.crt"
    INSECURE_BIND=false
    TLS_YAML=$'tls:\n  cert: "/etc/subserver/tls/mgmt.crt"\n  key: "/etc/subserver/tls/mgmt.key"\n'
    log "MGMT TLS self_signed enabled (CN=${HOST_CN}); panel AgentURL must use https:// (tls_insecure ok for labs)"
  fi

  {
    echo "node_id: \"$NODE_ID\""
    echo "token: \"$TOKEN\""
    echo "listen: \"$MGMT_LISTEN\""
    echo "data_dir: \"/var/lib/subserver\""
    echo "insecure_public_bind: $INSECURE_BIND"
    if [[ -n "$TLS_YAML" ]]; then
      printf '%s' "$TLS_YAML"
    fi
    if [[ -n "$PULL_URL" ]]; then
      cat <<EOF
pull:
  url: "$PULL_URL"
  interval_sec: $PULL_INTERVAL
  headers:
    Authorization: "Bearer $TOKEN"
EOF
    fi
    if [[ -n "$HB_URL" ]]; then
      cat <<EOF
heartbeat:
  url: "$HB_URL"
  interval_sec: $HB_INTERVAL
EOF
    fi
    echo "log:"
    echo "  level: info"
  } >"$REMOTE_DIR/agent.yaml"
  chmod 0600 "$REMOTE_DIR/agent.yaml"

  TLS_VOL=""
  if [[ "$MGMT_TLS_MODE" == "self_signed" ]]; then
    TLS_VOL=$'      - '"$REMOTE_DIR"'/tls:/etc/subserver/tls:ro\n'
  fi

  if [[ -n "$IMAGE" ]]; then
    cat >"$REMOTE_DIR/docker-compose.yml" <<EOF
services:
  subserver:
    image: $IMAGE
    container_name: subserver
    network_mode: host
    restart: unless-stopped
    volumes:
      - $REMOTE_DIR/agent.yaml:/etc/subserver/agent.yaml:ro
${TLS_VOL}      - $REMOTE_DIR/data:/var/lib/subserver
EOF
  else
    cat >"$REMOTE_DIR/docker-compose.yml" <<EOF
services:
  subserver:
    build:
      context: $REMOTE_DIR/src
      dockerfile: Dockerfile
    container_name: subserver
    network_mode: host
    restart: unless-stopped
    volumes:
      - $REMOTE_DIR/agent.yaml:/etc/subserver/agent.yaml:ro
${TLS_VOL}      - $REMOTE_DIR/data:/var/lib/subserver
EOF
  fi
}

command -v git >/dev/null 2>&1 || { apt-get update -y && apt-get install -y git || true; }
ensure_docker
cleanup_old_install
write_install_files

if [[ -z "$IMAGE" ]]; then
  if [[ ! -d "$REMOTE_DIR/src/.git" ]]; then
    rm -rf "$REMOTE_DIR/src"
    git clone --depth 1 --branch "$REPO_REF" "$REPO_URL" "$REMOTE_DIR/src" || git clone --depth 1 "$REPO_URL" "$REMOTE_DIR/src"
  else
    git -C "$REMOTE_DIR/src" fetch --depth 1 origin "$REPO_REF" || true
    git -C "$REMOTE_DIR/src" checkout "$REPO_REF" || true
    git -C "$REMOTE_DIR/src" pull --ff-only || true
  fi
  if [[ -f "$REMOTE_DIR/src/.gitmodules" ]]; then
    git -C "$REMOTE_DIR/src" submodule update --init --depth 1 || true
  fi
fi

cd "$REMOTE_DIR"
if [[ -n "$IMAGE" ]]; then docker compose pull || true; fi
docker compose up -d --build --remove-orphans

PORT="${MGMT_LISTEN##*:}"
PORT="${PORT%%]*}"
for _ in $(seq 1 60); do
  # CP builds terminate management over HTTPS; legacy plain HTTP still accepted as fallback.
  if curl -fkSs "https://127.0.0.1:${PORT}/v1/health" >/dev/null 2>&1; then
    log "health ok (https)"
    exit 0
  fi
  if curl -fsS "http://127.0.0.1:${PORT}/v1/health" >/dev/null 2>&1; then
    log "health ok (http)"
    exit 0
  fi
  sleep 2
done
log "ERROR: health check failed on 127.0.0.1:${PORT}"
docker compose logs --tail=80 || true
exit 3
