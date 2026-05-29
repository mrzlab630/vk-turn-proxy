# Backend Status Contract

Schema version: `status.v1`.

The status API is local-only. It must bind to `127.0.0.1` or `::1`; binding to
`0.0.0.0`, public, or LAN addresses is rejected. This keeps Android/control
clients explicit: remote access must go through a future authenticated channel,
not through an accidentally exposed diagnostics port.

## Endpoints

- `GET /health` - compact health response with `status` and `schema_version`.
- `GET /status` - full `status.v1` snapshot.
- `GET /events` - bounded in-memory event list for state changes and errors.
- `GET /logs` - bounded in-memory redacted log lines.
- `POST /restart-sidecar` - records an accepted restart request event. It does
  not restart host services in the current dev workflow.

## Component States

- `unknown`
- `disabled`
- `initializing`
- `ready`
- `listening`
- `connected`
- `disconnected`
- `degraded`
- `error`
- `auth_required`
- `captcha_required`
- `rate_limited`
- `provider_down`

## Error Codes

- `none`
- `config_invalid`
- `backend_down`
- `listen_failed`
- `provider_auth_required`
- `provider_captcha_required`
- `provider_rate_limited`
- `provider_down`
- `turn_allocation_failed`
- `dtls_handshake_failed`
- `kcp_session_failed`
- `smux_session_failed`
- `backend_dial_failed`
- `unknown`

## Provider Error Mapping

Provider state transitions are exposed through the `provider` object in
`GET /status` and as `component=provider` events in `GET /events`. The current
server implementation can record these transitions without adding a separate
provider-control endpoint; client-side provider wiring can attach to this
surface when the control channel is designed.

Provider adapters and client-side provider code should normalize external
failures before exposing them to control clients:

- captcha and VK `error_code=14` shapes -> `provider_captcha_required`;
- rate limits, VK `error_code=29`, and HTTP 429-shaped errors ->
  `provider_rate_limited`;
- auth failures, invalid credentials, and TURN `stale nonce` ->
  `provider_auth_required`;
- provider timeouts, DNS/connectivity failures, and HTTP 5xx-shaped errors ->
  `provider_down`;
- unknown parser drift -> `unknown`.

The shared classifier lives in `internal/providerstate` and redacts messages
before they can be displayed by future clients.

## Redaction

Status, events, and logs must redact known secret shapes before output:

- `access_token`, `token`, `password`, `secret`, `credential`, `username`,
  `session_token`, `session_key`, `success_token`, `captcha_sid`, and
  `captcha_key` in `key=value`, `key:value`, and JSON string forms;
- VK token values beginning with `vk1.`;
- VK call join links;
- Yandex Telemost join links.

Tests in `internal/statusmodel` and `server` prove these shapes do not appear in
API output.
