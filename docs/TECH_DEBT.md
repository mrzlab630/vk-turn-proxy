# Technical Baseline

This file tracks the starting point for future development. Keep it small and
actionable: move items out when they are fixed or deliberately accepted.

## Current Baseline

- Go module with three packages: `client`, `server`, and `tcputil`.
- The server package has no unit tests.
- The client package has parser/captcha unit tests and passes `go test -race`.
- External VK/Yandex flows are documented in `docs/API_FLOWS.md`.
- Product/deployment strategy is documented in `docs/STRATEGY.md`.
- Docker builds only the server binary.

## Fixed In This Baseline Pass

- Redirect-only VK captcha now parses without requiring `captcha_img`.
- Image-only legacy VK captcha can reach the manual image fallback.
- Manual captcha solving now respects the caller context timeout instead of
  leaving the local captcha server waiting forever.
- The manual captcha `generic_proxy` blocks localhost, private, link-local,
  multicast, unspecified IP targets, and non-HTTP schemes.
- TURN allocation setup is shared between UDP and VLESS client paths.
- Removed the dead `-no-dtls` client flag from the code path and documentation.

## Remaining Debt

- `client/main.go` is still too large. Candidate split:
  - `vk_auth.go` for VK token/TURN credential flow.
  - `yandex_auth.go` for Telemost credential flow.
  - `turn_relay.go` for TURN allocation and UDP relay logic.
  - `vless.go` for KCP/smux TCP forwarding.
  - `captcha.go` for shared captcha parsing and solve-mode control.
- `server/main.go` needs unit/integration coverage for UDP relay, VLESS relay,
  and shutdown behavior.
- `tcputil.NewKCPOverDTLS` should be tested with an in-memory packet transport
  or a loopback DTLS fixture before tuning KCP settings further.
- The current VK/Yandex flows depend on private browser APIs. Parser coverage
  should be expanded before changing request payloads.
- The hardcoded VK application credential list is operationally fragile. If it
  changes frequently, move it behind a documented config/env override.
- Route helper scripts are platform-specific and not covered by tests.
- Runtime integration tests should cover a fake UDP backend and a fake TCP
  backend with client/server binaries connected over loopback.
- Docker Compose development stack is not implemented yet. It should provide a
  disposable Xray/VLESS server, vk-turn-proxy server sidecar, vk-turn-proxy
  client sidecar, and test traffic generator before production installer work.

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
docker build -t vk-turn-proxy .
```
