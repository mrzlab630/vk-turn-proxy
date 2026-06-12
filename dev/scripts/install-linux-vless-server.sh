#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd)

GO_VERSION=${GO_VERSION:-$(awk '/^go / { print $2; exit }' "$ROOT_DIR/go.mod")}
XRAY_RELEASE_BASE=${XRAY_RELEASE_BASE:-https://github.com/XTLS/Xray-core/releases/latest/download}
XRAY_BINARY_INPUT=${XRAY_BINARY:-}

DEFAULT_SERVER_PUBLIC_ADDR=${SERVER_PUBLIC_ADDR:-}
DEFAULT_VKTURN_UDP_PORT=${VKTURN_UDP_PORT:-56000}
DEFAULT_XRAY_LISTEN=${XRAY_LISTEN:-127.0.0.1}
DEFAULT_XRAY_PORT=${XRAY_PORT:-10001}
DEFAULT_STATUS_PORT=${VKTURN_STATUS_PORT:-18081}
DEFAULT_LOCAL_LISTEN=${LOCAL_VLESS_LISTEN:-127.0.0.1}
DEFAULT_LOCAL_PORT=${LOCAL_VLESS_PORT:-9000}
DEFAULT_VK_LINK=${VK_LINK:-}
XRAY_LISTEN_INPUT=${XRAY_LISTEN:-}
XRAY_PORT_INPUT=${XRAY_PORT:-}
VLESS_UUID_INPUT=${VLESS_UUID:-}

DRY_RUN=${DRY_RUN:-0}
NONINTERACTIVE=${NONINTERACTIVE:-0}
ASSUME_YES=${ASSUME_YES:-0}
FORCE=${FORCE:-0}
START_SERVICES=${START_SERVICES:-1}
CONFIGURE_FIREWALL=${CONFIGURE_FIREWALL:-1}
DRY_RUN_ROOT=${DRY_RUN_ROOT:-}
UPDATE_XRAY=${UPDATE_XRAY:-1}
RESTART_XRAY=${RESTART_XRAY:-1}
XRAY_CONFIG_MODE=${XRAY_CONFIG_MODE:-auto}
XRAY_EXISTING_CONFIG=${XRAY_EXISTING_CONFIG:-}

SERVICE_USER=${SERVICE_USER:-nobody}
SERVICE_GROUP=${SERVICE_GROUP:-nogroup}
INSTALL_DIR=${INSTALL_DIR:-/opt/vk-turn-proxy}
SERVER_BINARY=${SERVER_BINARY:-$INSTALL_DIR/vk-turn-proxy-server}
VKTURN_CONFIG=${VKTURN_CONFIG:-/etc/vkturn/server.json}
VKTURN_PROFILE=${VKTURN_PROFILE:-/etc/vkturn/client-profile.env}
VKTURN_UNIT=${VKTURN_UNIT:-/etc/systemd/system/vkturn-server.service}
VKTURN_LOG_DIR=${VKTURN_LOG_DIR:-/var/log/vk-turn-proxy}
XRAY_BINARY=${XRAY_BINARY_INPUT:-/usr/local/bin/xray}
XRAY_CONFIG=${XRAY_CONFIG:-/etc/xray/config.json}
XRAY_SERVICE_NAME=${XRAY_SERVICE_NAME:-xray.service}
XRAY_UNIT=${XRAY_UNIT:-/etc/systemd/system/$XRAY_SERVICE_NAME}
XRAY_ASSET_DIR=${XRAY_ASSET_DIR:-/usr/local/share/xray}
XRAY_MANAGED_ASSETS=${XRAY_MANAGED_ASSETS:-0}
XRAY_LOG_DIR=${XRAY_LOG_DIR:-/var/log/xray}
BACKUP_DIR=${BACKUP_DIR:-/var/backups/vk-turn-proxy}
SUMMARY_PATH=${SUMMARY_PATH:-/root/vkturn-install-result.txt}
GO_BINARY=${GO_BINARY:-}
VLESS_NETWORK=${VLESS_NETWORK:-tcp}
VLESS_SECURITY=${VLESS_SECURITY:-none}
VLESS_ENCRYPTION=${VLESS_ENCRYPTION:-none}
EXISTING_XRAY_IMPORTED=0
EXISTING_XRAY_CONFIG=""
EXISTING_XRAY_SERVICE=0
EXISTING_XRAY_CONFIG_FOUND=0
EXISTING_XRAY_BINARY=""
EXISTING_XRAY_UNIT=""
EXISTING_XRAY_CONFIG_FROM_SERVICE=""
EXISTING_VLESS_TAG=""
EXISTING_VLESS_UUID=""
EXISTING_XRAY_LISTEN=""
EXISTING_XRAY_PORT=""
XRAY_STREAM_SETTINGS_B64=""
XRAY_CLIENT_B64=""
XRAY_INBOUND_B64=""
USE_EXISTING_XRAY_CONFIG=0
USE_EXISTING_XRAY_SERVICE=0
BACKUP_STAMP=""

if [[ -t 1 ]]; then
  BOLD=$'\033[1m'
  DIM=$'\033[2m'
  GREEN=$'\033[32m'
  YELLOW=$'\033[33m'
  RED=$'\033[31m'
  BLUE=$'\033[34m'
  RESET=$'\033[0m'
else
  BOLD=""
  DIM=""
  GREEN=""
  YELLOW=""
  RED=""
  BLUE=""
  RESET=""
fi

info() { printf '%s[INFO]%s %s\n' "$BLUE" "$RESET" "$*"; }
ok() { printf '%s[ OK ]%s %s\n' "$GREEN" "$RESET" "$*"; }
warn() { printf '%s[WARN]%s %s\n' "$YELLOW" "$RESET" "$*" >&2; }
die() { printf '%s[FAIL]%s %s\n' "$RED" "$RESET" "$*" >&2; exit 1; }

section() {
  printf '\n%s== %s ==%s\n' "$BOLD" "$1" "$RESET"
}

usage() {
  cat <<'EOF'
Usage:
  sudo bash dev/scripts/install-linux-vless-server.sh

Useful automation flags:
  DRY_RUN=1                render files under a temporary root and print commands
  NONINTERACTIVE=1         use env/default values without prompts
  ASSUME_YES=1             accept confirmation prompts
  FORCE=1                  overwrite existing managed files
  XRAY_CONFIG_MODE=auto     auto | preserve | replace
  UPDATE_XRAY=1             update Xray binary/assets from the latest release
  RESTART_XRAY=1            restart Xray after updating its binary/assets

Common inputs:
  SERVER_PUBLIC_ADDR=203.0.113.10
  VKTURN_UDP_PORT=56000
  XRAY_PORT=10001
  VKTURN_STATUS_PORT=18081
  LOCAL_VLESS_PORT=9000
  VK_LINK='https://vk.com/call/join/...'
  ALLOW_PUBLIC_XRAY=1     allow Xray security=none on a non-loopback address
EOF
}

for arg in "$@"; do
  case "$arg" in
    -h|--help)
      usage
      exit 0
      ;;
    *)
      die "unknown argument: $arg"
      ;;
  esac
done

is_dry_run() { [[ "$DRY_RUN" == "1" ]]; }
is_interactive() { [[ "$NONINTERACTIVE" != "1" && -t 0 ]]; }
is_truthy() {
  case "${1,,}" in
    1|true|yes|y) return 0 ;;
    *) return 1 ;;
  esac
}

require_linux() {
  [[ "$(uname -s)" == "Linux" ]] || die "this installer supports Linux only"
}

require_root_for_real_install() {
  if ! is_dry_run && [[ "${EUID:-$(id -u)}" -ne 0 ]]; then
    die "run as root: sudo bash dev/scripts/install-linux-vless-server.sh"
  fi
}

init_dry_run_root() {
  if ! is_dry_run; then
    return
  fi
  if [[ -z "$DRY_RUN_ROOT" ]]; then
    DRY_RUN_ROOT=$(mktemp -d /tmp/vkturn-installer-dry-run.XXXXXX)
  fi
  mkdir -p "$DRY_RUN_ROOT"
  info "dry-run root: $DRY_RUN_ROOT"
}

