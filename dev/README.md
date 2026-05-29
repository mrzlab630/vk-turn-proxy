# Docker Xray/VLESS Lab

This lab is the first backend-phase proof. It runs entirely in Docker and uses
repo-local files only. It does not modify host `systemd`, Docker daemon config,
firewall rules, `/etc`, or a host Xray installation.

## Route Under Test

```text
test-client
  -> xray-client HTTP inbound
  -> vkturn-client local VLESS sidecar
  -> vkturn-server UDP sidecar
  -> xray-server VLESS inbound
  -> echo-target HTTP server
```

The default proof uses `vk-turn-proxy-client -dev-direct`, which bypasses live
VK/Yandex/MAX provider calls and sends DTLS directly to `vkturn-server`. This is
for deterministic development only; production provider flows remain behind the
normal `-vk-link` and `-yandex-link` paths.

## Run

```bash
dev/scripts/prove-vless-roundtrip.sh
```

Expected success marker:

```text
DEV_LAB_OK
```

The script compiles Linux static binaries inside a `golang:1.25-alpine`
container with explicit per-container DNS, stores them in ignored `dev/bin/`,
then runs `dev/docker-compose.yml`. Compose services also set DNS explicitly so
the lab does not depend on global Docker daemon DNS changes.

## Files

- `dev/docker-compose.yml` - lab topology.
- `dev/vkturn/server.json` - server sidecar config used by the lab.
- `dev/xray/server.json` - disposable Xray VLESS server config.
- `dev/xray/client.json` - disposable Xray HTTP-to-VLESS client config.
- `dev/echo/index.html` - deterministic target response.
- `dev/scripts/prove-vless-roundtrip.sh` - one-command proof.
- `dev/scripts/prove-plan.sh` - read-only `vkturn doctor` and
  `vkturn server plan` smoke test against Linux/Xray fixtures.
- `dev/scripts/prove-install-dry-run.sh` - `vkturn server install --dry-run`
  smoke test that writes sidecar-only artifacts into a temporary root.
- `dev/scripts/prove-lifecycle-dry-run.sh` - `vkturn server
  status/start/stop/restart/logs` smoke test against a temporary dry-run root.
- `dev/scripts/prove-uninstall-dry-run.sh` - manifest-driven `vkturn server
  uninstall --dry-run` smoke test that removes only sidecar artifacts from a
  temporary root.
- `dev/scripts/prove-status-api.sh` - local-only `status.v1` smoke test for
  `health`, `status`, `events`, `logs`, redaction-ready empty logs, and
  loopback bind enforcement.
- `dev/scripts/prove-route-helpers.sh` - static smoke test for Linux, macOS,
  and Windows route helper scripts. It validates syntax/structure only and does
  not add or remove host routes.
- `dev/scripts/prove-docker-build.sh` - production image build proof. It first
  runs plain `docker build`; if Docker Desktop DNS blocks Go module downloads,
  it retries with host-resolved `proxy.golang.org` and `sum.golang.org` pins
  without changing the host Docker daemon config.
- `dev/scripts/prove-live-vk-opt-in.sh` - opt-in live VK credential proof. By
  default it prints `LIVE_VK_SKIPPED`; it runs only with
  `VKTURN_LIVE_VK_PROOF=1` and `VKTURN_LIVE_VK_LINK=...`.
- `dev/fixtures/linux-root` - fixture filesystem with an Xray systemd unit,
  Xray config, and process snapshot for deterministic discovery tests.
