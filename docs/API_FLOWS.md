# API Flows

This document is the local knowledge base for the external call flows used by
the client. It is intentionally based on the current code paths, because both
VK Calls and Yandex Telemost use private browser-facing APIs that can drift
without public changelogs.

## VK Calls TURN Credentials

Source: `client/main.go`, `getTokenChain`.

Fixture coverage lives under `client/testdata/vk/` and is exercised by
`go test ./client`. Default tests never call VK. Live VK validation is opt-in
only through `dev/scripts/prove-live-vk-opt-in.sh` with explicit environment
variables.

### Inputs

- Invite link: `https://vk.com/call/join/<join_id>`.
- The code stores only `<join_id>` after trimming `/`, `?`, and `#` suffixes.
- One of the hardcoded browser application credential pairs from
  `vkCredentialsList`.

### Flow

1. Anonymous VK token.

   ```http
   POST https://login.vk.ru/?act=get_anonym_token
   Content-Type: application/x-www-form-urlencoded
   Origin: https://vk.ru
   Referer: https://vk.ru/
   ```

   Form fields:

   - `client_id`
   - `token_type=messages`
   - `client_secret`
   - `version=1`
   - `app_id=<client_id>`

   Required response fields:

   - `data.access_token` -> `token1`

2. Optional call preview probe.

   ```http
   POST https://api.vk.ru/method/calls.getCallPreview?v=5.275&client_id=<client_id>
   ```

   Form fields:

   - `vk_join_link=https://vk.com/call/join/<join_id>`
   - `fields=photo_200`
   - `access_token=<token1>`

   The client logs failures and continues.

3. Anonymous call token.

   ```http
   POST https://api.vk.ru/method/calls.getAnonymousToken?v=5.275&client_id=<client_id>
   ```

   Form fields:

   - `vk_join_link=https://vk.com/call/join/<join_id>`
   - `name=<generated display name>`
   - `access_token=<token1>`

   Required response fields:

   - `response.token` -> `token2`

   Captcha response fields handled by the client:

   - `error.error_code=14`
   - `error.error_msg`
   - `error.captcha_sid`
   - `error.redirect_uri` with `session_token` query parameter
   - `error.captcha_img` for legacy/manual image captcha fallback
   - `error.captcha_ts`
   - `error.captcha_attempt`
   - `error.is_sound_captcha_available`

4. OK Calls anonymous session.

   ```http
   POST https://calls.okcdn.ru/fb.do
   ```

   Form fields:

   - `session_data={"version":2,"device_id":"<uuid>","client_version":1.1,"client_type":"SDK_JS"}`
   - `method=auth.anonymLogin`
   - `format=JSON`
   - `application_key=CGMMEJLGDIHBABABA`

   Required response fields:

   - `session_key` -> `token3`

5. Join conversation and read TURN credentials.

   ```http
   POST https://calls.okcdn.ru/fb.do
   ```

   Form fields:

   - `joinLink=<join_id>`
   - `isVideo=false`
   - `protocolVersion=5`
   - `capabilities=2F7F`
   - `anonymToken=<token2>`
   - `method=vchat.joinConversationByLink`
   - `format=JSON`
   - `application_key=CGMMEJLGDIHBABABA`
   - `session_key=<token3>`

   Required response fields:

   - `turn_server.username`
   - `turn_server.credential`
   - `turn_server.urls[0]`, with `turn:` or `turns:` prefix stripped and query
     string removed

   Current fixture names:

   - `turn_success.json`
   - `turn_success_tls.json`
   - `turn_missing_server.json`
   - `turn_missing_username.json`
   - `turn_missing_credential.json`
   - `turn_empty_urls.json`
   - `turn_non_string_url.json`

### VK Captcha NotRobot Flow

Source: `client/main.go`, `client/slider_captcha.go`, `client/manual_captcha.go`.

1. Fetch captcha bootstrap HTML from `error.redirect_uri`.
2. Parse:
   - `const powInput = "..."`
   - difficulty from either `startsWith('0'.repeat(N))` or
     `const difficulty = N`
   - optional `window.init.data.show_captcha_type`
   - optional `window.init.data.captcha_settings`
