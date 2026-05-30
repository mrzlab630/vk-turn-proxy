#!/bin/zsh
set -u

dry_run=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    --dry-run)
      dry_run=1
      shift
      ;;
    -h|--help)
      echo "Usage: $0 [--dry-run] < remote-ip-list" >&2
      exit 0
      ;;
    *)
      echo "Unexpected argument: $1" >&2
      echo "Usage: $0 [--dry-run] < remote-ip-list" >&2
      exit 2
      ;;
  esac
done

default_info="${VKTURN_ROUTE_DEFAULT_INFO:-}"
if [[ -z "${default_info:-}" ]]; then
  default_info="$(route -n get default 2>/dev/null || true)"
fi
default_if="$(printf '%s\n' "$default_info" | awk '/interface:/{print $2; exit}')"
gateway="$(printf '%s\n' "$default_info" | awk '/gateway:/{print $2; exit}')"

if [[ -n "${default_if:-}" && "$default_if" == utun* ]]; then
  echo "Default route is currently $default_if. Disconnect WireGuard/VPN first." >&2
  exit 1
fi

if [[ -z "${gateway:-}" ]]; then
  echo "Could not determine normal default gateway." >&2
  exit 1
fi

while IFS= read -r line; do
  line="${line//$'\r'/}"

  # Try to extract:
  # - plain IPv4
  # - IPv4/CIDR
  # - relayed-address=IPv4:port  -> use only IPv4
  remote="$(printf '%s\n' "$line" | sed -nE '
    s/.*relayed-address=(([0-9]{1,3}\.){3}[0-9]{1,3}):[0-9]+.*/\1/p
    t done
    s/^(([0-9]{1,3}\.){3}[0-9]{1,3}\/[0-9]{1,2})$/\1/p
    t done
    s/^(([0-9]{1,3}\.){3}[0-9]{1,3})$/\1/p
    :done
  ')"

  [[ -z "$remote" ]] && continue

  if [[ "$remote" == */* ]]; then
    if [[ "$dry_run" -eq 1 ]]; then
      echo "DRY RUN: sudo route -n delete -net $remote"
      echo "DRY RUN: sudo route -n add -net $remote $gateway"
    else
      sudo route -n delete -net "$remote" >/dev/null 2>&1 || true
      sudo route -n add -net "$remote" "$gateway" || true
    fi
  else
    if [[ "$dry_run" -eq 1 ]]; then
      echo "DRY RUN: sudo route -n delete -host $remote"
      echo "DRY RUN: sudo route -n add -host $remote $gateway"
    else
      sudo route -n delete -host "$remote" >/dev/null 2>&1 || true
      sudo route -n add -host "$remote" "$gateway" || true
    fi
  fi
done
