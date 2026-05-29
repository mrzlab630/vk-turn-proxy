# Strategy

This is the standing strategy document for future work. Re-read it before
planning architecture, installer, provider automation, or deployment changes.

## Product Direction

Build a VK-first sidecar transport for existing Xray/VLESS deployments.

The project should not become a monolithic replacement for Xray. Xray remains
the owner of VLESS users, routing, DNS, client profiles, and normal direct
connectivity. This project adds an optional VK TURN-routed transport path next
to an already working Xray server.

Target positioning:

```text
VK TURN Sidecar for existing Xray/VLESS.
Adds an alternative VK-routed transport without replacing or modifying the main
Xray server.
```

## Target Topology

Existing direct path:

```text
Xray client -> Internet -> Xray server VLESS inbound
```

Sidecar VK TURN path:

```text
Xray client
  -> local vk-turn-proxy client 127.0.0.1:<local_tcp_port>
  -> VK TURN
  -> vk-turn-proxy server 0.0.0.0:<sidecar_udp_port>/udp
  -> existing Xray server VLESS inbound 127.0.0.1:<xray_tcp_port>
```

The user must be able to choose the route:

- direct VLESS outbound to the server's normal Xray port;
- VK TURN VLESS outbound to the local sidecar TCP listener.

Routing choices should stay in Xray/v2rayN/nekoray/sing-box where possible.

## Execution Order

Operational backend tasks are tracked in `docs/BACKEND_TODO.md`. Treat that
file as the active checklist for phase 1 and keep this strategy document focused
on direction and constraints.

Current development machine rule: use Docker and repo-local files only. Do not
change host system services, Docker daemon settings, host Xray configs, firewall
rules, or `/etc` files from this project during development. Production install
commands may be designed in code, but on the dev machine they must be exercised
through dry runs, generated artifacts, fixtures, or disposable containers.

Development is intentionally split into two stages:

1. **Backend/server-side foundation first.** Build the sidecar backend,
   Xray detection/planning, Docker integration environment, health model, and
   server control surface before investing in mobile UX. The backend must be
   testable without Android and must prove TCP roundtrip through a disposable
   Xray/VLESS stack.
2. **Android client second.** Build the Android app after the backend contract is
   stable enough to consume. The first Android version is a control and tunnel
   client for the existing backend, not a place to invent backend behavior.

This order keeps the Android work from hiding server-side uncertainty inside a
mobile UI and gives the client a stable API, status model, and test environment.

## Boundaries

### Xray Owns

- VLESS users and UUIDs.
- Routing and DNS rules.
- Direct transport profiles.
- Server-side VLESS inbound configuration.
- Client-side outbound selection.

### vk-turn-proxy Owns

- Provider TURN credential acquisition.
- TURN allocation.
- DTLS over TURN.
- KCP + smux transport for VLESS TCP forwarding.
- Sidecar tunnel lifecycle, health, and reconnect.
- Provider-specific auth/captcha/degradation state.

### Installer/Manager Owns

- Xray discovery.
- Backend connectivity checks.
- Free sidecar port selection.
- systemd/Docker development service generation.
- Config snippets and diagnostics.

The installer must not silently rewrite an existing Xray config or restart Xray.
Any destructive or invasive action requires an explicit command and confirmation.

## Server Deployment Model

The server side should install beside an existing Xray service.

Example runtime command:

```bash
vk-turn-proxy-server \
  -listen 0.0.0.0:56000 \
  -connect 127.0.0.1:443 \
  -vless
```

Port policy:

- Default sidecar listen port: `56000/udp`.
- If busy, scan `56001..56100/udp`.
- Optional future UX: `--prefer-near <xray_port>` can propose a nearby UDP port,
  but it must not be mandatory.

The installer flow should be:

```bash
vkturn doctor
vkturn server detect-xray
vkturn server plan
vkturn server install --xray-inbound 127.0.0.1:443
vkturn server status
```

`plan` must be read-only and should show:

- detected Xray service;
- detected or selected Xray inbound;
- backend dial result;
- selected sidecar UDP port;
- files/services that will be created;
- files/services that will not be touched.

## Client Deployment Model

The client side runs a local VLESS TCP sidecar listener.

Example runtime command:

```bash
vk-turn-proxy-client \
  -peer <server_ip>:56000 \
  -vk-link <vk_call_join_link> \
  -listen 127.0.0.1:9000 \
  -vless
```

The Xray client receives an additional outbound that points to
`127.0.0.1:9000`. The original direct outbound remains available.

The manager should report:

- provider state: `ready`, `captcha_required`, `auth_required`, `rate_limited`,
  `provider_down`;
- TURN allocation state;
- DTLS state;
- KCP/smux session count;
- local TCP listener state;
- remote backend reachability when measurable.

## Development Model

Development and integration testing should happen in Docker.

