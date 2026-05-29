#!/bin/sh
set -eu

mkdir -p /var/log/xray /var/log/vk-turn-proxy /run/vkturn

/usr/local/bin/xray run -config /etc/xray/config.json \
  > /var/log/xray/stdout.log \
  2> /var/log/xray/stderr.log &
xray_pid="$!"

cleanup() {
  kill "$vkturn_pid" "$xray_pid" >/dev/null 2>&1 || true
  wait "$vkturn_pid" >/dev/null 2>&1 || true
  wait "$xray_pid" >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM

for i in $(seq 1 50); do
  /opt/vk-turn-proxy/vk-turn-proxy-server \
    -config /etc/vkturn/server.json \
    -listen 127.0.0.1:55999 \
    -check-backend=true \
    >/tmp/vkturn-probe.log \
    2>&1 &
  probe_pid="$!"
  sleep 0.2
  if kill -0 "$probe_pid" >/dev/null 2>&1; then
    kill "$probe_pid" >/dev/null 2>&1 || true
    wait "$probe_pid" >/dev/null 2>&1 || true
    break
  fi
  wait "$probe_pid" >/dev/null 2>&1 || true
  sleep 0.1
  if [ "$i" = "50" ]; then
    echo "xray backend did not become reachable" >&2
    cat /tmp/vkturn-probe.log >&2 || true
    cat /var/log/xray/stderr.log >&2 || true
    exit 1
  fi
done

/opt/vk-turn-proxy/vk-turn-proxy-server -config /etc/vkturn/server.json \
  > /var/log/vk-turn-proxy/server.log \
  2> /var/log/vk-turn-proxy/server.err &
vkturn_pid="$!"
sleep 0.2
if ! kill -0 "$vkturn_pid" >/dev/null 2>&1; then
  wait "$vkturn_pid" >/dev/null 2>&1 || true
  echo "vkturn sidecar failed to start" >&2
  cat /var/log/vk-turn-proxy/server.err >&2 || true
  exit 1
fi

echo "$xray_pid" > /run/vkturn/xray.pid
echo "$vkturn_pid" > /run/vkturn/vkturn.pid
echo "PROD_SIM_READY"

wait -n "$xray_pid" "$vkturn_pid"
