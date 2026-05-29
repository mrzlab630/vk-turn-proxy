#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
cd "$ROOT_DIR"

DRY_ROOT=$(mktemp -d)
MISSING_ROOT=$(mktemp -d)
cleanup() {
  rm -rf "$DRY_ROOT" "$MISSING_ROOT"
}
trap cleanup EXIT

go run ./cmd/vkturn server install \
  --dry-run \
  --write \
  --root "$DRY_ROOT" \
  --xray-config dev/fixtures/xray/vless-tcp.json \
  --no-port-probe >/tmp/vkturn-lifecycle-install.out

STATUS_OUTPUT=$(go run ./cmd/vkturn server status --dry-run --root "$DRY_ROOT")
printf '%s\n' "$STATUS_OUTPUT"
grep -Fq 'vkturn server status' <<<"$STATUS_OUTPUT"
grep -Fq 'State: stopped' <<<"$STATUS_OUTPUT"

START_OUTPUT=$(go run ./cmd/vkturn server start --dry-run --root "$DRY_ROOT")
printf '%s\n' "$START_OUTPUT"
grep -Fq 'vkturn server start' <<<"$START_OUTPUT"
grep -Fq 'State: running' <<<"$START_OUTPUT"
grep -Fq 'Changed: true' <<<"$START_OUTPUT"

STATUS_JSON=$(go run ./cmd/vkturn server status --dry-run --root "$DRY_ROOT" --json)
grep -Fq '"state": "running"' <<<"$STATUS_JSON"

RESTART_OUTPUT=$(go run ./cmd/vkturn server restart --dry-run --root "$DRY_ROOT")
printf '%s\n' "$RESTART_OUTPUT"
grep -Fq 'vkturn server restart' <<<"$RESTART_OUTPUT"
grep -Fq 'State: running' <<<"$RESTART_OUTPUT"

STOP_OUTPUT=$(go run ./cmd/vkturn server stop --dry-run --root "$DRY_ROOT")
printf '%s\n' "$STOP_OUTPUT"
grep -Fq 'vkturn server stop' <<<"$STOP_OUTPUT"
grep -Fq 'State: stopped' <<<"$STOP_OUTPUT"

printf '%s\n%s\n%s\n' 'line-one' 'line-two' 'line-three' > "$DRY_ROOT/var/log/vk-turn-proxy/server.log"
LOGS_OUTPUT=$(go run ./cmd/vkturn server logs --dry-run --root "$DRY_ROOT" --lines 2)
printf '%s\n' "$LOGS_OUTPUT"
grep -Fq 'vkturn server logs' <<<"$LOGS_OUTPUT"
grep -Fq -- '- line-two' <<<"$LOGS_OUTPUT"
grep -Fq -- '- line-three' <<<"$LOGS_OUTPUT"

grep -Fq 'action=start' "$DRY_ROOT/run/vkturn/lifecycle.log"
grep -Fq 'action=restart' "$DRY_ROOT/run/vkturn/lifecycle.log"
grep -Fq 'action=stop' "$DRY_ROOT/run/vkturn/lifecycle.log"

if go run ./cmd/vkturn server status --dry-run --root "$MISSING_ROOT" >/tmp/vkturn-lifecycle-missing.out 2>/tmp/vkturn-lifecycle-missing.err; then
  echo 'missing unit status unexpectedly succeeded' >&2
  exit 1
fi
grep -Fq 'sidecar unit is not installed' /tmp/vkturn-lifecycle-missing.err

printf '%s\n' 'LIFECYCLE_DRY_RUN_OK'
