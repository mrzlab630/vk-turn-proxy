#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
cd "$ROOT_DIR"

bash -n routes.sh

grep -Fq '#!/bin/bash' routes.sh
grep -Fq 'set -euo pipefail' routes.sh
grep -Fq 'ip -o -4 route show to default' routes.sh
grep -Fq 'VKTURN_ROUTE_GATEWAY' routes.sh
grep -Fq -- '--dry-run' routes.sh
grep -Fq 'route replace "$remote" via "$gateway"' routes.sh

LINUX_OUTPUT=$(VKTURN_ROUTE_GATEWAY=192.0.2.1 bash routes.sh --dry-run <<'EOF' 2>&1
203.0.113.10
unexpected-input

198.51.100.7
EOF
)
grep -Fq 'Ensuring route to 203.0.113.10 via 192.0.2.1' <<<"$LINUX_OUTPUT"
grep -Eq 'DRY RUN: (sudo )?ip route replace 203\.0\.113\.10 via 192\.0\.2\.1' <<<"$LINUX_OUTPUT"
grep -Fq 'Skipping unexpected input: unexpected-input' <<<"$LINUX_OUTPUT"
grep -Eq 'DRY RUN: (sudo )?ip route replace 198\.51\.100\.7 via 192\.0\.2\.1' <<<"$LINUX_OUTPUT"

grep -Fq '#!/bin/zsh' routes-macos.sh
grep -Fq 'set -u' routes-macos.sh
grep -Fq 'route -n get default' routes-macos.sh
grep -Fq 'VKTURN_ROUTE_DEFAULT_INFO' routes-macos.sh
grep -Fq -- '--dry-run' routes-macos.sh
grep -Fq 'Disconnect WireGuard/VPN first' routes-macos.sh
grep -Fq 'sudo route -n add -host "$remote" "$gateway"' routes-macos.sh

python3 - <<'PY'
from pathlib import Path

raw = Path("routes.ps1").read_bytes()
text = raw.decode("utf-8-sig")
required = [
    'Get-NetRoute',
    'DestinationPrefix "0.0.0.0/0"',
    'PolicyStore ActiveStore',
    'Remove-NetRoute -Confirm:$false',
    'New-NetRoute',
    'if ($addr -notmatch',
]
missing = [needle for needle in required if needle not in text]
if missing:
    raise SystemExit("missing PowerShell route helper fragments: " + ", ".join(missing))
PY

printf '%s\n' 'ROUTE_HELPERS_OK'