Reason: the project needs reproducible test infrastructure with a disposable
Xray/VLESS server, vk-turn-proxy server sidecar, vk-turn-proxy client sidecar,
and test traffic generator. Docker lets us prove behavior without touching a
real production Xray install.

Target development stack:

```text
docker compose
  xray-server
    listens on a test VLESS inbound
  vkturn-server
    connects to xray-server:<vless_port>
  vkturn-client
    listens on local test TCP port and connects to vkturn-server
  test-client
    opens TCP connections through vkturn-client and verifies roundtrip
  optional fake-provider
    supplies deterministic TURN credentials for offline tests
```

Add a second production-like Docker profile for installer/discovery work:

```text
prod-server container
  /etc/xray/config.json
  /etc/vkturn/server.json
  /opt/vk-turn-proxy/vk-turn-proxy-server
  /var/log/xray
  /var/log/vk-turn-proxy
  real xray process and vkturn sidecar process
```

This profile is for `doctor`, `server plan`, filesystem layout, process, and log
contract work. It must not use host `systemd`, host `/etc`, host firewall, or
privileged Docker by default. A systemd-in-Docker profile can be added later as
an explicit opt-in test for install/lifecycle commands.

Testing layers:

1. Unit tests for parsers, provider state, and config planning.
2. Loopback integration tests with a fake TCP backend.
3. Docker integration tests with Xray VLESS server and vk-turn-proxy sidecars.
4. Optional live-provider tests for VK, gated by explicit environment variables
   and never run by default.

The first Docker milestone should not depend on live VK. Use deterministic fake
or loopback components first, then add opt-in live VK proof.

## Provider Roadmap

### M1: VK Existing Link

- Existing VK call join link is provided by the user.
- Client obtains TURN credentials using the current VK flow.
- Sidecar keeps VLESS tunnel alive through VK TURN.
- Manual captcha remains explicit user interaction.

### M2: VK Managed Identity

- Persist authorized VK identity/session with explicit user consent.
- Add account health and re-auth state.
- Avoid storing raw passwords.
- Treat captcha as `auth_required` or `captcha_required`, not as a hidden loop.

### M3: VK Managed Calls

- Create or refresh calls automatically.
- Rotate stale/broken call sessions.
- Keep call/session lifecycle isolated inside the VK provider adapter.

### Research: MAX Provider Candidate

- Use `~/projects/maxBridge` as a sidecar auth/control-plane candidate.
- Integrate through IPC first, not by merging Python code into the Go tunnel.
- Promote MAX to implementation only after call/RTC/TURN credentials are proven.

### Legacy: Yandex

- Keep as best-effort while code still works.
- Do not make it a production dependency without new proof of stability.

## Milestones

### M1: VK TURN Sidecar for Existing Xray/VLESS

Definition of done:

- `doctor` can detect likely Xray installations and report candidates.
- `plan` can select a sidecar UDP port and backend TCP address without changes.
- `install` can create a sidecar service without modifying Xray.
- Backend exposes a server control/status contract for health and events.
- Docker integration proves TCP roundtrip through Xray + vk-turn sidecars.

### M2: Control Plane

- Local CLI/API for `status`, `start`, `stop`, `restart`, `logs`, and `doctor`.
- Structured health states.
- Config file for provider, ports, backend, and session count.
- Redacted logs for secrets and TURN credentials.

### M3: Android Client

- Android app connects to the backend control/status contract.
- Android app can authorize/use VK provider state locally where possible.
- Android app can start/stop the local VK TURN VLESS sidecar.
- Android app displays backend events, provider errors, and actionable recovery
  states.
- First Android release may export a profile for an existing Android Xray client;
  integrated VPNService can follow after the backend and sidecar path are stable.

### M4: Provider Automation

- VK account/session management.
- Managed VK call creation/refresh.
- Explicit re-auth/captcha workflows.
- Provider state persisted in local storage.

## Premortem

Most likely failure: the installer touches or restarts a working Xray setup and
users lose their primary direct route. Countermeasure: read-only `doctor/plan`
first, no implicit Xray config writes, no implicit restarts.

Most damaging failure: provider secrets or session tokens leak through logs,
config files, or Docker artifacts. Countermeasure: redaction, encrypted storage,
`0600` permissions, and no live credentials in committed compose files.

Riskiest assumption: VK provider flows remain stable enough for unattended use.
Countermeasure: keep provider adapters isolated, document flows in
`docs/API_FLOWS.md`, and add parser tests for every observed shape change.

Cheapest proof: Docker compose roundtrip with fake provider and test Xray before
any production installer work.

## Non-Goals For The Near Term

- Replacing Xray.
- Owning Xray user management.
- Auto-patching production Xray configs by default.
- Building a GUI before CLI/daemon/health are stable.
- Treating Yandex or MAX as production providers before fresh proof.
