# Backend Phase TODO

This is the working checklist for the first development phase. Keep it focused
on the server-side foundation: sidecar runtime, Xray/VLESS discovery, Docker
proof, health/control contracts, and VK provider readiness. Android work starts
only after this contract is stable.

## Scope

In scope:

- run `vk-turn-proxy` beside an existing Xray/VLESS server as an optional
  sidecar path;
- detect Xray and produce a read-only install plan;
- prove TCP forwarding through a disposable Docker Xray/VLESS stack;
- keep all development work repo-local or inside Docker on the current dev
  machine;
- expose backend status, health, events, and logs in a shape an Android client
  can consume later;
- keep VK as the first provider and treat Yandex/MAX as non-blocking research.

Out of scope for this phase:

- replacing Xray or owning Xray user management;
- silently modifying or restarting production Xray;
- changing host system services, Docker daemon settings, host Xray configs,
  firewall rules, or `/etc` files from the dev workflow;
- Android VPNService, mobile UI, and mobile VK login UX;
- unattended VK managed-call creation;
- production support for Yandex or MAX without fresh proof.

## Dev Machine Rules

- Treat this machine as development-only infrastructure.
- Use Docker, generated fixtures, and repo-local files for all proofs.
- Do not run apply-mode installer commands against the host.
- Do not edit host `systemd`, Docker daemon, firewall, Xray, or `/etc` config.
- Systemd/install features must be tested with dry-run output, golden files,
  temporary directories, or disposable containers until a separate production
  target is explicitly chosen.
- Docker DNS or networking workarounds must live in repo-local Compose/build
  configuration or command documentation, not in global host settings.

## Phase Gate

Backend phase is done when all of these pass:

- `gofmt -w client server tcputil` leaves no formatting diff;
- `go test ./...`, `go vet ./...`, and `go test -race ./...` pass;
- Docker integration proves TCP roundtrip through Xray/VLESS and both sidecars;
- `doctor` and `plan` run read-only and report what will and will not be
  touched;
- sidecar service install/start/stop/status is proven through dry-run artifacts,
  fixtures, or containers without changing the dev host or Xray;
- backend health/status/events contract is documented and redacts secrets.

## First Step: Docker Xray/VLESS Lab

- [x] Build the minimal Docker lab before refactoring production code.
  - Depends on: current Go tests passing and Docker daemon access.
  - Work: create repo-local Compose/dev files for `xray-server`,
    `vkturn-server`, `vkturn-client`, `xray-client`, `echo-target`, and
    `test-client`.
  - Acceptance: one command starts the lab and proves this route:
    `test-client -> xray-client -> vkturn-client -> vkturn-server -> xray-server -> echo-target`.
  - Proof: `dev/scripts/prove-vless-roundtrip.sh` returned `DEV_LAB_OK`.

- [x] Keep the first lab deterministic and offline from live VK.
  - Depends on: Docker lab skeleton.
  - Work: use a fake/local provider path or loopback transport mode for the
    default proof; reserve live VK for an explicit opt-in command with env vars.
  - Acceptance: default Docker proof never calls VK/Yandex/MAX and fails only on
    local transport/config errors.
  - Proof: lab uses `vk-turn-proxy-client -dev-direct` and no provider links.

- [x] Solve Docker DNS only through repo-local dev configuration.
  - Depends on: current Docker Desktop default network DNS issue.
  - Work: document or encode DNS/build networking workaround in Compose/dev
    commands without changing host Docker daemon settings.
  - Acceptance: lab build/run works on this dev machine without host config
    edits.
  - Proof: build step and Compose services set explicit DNS in repo-local dev
    configuration.

## Milestone 0: Repo Boundaries And Test Harness

- [x] Split runtime responsibilities without changing behavior.
  - Depends on: current tests passing.
  - Work: introduce internal package boundaries for provider flow, TURN relay,
    VLESS forwarding, config planning, and diagnostics as code is touched.
  - Acceptance: existing CLI flags still work; `go test ./...` passes.
  - Proof: server CLI now calls `RunServer(ServerConfig)`, keeping flags stable
    while making startup/shutdown testable.

