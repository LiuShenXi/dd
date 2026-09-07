#!/bin/sh
set -eu

task_label='rolling-reset-20260907'
network_name='rolling-reset-app-net'
ingress_network_name='rolling-reset-ingress-net'
postgres_volume='rolling-reset-app-pgdata'
app_volume='rolling-reset-app-data'

remove_container() {
  resource_name=$1
  if ! docker container inspect "$resource_name" >/dev/null 2>&1; then return; fi
  label=$(docker inspect --format '{{ index .Config.Labels "com.codex.local-task" }}' "$resource_name")
  if [ "$label" != "$task_label" ]; then
    echo "refusing to remove relabeled container $resource_name" >&2
    exit 1
  fi
  docker rm -f -v "$resource_name" >/dev/null
}

remove_container rolling-reset-ingress
remove_container rolling-reset-app
remove_container rolling-reset-app-redis
remove_container rolling-reset-app-postgres

if docker network inspect "$network_name" >/dev/null 2>&1; then
  label=$(docker network inspect --format '{{ index .Labels "com.codex.local-task" }}' "$network_name")
  if [ "$label" != "$task_label" ]; then
    echo "refusing to remove relabeled network $network_name" >&2
    exit 1
  fi
  docker network rm "$network_name" >/dev/null
fi

if docker network inspect "$ingress_network_name" >/dev/null 2>&1; then
  label=$(docker network inspect --format '{{ index .Labels "com.codex.local-task" }}' "$ingress_network_name")
  if [ "$label" != "$task_label" ]; then
    echo "refusing to remove relabeled network $ingress_network_name" >&2
    exit 1
  fi
  docker network rm "$ingress_network_name" >/dev/null
fi

if [ "${ROLLING_RESET_PURGE:-0}" = 1 ]; then
  for volume_name in "$postgres_volume" "$app_volume"; do
    if ! docker volume inspect "$volume_name" >/dev/null 2>&1; then continue; fi
    label=$(docker volume inspect --format '{{ index .Labels "com.codex.local-task" }}' "$volume_name")
    if [ "$label" != "$task_label" ]; then
      echo "refusing to remove relabeled volume $volume_name" >&2
      exit 1
    fi
    docker volume rm "$volume_name" >/dev/null
  done
fi

echo 'ROLLING_RESET_APP_STOPPED'
