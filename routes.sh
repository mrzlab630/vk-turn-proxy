#!/bin/bash
set -euo pipefail

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

gateway="${VKTURN_ROUTE_GATEWAY:-}"
if [[ -z "${gateway}" ]]; then
  gateway="$(ip -o -4 route show to default | awk '/via/ {print $3}' | head -1)"
fi
if [[ -z "${gateway}" ]]; then
  echo "Could not determine default gateway" >&2
  exit 1
fi

ip_cmd=(ip)
if [[ "${EUID:-$(id -u)}" -ne 0 ]]; then
  ip_cmd=(sudo ip)
fi

while IFS= read -r remote; do
  remote="${remote%$'\r'}"
  [[ -z "$remote" ]] && continue

  if [[ ! "$remote" =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}$ ]]; then
    echo "Skipping unexpected input: $remote" >&2
    continue
  fi

  echo "Ensuring route to $remote via $gateway"
  if [[ "$dry_run" -eq 1 ]]; then
    printf 'DRY RUN:'
    printf ' %q' "${ip_cmd[@]}" route replace "$remote" via "$gateway"
    printf '\n'
  else
    "${ip_cmd[@]}" route replace "$remote" via "$gateway"
  fi
done