target_path() {
  local path=$1
  if is_dry_run; then
    printf '%s/%s' "$DRY_RUN_ROOT" "${path#/}"
  else
    printf '%s' "$path"
  fi
}

read_path() {
  local path=$1
  if is_dry_run && [[ "$path" == /* ]]; then
    target_path "$path"
  else
    printf '%s' "$path"
  fi
}

backup_stamp() {
  if [[ -z "$BACKUP_STAMP" ]]; then
    BACKUP_STAMP=$(date -u +%Y%m%dT%H%M%SZ)
  fi
  printf '%s' "$BACKUP_STAMP"
}

backup_existing_path() {
  local path=$1
  local source
  source=$(read_path "$path")
  if [[ ! -e "$source" ]]; then
    return 0
  fi
  local dest="$BACKUP_DIR/$(backup_stamp)/${path#/}"
  if is_dry_run; then
    printf 'DRY RUN: backup %q -> %q\n' "$path" "$dest"
    return 0
  fi
  install -d -m 0700 "$(dirname "$dest")"
  cp -a "$source" "$dest"
}

print_command() {
  local quoted=()
  local arg
  for arg in "$@"; do
    quoted+=("$(printf '%q' "$arg")")
  done
  printf '%s' "${quoted[*]}"
}

run() {
  if is_dry_run; then
    printf 'DRY RUN: '
    print_command "$@"
    printf '\n'
    return 0
  fi
  "$@"
}

write_file() {
  local path=$1
  local mode=$2
  local content=$3
  local target
  target=$(target_path "$path")
  if is_dry_run; then
    info "render $path -> $target mode=$mode"
  fi
  backup_existing_path "$path"
  mkdir -p "$(dirname "$target")"
  local tmp
  tmp=$(mktemp)
  printf '%s' "$content" >"$tmp"
  install -m "$mode" "$tmp" "$target"
  rm -f "$tmp"
}

ensure_dir() {
  local path=$1
  local mode=$2
  local target
  target=$(target_path "$path")
  if is_dry_run; then
    info "create directory $path -> $target mode=$mode"
  fi
  install -d -m "$mode" "$target"
}

chown_path() {
  local owner=$1
  local path=$2
  if is_dry_run; then
    printf 'DRY RUN: chown -R %q %q\n' "$owner" "$path"
    return 0
  fi
  chown -R "$owner" "$path"
}

chown_one() {
  local owner=$1
  local path=$2
  if is_dry_run; then
    printf 'DRY RUN: chown %q %q\n' "$owner" "$path"
    return 0
  fi
  chown "$owner" "$path"
}

ask_value() {
  local __var=$1
  local prompt=$2
  local default=$3
  local value
  if is_interactive; then
    read -r -p "$prompt [$default]: " value || true
    value=${value:-$default}
  else
    value=$default
  fi
  printf -v "$__var" '%s' "$value"
}

ask_yes_no() {
  local __var=$1
  local prompt=$2
  local default=$3
  local answer
  if [[ "$ASSUME_YES" == "1" ]]; then
    printf -v "$__var" '%s' "1"
    return
  fi
  if ! is_interactive; then
    if [[ "$default" =~ ^[Yy]$ ]]; then
      printf -v "$__var" '%s' "1"
    else
      printf -v "$__var" '%s' "0"
    fi
    return
  fi
  while true; do
    read -r -p "$prompt [${default}/$( [[ "$default" =~ ^[Yy]$ ]] && echo n || echo y )]: " answer || true
    answer=${answer:-$default}
    case "$answer" in
      y|Y|yes|YES) printf -v "$__var" '%s' "1"; return ;;
      n|N|no|NO) printf -v "$__var" '%s' "0"; return ;;
      *) warn "answer y or n" ;;
    esac
  done
}

yes_no_default() {
  if [[ "$1" == "1" ]]; then
    printf 'y'
  else
    printf 'n'
  fi
}

value_or_empty() {
  if [[ -n "$1" ]]; then
    printf '%s' "$1"
  else
    printf '<empty>'
  fi
}

bool_label() {
  if is_truthy "$1"; then
    printf 'yes'
  else
    printf 'no'
  fi
}

tui_clear() {
  if is_interactive && [[ "${TERM:-}" != "dumb" ]] && command -v clear >/dev/null 2>&1; then
    clear
  fi
}

tui_pause() {
  if is_interactive; then
    read -r -p "Press Enter to continue..." _ || true
  fi
}

load_default_inputs() {
  local detected_public
  detected_public=$(detect_public_addr)
  VLESS_UUID=$(generate_uuid)
  SERVER_PUBLIC_ADDR=$detected_public
  VKTURN_UDP_PORT=$DEFAULT_VKTURN_UDP_PORT
  XRAY_LISTEN=$DEFAULT_XRAY_LISTEN
  XRAY_PORT=$DEFAULT_XRAY_PORT
  VKTURN_STATUS_PORT=$DEFAULT_STATUS_PORT
  LOCAL_VLESS_LISTEN=$DEFAULT_LOCAL_LISTEN
  LOCAL_VLESS_PORT=$DEFAULT_LOCAL_PORT
  VK_LINK=$DEFAULT_VK_LINK
}

find_existing_xray_service() {
  local service_path
  local unit_path
  for unit_path in "/etc/systemd/system/$XRAY_SERVICE_NAME" "/lib/systemd/system/$XRAY_SERVICE_NAME" "/usr/lib/systemd/system/$XRAY_SERVICE_NAME"; do
    service_path=$(read_path "$unit_path")
    if [[ -f "$service_path" ]]; then
      EXISTING_XRAY_SERVICE=1
      EXISTING_XRAY_UNIT=$unit_path
      USE_EXISTING_XRAY_SERVICE=1
      return
    fi
  done
  if ! is_dry_run && command -v systemctl >/dev/null 2>&1 && systemctl list-unit-files "$XRAY_SERVICE_NAME" --no-legend 2>/dev/null | grep -q '^'; then
    EXISTING_XRAY_SERVICE=1
    USE_EXISTING_XRAY_SERVICE=1
    unit_path=$(systemctl show -p FragmentPath --value "$XRAY_SERVICE_NAME" 2>/dev/null || true)
    if [[ -n "$unit_path" && "$unit_path" != "/dev/null" && -f "$unit_path" ]]; then
      EXISTING_XRAY_UNIT=$unit_path
    fi
  fi
}

normalize_existing_xray_binary() {
  local candidate=$EXISTING_XRAY_BINARY
  local basename
  [[ -n "$candidate" ]] || return 0
  basename=${candidate##*/}
  if [[ "$basename" != "xray" ]]; then
    EXISTING_XRAY_BINARY=""
    return 0
  fi
  if [[ "$candidate" != /* ]]; then
    if ! is_dry_run && command -v "$candidate" >/dev/null 2>&1; then
      EXISTING_XRAY_BINARY=$(command -v "$candidate")
    else
      EXISTING_XRAY_BINARY=""
    fi
  fi
}

extract_xray_service_config() {
  local unit_path=$1
  local unit_source
  unit_source=$(read_path "$unit_path")
  [[ -f "$unit_source" ]] || return 0
  EXISTING_XRAY_CONFIG_FROM_SERVICE=$(sed -nE 's/.*(^|[[:space:]])(-config|-c|--config=)[[:space:]]*([^[:space:]]+).*/\3/p; s/.*--config=([^[:space:]]+).*/\1/p' "$unit_source" | head -n1 | tr -d "'\"")
  EXISTING_XRAY_BINARY=$(sed -nE 's/^[[:space:]]*ExecStart=([^[:space:]]+).*/\1/p' "$unit_source" | head -n1 | tr -d "'\"")
  normalize_existing_xray_binary
}

candidate_xray_configs() {
  if [[ -n "$XRAY_EXISTING_CONFIG" ]]; then
    printf '%s\n' "$XRAY_EXISTING_CONFIG"
  fi
  if [[ -n "$EXISTING_XRAY_CONFIG_FROM_SERVICE" ]]; then
    printf '%s\n' "$EXISTING_XRAY_CONFIG_FROM_SERVICE"
  fi
  printf '%s\n' "$XRAY_CONFIG" "/etc/xray/config.json" "/usr/local/etc/xray/config.json" "/etc/v2ray/config.json"
}

python_json_available() {
  command -v python3 >/dev/null 2>&1
}

extract_vless_from_config() {
  local config_path=$1
  local source
  source=$(read_path "$config_path")
  [[ -f "$source" ]] || return 1
  python_json_available || return 1
  python3 - "$source" <<'PY'
import base64
import json
import sys

path = sys.argv[1]
with open(path, "r", encoding="utf-8") as fh:
    cfg = json.load(fh)


def b64_json(value):
    data = json.dumps(value or {}, separators=(",", ":"), sort_keys=True).encode("utf-8")
    return base64.b64encode(data).decode("ascii")

for inbound in cfg.get("inbounds", []):
    if str(inbound.get("protocol", "")).lower() != "vless":
        continue
    stream = inbound.get("streamSettings") or {}
    network = str(stream.get("network") or "tcp").lower()
    if network != "tcp":
        continue
    clients = (inbound.get("settings") or {}).get("clients") or []
    if not clients:
        continue
    first = clients[0] or {}
    uuid = str(first.get("id") or "")
    if not uuid:
        continue
    settings = inbound.get("settings") or {}
    print("tag=" + str(inbound.get("tag") or "vless-in"))
    print("listen=" + str(inbound.get("listen") or "127.0.0.1"))
    print("port=" + str(inbound.get("port") or ""))
    print("uuid=" + uuid)
    print("network=" + network)
    print("security=" + str(stream.get("security") or "none").lower())
    print("encryption=" + str(settings.get("decryption") or "none").lower())
    print("inbound_b64=" + b64_json(inbound))
    print("stream_b64=" + b64_json(stream))
    print("client_b64=" + b64_json(first))
    sys.exit(0)

sys.exit(1)
PY
}

apply_imported_vless_settings() {
  local key value
  while IFS='=' read -r key value; do
    case "$key" in
      tag) EXISTING_VLESS_TAG=$value ;;
      listen)
        EXISTING_XRAY_LISTEN=$value
        if [[ -z "$XRAY_LISTEN_INPUT" ]]; then
          XRAY_LISTEN=$value
        fi
        ;;
      port)
        EXISTING_XRAY_PORT=$value
        if [[ -z "$XRAY_PORT_INPUT" ]]; then
          XRAY_PORT=$value
        fi
        ;;
      uuid)
        EXISTING_VLESS_UUID=$value
        if [[ -z "$VLESS_UUID_INPUT" ]]; then
          VLESS_UUID=$value
        fi
        ;;
      network) VLESS_NETWORK=$value ;;
      security) VLESS_SECURITY=$value ;;
      encryption) VLESS_ENCRYPTION=$value ;;
      inbound_b64) XRAY_INBOUND_B64=$value ;;
      stream_b64) XRAY_STREAM_SETTINGS_B64=$value ;;
      client_b64) XRAY_CLIENT_B64=$value ;;
    esac
  done <<<"$1"
}

