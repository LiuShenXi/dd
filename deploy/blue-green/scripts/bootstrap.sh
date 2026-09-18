#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
# shellcheck source=common.sh
source "$SCRIPT_DIR/common.sh"

DEPLOY_LOCK_FILE="${DEPLOY_LOCK_FILE:-$DEPLOY_ROOT/run/deploy.lock}"
HOST_NGINX_SITE="${HOST_NGINX_SITE:-/etc/nginx/sites-available/sub2api.monasapi.com}"
DRAIN_MAX_SECONDS="${DRAIN_MAX_SECONDS:-900}"
mkdir -p "$(dirname "$DEPLOY_LOCK_FILE")"
exec 9>"$DEPLOY_LOCK_FILE"
flock -n 9 || { echo "another Sub2API deployment is running" >&2; exit 1; }

docker inspect sub2api >/dev/null
if docker inspect "$ROUTER_CONTAINER" >/dev/null 2>&1; then
  echo "$ROUTER_CONTAINER already exists; bootstrap is not applicable" >&2
  exit 1
fi

"$SCRIPT_DIR/backup.sh"
compose config -q
compose up -d --no-deps sub2api-blue
wait_for_slot blue

render_router_config blue "$ROUTER_CONF"
host_site_backup="$(mktemp)"
sudo -n cp "$HOST_NGINX_SITE" "$host_site_backup"
legacy_ip="$(docker inspect sub2api --format \
  "{{with index .NetworkSettings.Networks \"$NETWORK_NAME\"}}{{.IPAddress}}{{end}}")"
test -n "$legacy_ip"

set_host_upstream_port() {
  local from="$1" to="$2" matches
  matches="$(sudo -n grep -c "proxy_pass http://127.0.0.1:${from};" "$HOST_NGINX_SITE")"
  if [ "$matches" -ne 1 ]; then
    echo "expected one host Nginx upstream on port $from, found $matches" >&2
    return 1
  fi
  sudo -n sed -i \
    "s|proxy_pass http://127.0.0.1:${from};|proxy_pass http://127.0.0.1:${to};|" \
    "$HOST_NGINX_SITE"
  sudo -n nginx -t
  sudo -n systemctl reload nginx
}

rollback_legacy() {
  if [ "${rollback_started:-false}" = true ]; then
    return
  fi
  rollback_started=true
  set +e
  compose rm -sf router >/dev/null 2>&1 || true
  if docker inspect sub2api-legacy >/dev/null 2>&1; then
    docker rename sub2api-legacy sub2api
    docker network connect --alias sub2api "$NETWORK_NAME" sub2api >/dev/null 2>&1 || true
    docker start sub2api >/dev/null
  elif docker inspect sub2api >/dev/null 2>&1; then
    docker start sub2api >/dev/null 2>&1 || true
  fi
  sudo -n install -m 0644 "$host_site_backup" "$HOST_NGINX_SITE"
  sudo -n nginx -t >/dev/null 2>&1 && sudo -n systemctl reload nginx
  compose stop sub2api-blue >/dev/null 2>&1 || true
}

trap rollback_legacy ERR
trap 'rollback_legacy; exit 130' INT TERM
set_host_upstream_port 8080 "$(slot_port blue)"

deadline=$((SECONDS + DRAIN_MAX_SECONDS))
while [ "$SECONDS" -lt "$deadline" ]; do
  established="$(sudo -n ss -Htn state established | awk \
    -v endpoint="${legacy_ip}:8080" \
    '$4 == endpoint || $5 == endpoint { count++ } END { print count+0 }')"
  if [ "$established" -eq 0 ]; then
    break
  fi
  printf 'waiting_for_legacy_connections=%s\n' "$established"
  sleep 5
done
if [ "$established" -ne 0 ]; then
  echo "legacy connections did not drain within ${DRAIN_MAX_SECONDS}s" >&2
  exit 1
fi

docker stop --time 30 sub2api >/dev/null
docker network disconnect "$NETWORK_NAME" sub2api >/dev/null 2>&1 || true
docker rename sub2api sub2api-legacy

compose up -d --no-deps router
for _ in $(seq 1 30); do
  if verify_stable_route >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
verify_stable_route
set_host_upstream_port "$(slot_port blue)" 8080
verify_stable_route
write_active_slot blue

trap - ERR INT TERM
docker rm sub2api-legacy >/dev/null
sudo -n rm -f "$host_site_backup"
printf 'bootstrap_complete active_slot=blue\n'
