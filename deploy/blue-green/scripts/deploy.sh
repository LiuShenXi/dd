#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
# shellcheck source=common.sh
source "$SCRIPT_DIR/common.sh"

IMAGE_REF="${1:-weishaw/sub2api:latest}"
DEPLOY_LOCK_FILE="${DEPLOY_LOCK_FILE:-$DEPLOY_ROOT/run/deploy.lock}"
DRAIN_MAX_SECONDS="${DRAIN_MAX_SECONDS:-900}"

mkdir -p "$(dirname "$DEPLOY_LOCK_FILE")"
exec 9>"$DEPLOY_LOCK_FILE"
flock -n 9 || { echo "another Sub2API deployment is running" >&2; exit 1; }

available_kb="$(awk '/MemAvailable:/ {print $2}' /proc/meminfo)"
available_disk_kb="$(df -Pk "$DEPLOY_ROOT" | awk 'NR==2 {print $4}')"
if [ "$available_kb" -lt 716800 ]; then
  echo "less than 700MiB memory is available; deployment aborted" >&2
  exit 1
fi
if [ "$available_disk_kb" -lt 2097152 ]; then
  echo "less than 2GiB disk is available; deployment aborted" >&2
  exit 1
fi

"$SCRIPT_DIR/backup.sh"
docker pull "$IMAGE_REF"
resolved_image="$(docker image inspect "$IMAGE_REF" --format '{{index .RepoDigests 0}}')"
test -n "$resolved_image"

active="$(read_active_slot)"
if [ "$active" = blue ]; then
  target=green
else
  target=blue
fi
target_service="$(slot_service "$target")"
old_service="$(slot_service "$active")"
image_key="SUB2API_${target^^}_IMAGE"

tmp_images="$(mktemp "${SLOT_IMAGES_FILE}.XXXXXX")"
awk -F= -v key="$image_key" -v value="$resolved_image" '
  BEGIN { found=0 }
  $1 == key { print key "=" value; found=1; next }
  { print }
  END { if (!found) print key "=" value }
' "$SLOT_IMAGES_FILE" > "$tmp_images"
chmod 600 "$tmp_images"
mv "$tmp_images" "$SLOT_IMAGES_FILE"

compose config -q
if ! compose up -d --no-deps --force-recreate "$target_service"; then
  compose stop "$target_service" || true
  exit 1
fi
if ! wait_for_slot "$target"; then
  compose stop "$target_service" || true
  exit 1
fi

"$SCRIPT_DIR/switch-slot.sh" "$target"

deadline=$((SECONDS + DRAIN_MAX_SECONDS))
while [ "$SECONDS" -lt "$deadline" ]; do
  if ! docker exec "$ROUTER_CONTAINER" ps 2>/dev/null | grep -q 'worker process is shutting down'; then
    break
  fi
  sleep 5
done

compose stop --timeout 30 "$old_service"
verify_stable_route
printf 'deployed=%s active_slot=%s stopped_slot=%s\n' "$resolved_image" "$target" "$active"