validate_imported_overrides() {
  if [[ "$EXISTING_XRAY_IMPORTED" != "1" ]]; then
    return 0
  fi
  if [[ -n "$XRAY_LISTEN_INPUT" && "$XRAY_LISTEN" != "$EXISTING_XRAY_LISTEN" ]]; then
    die "XRAY_LISTEN=$XRAY_LISTEN conflicts with preserved Xray inbound listen $EXISTING_XRAY_LISTEN; unset XRAY_LISTEN or use XRAY_CONFIG_MODE=replace"
  fi
  if [[ -n "$XRAY_PORT_INPUT" && "$XRAY_PORT" != "$EXISTING_XRAY_PORT" ]]; then
    die "XRAY_PORT=$XRAY_PORT conflicts with preserved Xray inbound port $EXISTING_XRAY_PORT; unset XRAY_PORT or use XRAY_CONFIG_MODE=replace"
  fi
  if [[ -n "$VLESS_UUID_INPUT" && "$VLESS_UUID" != "$EXISTING_VLESS_UUID" ]]; then
    die "VLESS_UUID conflicts with preserved Xray client id; unset VLESS_UUID or use XRAY_CONFIG_MODE=replace"
  fi
}

discover_existing_xray() {
  section "Existing Xray/VLESS discovery"
  case "$XRAY_CONFIG_MODE" in
    auto|preserve|replace) ;;
    *) die "XRAY_CONFIG_MODE must be auto, preserve, or replace" ;;
  esac

  find_existing_xray_service
  if [[ -n "$EXISTING_XRAY_UNIT" ]]; then
    extract_xray_service_config "$EXISTING_XRAY_UNIT"
  fi
  if [[ -n "$EXISTING_XRAY_BINARY" && -z "$XRAY_BINARY_INPUT" ]]; then
    XRAY_BINARY=$EXISTING_XRAY_BINARY
  elif ! is_dry_run && command -v xray >/dev/null 2>&1 && [[ -z "$XRAY_BINARY_INPUT" ]]; then
    XRAY_BINARY=$(command -v xray)
    EXISTING_XRAY_BINARY=$XRAY_BINARY
  fi

  if [[ "$XRAY_CONFIG_MODE" == "replace" ]]; then
    USE_EXISTING_XRAY_CONFIG=0
    USE_EXISTING_XRAY_SERVICE=0
    warn "XRAY_CONFIG_MODE=replace selected; existing Xray config/unit may be overwritten after confirmation"
    return
  fi

  local config_path imported
  if ! python_json_available; then
    warn "python3 is unavailable; existing Xray JSON configs cannot be parsed before install"
  fi
  while IFS= read -r config_path; do
    [[ -n "$config_path" ]] || continue
    if [[ -f "$(read_path "$config_path")" ]]; then
      EXISTING_XRAY_CONFIG_FOUND=1
    fi
    imported=$(extract_vless_from_config "$config_path" 2>/dev/null || true)
    if [[ -n "$imported" ]]; then
      EXISTING_XRAY_IMPORTED=1
      EXISTING_XRAY_CONFIG=$config_path
      XRAY_CONFIG=$config_path
      USE_EXISTING_XRAY_CONFIG=1
      apply_imported_vless_settings "$imported"
      validate_imported_overrides
      ok "imported existing VLESS inbound from $config_path"
      break
    fi
  done < <(candidate_xray_configs | awk 'NF && !seen[$0]++')

  if [[ "$XRAY_CONFIG_MODE" == "preserve" && "$EXISTING_XRAY_IMPORTED" != "1" ]]; then
    die "XRAY_CONFIG_MODE=preserve requires an existing VLESS TCP inbound"
  fi
  if [[ "$EXISTING_XRAY_IMPORTED" != "1" && ( "$EXISTING_XRAY_SERVICE" == "1" || "$EXISTING_XRAY_CONFIG_FOUND" == "1" ) ]]; then
    die "existing Xray was detected, but no VLESS TCP inbound was imported; use XRAY_CONFIG_MODE=preserve with a valid config or XRAY_CONFIG_MODE=replace to overwrite explicitly"
  fi
  if [[ "$EXISTING_XRAY_IMPORTED" != "1" ]]; then
    info "no existing VLESS TCP inbound imported; a managed Xray config will be rendered"
  fi
}

