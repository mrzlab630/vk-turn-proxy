#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
cd "$ROOT_DIR"

BIN_DIR="$ROOT_DIR/dev/bin"
mkdir -p "$BIN_DIR"

go build -o "$BIN_DIR/vk-turn-proxy-server-status-test" ./server

TCP_BACKEND_LOG=$(mktemp)
SERVER_LOG=$(mktemp)
cleanup() {
  if [ -n "${SERVER_PID:-}" ]; then
    kill "$SERVER_PID" >/dev/null 2>&1 || true
    wait "$SERVER_PID" >/dev/null 2>&1 || true
  fi
  if [ -n "${BACKEND_PID:-}" ]; then
    kill "$BACKEND_PID" >/dev/null 2>&1 || true
    wait "$BACKEND_PID" >/dev/null 2>&1 || true
  fi
  rm -f "$TCP_BACKEND_LOG" "$SERVER_LOG"
}
trap cleanup EXIT

python3 - <<'PY' >"$TCP_BACKEND_LOG" 2>&1 &
import socket
import sys

s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
s.bind(("127.0.0.1", 10081))
s.listen(16)
print("BACKEND_READY", flush=True)
while True:
    conn, _ = s.accept()
    conn.close()
PY
BACKEND_PID=$!

for _ in $(seq 1 50); do
  if grep -Fq BACKEND_READY "$TCP_BACKEND_LOG"; then
    break
  fi
  sleep 0.1
done
grep -Fq BACKEND_READY "$TCP_BACKEND_LOG"

"$BIN_DIR/vk-turn-proxy-server-status-test" \
  -listen 127.0.0.1:0 \
  -connect 127.0.0.1:10081 \
  -vless \
  -status-api 127.0.0.1:18081 \
  -service-name vkturn-status-smoke \
  >"$SERVER_LOG" 2>&1 &
SERVER_PID=$!

for _ in $(seq 1 80); do
  if curl -fsS http://127.0.0.1:18081/health >/tmp/vkturn-status-health.json 2>/dev/null; then
    break
  fi
  sleep 0.1
done

HEALTH=$(curl -fsS http://127.0.0.1:18081/health)
STATUS=$(curl -fsS http://127.0.0.1:18081/status)
EVENTS=$(curl -fsS http://127.0.0.1:18081/events)
LOGS=$(curl -fsS http://127.0.0.1:18081/logs)
RESTART=$(curl -fsS -X POST http://127.0.0.1:18081/restart-sidecar)

printf '%s\n' "$HEALTH"
printf '%s\n' "$STATUS"
printf '%s\n' "$EVENTS"
printf '%s\n' "$LOGS"
printf '%s\n' "$RESTART"

grep -Fq '"schema_version": "status.v1"' <<<"$HEALTH"
grep -Fq '"service_name": "vkturn-status-smoke"' <<<"$STATUS"
grep -Fq '"mode": "vless"' <<<"$STATUS"
grep -Fq '"network": "tcp"' <<<"$STATUS"
grep -Fq '"events"' <<<"$EVENTS"
grep -Fq '"logs"' <<<"$LOGS"
grep -Fq '"status": "accepted"' <<<"$RESTART"

if "$BIN_DIR/vk-turn-proxy-server-status-test" \
  -listen 127.0.0.1:0 \
  -connect 127.0.0.1:10081 \
  -vless \
  -status-api 0.0.0.0:18082 \
  > /tmp/vkturn-status-nonlocal.out 2>&1; then
  echo 'status API accepted non-loopback bind' >&2
  exit 1
fi
grep -Fq 'loopback' /tmp/vkturn-status-nonlocal.out

printf '%s\n' 'STATUS_API_OK'
