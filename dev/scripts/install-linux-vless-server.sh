#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
ROOT_DIR=$(cd "$SCRIPT_DIR/../.." && pwd)

GO_VERSION=${GO_VERSION:-$(awk '/^go / { print $2; exit }' "$ROOT_DIR/go.mod")}
XRAY_RELEASE_BASE=${XRAY_RELEASE_BASE:-https://github.com/XTLS/Xray-core/releases/latest/download}

DEFAULT_SERVER_PUBLIC_ADDR=${SERVER_PUBLIC_ADDR:-}
DEFAULT_VKTURN_UDP_PORT=${VKTURN_UDP_PORT:-56000}
DEFAULT_XRAY_LISTEN=${XRAY_LISTEN:-127.0.0.1}
DEFAULT_XRAY_PORT=${XRAY_PORT:-10001}
DEFAULT_STATUS_PORT=${VKTURN_STATUS_PORT:-18081}
DEFAULT_LOCAL_LISTEN=${LOCAL_VLESS_LISTEN:-127.0.0.1}
DEFAULT_LOCAL_PORT=${LOCAL_VLESS_PORT:-9000}
DEFAULT_VK_LINK=${VK_LINK:-}

DRY_RUN=${DRY_RUN:-0}
NONINTERACTIVE=${NONINTERACTIVE:-0}
ASSUME_YES=${ASSUME_YES:-0}
FORCE=${FORCE:-0}
START_SERVICES=${START_SERVICES:-1}
CONFIGURE_FIREWALL=${CONFIGURE_FIREWALL:-1}
DRY_RUN_ROOT=${DRY_RUN_ROOT:-}

SERVICE_USER=${SERVICE_USER:-nobody}
SERVICE_GROUP=${SERVICE_GROUP:-nogroup}
INSTALL_DIR=${INSTALL_DIR:-/opt/vk-turn-proxy}
SERVER_BINARY=${SERVER_BINARY:-$INSTALL_DIR/vk-turn-proxy-server}
VKTURN_CONFIG=${VKTURN_CONFIG:-/etc/vkturn/server.json}
VKTURN_PROFILE=${VKTURN_PROFILE:-/etc/vkturn/client-profile.env}
VKTURN_UNIT=${VKTURN_UNIT:-/etc/systemd/system/vkturn-server.service}
VKTURN_LOG_DIR=${VKTURN_LOG_DIR:-/var/log/vk-turn-proxy}
XRAY_BINARY=${XRAY_BINARY:-/usr/local/bin/xray}
XRAY_CONFIG=${XRAY_CONFIG:-/etc/xray/config.json}
XRAY_UNIT=${XRAY_UNIT:-/etc/systemd/system/xray.service}
XRAY_ASSET_DIR=${XRAY_ASSET_DIR:-/usr/local/share/xray}
XRAY_MANAGED_ASSETS=${XRAY_MANAGED_ASSETS:-0}
XRAY_LOG_DIR=${XRAY_LOG_DIR:-/var/log/xray}
SUMMARY_PATH=${SUMMARY_PATH:-/root/vkturn-install-result.txt}
GO_BINARY=${GO_BINARY:-}

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
  if [[ "$1" == "1" ]]; then
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

print_tui_config() {
  tui_clear
  printf '%sVK TURN VLESS Linux installer%s\n' "$BOLD" "$RESET"
  printf '%sClean Debian/Ubuntu VPS bootstrap. Values marked generated are created by this script.%s\n\n' "$DIM" "$RESET"
  printf '%sMode%s\n' "$BOLD" "$RESET"
  printf '  install mode:          %s\n' "$(is_dry_run && echo dry-run || echo real install)"
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
  printf ' 10) start services:     %s\n\n' "$(bool_label "$START_SERVICES")"
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
      ask_yes_no start_svcs "Enable and start xray.service and vkturn-server.service" "$(yes_no_default "$START_SERVICES")"
      START_SERVICES=$start_svcs
      ;;
    *) warn "unknown menu item: $1"; tui_pause ;;
  esac
}