print_tui_config() {
  tui_clear
  printf '%sVK TURN VLESS Linux installer%s\n' "$BOLD" "$RESET"
  printf '%sClean Debian/Ubuntu VPS bootstrap. Values marked generated are created by this script.%s\n\n' "$DIM" "$RESET"
  printf '%sMode%s\n' "$BOLD" "$RESET"
  printf '  install mode:          %s\n' "$(is_dry_run && echo dry-run || echo real install)"
  printf '  Xray config mode:      %s\n' "$XRAY_CONFIG_MODE"
  printf '  existing Xray import:  %s\n' "$(bool_label "$EXISTING_XRAY_IMPORTED")"
  if [[ "$EXISTING_XRAY_IMPORTED" == "1" ]]; then
    printf '  existing Xray config:  %s\n' "$EXISTING_XRAY_CONFIG"
    printf '  existing VLESS tag:    %s\n' "$EXISTING_VLESS_TAG"
  fi
  printf '  repo root:             %s\n\n' "$ROOT_DIR"
  printf '%sGenerated credentials%s\n' "$BOLD" "$RESET"
  printf '  r) VLESS UUID:         %s\n\n' "$VLESS_UUID"
  printf '%sServer%s\n' "$BOLD" "$RESET"
  printf '  1) public IP/DNS:      %s\n' "$(value_or_empty "$SERVER_PUBLIC_ADDR")"
  printf '  2) UDP peer port:      %s\n' "$VKTURN_UDP_PORT"
  printf '  3) Xray listen:        %s\n' "$XRAY_LISTEN"
  printf '  4) Xray TCP port:      %s\n' "$XRAY_PORT"
  printf '  5) status API port:    %s\n\n' "$VKTURN_STATUS_PORT"
  printf '%sLocal client and v2RayA%s\n' "$BOLD" "$RESET"
  printf '  6) local listen host:  %s\n' "$LOCAL_VLESS_LISTEN"
  printf '  7) local listen port:  %s\n' "$LOCAL_VLESS_PORT"
  printf '  8) VK call link:       %s\n\n' "$(value_or_empty "$VK_LINK")"
  printf '%sInstall actions%s\n' "$BOLD" "$RESET"
  printf '  9) host firewall:      %s\n' "$(bool_label "$CONFIGURE_FIREWALL")"
  printf ' 10) start services:     %s\n' "$(bool_label "$START_SERVICES")"
  printf '     update Xray:        %s\n' "$(bool_label "$UPDATE_XRAY")"
  printf '     restart Xray:       %s\n\n' "$(bool_label "$RESTART_XRAY")"
  printf 'Select 1-10 to edit, r to regenerate UUID, s to start, q to quit.\n'
}

edit_tui_field() {
  local configure_fw start_svcs
  case "$1" in
    1) ask_value SERVER_PUBLIC_ADDR "Public server IP or DNS for local client" "$SERVER_PUBLIC_ADDR" ;;
    2) ask_value VKTURN_UDP_PORT "VK TURN UDP sidecar port" "$VKTURN_UDP_PORT" ;;
    3) ask_value XRAY_LISTEN "Xray VLESS listen address on VPS" "$XRAY_LISTEN" ;;
    4) ask_value XRAY_PORT "Xray VLESS TCP port on VPS" "$XRAY_PORT" ;;
    5) ask_value VKTURN_STATUS_PORT "Loopback status API port" "$VKTURN_STATUS_PORT" ;;
    6) ask_value LOCAL_VLESS_LISTEN "Local v2RayA/VLESS address" "$LOCAL_VLESS_LISTEN" ;;
    7) ask_value LOCAL_VLESS_PORT "Local v2RayA/VLESS port" "$LOCAL_VLESS_PORT" ;;
    8) ask_value VK_LINK "Optional VK call link for printed local command" "$VK_LINK" ;;
    9)
      ask_yes_no configure_fw "Allow $VKTURN_UDP_PORT/udp in active host firewall if ufw/firewalld is active" "$(yes_no_default "$CONFIGURE_FIREWALL")"
      CONFIGURE_FIREWALL=$configure_fw
      ;;
    10)
      ask_yes_no start_svcs "Start $XRAY_SERVICE_NAME and enable/start vkturn-server.service" "$(yes_no_default "$START_SERVICES")"
      START_SERVICES=$start_svcs
      ;;
    *) warn "unknown menu item: $1"; tui_pause ;;
  esac
}

ensure_xray_listen_is_safe() {
  if [[ "$XRAY_LISTEN" == 127.* || "$XRAY_LISTEN" == "localhost" || "$XRAY_LISTEN" == "::1" ]]; then
    return 0
  fi
  if [[ "$VLESS_SECURITY" != "none" ]]; then
    warn "Xray listen address is non-loopback; imported VLESS security is $VLESS_SECURITY"
    return 0
  fi
  if [[ "$USE_EXISTING_XRAY_CONFIG" == "1" ]]; then
    warn "preserving existing Xray security=none on non-loopback $XRAY_LISTEN; installer will not widen exposure"
    return 0
  fi
  warn "Xray security=none should stay loopback-only for a managed test inbound"
  if ! is_interactive; then
    [[ "${ALLOW_PUBLIC_XRAY:-0}" == "1" ]] || { warn "set ALLOW_PUBLIC_XRAY=1 to allow non-loopback Xray listen address in non-interactive mode"; return 1; }
    return 0
  fi
  local expose=0
  ask_yes_no expose "Continue with non-loopback Xray listen address $XRAY_LISTEN" "n"
  [[ "$expose" == "1" ]]
}

run_interactive_menu() {
  local choice
  while true; do
    print_tui_config
    read -r -p "Choice: " choice || true
    case "$choice" in
      1|2|3|4|5|6|7|8|9|10)
        edit_tui_field "$choice"
        ;;
      r|R)
        VLESS_UUID=
        VLESS_UUID=$(generate_uuid)
        ;;
      s|S)
        if validate_inputs && ensure_xray_listen_is_safe; then
          return 0
        fi
        tui_pause
        ;;
      q|Q)
        die "installation cancelled"
        ;;
      *)
        warn "unknown choice: $choice"
        tui_pause
        ;;
    esac
  done
}

