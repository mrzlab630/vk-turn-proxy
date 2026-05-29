# Production-Like Server Simulation

This profile runs a single Linux container that looks closer to a VPS layout
than the fast transport lab. It is still development-only and does not use
`systemd`, privileged containers, host `/etc`, host firewall, or host Xray.

## Layout Under Test

Inside `prod-server`:

```text
/etc/xray/config.json
/etc/vkturn/server.json
/opt/vk-turn-proxy/vk-turn-proxy-server
/usr/local/bin/xray
/var/log/xray
/var/log/vk-turn-proxy
```

`prod-server` starts real `xray` and `vk-turn-proxy-server` processes from an
entrypoint script. This gives future `doctor` and `server plan` work a stable
production-like filesystem/process target without touching the dev machine.

## Route Under Test

```text
test-client
  -> xray-client HTTP inbound
  -> vkturn-client local VLESS sidecar
  -> prod-server: vk-turn-proxy-server UDP sidecar
  -> prod-server: xray VLESS inbound
  -> public HTTP target through Xray freedom outbound
```

The proof uses `vk-turn-proxy-client -dev-direct`, so it does not call
VK/Yandex/MAX provider flows.

## Run

```bash
dev/prod-sim/scripts/prove-prod-sim.sh
```

Expected success marker:

```text
PROD_SIM_OK
```

The script builds project binaries in a Go container with explicit DNS, copies
`xray` out of the local `ghcr.io/xtls/xray-core:latest` image, starts Compose,
and tears the stack down after the test client exits.

## Read-only Plan Smoke Test

The same production-like Xray config is also used by the `vkturn` manager CLI:

```bash
go run ./cmd/vkturn server plan --xray-config dev/prod-sim/etc/xray/config.json
```

This command is read-only. It parses the Xray VLESS TCP inbound, selects a
sidecar UDP port with a bind/release availability probe, and prints the sidecar
artifacts that a future install step would create. It does not write files or
restart Xray.
