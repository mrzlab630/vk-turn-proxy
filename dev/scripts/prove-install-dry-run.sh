#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
cd "$ROOT_DIR"

DRY_ROOT=$(mktemp -d)
cleanup() {
  rm -rf "$DRY_ROOT"
}
trap cleanup EXIT

INSTALL_OUTPUT=$(go run ./cmd/vkturn server install \
  --dry-run \
  --write \
  --root "$DRY_ROOT" \
  --xray-config dev/fixtures/xray/vless-tcp.json \
  --no-port-probe)

printf '%s\n' "$INSTALL_OUTPUT"

grep -Fq 'mode: dry-run' <<<"$INSTALL_OUTPUT"
grep -Fq 'Artifacts written:' <<<"$INSTALL_OUTPUT"
grep -Fq '/etc/vkturn/server.json' <<<"$INSTALL_OUTPUT"
grep -Fq '/etc/systemd/system/vkturn-server.service' <<<"$INSTALL_OUTPUT"
grep -Fq 'restart or reload Xray' <<<"$INSTALL_OUTPUT"

test -f "$DRY_ROOT/etc/vkturn/server.json"
test -f "$DRY_ROOT/etc/default/vkturn-server"
test -f "$DRY_ROOT/etc/systemd/system/vkturn-server.service"
test -f "$DRY_ROOT/etc/vkturn/install-manifest.json"
test -x "$DRY_ROOT/opt/vk-turn-proxy/vk-turn-proxy-server"

grep -Fq '"connect_addr": "127.0.0.1:10001"' "$DRY_ROOT/etc/vkturn/server.json"
grep -Fq 'ExecStart=/opt/vk-turn-proxy/vk-turn-proxy-server -config /etc/vkturn/server.json' "$DRY_ROOT/etc/systemd/system/vkturn-server.service"
grep -Fq '"xray_config_path": "dev/fixtures/xray/vless-tcp.json"' "$DRY_ROOT/etc/vkturn/install-manifest.json"

if [ -e "$DRY_ROOT/etc/xray/config.json" ]; then
  echo 'dry-run unexpectedly created an Xray config' >&2
  exit 1
fi

JSON_OUTPUT=$(go run ./cmd/vkturn server install \
  --dry-run \
  --root "$DRY_ROOT" \
  --xray-config dev/fixtures/xray/vless-tcp.json \
  --no-port-probe \
  --json)

grep -Fq '"dry_run": true' <<<"$JSON_OUTPUT"
grep -Fq '"path": "/etc/systemd/system/vkturn-server.service"' <<<"$JSON_OUTPUT"

printf '%s\n' 'INSTALL_DRY_RUN_OK'