valid_port() {
  [[ "$1" =~ ^[0-9]+$ ]] && ((10#$1 >= 1 && 10#$1 <= 65535))
}

valid_host() {
  [[ "$1" =~ ^[A-Za-z0-9._:-]+$ ]]
}

valid_uuid() {
  [[ "$1" =~ ^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$ ]]
}

validate_inputs() {
  valid_port "$VKTURN_UDP_PORT" || { warn "invalid VK TURN UDP port: $VKTURN_UDP_PORT"; return 1; }
  valid_port "$XRAY_PORT" || { warn "invalid Xray port: $XRAY_PORT"; return 1; }
  valid_port "$VKTURN_STATUS_PORT" || { warn "invalid status API port: $VKTURN_STATUS_PORT"; return 1; }
  valid_port "$LOCAL_VLESS_PORT" || { warn "invalid local VLESS port: $LOCAL_VLESS_PORT"; return 1; }
  valid_host "$XRAY_LISTEN" || { warn "invalid Xray listen address: $XRAY_LISTEN"; return 1; }
  valid_host "$LOCAL_VLESS_LISTEN" || { warn "invalid local VLESS listen address: $LOCAL_VLESS_LISTEN"; return 1; }
  if [[ -z "$SERVER_PUBLIC_ADDR" || "$SERVER_PUBLIC_ADDR" == "<SERVER_PUBLIC_IP>" ]]; then
    warn "server public IP/DNS is required for the local client profile"
    return 1
  fi
  valid_host "$SERVER_PUBLIC_ADDR" || { warn "invalid server public address: $SERVER_PUBLIC_ADDR"; return 1; }
  valid_uuid "$VLESS_UUID" || { warn "invalid generated VLESS UUID: $VLESS_UUID"; return 1; }
  [[ "$XRAY_SERVICE_NAME" =~ ^[A-Za-z0-9_.@-]+\.service$ ]] || { warn "invalid Xray systemd service name: $XRAY_SERVICE_NAME"; return 1; }
  [[ "$VLESS_NETWORK" == "tcp" ]] || { warn "only VLESS network=tcp is supported by vk-turn-proxy sidecar; got $VLESS_NETWORK"; return 1; }
  if [[ "$USE_EXISTING_XRAY_CONFIG" == "1" ]]; then
    [[ "$XRAY_LISTEN" == "$EXISTING_XRAY_LISTEN" ]] || { warn "preserved Xray listen cannot be changed from $EXISTING_XRAY_LISTEN to $XRAY_LISTEN"; return 1; }
    [[ "$XRAY_PORT" == "$EXISTING_XRAY_PORT" ]] || { warn "preserved Xray port cannot be changed from $EXISTING_XRAY_PORT to $XRAY_PORT"; return 1; }
    [[ "$VLESS_UUID" == "$EXISTING_VLESS_UUID" ]] || { warn "preserved Xray UUID cannot be changed from imported client id"; return 1; }
  fi
  [[ "$VKTURN_STATUS_PORT" != "$XRAY_PORT" ]] || { warn "status API port and Xray port must differ"; return 1; }
  if [[ "$SERVICE_USER" != "root" ]]; then
    ((10#$VKTURN_UDP_PORT >= 1024)) || { warn "VK TURN UDP port $VKTURN_UDP_PORT requires root; choose >=1024 or set SERVICE_USER=root"; return 1; }
    ((10#$XRAY_PORT >= 1024)) || { warn "Xray port $XRAY_PORT requires root; choose >=1024 or set SERVICE_USER=root"; return 1; }
    ((10#$VKTURN_STATUS_PORT >= 1024)) || { warn "status API port $VKTURN_STATUS_PORT requires root; choose >=1024 or set SERVICE_USER=root"; return 1; }
  fi
}

generate_uuid() {
  if [[ -n "${VLESS_UUID:-}" ]]; then
    printf '%s' "$VLESS_UUID"
    return
  fi
  if [[ -r /proc/sys/kernel/random/uuid ]]; then
    tr '[:upper:]' '[:lower:]' </proc/sys/kernel/random/uuid
    return
  fi
  if command -v uuidgen >/dev/null 2>&1; then
    uuidgen | tr '[:upper:]' '[:lower:]'
    return
  fi
  die "cannot generate UUID; /proc random UUID and uuidgen are unavailable"
}

detect_public_addr() {
  if [[ -n "$DEFAULT_SERVER_PUBLIC_ADDR" ]]; then
    printf '%s' "$DEFAULT_SERVER_PUBLIC_ADDR"
    return
  fi
  if command -v curl >/dev/null 2>&1; then
    local detected
    detected=$(curl -4fsS --max-time 4 https://api.ipify.org 2>/dev/null || true)
    if [[ -n "$detected" && "$detected" =~ ^[0-9.]+$ ]]; then
      printf '%s' "$detected"
      return
    fi
  fi
  printf '<SERVER_PUBLIC_IP>'
}

resolve_service_identity() {
  if ! getent passwd "$SERVICE_USER" >/dev/null 2>&1; then
    SERVICE_USER=nobody
  fi
  if ! getent group "$SERVICE_GROUP" >/dev/null 2>&1; then
    if getent group nobody >/dev/null 2>&1; then
      SERVICE_GROUP=nobody
    else
      SERVICE_GROUP=$(id -gn "$SERVICE_USER" 2>/dev/null || echo root)
    fi
  fi
}

cpu_arch() {
  case "$(uname -m)" in
    x86_64|amd64) printf 'amd64' ;;
    aarch64|arm64) printf 'arm64' ;;
    *) die "unsupported CPU architecture: $(uname -m). Supported: amd64, arm64" ;;
  esac
}

go_sha256() {
  local arch=$1
  case "$GO_VERSION:$arch" in
    1.25.5:amd64) printf '9e9b755d63b36acf30c12a9a3fc379243714c1c6d3dd72861da637f336ebb35b' ;;
    1.25.5:arm64) printf 'b00b694903d126c588c378e72d3545549935d3982635ba3f7a964c9fa23fe3b9' ;;
    *) die "no pinned Go checksum for go$GO_VERSION linux-$arch; update this installer before running" ;;
  esac
}

xray_asset_name() {
  local arch=$1
  case "$arch" in
    amd64) printf 'Xray-linux-64.zip' ;;
    arm64) printf 'Xray-linux-arm64-v8a.zip' ;;
    *) die "unsupported Xray architecture: $arch" ;;
  esac
}

version_ge() {
  local have=$1
  local want=$2
  [[ "$(printf '%s\n%s\n' "$want" "$have" | sort -V | head -n1)" == "$want" ]]
}

current_go_version() {
  if command -v go >/dev/null 2>&1; then
    go env GOVERSION 2>/dev/null | sed 's/^go//'
  elif [[ -x /usr/local/go/bin/go ]]; then
    /usr/local/go/bin/go env GOVERSION 2>/dev/null | sed 's/^go//'
  fi
}

select_go_binary() {
  if [[ -n "$GO_BINARY" ]]; then
    printf '%s' "$GO_BINARY"
    return
  fi
  if command -v go >/dev/null 2>&1; then
    command -v go
  elif [[ -x /usr/local/go/bin/go ]]; then
    printf '/usr/local/go/bin/go'
  else
    die "Go is not installed"
  fi
}

ensure_can_overwrite() {
  local path=$1
  if is_dry_run; then
    return
  fi
  if [[ ! -e "$path" || "$FORCE" == "1" ]]; then
    return
  fi
  if ! is_interactive; then
    die "managed path exists: $path; rerun interactively or set FORCE=1 to overwrite"
  fi
  local yes=0
  ask_yes_no yes "Managed path exists: $path. Overwrite it" "n"
  [[ "$yes" == "1" ]] || die "refusing to overwrite $path"
}

preflight_existing_paths() {
  local path
  for path in "$SERVER_BINARY" "$VKTURN_CONFIG" "$VKTURN_PROFILE" "$VKTURN_UNIT" "$SUMMARY_PATH"; do
    ensure_can_overwrite "$path"
  done
  if [[ "$USE_EXISTING_XRAY_CONFIG" != "1" ]]; then
    ensure_can_overwrite "$XRAY_CONFIG"
  fi
  if [[ "$USE_EXISTING_XRAY_SERVICE" != "1" ]]; then
    ensure_can_overwrite "$XRAY_UNIT"
  fi
}

install_packages() {
  section "System packages"
  if ! is_dry_run; then
    command -v systemctl >/dev/null 2>&1 || die "systemctl is required; use a systemd-based VPS image"
    if ! command -v apt-get >/dev/null 2>&1; then
      die "this installer currently supports Debian/Ubuntu with apt-get"
    fi
  fi
  run apt-get update
  run env DEBIAN_FRONTEND=noninteractive apt-get install -y ca-certificates curl unzip tar gzip openssl iproute2 lsof python3
}

install_go_if_needed() {
  section "Go toolchain"
  local arch have tarball sha tmp
  arch=$(cpu_arch)
  have=$(current_go_version || true)
  if [[ -n "$have" ]] && version_ge "$have" "$GO_VERSION"; then
    GO_BINARY=$(select_go_binary)
    ok "Go $have is available"
    return
  fi

  info "install Go $GO_VERSION for linux-$arch"
  tarball="go${GO_VERSION}.linux-${arch}.tar.gz"
  sha=$(go_sha256 "$arch")
  if is_dry_run; then
    run curl -fL -o "/tmp/$tarball" "https://go.dev/dl/$tarball"
    run bash -lc "printf '%s  %s\\n' '$sha' '/tmp/$tarball' | sha256sum -c -"
    run rm -rf /usr/local/go
    run tar -C /usr/local -xzf "/tmp/$tarball"
    run ln -sf /usr/local/go/bin/go /usr/local/bin/go
    GO_BINARY=/usr/local/go/bin/go
    return
  fi

  if [[ -e /usr/local/go ]]; then
    if [[ "$FORCE" == "1" ]]; then
      mv /usr/local/go "/usr/local/go.bak.$(date +%Y%m%d%H%M%S)"
    else
      die "/usr/local/go already exists and current Go is too old; rerun with FORCE=1 to move it aside"
    fi
  fi
  tmp=$(mktemp -d)
  curl -fL -o "$tmp/$tarball" "https://go.dev/dl/$tarball"
  printf '%s  %s\n' "$sha" "$tmp/$tarball" | sha256sum -c -
  tar -C /usr/local -xzf "$tmp/$tarball"
  ln -sf /usr/local/go/bin/go /usr/local/bin/go
  hash -r
  GO_BINARY=/usr/local/go/bin/go
  rm -rf "$tmp"
  ok "installed $(/usr/local/go/bin/go env GOVERSION)"
}

