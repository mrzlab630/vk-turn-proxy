# Technical Baseline

This file tracks the starting point for future development. Keep it small and
actionable: move items out when they are fixed or deliberately accepted.

## Current Baseline

- Go module with `client`, `server`, `tcputil`, `cmd/vkturn`,
  `internal/xrayplan`, `internal/xraydoctor`, `internal/sidecarinstall`,
  `internal/sidecarlifecycle`, `internal/sidecarrollback`, and
  `internal/statusmodel`, and `internal/providerstate` packages.
- The server package has loopback unit/integration tests for UDP relay, VLESS
  forwarding, config loading, backend checks, status API, and shutdown behavior.
- The client package has parser/captcha unit tests and passes `go test -race`.
- External VK/Yandex flows are documented in `docs/API_FLOWS.md`.
- Product/deployment strategy is documented in `docs/STRATEGY.md`.
- Docker builds the server binary as `vk-turn-proxy-server`; the Docker lab
  builds both server and client binaries in a container.

## Fixed In This Baseline Pass

- Redirect-only VK captcha now parses without requiring `captcha_img`.
- Image-only legacy VK captcha can reach the manual image fallback.
- Manual captcha solving now respects the caller context timeout instead of
  leaving the local captcha server waiting forever.
- The manual captcha `generic_proxy` blocks localhost, private, link-local,
  multicast, unspecified IP targets, and non-HTTP schemes.
- TURN allocation setup is shared between UDP and VLESS client paths.
- Removed the dead `-no-dtls` client flag from the code path and documentation.
- Xray/VLESS config parsing, service/config discovery, read-only diagnostics,
  sidecar planning, dry-run install artifact generation, and dry-run lifecycle
  management live in `internal/xrayplan`, `internal/xraydoctor`,
  `internal/sidecarinstall`, `internal/sidecarlifecycle`,
  `internal/sidecarrollback`, and `cmd/vkturn`.
- Backend observability now has a documented `status.v1` model, loopback-only
  status API, bounded event/log buffers, stable error codes, and redaction tests
  for URL, token, TURN credential, session, and captcha secret shapes in
  `internal/statusmodel` and `server`.
- VK provider readiness has fixture-backed captcha/TURN response parser tests,
  an opt-in-only live VK proof, and shared provider error classification in
  `internal/providerstate`.
- `tcputil.NewKCPOverDTLS` has a loopback DTLS fixture test proving a KCP
  client/server round trip.
- VK app credentials are supplied through the documented
  `VKTURN_VK_CREDENTIALS` override instead of a committed application list.
- Route helper scripts have a repo-local smoke proof that dry-runs the Linux
  helper and statically validates macOS/Windows helpers without host route
  changes.
- Provider state transitions can be recorded into the existing status API and
  event stream without a separate provider-control endpoint.

## Remaining Debt

- `client/main.go` has been reduced by provider/runtime splits: `yandex_auth.go`
  owns Telemost, `vk_auth.go` owns VK credential config/parser helpers,
  `vless.go` owns KCP/smux TCP forwarding, `turn_relay.go` owns TURN allocation
  and UDP relay transport, `dtls_udp.go` owns the non-VLESS DTLS packet loop,
  `vk_token_chain.go` owns VK token request/captcha orchestration, and
  `captcha.go` owns shared captcha parsing and solve-mode control.
- The current VK/Yandex flows depend on private browser APIs. Parser coverage
  should be expanded before changing request payloads.
- Route helper scripts still need live platform validation on Linux/macOS/Windows
  before relying on them for production operator workflows; macOS/Windows are
  not executable in the current Linux CI/dev environment.
- Runtime integration tests now cover fake UDP/TCP backends, local status API,
  provider-state status/event exposure, and a Docker Xray/VLESS lab. Next
  integration debt is wiring live client provider transitions into the status
  surface after the client control channel is designed.

## Quality Gates

Run these before merging functional changes:

```bash
gofmt -w client server tcputil
go test ./...
go vet ./...
go test -race ./...
```

For changes touching Docker or release packaging:

```bash
dev/scripts/prove-docker-build.sh
```

The script keeps the normal `docker build` path first and uses a repo-local DNS
pin retry only when Docker Desktop DNS cannot resolve Go module endpoints.
