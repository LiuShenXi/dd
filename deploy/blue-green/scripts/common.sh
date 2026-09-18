#!/usr/bin/env bash

DEPLOY_ROOT="${DEPLOY_ROOT:-/home/linuxuser/apps/sub2api}"
BLUE_GREEN_ROOT="${BLUE_GREEN_ROOT:-$DEPLOY_ROOT/blue-green}"
COMPOSE_FILE="${COMPOSE_FILE:-$BLUE_GREEN_ROOT/docker-compose.yml}"
ENV_FILE="${ENV_FILE:-$DEPLOY_ROOT/.env}"
SLOT_IMAGES_FILE="${SLOT_IMAGES_FILE:-$BLUE_GREEN_ROOT/slot-images.env}"
ACTIVE_SLOT_FILE="${ACTIVE_SLOT_FILE:-$DEPLOY_ROOT/run/active-slot}"
ROUTER_CONF="${ROUTER_CONF:-$BLUE_GREEN_ROOT/router/conf.d/default.conf}"
ROUTER_CONTAINER="${ROUTER_CONTAINER:-sub2api-router}"
NETWORK_NAME="${SUB2API_NETWORK_NAME:-sub2api_sub2api-network}"
RELEASE_GUARD="${RELEASE_GUARD:-$BLUE_GREEN_ROOT/scripts/release-guard.py}"

compose() {
  # Validate merged settings before any command can recreate a slot.
  local resolved
  resolved="$(COMPOSE_IGNORE_ORPHANS=true docker compose \
    --env-file "$ENV_FILE" --env-file "$SLOT_IMAGES_FILE" \
    -f "$COMPOSE_FILE" config --format json)" || return 1
  python3 "$RELEASE_GUARD" compose-config <<< "$resolved" >/dev/null || return 1
  COMPOSE_IGNORE_ORPHANS=true docker compose \
    --env-file "$ENV_FILE" \
    --env-file "$SLOT_IMAGES_FILE" \
    -f "$COMPOSE_FILE" "$@"
}

require_slot() {
  case "$1" in
    blue|green) ;;
    *) echo "invalid slot: $1" >&2; return 2 ;;
  esac
}

slot_service() {
  printf 'sub2api-%s\n' "$1"
}

slot_port() {
  if [ "$1" = blue ]; then
    printf '%s\n' "${SUB2API_BLUE_PORT:-18080}"
  else
    printf '%s\n' "${SUB2API_GREEN_PORT:-28080}"
  fi
}

read_active_slot() {
  local slot=blue
  if [ -f "$ACTIVE_SLOT_FILE" ]; then
    slot="$(tr -d '[:space:]' < "$ACTIVE_SLOT_FILE")"
  fi
  require_slot "$slot"
  printf '%s\n' "$slot"
}

write_active_slot() {
  local slot="$1" tmp
  require_slot "$slot"
  mkdir -p "$(dirname "$ACTIVE_SLOT_FILE")"
  tmp="$(mktemp "${ACTIVE_SLOT_FILE}.XXXXXX")"
  printf '%s\n' "$slot" > "$tmp"
  mv "$tmp" "$ACTIVE_SLOT_FILE"
}

wait_for_slot() {
  local slot="$1" port attempt image
  require_slot "$slot"
  port="$(slot_port "$slot")"
  image="$(awk -F= -v key="SUB2API_${slot^^}_IMAGE" '$1 == key {print $2}' "$SLOT_IMAGES_FILE")"
  test -n "$image" || { echo "slot image is missing" >&2; return 1; }
  python3 "$RELEASE_GUARD" container "$(slot_service "$slot")" --image "$image" >/dev/null || return 1
  for attempt in $(seq 1 45); do
    if python3 "$RELEASE_GUARD" probe "http://127.0.0.1:${port}" >/dev/null; then
      return 0
    fi
    sleep 2
  done
  echo "slot $slot failed health check on port $port" >&2
  return 1
}

verify_stable_route() {
  python3 "$RELEASE_GUARD" probe http://127.0.0.1:8080 >/dev/null || return 1
  python3 "$RELEASE_GUARD" probe https://sub2api.monasapi.com >/dev/null || return 1
  # The new production host has no new-api container. The router and public
  # probes above require /health=200 and unauthenticated /v1/models=401.
}

render_router_config() {
  local slot="$1" output="$2"
  require_slot "$slot"
  cat > "$output" <<EOF
map \$http_upgrade \$connection_upgrade {
    default upgrade;
    ''      '';
}

map \$http_x_forwarded_proto \$forwarded_proto {
    default \$http_x_forwarded_proto;
    ''      \$scheme;
}

map \$http_x_real_ip \$forwarded_real_ip {
    default \$http_x_real_ip;
    ''      \$remote_addr;
}

upstream sub2api_active {
    server sub2api-${slot}:8080 max_fails=1 fail_timeout=5s;
    keepalive 64;
}

server {
    listen 8080;
    server_name _;

    client_max_body_size 256m;

    location / {
        proxy_pass http://sub2api_active;
        proxy_http_version 1.1;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$forwarded_real_ip;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$forwarded_proto;
        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection \$connection_upgrade;
        proxy_buffering off;
        proxy_request_buffering off;
        proxy_read_timeout 1200s;
        proxy_send_timeout 1200s;
    }
}
EOF
}