install_xray_binary() {
  section "Xray core"
  if ! is_truthy "$UPDATE_XRAY"; then
    if [[ -x "$(read_path "$XRAY_BINARY")" ]]; then
      XRAY_MANAGED_ASSETS=0
      ok "Xray update disabled; preserving existing binary: $XRAY_BINARY"
      return
    fi
    if ! is_dry_run && command -v xray >/dev/null 2>&1 && [[ -z "$XRAY_BINARY_INPUT" ]]; then
      XRAY_BINARY=$(command -v xray)
      XRAY_MANAGED_ASSETS=0
      ok "Xray update disabled; preserving existing binary: $XRAY_BINARY"
      return
    fi
    warn "UPDATE_XRAY=0 but no Xray binary was found at $XRAY_BINARY; installing latest release"
  fi

  local arch asset tmp expected
  arch=$(cpu_arch)
  asset=$(xray_asset_name "$arch")
  info "install Xray from $asset with SHA2-256 verification"
  if is_dry_run; then
    run curl -fL -o "/tmp/$asset" "$XRAY_RELEASE_BASE/$asset"
    run curl -fL -o "/tmp/$asset.dgst" "$XRAY_RELEASE_BASE/$asset.dgst"
    run bash -lc "awk -F'= ' '/^SHA2-256=/{print \$2 \"  /tmp/$asset\"; exit}' '/tmp/$asset.dgst' | sha256sum -c -"
    run unzip -q "/tmp/$asset" -d /tmp/xray
    run install -m 0755 /tmp/xray/xray "$XRAY_BINARY"
    run install -m 0644 /tmp/xray/geoip.dat "$XRAY_ASSET_DIR/geoip.dat"
    run install -m 0644 /tmp/xray/geosite.dat "$XRAY_ASSET_DIR/geosite.dat"
    write_file "$XRAY_BINARY" 0755 "dry-run placeholder for xray\n"
    write_file "$XRAY_ASSET_DIR/geoip.dat" 0644 "dry-run placeholder for geoip.dat\n"
    write_file "$XRAY_ASSET_DIR/geosite.dat" 0644 "dry-run placeholder for geosite.dat\n"
    XRAY_MANAGED_ASSETS=1
    return
  fi

  tmp=$(mktemp -d)
  curl -fL -o "$tmp/$asset" "$XRAY_RELEASE_BASE/$asset"
  curl -fL -o "$tmp/$asset.dgst" "$XRAY_RELEASE_BASE/$asset.dgst"
  expected=$(awk -F'= ' '/^SHA2-256=/{print $2; exit}' "$tmp/$asset.dgst")
  [[ -n "$expected" ]] || die "cannot parse SHA2-256 from $asset.dgst"
  printf '%s  %s\n' "$expected" "$tmp/$asset" | sha256sum -c -
  unzip -q "$tmp/$asset" -d "$tmp/xray"
  install -d -m 0755 "$(dirname "$XRAY_BINARY")" "$XRAY_ASSET_DIR"
  backup_existing_path "$XRAY_BINARY"
  backup_existing_path "$XRAY_ASSET_DIR/geoip.dat"
  backup_existing_path "$XRAY_ASSET_DIR/geosite.dat"
  install -m 0755 "$tmp/xray/xray" "$XRAY_BINARY"
  install -m 0644 "$tmp/xray/geoip.dat" "$XRAY_ASSET_DIR/geoip.dat"
  install -m 0644 "$tmp/xray/geosite.dat" "$XRAY_ASSET_DIR/geosite.dat"
  XRAY_MANAGED_ASSETS=1
  rm -rf "$tmp"
  ok "installed Xray: $($XRAY_BINARY version | head -n1)"
}

xray_asset_environment() {
  if [[ "$XRAY_MANAGED_ASSETS" == "1" ]]; then
    printf 'Environment=XRAY_LOCATION_ASSET=%s\n' "$XRAY_ASSET_DIR"
  fi
}

xray_backend_host() {
  case "$XRAY_LISTEN" in
    0.0.0.0|::|\[::\]) printf '127.0.0.1' ;;
    *) printf '%s' "$XRAY_LISTEN" ;;
  esac
}

render_xray_config() {
  cat <<EOF
{
  "log": {
    "loglevel": "warning",
    "access": "$XRAY_LOG_DIR/access.log",
    "error": "$XRAY_LOG_DIR/error.log"
  },
  "inbounds": [
    {
      "tag": "vless-in",
      "listen": "$XRAY_LISTEN",
      "port": $XRAY_PORT,
      "protocol": "vless",
      "settings": {
        "clients": [
          {
            "id": "$VLESS_UUID",
            "level": 0,
            "email": "vkturn-test"
          }
        ],
        "decryption": "none"
      },
      "streamSettings": {
        "network": "tcp",
        "security": "none"
      }
    }
  ],
  "outbounds": [
    {
      "tag": "direct",
      "protocol": "freedom",
      "settings": {
        "domainStrategy": "UseIPv4"
      }
    }
  ]
}
EOF
}

render_xray_unit() {
  cat <<EOF
[Unit]
Description=Xray Service for VK TURN VLESS test
Documentation=https://github.com/XTLS/Xray-core
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=$SERVICE_USER
Group=$SERVICE_GROUP
$(xray_asset_environment)
ExecStart=$XRAY_BINARY run -config $XRAY_CONFIG
Restart=on-failure
RestartSec=5s
LimitNOFILE=1048576
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=full
ProtectHome=true
ReadWritePaths=$XRAY_LOG_DIR

[Install]
WantedBy=multi-user.target
EOF
}

render_vkturn_config() {
  local backend_host
  backend_host=$(xray_backend_host)
  cat <<EOF
{
  "listen_addr": "0.0.0.0:$VKTURN_UDP_PORT",
  "connect_addr": "$backend_host:$XRAY_PORT",
  "vless_mode": true,
  "check_backend": true,
  "backend_network": "tcp",
  "status_api_addr": "127.0.0.1:$VKTURN_STATUS_PORT",
  "log_level": "info",
  "service_name": "vkturn-server"
}
EOF
}

render_vkturn_unit() {
  cat <<EOF
[Unit]
Description=VK TURN VLESS Sidecar
After=network-online.target $XRAY_SERVICE_NAME
Wants=network-online.target $XRAY_SERVICE_NAME

[Service]
Type=simple
Environment=VKTURN_CONFIG=$VKTURN_CONFIG
Environment=VKTURN_LOG_DIR=$VKTURN_LOG_DIR
ExecStart=$SERVER_BINARY -config $VKTURN_CONFIG
Restart=on-failure
RestartSec=5s
User=$SERVICE_USER
Group=$SERVICE_GROUP
RuntimeDirectory=vkturn
StateDirectory=vkturn
LogsDirectory=vk-turn-proxy
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=full
ProtectHome=true
ReadWritePaths=$VKTURN_LOG_DIR
StandardOutput=append:$VKTURN_LOG_DIR/server.log
StandardError=append:$VKTURN_LOG_DIR/server.err

[Install]
WantedBy=multi-user.target
EOF
}

local_vless_uri() {
  printf 'vless://%s@%s:%s?encryption=%s&security=%s&type=%s#vkturn-local-sidecar' \
    "$VLESS_UUID" "$LOCAL_VLESS_LISTEN" "$LOCAL_VLESS_PORT" "$VLESS_ENCRYPTION" "$VLESS_SECURITY" "$VLESS_NETWORK"
}

local_client_command() {
  local peer vk_link_display
  peer="$SERVER_PUBLIC_ADDR:$VKTURN_UDP_PORT"
  vk_link_display=${VK_LINK:-<VK_LINK>}
  printf './dev/bin/vk-turn-proxy-client -peer %s -vk-link %s -listen %s -vless' \
    "$(printf '%q' "$peer")" "$(printf '%q' "$vk_link_display")" "$(printf '%q' "$LOCAL_VLESS_LISTEN:$LOCAL_VLESS_PORT")"
}