- [x] Add server package coverage around the current forwarding primitives.
  - Depends on: stable package boundaries or testable helper extraction.
  - Work: cover UDP relay cancellation, TCP/VLESS stream forwarding, listener
    shutdown, and dial failures.
  - Acceptance: server behavior has repeatable unit or loopback tests.
  - Proof: `go test ./server` covers `pipeConn`, UDP relay, and VLESS
    DTLS/KCP/smux forwarding to a TCP backend.

- [x] Add a loopback integration test with fake TCP backend.
  - Depends on: server/client binaries or callable helpers.
  - Work: start a local echo backend, connect through vk-turn client/server in
    VLESS mode, and verify byte-for-byte roundtrip without live VK.
  - Acceptance: test runs locally without external credentials.
  - Proof: `TestHandleVLESSConnectionForwardsSmuxStreamsToTCPBackend` verifies
    byte-for-byte TCP roundtrip over DTLS/KCP/smux without provider credentials.

## Milestone 1: Docker Development Stack

- [x] Create Docker Compose development topology.
  - Depends on: Dockerfile can build required binaries.
  - Work: add services for `xray-server`, `vkturn-server`, `vkturn-client`,
    `test-client`, and optional `fake-provider`.
  - Acceptance: `docker compose` starts all required services from clean checkout.
  - Proof: `dev/docker-compose.yml` starts `xray-server`, `vkturn-server`,
    `vkturn-client`, `xray-client`, `echo-target`, and `test-client`.

- [x] Build both server and client binaries in Docker.
  - Depends on: current single-binary Dockerfile.
  - Work: update build targets or add a dev Dockerfile so integration tests can
    run the server sidecar and local client sidecar.
  - Acceptance: compose can run `vk-turn-proxy-server` and
    `vk-turn-proxy-client` without host-built artifacts.
  - Proof: `dev/scripts/prove-vless-roundtrip.sh` builds both binaries in a
    `golang:1.25-alpine` container and mounts them into runtime containers.

- [x] Add disposable Xray/VLESS test config.
  - Depends on: chosen Xray image and fixed test UUID/port.
  - Work: create minimal VLESS TCP inbound and a deterministic test route for
    roundtrip verification.
  - Acceptance: Xray service is reachable from compose network and logs config
    load success.
  - Proof: `dev/xray/server.json` and `dev/xray/client.json` are exercised by
    the Docker lab.

- [x] Add deterministic provider bypass for integration tests.
  - Depends on: provider adapter boundary.
  - Work: support fake TURN credentials or local loopback mode for compose tests
    so live VK is not required.
  - Acceptance: default integration test never calls VK/Yandex/MAX.
  - Proof: Docker lab uses `vk-turn-proxy-client -dev-direct` and no provider
    links.

- [x] Add one-command Docker proof.
  - Depends on: compose topology, fake provider, Xray config.
  - Work: add `make`, script, or documented command that starts the stack,
    sends TCP traffic, verifies response, and tears down.
  - Acceptance: command exits non-zero on broken forwarding.
  - Proof: `dev/scripts/prove-vless-roundtrip.sh` exits only after
    `test-client` prints `DEV_LAB_OK`.

## Milestone 2: Server Sidecar Runtime

- [x] Normalize binary names and runtime flags.
  - Depends on: current `client` and `server` commands.
  - Work: settle names such as `vk-turn-proxy-server`,
    `vk-turn-proxy-client`, and future `vkturn`; document compatibility with
    old commands if needed.
  - Acceptance: README and Docker commands match actual binaries.
  - Proof: Docker/dev scripts now build and run `vk-turn-proxy-server` and
    `vk-turn-proxy-client`; the production image keeps a `vk-turn-proxy`
    compatibility symlink.

- [x] Add structured config file support for server mode.
  - Depends on: stable runtime flags.
  - Work: support listen UDP address, backend TCP address, VLESS mode, log level,
    status API bind address, and service metadata.
  - Acceptance: flags and config file produce the same effective runtime state.
  - Proof: `vk-turn-proxy-server -config` loads JSON config, explicit CLI flags
    override config values, and `dev/vkturn/server.json` is used by Docker lab.

