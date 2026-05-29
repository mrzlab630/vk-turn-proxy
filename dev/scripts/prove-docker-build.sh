#!/bin/sh
set -eu

ROOT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)"
IMAGE_NAME="${VKTURN_DOCKER_IMAGE:-vk-turn-proxy}"

run_build() {
  docker build -t "$IMAGE_NAME" "$ROOT_DIR"
}

resolve_first_ipv4() {
  host="$1"
  getent ahostsv4 "$host" | awk '{print $1; exit}'
}

if run_build; then
  echo "DOCKER_BUILD_OK"
  exit 0
fi

echo "docker build failed; retrying with host-resolved Go proxy DNS pins" >&2

proxy_ip="$(resolve_first_ipv4 proxy.golang.org)"
sum_ip="$(resolve_first_ipv4 sum.golang.org)"

docker build \
  --add-host="proxy.golang.org:$proxy_ip" \
  --add-host="sum.golang.org:$sum_ip" \
  -t "$IMAGE_NAME" \
  "$ROOT_DIR"

echo "DOCKER_BUILD_OK"
