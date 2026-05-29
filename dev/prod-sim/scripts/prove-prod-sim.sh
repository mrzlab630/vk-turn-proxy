#!/bin/sh
set -eu

ROOT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")/../../.." && pwd)"
COMPOSE_FILE="$ROOT_DIR/dev/prod-sim/docker-compose.yml"
PROJECT_NAME="vkturn-prod-sim"

cleanup() {
  docker compose -p "$PROJECT_NAME" -f "$COMPOSE_FILE" down --remove-orphans >/dev/null 2>&1 || true
}

copy_xray() {
  mkdir -p "$ROOT_DIR/dev/prod-sim/bin"
  container_id="$(docker create --entrypoint /usr/local/bin/xray ghcr.io/xtls/xray-core:latest version)"
  trap 'docker rm -f "$container_id" >/dev/null 2>&1 || true; cleanup' EXIT INT TERM
  docker cp "$container_id:/usr/local/bin/xray" "$ROOT_DIR/dev/prod-sim/bin/xray"
  docker rm -f "$container_id" >/dev/null
  chmod +x "$ROOT_DIR/dev/prod-sim/bin/xray"
}

cleanup

mkdir -p "$ROOT_DIR/dev/bin" "$ROOT_DIR/dev/prod-sim/var/log/vk-turn-proxy"
docker run --rm \
  --dns 1.1.1.1 \
  --dns 8.8.8.8 \
  -v "$ROOT_DIR:/src" \
  -v vkturn-go-mod:/go/pkg/mod \
  -v vkturn-go-cache:/root/.cache/go-build \
  -w /src \
  golang:1.25-alpine \
  sh -c 'CGO_ENABLED=0 go build -ldflags="-s -w" -o dev/bin/vk-turn-proxy-server ./server && CGO_ENABLED=0 go build -ldflags="-s -w" -o dev/bin/vk-turn-proxy-client ./client'

copy_xray

docker compose -p "$PROJECT_NAME" -f "$COMPOSE_FILE" up --abort-on-container-exit --exit-code-from test-client test-client

cleanup