- [x] Add startup health probes.
  - Depends on: config model.
  - Work: validate UDP listen bind, backend TCP dial, and VLESS mode compatibility
    before declaring the sidecar ready.
  - Acceptance: bad backend address is reported as unhealthy with a clear error.
  - Proof: server config validation covers listen/backend addresses, and TCP
    backend health checks fail before accepting sidecar traffic.

- [x] Add graceful shutdown contract.
  - Depends on: server tests.
  - Work: ensure signal handling closes listeners, active sessions, smux streams,
    and backend dials without panics.
  - Acceptance: shutdown test passes under `go test -race`.
  - Proof: `RunServer` exits on context cancellation and `go test -race ./...`
    covers listener/session shutdown.

## Milestone 3: Xray Discovery And Read-Only Plan

- [x] Build production-like Docker Linux server simulation.
  - Depends on: Docker lab and server runtime config.
  - Work: add a `prod-server` container with real Xray, vkturn sidecar,
    `/etc/xray/config.json`, `/etc/vkturn/server.json`, `/opt/vk-turn-proxy`,
    and `/var/log` layout. Do not use host systemd or host config.
  - Acceptance: one command proves traffic through the production-like container
    and leaves no running containers or networks.
  - Proof: `dev/prod-sim/scripts/prove-prod-sim.sh` returned `PROD_SIM_OK`.

- [x] Implement Xray service discovery.
  - Depends on: CLI/manager command shape.
  - Work: detect common systemd unit names and process/config shapes from
    fixtures first, then add host probes behind read-only commands.
  - Acceptance: fixture coverage reports candidates and confidence without
    reading or changing host services by default.
  - Proof: `internal/xraydoctor` detects `xray.service`, extracts
    `/etc/xray/config.json` from `ExecStart`, reads a fixture process snapshot,
    and reports candidate confidence without invoking `systemctl`.

- [x] Implement Xray config discovery and parsing.
  - Depends on: service discovery.
  - Work: inspect common config paths, parse JSON safely, and identify VLESS TCP
    inbounds with address/port/security fields.
  - Acceptance: parser handles valid configs, missing files, malformed JSON, and
    unsupported inbound types.
  - Proof: `internal/xrayplan` parses fixture and prod-sim Xray configs, skips
    unsupported non-TCP/non-VLESS inbounds, and reports malformed JSON with
    context.

- [x] Implement sidecar UDP port selection.
  - Depends on: plan model.
  - Work: prefer `56000/udp`, scan `56001..56100/udp`, and report conflicts.
  - Acceptance: busy ports are skipped and selected port is reproducible in
    tests.
  - Proof: `SelectUDPPortInRange` has deterministic busy-port coverage and the
    CLI uses a read-only UDP bind/release probe by default.

- [x] Implement `doctor` command.
  - Depends on: discovery helpers.
  - Work: report OS, privileges, available commands, Xray candidates, backend
    reachability, port availability, and Docker/dev environment status.
  - Acceptance: command is read-only and produces human-readable plus structured
    output.
  - Proof: `go run ./cmd/vkturn doctor --root dev/fixtures/linux-root
    --skip-host-commands --no-port-probe` emits human-readable read-only
    diagnostics, `--json` emits structured output, and
    `dev/scripts/prove-plan.sh` returns both `PLAN_OK` and `DOCTOR_OK`.

- [x] Implement `server plan` command.
  - Depends on: discovery, config parser, port selector.
  - Work: show detected Xray inbound, selected sidecar UDP port, files/services
    to create, commands to run, and files/services that will not be touched.
  - Acceptance: plan makes no writes and never restarts Xray.
  - Proof: `go run ./cmd/vkturn server plan --xray-config
    dev/fixtures/xray/vless-tcp.json` emits a read-only plan with backend
    `127.0.0.1:10001`, sidecar UDP listen address, create/not-touch lists, and
    JSON output support; `dev/scripts/prove-plan.sh` returns `PLAN_OK`.

## Milestone 4: Install And Manage Only The Sidecar