ensure_xray_listen_is_safe() {
  if [[ "$XRAY_LISTEN" == 127.* || "$XRAY_LISTEN" == "localhost" || "$XRAY_LISTEN" == "::1" ]]; then
    return 0
  fi
  warn "Xray security=none should stay loopback-only for this test path"
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
  for path in "$SERVER_BINARY" "$VKTURN_CONFIG" "$VKTURN_PROFILE" "$VKTURN_UNIT" "$XRAY_CONFIG" "$XRAY_UNIT" "$SUMMARY_PATH"; do
    ensure_can_overwrite "$path"
  done
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
  run env DEBIAN_FRONTEND=noninteractive apt-get install -y ca-certificates curl unzip tar gzip openssl iproute2 lsof
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
  if command -v xray >/dev/null 2>&1 && [[ "$FORCE" != "1" ]]; then
    XRAY_BINARY=$(command -v xray)
    XRAY_MANAGED_ASSETS=0
    ok "Xray binary is available: $XRAY_BINARY"
    return
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
    0.0.0.0|::) printf '127.0.0.1' ;;
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
After=network-online.target xray.service
Wants=network-online.target xray.service

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

render_profile() {
  local peer uri vk_link_display local_command
  peer="$SERVER_PUBLIC_ADDR:$VKTURN_UDP_PORT"
  uri="vless://$VLESS_UUID@$LOCAL_VLESS_LISTEN:$LOCAL_VLESS_PORT?encryption=none&security=none&type=tcp#vkturn-local-sidecar"
  vk_link_display=${VK_LINK:-<VK_LINK>}
  local_command="./dev/bin/vk-turn-proxy-client -peer $(printf '%q' "$peer") -vk-link $(printf '%q' "$vk_link_display") -listen $(printf '%q' "$LOCAL_VLESS_LISTEN:$LOCAL_VLESS_PORT") -vless"
  cat <<EOF
VLESS_UUID=$(printf '%q' "$VLESS_UUID")
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
  local peer uri vk_link_display local_command
  peer="$SERVER_PUBLIC_ADDR:$VKTURN_UDP_PORT"
  uri="vless://$VLESS_UUID@$LOCAL_VLESS_LISTEN:$LOCAL_VLESS_PORT?encryption=none&security=none&type=tcp#vkturn-local-sidecar"
  vk_link_display=${VK_LINK:-<VK_LINK>}
  local_command="./dev/bin/vk-turn-proxy-client -peer $(printf '%q' "$peer") -vk-link $(printf '%q' "$vk_link_display") -listen $(printf '%q' "$LOCAL_VLESS_LISTEN:$LOCAL_VLESS_PORT") -vless"
  cat <<EOF
----- CODEX HANDOFF BEGIN -----
SERVER_PUBLIC_ADDR=$(printf '%q' "$SERVER_PUBLIC_ADDR")
VKTURN_PEER=$(printf '%q' "$peer")
VKTURN_UDP_PORT=$(printf '%q' "$VKTURN_UDP_PORT")
VLESS_UUID=$(printf '%q' "$VLESS_UUID")
LOCAL_VLESS_ENDPOINT=$(printf '%q' "$LOCAL_VLESS_LISTEN:$LOCAL_VLESS_PORT")
LOCAL_VLESS_URI=$(printf '%q' "$uri")
LOCAL_CLIENT_COMMAND=$(printf '%q' "$local_command")
V2RAYA_ADDRESS=$(printf '%q' "$LOCAL_VLESS_LISTEN")
V2RAYA_PORT=$(printf '%q' "$LOCAL_VLESS_PORT")
V2RAYA_UUID=$(printf '%q' "$VLESS_UUID")
V2RAYA_NETWORK=tcp
V2RAYA_SECURITY=none
V2RAYA_ENCRYPTION=none
SERVER_HEALTH_URL=$(printf '%q' "http://127.0.0.1:$VKTURN_STATUS_PORT/health")
SERVER_HEALTH_COMMAND=$(printf '%q' "curl -fsS http://127.0.0.1:$VKTURN_STATUS_PORT/health")
SERVER_STATUS_COMMAND=$(printf '%q' "systemctl --no-pager status xray.service vkturn-server.service")
SERVER_LOG_COMMAND=$(printf '%q' "journalctl -u xray -u vkturn-server --no-pager -n 120")
SERVER_UDP_LISTEN_COMMAND=$(printf '%q' "ss -lunp | grep ':$VKTURN_UDP_PORT'")
SERVER_SUMMARY_PATH=$(printf '%q' "$SUMMARY_PATH")
----- CODEX HANDOFF END -----
EOF
}