render_profile() {
  local peer uri local_command
  peer="$SERVER_PUBLIC_ADDR:$VKTURN_UDP_PORT"
  uri=$(local_vless_uri)
  local_command=$(local_client_command)
  cat <<EOF
XRAY_CONFIG_MODE=$(printf '%q' "$XRAY_CONFIG_MODE")
XRAY_SERVICE_NAME=$(printf '%q' "$XRAY_SERVICE_NAME")
XRAY_BINARY=$(printf '%q' "$XRAY_BINARY")
XRAY_CONFIG=$(printf '%q' "$XRAY_CONFIG")
EXISTING_XRAY_IMPORTED=$(printf '%q' "$EXISTING_XRAY_IMPORTED")
EXISTING_XRAY_CONFIG=$(printf '%q' "$EXISTING_XRAY_CONFIG")
EXISTING_VLESS_TAG=$(printf '%q' "$EXISTING_VLESS_TAG")
VLESS_UUID=$(printf '%q' "$VLESS_UUID")
VLESS_ENCRYPTION=$(printf '%q' "$VLESS_ENCRYPTION")
VLESS_NETWORK=$(printf '%q' "$VLESS_NETWORK")
VLESS_SECURITY=$(printf '%q' "$VLESS_SECURITY")
XRAY_INBOUND_B64=$(printf '%q' "$XRAY_INBOUND_B64")
XRAY_STREAM_SETTINGS_B64=$(printf '%q' "$XRAY_STREAM_SETTINGS_B64")
XRAY_CLIENT_B64=$(printf '%q' "$XRAY_CLIENT_B64")
VKTURN_PEER=$(printf '%q' "$peer")
VKTURN_UDP_PORT=$(printf '%q' "$VKTURN_UDP_PORT")
XRAY_BACKEND=$(printf '%q' "$(xray_backend_host):$XRAY_PORT")
VKTURN_STATUS_URL=$(printf '%q' "http://127.0.0.1:$VKTURN_STATUS_PORT/health")
LOCAL_VLESS_ENDPOINT=$(printf '%q' "$LOCAL_VLESS_LISTEN:$LOCAL_VLESS_PORT")
LOCAL_VLESS_URI=$(printf '%q' "$uri")
LOCAL_CLIENT_COMMAND=$(printf '%q' "$local_command")
EOF
}

render_codex_handoff() {
  local peer uri local_command
  peer="$SERVER_PUBLIC_ADDR:$VKTURN_UDP_PORT"
  uri=$(local_vless_uri)
  local_command=$(local_client_command)
  cat <<EOF
----- CODEX HANDOFF BEGIN -----
SERVER_PUBLIC_ADDR=$(printf '%q' "$SERVER_PUBLIC_ADDR")
VKTURN_PEER=$(printf '%q' "$peer")
VKTURN_UDP_PORT=$(printf '%q' "$VKTURN_UDP_PORT")
XRAY_CONFIG_MODE=$(printf '%q' "$XRAY_CONFIG_MODE")
XRAY_SERVICE_NAME=$(printf '%q' "$XRAY_SERVICE_NAME")
XRAY_BINARY=$(printf '%q' "$XRAY_BINARY")
XRAY_CONFIG=$(printf '%q' "$XRAY_CONFIG")
EXISTING_XRAY_IMPORTED=$(printf '%q' "$EXISTING_XRAY_IMPORTED")
EXISTING_XRAY_CONFIG=$(printf '%q' "$EXISTING_XRAY_CONFIG")
EXISTING_VLESS_TAG=$(printf '%q' "$EXISTING_VLESS_TAG")
VLESS_UUID=$(printf '%q' "$VLESS_UUID")
VLESS_ENCRYPTION=$(printf '%q' "$VLESS_ENCRYPTION")
VLESS_NETWORK=$(printf '%q' "$VLESS_NETWORK")
VLESS_SECURITY=$(printf '%q' "$VLESS_SECURITY")
XRAY_INBOUND_B64=$(printf '%q' "$XRAY_INBOUND_B64")
XRAY_STREAM_SETTINGS_B64=$(printf '%q' "$XRAY_STREAM_SETTINGS_B64")
XRAY_CLIENT_B64=$(printf '%q' "$XRAY_CLIENT_B64")
LOCAL_VLESS_ENDPOINT=$(printf '%q' "$LOCAL_VLESS_LISTEN:$LOCAL_VLESS_PORT")
LOCAL_VLESS_URI=$(printf '%q' "$uri")
LOCAL_CLIENT_COMMAND=$(printf '%q' "$local_command")
V2RAYA_ADDRESS=$(printf '%q' "$LOCAL_VLESS_LISTEN")
V2RAYA_PORT=$(printf '%q' "$LOCAL_VLESS_PORT")
V2RAYA_UUID=$(printf '%q' "$VLESS_UUID")
V2RAYA_NETWORK=$(printf '%q' "$VLESS_NETWORK")
V2RAYA_SECURITY=$(printf '%q' "$VLESS_SECURITY")
V2RAYA_ENCRYPTION=$(printf '%q' "$VLESS_ENCRYPTION")
SERVER_HEALTH_URL=$(printf '%q' "http://127.0.0.1:$VKTURN_STATUS_PORT/health")
SERVER_HEALTH_COMMAND=$(printf '%q' "curl -fsS http://127.0.0.1:$VKTURN_STATUS_PORT/health")
SERVER_STATUS_COMMAND=$(printf '%q' "systemctl --no-pager status $XRAY_SERVICE_NAME vkturn-server.service")
SERVER_LOG_COMMAND=$(printf '%q' "journalctl -u $XRAY_SERVICE_NAME -u vkturn-server.service --no-pager -n 120")
SERVER_UDP_LISTEN_COMMAND=$(printf '%q' "ss -lunp | grep ':$VKTURN_UDP_PORT'")
SERVER_SUMMARY_PATH=$(printf '%q' "$SUMMARY_PATH")
----- CODEX HANDOFF END -----
EOF
}

render_summary() {
  local peer uri local_command
  peer="$SERVER_PUBLIC_ADDR:$VKTURN_UDP_PORT"
  uri=$(local_vless_uri)
  local_command=$(local_client_command)
  cat <<EOF
VK TURN VLESS server install result
Generated at: $(date -u +%Y-%m-%dT%H:%M:%SZ)

Server side:
  UDP peer: $peer
  vkturn config: $VKTURN_CONFIG
  Xray service: $XRAY_SERVICE_NAME
  Xray binary: $XRAY_BINARY
  xray config: $XRAY_CONFIG
  existing Xray imported: $EXISTING_XRAY_IMPORTED
  existing VLESS tag: ${EXISTING_VLESS_TAG:-<none>}
  backup dir: $BACKUP_DIR/$(backup_stamp)
  status API: http://127.0.0.1:$VKTURN_STATUS_PORT/health
  logs: journalctl -u vkturn-server.service -u $XRAY_SERVICE_NAME --no-pager -n 100

VLESS credentials for local v2RayA outbound:
  UUID: $VLESS_UUID
  encryption: $VLESS_ENCRYPTION
  network: $VLESS_NETWORK
  security: $VLESS_SECURITY
  selected inbound base64 JSON: ${XRAY_INBOUND_B64:-<managed-by-installer>}
  streamSettings base64 JSON: ${XRAY_STREAM_SETTINGS_B64:-<managed-by-installer>}
  client base64 JSON: ${XRAY_CLIENT_B64:-<managed-by-installer>}

Local client sidecar on your Linux workstation:
  go build -o ./dev/bin/vk-turn-proxy-client ./client
  $local_command

v2RayA local node:
  address: $LOCAL_VLESS_LISTEN
  port: $LOCAL_VLESS_PORT
  UUID: $VLESS_UUID
  encryption: $VLESS_ENCRYPTION
  network: $VLESS_NETWORK
  security: $VLESS_SECURITY
  import URI: $uri

Notes:
  The VPS Xray inbound listens on $XRAY_LISTEN:$XRAY_PORT and is intended for local sidecar traffic only.
  If security is not none, use the base64 streamSettings/client data above to mirror exact TLS/REALITY settings in v2RayA.
  Open the UDP peer port $VKTURN_UDP_PORT/udp in the provider firewall/security group if the VPS provider has one.

Copy this block back to Codex for local client testing:

$(render_codex_handoff)
EOF
}