- [x] Add sidecar systemd unit generation.
  - Depends on: `server plan` output.
  - Work: generate service unit, environment/config path, log policy, restart
    policy, and permissions for the sidecar process.
  - Acceptance: generated unit references selected backend and UDP port and is
    verified as a repo-local golden file.
  - Proof: `internal/sidecarinstall` renders `vkturn-server.service`,
    `/etc/vkturn/server.json`, `/etc/default/vkturn-server`, and an install
    manifest from `server plan`; tests assert the unit references
    `127.0.0.1:10001` through the generated config and `0.0.0.0:56000` sidecar
    listen address.

- [x] Add `server install` dry-run with explicit apply boundary.
  - Depends on: unit generation.
  - Work: install binary/config/unit and enable/start only the sidecar service.
  - Acceptance: dry-run and temporary-root tests prove install would not modify
    Xray files or restart Xray; apply mode is not run on the dev machine.
  - Proof: `go run ./cmd/vkturn server install --dry-run --write --root
    $(mktemp -d) --xray-config dev/fixtures/xray/vless-tcp.json
    --no-port-probe` writes only sidecar artifacts under the temporary root,
    refuses non-dry-run mode, and `dev/scripts/prove-install-dry-run.sh`
    returns `INSTALL_DRY_RUN_OK`.

- [x] Add sidecar lifecycle commands.
  - Depends on: install command.
  - Work: implement `server status`, `server start`, `server stop`,
    `server restart`, and `server logs` for the sidecar unit.
  - Acceptance: commands are tested against fakes/containers and report missing
    units cleanly; no host `systemctl` mutation is required.
  - Proof: `internal/sidecarlifecycle` implements dry-run root lifecycle state,
    journal, log tailing, and missing-unit errors; `go run ./cmd/vkturn server
    status|start|stop|restart|logs --dry-run --root <tmp>` runs without host
    `systemctl`, and `dev/scripts/prove-lifecycle-dry-run.sh` returns
    `LIFECYCLE_DRY_RUN_OK`.

- [x] Add uninstall/rollback plan.
  - Depends on: install artifacts list.
  - Work: remove sidecar unit/config/binary created by this project while
    preserving Xray and user-owned files.
  - Acceptance: dry run shows exact removals before apply.
  - Proof: `internal/sidecarrollback` reads the sidecar install manifest,
    plans removal of only sidecar-owned unit/config/env/binary/log/state paths,
    preserves Xray config/service in `will_not_touch`, refuses non-dry-run, and
    `dev/scripts/prove-uninstall-dry-run.sh` returns `UNINSTALL_DRY_RUN_OK`.

## Milestone 5: Backend Control And Observability Contract

- [x] Define status model.
  - Depends on: runtime health probes.
  - Work: model provider state, TURN allocation state, DTLS state, KCP/smux
    sessions, local listener state, backend reachability, and last error.
  - Acceptance: documented states map to actual runtime transitions.
  - Proof: `internal/statusmodel` defines `status.v1`, component states,
    stable error codes, metrics, redaction helpers, and local-bind validation;
    `docs/status/STATUS_CONTRACT.md` documents the contract, and
    `go test ./internal/statusmodel` passes.

- [x] Expose local status API.
  - Depends on: status model.
  - Work: add localhost-only HTTP or Unix socket API for `health`, `status`,
    `events`, `logs`, and `restart-sidecar`.
  - Acceptance: API returns structured JSON and refuses remote access by default.
  - Proof: `server/status_api.go` exposes loopback-only `GET /health`,
    `GET /status`, `GET /events`, `GET /logs`, and `POST /restart-sidecar`;
    non-loopback binds are rejected, and `dev/scripts/prove-status-api.sh`
    returns `STATUS_API_OK`.

- [x] Add bounded event list for clients.
  - Depends on: status API.
  - Work: publish state changes and recoverable errors with stable codes for the
    future Android client.
  - Acceptance: reconnect, captcha, backend-down, provider-down, and session
    events are visible without parsing raw logs.
  - Proof: runtime events are recorded for server initialization, listener
    accepts, DTLS/KCP/smux session transitions, backend failures, and accepted
    restart requests; `/events` returns the bounded redacted event list. A true
    streaming/SSE transport is deferred until the Android client needs it.

