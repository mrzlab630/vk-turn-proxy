#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
cd "$ROOT_DIR"

DOCTOR_OUTPUT=$(go run ./cmd/vkturn doctor \
  --root dev/fixtures/linux-root \
  --skip-host-commands \
  --no-port-probe)

printf '%s\n' "$DOCTOR_OUTPUT"

grep -Fq 'vkturn doctor' <<<"$DOCTOR_OUTPUT"
grep -Fq 'mode: read-only' <<<"$DOCTOR_OUTPUT"
grep -Fq 'xray.service status=ok confidence=high' <<<"$DOCTOR_OUTPUT"
grep -Fq 'Selected Xray config: dev/fixtures/linux-root/etc/xray/config.json' <<<"$DOCTOR_OUTPUT"
grep -Fq 'VLESS TCP inbound: tag=vless-in' <<<"$DOCTOR_OUTPUT"
grep -Fq 'Sidecar port checks:' <<<"$DOCTOR_OUTPUT"

PLAN_OUTPUT=$(go run ./cmd/vkturn server plan \
  --xray-config dev/fixtures/xray/vless-tcp.json \
  --no-port-probe)

printf '%s\n' "$PLAN_OUTPUT"

grep -Fq 'mode: read-only' <<<"$PLAN_OUTPUT"
grep -Fq 'Backend target for sidecar: 127.0.0.1:10001' <<<"$PLAN_OUTPUT"
grep -Fq 'Selected sidecar UDP listen: 0.0.0.0:56000' <<<"$PLAN_OUTPUT"
grep -Fq 'Will create:' <<<"$PLAN_OUTPUT"
grep -Fq 'Will not touch:' <<<"$PLAN_OUTPUT"

JSON_OUTPUT=$(go run ./cmd/vkturn server plan \
  --xray-config dev/fixtures/xray/vless-tcp.json \
  --no-port-probe \
  --json)

grep -Fq '"read_only": true' <<<"$JSON_OUTPUT"
grep -Fq '"backend_address": "127.0.0.1:10001"' <<<"$JSON_OUTPUT"

DOCTOR_JSON_OUTPUT=$(go run ./cmd/vkturn doctor \
  --root dev/fixtures/linux-root \
  --skip-host-commands \
  --no-port-probe \
  --json)

grep -Fq '"read_only": true' <<<"$DOCTOR_JSON_OUTPUT"
grep -Fq '"selected_config": "dev/fixtures/linux-root/etc/xray/config.json"' <<<"$DOCTOR_JSON_OUTPUT"

printf '%s\n' 'PLAN_OK'
printf '%s\n' 'DOCTOR_OK'