write_configs() {
  section "Render configs and units"
  if [[ "$USE_EXISTING_XRAY_CONFIG" == "1" ]]; then
    info "preserve existing Xray config: $XRAY_CONFIG"
  else
    ensure_dir "$(dirname "$XRAY_CONFIG")" 0750
    ensure_dir "$XRAY_LOG_DIR" 0750
    write_file "$XRAY_CONFIG" 0640 "$(render_xray_config)"$'\n'
  fi
  ensure_dir "$(dirname "$VKTURN_CONFIG")" 0750
  ensure_dir "$INSTALL_DIR" 0755
  ensure_dir "$VKTURN_LOG_DIR" 0750

  if [[ "$USE_EXISTING_XRAY_SERVICE" == "1" ]]; then
    info "preserve existing Xray systemd unit: $XRAY_SERVICE_NAME"
  else
    write_file "$XRAY_UNIT" 0644 "$(render_xray_unit)"$'\n'
  fi
  write_file "$VKTURN_CONFIG" 0640 "$(render_vkturn_config)"$'\n'
  write_file "$VKTURN_UNIT" 0644 "$(render_vkturn_unit)"$'\n'
  write_file "$VKTURN_PROFILE" 0600 "$(render_profile)"

  if [[ "$USE_EXISTING_XRAY_CONFIG" != "1" ]]; then
    chown_one "root:$SERVICE_GROUP" "$(dirname "$XRAY_CONFIG")"
    chown_one "root:$SERVICE_GROUP" "$XRAY_CONFIG"
    chown_path "$SERVICE_USER:$SERVICE_GROUP" "$XRAY_LOG_DIR"
  fi
  chown_one "root:$SERVICE_GROUP" "$(dirname "$VKTURN_CONFIG")"
  chown_one "root:$SERVICE_GROUP" "$VKTURN_CONFIG"
  chown_path "$SERVICE_USER:$SERVICE_GROUP" "$VKTURN_LOG_DIR"
}

build_vkturn_server() {
  section "Build vk-turn-proxy server"
  local go_bin
  go_bin=$(select_go_binary)
  if is_dry_run; then
    run env CGO_ENABLED=0 "$go_bin" build -trimpath -ldflags=-s\ -w -o "$SERVER_BINARY" ./server
    write_file "$SERVER_BINARY" 0755 "dry-run placeholder for vk-turn-proxy-server\n"
    return
  fi
  cd "$ROOT_DIR"
  env CGO_ENABLED=0 "$go_bin" build -trimpath -ldflags="-s -w" -o "$SERVER_BINARY" ./server
  chmod 0755 "$SERVER_BINARY"
  ok "built $SERVER_BINARY"
}

configure_firewall() {
  section "Firewall"
  if [[ "$CONFIGURE_FIREWALL" != "1" ]]; then
    warn "firewall configuration skipped by operator"
    return
  fi
  if command -v ufw >/dev/null 2>&1 && ufw status 2>/dev/null | grep -qi '^Status: active'; then
    run ufw allow "$VKTURN_UDP_PORT/udp" comment 'vk-turn-proxy UDP sidecar'
    ok "allowed $VKTURN_UDP_PORT/udp in ufw"
    return
  fi
  if command -v firewall-cmd >/dev/null 2>&1 && systemctl is-active --quiet firewalld 2>/dev/null; then
    run firewall-cmd --permanent --add-port="$VKTURN_UDP_PORT/udp"
    run firewall-cmd --reload
    ok "allowed $VKTURN_UDP_PORT/udp in firewalld"
    return
  fi
  warn "no active ufw/firewalld detected; open $VKTURN_UDP_PORT/udp in the provider firewall if needed"
}

service_health_or_fail() {
  local unit=$1
  if is_dry_run; then
    return
  fi
  if systemctl is-active --quiet "$unit"; then
    ok "$unit is active"
    return
  fi
  journalctl -u "$unit" --no-pager -n 80 >&2 || true
  die "$unit failed to start"
}

start_and_verify_services() {
  section "Start services"
  if [[ "$START_SERVICES" != "1" ]]; then
    warn "service start skipped by operator"
    return
  fi
  run systemctl daemon-reload
  if [[ "$USE_EXISTING_XRAY_SERVICE" == "1" ]]; then
    if is_truthy "$RESTART_XRAY"; then
      run systemctl restart "$XRAY_SERVICE_NAME"
      service_health_or_fail "$XRAY_SERVICE_NAME"
    else
      warn "Xray restart skipped by operator; updated binary will apply after the next $XRAY_SERVICE_NAME restart"
      service_health_or_fail "$XRAY_SERVICE_NAME"
    fi
  else
    run systemctl enable --now "$XRAY_SERVICE_NAME"
    service_health_or_fail "$XRAY_SERVICE_NAME"
  fi

  local backend_host
  backend_host=$(xray_backend_host)
  if ! is_dry_run; then
    timeout 5 bash -c ": >/dev/tcp/$backend_host/$XRAY_PORT" || die "Xray TCP backend is not reachable at $backend_host:$XRAY_PORT"
    ok "Xray backend accepts TCP at $backend_host:$XRAY_PORT"
  fi

  run systemctl enable --now vkturn-server.service
  service_health_or_fail vkturn-server.service
  if ! is_dry_run; then
    curl -fsS "http://127.0.0.1:$VKTURN_STATUS_PORT/health" >/dev/null || die "vkturn status API is not healthy"
    ok "vkturn status API is healthy"
    ss -lun | grep -q ":$VKTURN_UDP_PORT" || warn "UDP listener was not visible in ss output; check journalctl if clients cannot connect"
  fi
}

write_summary() {
  section "Install result"
  local summary
  summary=$(render_summary)
  write_file "$SUMMARY_PATH" 0600 "$summary"$'\n'
  printf '%s\n' "$summary"
  ok "saved install result to $(target_path "$SUMMARY_PATH")"
}

collect_inputs() {
  section "Interactive setup"
  if is_interactive; then
    run_interactive_menu
  else
    validate_inputs || die "invalid non-interactive installer inputs"
    ensure_xray_listen_is_safe || die "unsafe Xray listen address: $XRAY_LISTEN"
  fi
}

confirm_plan() {
  section "Plan"
  cat <<EOF
Repo root:          $ROOT_DIR
Mode:               $(is_dry_run && echo dry-run || echo real install)
Public peer:        $SERVER_PUBLIC_ADDR:$VKTURN_UDP_PORT/udp
Xray inbound:       $XRAY_LISTEN:$XRAY_PORT/tcp
VLESS UUID:         $VLESS_UUID
Status API:         http://127.0.0.1:$VKTURN_STATUS_PORT/health
Local v2RayA node:  $LOCAL_VLESS_LISTEN:$LOCAL_VLESS_PORT $VLESS_NETWORK security=$VLESS_SECURITY
Server binary:      $SERVER_BINARY
Xray mode:          $XRAY_CONFIG_MODE imported=$EXISTING_XRAY_IMPORTED update=$(bool_label "$UPDATE_XRAY") restart=$(bool_label "$RESTART_XRAY")
Xray service:       $XRAY_SERVICE_NAME
Xray binary:        $XRAY_BINARY
Xray config:        $XRAY_CONFIG ($( [[ "$USE_EXISTING_XRAY_CONFIG" == "1" ]] && echo preserved || echo managed ))
Managed configs:    $VKTURN_CONFIG, $VKTURN_PROFILE, $VKTURN_UNIT
EOF
  local yes=0
  ask_yes_no yes "Proceed with this installation" "y"
  [[ "$yes" == "1" ]] || die "installation cancelled"
}

main() {
  printf '%sVK TURN VLESS Linux installer%s\n' "$BOLD" "$RESET"
  printf '%sInteractive server bootstrap for a clean Debian/Ubuntu VPS.%s\n' "$DIM" "$RESET"
  require_linux
  require_root_for_real_install
  init_dry_run_root
  resolve_service_identity
  load_default_inputs
  discover_existing_xray
  collect_inputs
  confirm_plan
  preflight_existing_paths
  install_packages
  install_go_if_needed
  install_xray_binary
  write_configs
  build_vkturn_server
  configure_firewall
  start_and_verify_services
  write_summary
  ok "installation flow completed"
}

main "$@"