- [x] Add log redaction.
  - Depends on: status/log endpoints.
  - Work: redact TURN username/password, VK tokens, captcha session tokens, call
    links where needed, and local secret config paths.
  - Acceptance: tests prove known secret shapes do not appear in logs/API.
  - Proof: `statusmodel.Redact` covers token/password-like key-value pairs, VK
    `vk1.*` tokens, VK call links, and Yandex Telemost links; server tests prove
    `/status`, `/events`, and `/logs` do not leak those shapes.

## Milestone 6: VK Provider Backend Readiness

- [x] Keep existing VK link flow as the only production provider path.
  - Depends on: provider adapter boundary.
  - Work: expose current join-link flow through a provider interface without
    changing request payloads.
  - Acceptance: existing `-vk-link` behavior is preserved.
  - Proof: no live VK request payloads were changed; `getTokenChain` still owns
    the existing VK join-link flow, while only `turn_server` parsing was
    extracted into `parseVKTurnServer` for fixture coverage.

- [x] Normalize provider states.
  - Depends on: status model.
  - Work: map provider errors into `ready`, `captcha_required`, `auth_required`,
    `rate_limited`, `provider_down`, and `unknown`.
  - Acceptance: captcha and auth failures never become hidden retry loops.
  - Proof: `internal/providerstate` classifies VK/Yandex-style captcha, rate
    limit, auth, provider-down, and parser-drift errors into `status.v1` states
    and redacts messages; `isAuthError` now uses the shared classifier.

- [x] Expand parser tests for VK drift.
  - Depends on: documented API flows.
  - Work: add fixtures for redirect captcha, image captcha, missing fields, rate
    limits, and TURN credential response variants.
  - Acceptance: parser changes require fixture updates.
  - Proof: `client/testdata/vk/` covers redirect captcha, image captcha,
    successful TURN/TURNS responses, missing `turn_server`, missing username,
    missing credential, empty URL list, and non-string URL drift; `go test
    ./client` exercises those fixtures.

- [x] Document live VK proof as opt-in.
  - Depends on: fake-provider integration tests.
  - Work: add environment-variable gated command for real VK link validation.
  - Acceptance: CI/default local tests never require live VK credentials.
  - Proof: `client/live_vk_test.go` skips unless `VKTURN_LIVE_VK_PROOF=1`,
    `dev/scripts/prove-live-vk-opt-in.sh` prints `LIVE_VK_SKIPPED` by default,
    and docs show the explicit `VKTURN_LIVE_VK_LINK` command.

## Milestone 7: Documentation And Release Baseline

- [x] Update README around sidecar-first Xray/VLESS usage.
  - Depends on: working Docker proof and runtime command names.
  - Work: separate legacy WireGuard examples from the new Xray/VLESS sidecar
    path and document safe install flow.
  - Acceptance: first-time backend user can run `doctor`, `plan`, and Docker
    proof from docs.
  - Proof: README now starts with an Xray/VLESS sidecar quick start covering
    `doctor`, `server plan`, dry-run `server install`, and
    `dev/scripts/prove-vless-roundtrip.sh` before legacy examples.

- [x] Keep `docs/API_FLOWS.md` current as provider behavior changes.
  - Depends on: provider tests and live proof observations.
  - Work: record endpoint, request fields, response fields, drift risk, and test
    fixture names for every flow change.
  - Acceptance: no provider code change lands without doc/test update.
  - Proof: `docs/API_FLOWS.md` documents VK fixture names, opt-in live proof,
    and provider-state mapping for captcha, rate limit, auth, provider-down,
    and unknown parser drift.

- [x] Keep `docs/TECH_DEBT.md` pruned.
  - Depends on: each milestone completion.
  - Work: remove resolved debt and add new risks only when they block future
    work.
  - Acceptance: tech debt stays small, current, and actionable.
  - Proof: `docs/TECH_DEBT.md` reflects the current package baseline,
    fixed status/provider readiness work, and keeps remaining debt focused on
    provider drift, route-helper live platform validation, and future
    provider-state control API exposure.

## Deferred Until Phase 2

- Android control client and mobile UI.
- Android local tunnel client and VPNService integration.
- Android VK authorization UX.
- Managed VK identity/session storage.
- Managed VK call creation and refresh.
- MAX provider promotion from research to implementation.
- Yandex production support.
