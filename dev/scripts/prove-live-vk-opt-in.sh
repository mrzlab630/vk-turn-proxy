#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
cd "$ROOT_DIR"

if [ "${VKTURN_LIVE_VK_PROOF:-}" != "1" ]; then
  printf '%s\n' 'LIVE_VK_SKIPPED set VKTURN_LIVE_VK_PROOF=1 and VKTURN_LIVE_VK_LINK=https://vk.com/call/join/... to run'
  exit 0
fi

if [ -z "${VKTURN_LIVE_VK_LINK:-}" ]; then
  printf '%s\n' 'VKTURN_LIVE_VK_LINK is required when VKTURN_LIVE_VK_PROOF=1' >&2
  exit 1
fi

go test ./client -run TestLiveVKCredentialFlowOptIn -count=1 -v