render_summary() {
  local peer uri vk_link_display local_command
  peer="$SERVER_PUBLIC_ADDR:$VKTURN_UDP_PORT"
  uri="vless://$VLESS_UUID@$LOCAL_VLESS_LISTEN:$LOCAL_VLESS_PORT?encryption=none&security=none&type=tcp#vkturn-local-sidecar"
  vk_link_display=${VK_LINK:-<VK_LINK>}
  local_command="./dev/bin/vk-turn-proxy-client -peer $(printf '%q' "$peer") -vk-link $(printf '%q' "$vk_link_display") -listen $(printf '%q' "$LOCAL_VLESS_LISTEN:$LOCAL_VLESS_PORT") -vless"
  cat <<EOF
VK TURN VLESS server install result
Generated at: $(date -u +%Y-%m-%dT%H:%M:%SZ)

Server side:
  UDP peer: $peer
  vkturn config: $VKTURN_CONFIG
  xray config: $XRAY_CONFIG
  status API: http://127.0.0.1:$VKTURN_STATUS_PORT/health
  logs: journalctl -u vkturn-server -u xray --no-pager -n 100

Generated VLESS credentials:
  UUID: $VLESS_UUID
  encryption: none
  network: tcp
  security: none

Local client sidecar on your Linux workstation:
  go build -o ./dev/bin/vk-turn-proxy-client ./client
  $local_command

v2RayA local node:
  address: $LOCAL_VLESS_LISTEN
  port: $LOCAL_VLESS_PORT
  UUID: $VLESS_UUID
  encryption: none
  network: tcp
  security: none
  import URI: $uri

Notes:
  The VPS Xray inbound listens on $XRAY_LISTEN:$XRAY_PORT and is intended for local sidecar traffic only.
  Open the UDP peer port $VKTURN_UDP_PORT/udp in the provider firewall/security group if the VPS provider has one.

Copy this block back to Codex for local client testing:

$(render_codex_handoff)
EOF
}

write_configs() {
  section "Render configs and units"
  ensure_dir "$(dirname "$XRAY_CONFIG")" 0750
  ensure_dir "$XRAY_LOG_DIR" 0750
  ensure_dir "$(dirname "$VKTURN_CONFIG")" 0750
  ensure_dir "$INSTALL_DIR" 0755
  ensure_dir "$VKTURN_LOG_DIR" 0750

  write_file "$XRAY_CONFIG" 0640 "$(render_xray_config)"$'\n'
  write_file "$XRAY_UNIT" 0644 "$(render_xray_unit)"$'\n'
  write_file "$VKTURN_CONFIG" 0640 "$(render_vkturn_config)"$'\n'
  write_file "$VKTURN_UNIT" 0644 "$(render_vkturn_unit)"$'\n'
  write_file "$VKTURN_PROFILE" 0600 "$(render_profile)"

  chown_one "root:$SERVICE_GROUP" "$(dirname "$XRAY_CONFIG")"
  chown_one "root:$SERVICE_GROUP" "$XRAY_CONFIG"
  chown_one "root:$SERVICE_GROUP" "$(dirname "$VKTURN_CONFIG")"
  chown_one "root:$SERVICE_GROUP" "$VKTURN_CONFIG"
  chown_path "$SERVICE_USER:$SERVICE_GROUP" "$XRAY_LOG_DIR"
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
  run systemctl enable --now xray.service
  service_health_or_fail xray.service

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
  load_default_inputs
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
Local v2RayA node:  $LOCAL_VLESS_LISTEN:$LOCAL_VLESS_PORT tcp security=none
Server binary:      $SERVER_BINARY
Managed configs:    $XRAY_CONFIG, $VKTURN_CONFIG
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
