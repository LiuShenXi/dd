#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
# shellcheck source=common.sh
source "$SCRIPT_DIR/common.sh"

target="${1:?usage: switch-slot.sh <blue|green>}"
require_slot "$target"
wait_for_slot "$target"

tmp_conf="$(mktemp "${ROUTER_CONF}.XXXXXX")"
backup_conf="$(mktemp "${ROUTER_CONF}.backup.XXXXXX")"
cleanup() { rm -f "$tmp_conf" "$backup_conf"; }
trap cleanup EXIT

render_router_config "$target" "$tmp_conf"
cp "$ROUTER_CONF" "$backup_conf"
install -m 0644 "$tmp_conf" "$ROUTER_CONF"

if ! docker exec "$ROUTER_CONTAINER" nginx -t; then
  install -m 0644 "$backup_conf" "$ROUTER_CONF"
  docker exec "$ROUTER_CONTAINER" nginx -s reload || true
  echo "router config validation failed; previous route restored" >&2
  exit 1
fi

docker exec "$ROUTER_CONTAINER" nginx -s reload
if ! verify_stable_route; then
  install -m 0644 "$backup_conf" "$ROUTER_CONF"
  docker exec "$ROUTER_CONTAINER" nginx -t
  docker exec "$ROUTER_CONTAINER" nginx -s reload
  echo "stable route verification failed; previous route restored" >&2
  exit 1
fi

write_active_slot "$target"
printf 'active_slot=%s\n' "$target"