3. Solve SHA-256 proof of work by finding a nonce where
   `sha256(powInput + nonce)` starts with `difficulty` zeroes.
4. Call VK methods under `https://api.vk.ru/method/<method>?v=5.131`:
   - `captchaNotRobot.settings`
   - `captchaNotRobot.componentDone`
   - `captchaNotRobot.check`
   - `captchaNotRobot.endSession`
5. Submit either an empty checkbox answer or a ranked slider answer, depending
   on the server response and the enabled solve mode.

Private/unstable fields:

- `debug_info`
- browser/device fingerprint fields
- slider `captcha_settings`, `image`, `steps`, and status values
- `show_captcha_type`

Manual fallback:

- Redirect captcha is proxied locally through `http://localhost:8765` so the
  browser can solve it and return `success_token`.
- Image-only captcha opens a small local form and returns `captcha_key`.

Current fixture names:

- `captcha_redirect.json`
- `captcha_image.json`

### Provider State Mapping

Source: `internal/providerstate`.

Provider errors are normalized for the backend/client control contract before a
future Android client consumes them:

- captcha strings, `error_code=14`, `error_code:14`, and `captcha_sid` ->
  `provider_captcha_required` / `captcha_required`;
- `error_code=29`, `error_code:29`, `Rate limit`, `Too many requests`, and
  HTTP 429-shaped errors -> `provider_rate_limited` / `rate_limited`;
- `401`, `403`, `Unauthorized`, `authentication`, `invalid credential`, and
  `stale nonce` -> `provider_auth_required` / `auth_required`;
- timeouts, DNS failures, connection failures, and HTTP 5xx-shaped errors ->
  `provider_down` / `provider_down`;
- parser drift such as missing expected fields remains `unknown` so it is not
  hidden as a retry-only transport issue.

## Yandex Telemost TURN Credentials

Source: `client/main.go`, `getYandexCreds`.

The README currently marks Telemost as closed. The implementation remains in
the client and should be treated as best-effort and drift-prone.

### Inputs

- Invite link: `https://telemost.yandex.ru/j/<room_id>`.
- The code stores only `<room_id>` after trimming `/`, `?`, and `#` suffixes.

### Flow

1. Read conference metadata.

   ```http
   GET https://cloud-api.yandex.ru/telemost_front/v2/telemost/conferences/https%3A%2F%2Ftelemost.yandex.ru%2Fj%2F<room_id>/connection?next_gen_media_platform_allowed=false
   Origin: https://telemost.yandex.ru
   Referer: https://telemost.yandex.ru/
   Client-Instance-Id: <uuid>
   ```

   Required response fields:

   - `room_id`
   - `peer_id`
   - `credentials`
   - `client_configuration.media_server_url` -> WebSocket URL

2. Open WebSocket to `client_configuration.media_server_url`.

   Headers:

   - `Origin: https://telemost.yandex.ru`
   - browser-like `User-Agent`

3. Send `hello` payload.

   Important fields:

   - `participantMeta` / `participantAttributes` with generated name
   - `participantId=<peer_id>`
   - `roomId=<room_id>`
   - `serviceName=telemost`
   - `credentials=<credentials>`
   - browser SDK info and capability lists

4. Read WebSocket messages until `serverHello.rtcConfiguration.iceServers` is
   present.

   Required fields in an ICE server entry:

   - `urls`, either string or list
   - `username`
   - `credential`

   The client skips `transport=tcp` URLs, strips `turn:`/`turns:` and query
   string, then returns `username`, `credential`, and address.

## Drift Risks

- VK and Yandex endpoints here are browser-facing/private, not stable public
  contracts.
- Captcha field shapes have already changed enough to require support for
  redirect-only and image-only variants.
- Browser fingerprint, SDK version, `capabilities`, and `protocolVersion` are
  likely high-drift values.
- TURN URLs can switch address family, transport, or include query parameters.
- Hardcoded VK app credentials may be rate-limited or invalidated.

When changing these flows, update this file and add parser tests for every new
response shape before changing transport code.
