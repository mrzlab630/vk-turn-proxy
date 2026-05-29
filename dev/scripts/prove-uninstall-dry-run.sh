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
  --no-port-probe >/tmp/vkturn-uninstall-install.out

mkdir -p "$DRY_ROOT/etc/xray"
printf '%s\n' 'xray must stay' > "$DRY_ROOT/etc/xray/config.json"

PLAN_OUTPUT=$(go run ./cmd/vkturn server uninstall \
  --dry-run \
  --root "$DRY_ROOT")

printf '%s\n' "$PLAN_OUTPUT"
grep -Fq 'vkturn server uninstall' <<<"$PLAN_OUTPUT"
grep -Fq 'Removals planned:' <<<"$PLAN_OUTPUT"
grep -Fq '/etc/vkturn/server.json' <<<"$PLAN_OUTPUT"
grep -Fq '/etc/systemd/system/vkturn-server.service' <<<"$PLAN_OUTPUT"
grep -Fq 'Will not touch:' <<<"$PLAN_OUTPUT"

test -f "$DRY_ROOT/etc/vkturn/server.json"

JSON_OUTPUT=$(go run ./cmd/vkturn server uninstall \
  --dry-run \
  --root "$DRY_ROOT" \
  --json)
grep -Fq '"dry_run": true' <<<"$JSON_OUTPUT"
grep -Fq '"path": "/etc/vkturn/server.json"' <<<"$JSON_OUTPUT"

APPLY_OUTPUT=$(go run ./cmd/vkturn server uninstall \
  --dry-run \
  --write \
  --root "$DRY_ROOT")

printf '%s\n' "$APPLY_OUTPUT"
grep -Fq 'Applied: true' <<<"$APPLY_OUTPUT"
grep -Fq 'Removals applied:' <<<"$APPLY_OUTPUT"

if [ -e "$DRY_ROOT/etc/vkturn/server.json" ]; then
  echo 'server config was not removed' >&2
  exit 1
fi
if [ -e "$DRY_ROOT/etc/systemd/system/vkturn-server.service" ]; then
  echo 'sidecar unit was not removed' >&2
  exit 1
fi
grep -Fq 'xray must stay' "$DRY_ROOT/etc/xray/config.json"

if go run ./cmd/vkturn server uninstall --dry-run --root "$MISSING_ROOT" >/tmp/vkturn-uninstall-missing.out 2>/tmp/vkturn-uninstall-missing.err; then
  echo 'missing manifest uninstall unexpectedly succeeded' >&2
  exit 1
fi
grep -Fq 'manifest is missing' /tmp/vkturn-uninstall-missing.err

printf '%s\n' 'UNINSTALL_DRY_RUN_OK'
