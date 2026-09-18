#!/usr/bin/env bash
set -Eeuo pipefail

DEPLOY_ROOT="${DEPLOY_ROOT:-/home/linuxuser/apps/sub2api}"
BACKUP_ROOT="${BACKUP_ROOT:-$DEPLOY_ROOT/backups/automated}"
RETENTION_DAYS="${RETENTION_DAYS:-14}"
LOCK_FILE="${BACKUP_LOCK_FILE:-$DEPLOY_ROOT/run/backup.lock}"
ENV_FILE="${ENV_FILE:-$DEPLOY_ROOT/.env}"
SLOT_IMAGES_FILE="${SLOT_IMAGES_FILE:-$DEPLOY_ROOT/blue-green/slot-images.env}"

mkdir -p "$BACKUP_ROOT" "$(dirname "$LOCK_FILE")"
exec 9>"$LOCK_FILE"
flock -n 9 || { echo "another Sub2API backup is running" >&2; exit 1; }

stamp="$(date -u +%Y%m%dT%H%M%SZ)"
staging="$(mktemp -d "$BACKUP_ROOT/.${stamp}.XXXXXX")"
final="$BACKUP_ROOT/$stamp"
trap 'rm -rf "$staging"' EXIT

docker exec sub2api-postgres sh -lc \
  'exec pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc' \
  > "$staging/postgres.dump"
test -s "$staging/postgres.dump"

docker cp "$staging/postgres.dump" sub2api-postgres:/tmp/sub2api-backup-check.dump >/dev/null
docker exec sub2api-postgres pg_restore -l /tmp/sub2api-backup-check.dump >/dev/null
docker exec sub2api-postgres rm -f /tmp/sub2api-backup-check.dump

docker exec sub2api-redis sh -lc '
  if [ -n "${REDISCLI_AUTH:-}" ]; then
    exec redis-cli SAVE
  fi
  unset REDISCLI_AUTH
  exec redis-cli SAVE
' >/dev/null
docker cp sub2api-redis:/data/dump.rdb "$staging/redis-dump.rdb" >/dev/null
tar -C "$DEPLOY_ROOT" --exclude='data/logs' -czf "$staging/app-data.tar.gz" data
cp "$DEPLOY_ROOT/docker-compose.local.yml" "$staging/docker-compose.local.yml"
cp "$ENV_FILE" "$staging/runtime.env"
tar -C "$DEPLOY_ROOT" -czf "$staging/blue-green-deployment.tar.gz" blue-green
if [ -f "$SLOT_IMAGES_FILE" ]; then
  cp "$SLOT_IMAGES_FILE" "$staging/slot-images.env"
fi

chmod 600 "$staging"/*
(
  cd "$staging"
  files=(app-data.tar.gz blue-green-deployment.tar.gz docker-compose.local.yml postgres.dump redis-dump.rdb runtime.env)
  if [ -f slot-images.env ]; then
    files+=(slot-images.env)
  fi
  sha256sum "${files[@]}" > SHA256SUMS
  sha256sum -c SHA256SUMS >/dev/null
)
chmod 600 "$staging/SHA256SUMS"
mv "$staging" "$final"
trap - EXIT

find "$BACKUP_ROOT" -mindepth 1 -maxdepth 1 -type d \
  -name '20??????T??????Z' -mtime "+$RETENTION_DAYS" -exec rm -rf -- {} +

printf 'backup=%s\n' "$final"
du -sh "$final"
