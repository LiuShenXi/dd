#!/bin/sh
set -eu
umask 077
run_dir=${1:?}
run_id=${2:?}
dump_path=${3:?}
expected_sha=${4:?}
case "$run_id" in *[!A-Za-z0-9-]*|'') exit 64 ;; esac
[ "$run_dir" = "/tmp/carpool-backup-restore-$run_id" ] || exit 64
case "$dump_path" in /home/linuxuser/apps/sub2api/production-release-[0-9-]*/production-before.dump) ;; *) exit 64 ;; esac
case "$expected_sha" in *[!0-9a-f]*|'') exit 64 ;; esac
[ "${#expected_sha}" -eq 64 ] || exit 64
[ "$(sha256sum "$dump_path" | awk '{print $1}')" = "$expected_sha" ] || { echo 'backup fingerprint mismatch' >&2; exit 1; }

task_label=production-release-backup-20260908
network_name="carpool-restore-net-$run_id"
volume_name="carpool-restore-data-$run_id"
postgres_name="carpool-restore-pg-$run_id"
network_id=''
postgres_id=''
volume_created=false
cleanup_failed=0
cleanup() {
  original_exit=$?
  trap - EXIT HUP INT TERM
  if [ -n "$postgres_id" ]; then
    owner=$(docker inspect --format '{{index .Config.Labels "com.codex.local-task"}}' "$postgres_id" 2>/dev/null || true)
    generation=$(docker inspect --format '{{index .Config.Labels "com.codex.validation-run"}}' "$postgres_id" 2>/dev/null || true)
    if [ "$owner" = "$task_label" ] && [ "$generation" = "$run_id" ]; then
      docker logs "$postgres_id" >"$run_dir/postgres.private.log" 2>&1 || true
      docker rm -f "$postgres_id" >>"$run_dir/cleanup.private.log" 2>&1 || cleanup_failed=1
    else cleanup_failed=1; fi
  fi
  if [ "$volume_created" = true ]; then
    owner=$(docker volume inspect --format '{{index .Labels "com.codex.local-task"}}' "$volume_name" 2>/dev/null || true)
    generation=$(docker volume inspect --format '{{index .Labels "com.codex.validation-run"}}' "$volume_name" 2>/dev/null || true)
    if [ "$owner" = "$task_label" ] && [ "$generation" = "$run_id" ]; then
      docker volume rm "$volume_name" >>"$run_dir/cleanup.private.log" 2>&1 || cleanup_failed=1
    else cleanup_failed=1; fi
  fi
  if [ -n "$network_id" ]; then
    owner=$(docker network inspect --format '{{index .Labels "com.codex.local-task"}}' "$network_id" 2>/dev/null || true)
    generation=$(docker network inspect --format '{{index .Labels "com.codex.validation-run"}}' "$network_id" 2>/dev/null || true)
    if [ "$owner" = "$task_label" ] && [ "$generation" = "$run_id" ]; then
      docker network rm "$network_id" >>"$run_dir/cleanup.private.log" 2>&1 || cleanup_failed=1
    else cleanup_failed=1; fi
  fi
  rm -f "$run_dir/postgres.env"
  printf 'exit=%s\ncleanup_failed=%s\n' "$original_exit" "$cleanup_failed" >"$run_dir/result"
  if [ "$cleanup_failed" -ne 0 ]; then exit 1; fi
  exit "$original_exit"
}
trap cleanup EXIT
trap 'exit 130' HUP INT TERM

postgres_image=$(docker image inspect --format '{{.Id}}' postgres:18-alpine)
if docker volume inspect "$volume_name" >/dev/null 2>&1; then echo 'refusing pre-existing volume' >&2; exit 1; fi
network_id=$(docker network create --internal --label "com.codex.local-task=$task_label" --label "com.codex.validation-run=$run_id" "$network_name")
printf 'network=%s\n' "$network_id" >"$run_dir/resources"
docker volume create --label "com.codex.local-task=$task_label" --label "com.codex.validation-run=$run_id" "$volume_name" >/dev/null
volume_created=true
printf 'volume=%s\n' "$volume_name" >>"$run_dir/resources"
password=$(openssl rand -hex 32)
printf 'POSTGRES_DB=sub2api_release_restore\nPOSTGRES_USER=carpool_restore\nPOSTGRES_PASSWORD=%s\n' "$password" >"$run_dir/postgres.env"
password=''
postgres_id=$(docker run -d --name "$postgres_name" --network "$network_id" \
  --cpus 0.5 --memory 384m --memory-swap 384m --pids-limit 64 \
  --label "com.codex.local-task=$task_label" --label "com.codex.validation-run=$run_id" \
  -v "$volume_name:/var/lib/postgresql" --env-file "$run_dir/postgres.env" "$postgres_image" \
  postgres -c shared_buffers=32MB -c work_mem=1MB -c maintenance_work_mem=32MB -c max_connections=20)
printf 'postgres=%s\n' "$postgres_id" >>"$run_dir/resources"
attempt=0
while [ "$attempt" -lt 60 ]; do
  if docker exec "$postgres_id" pg_isready -h 127.0.0.1 -U carpool_restore -d sub2api_release_restore >/dev/null 2>&1; then break; fi
  attempt=$((attempt + 1)); sleep 1
done
[ "$attempt" -lt 60 ] || { echo 'restore database readiness timed out' >&2; exit 1; }
empty_count=$(docker exec "$postgres_id" psql -U carpool_restore -d sub2api_release_restore -Atqc "SELECT count(*) FROM pg_catalog.pg_tables WHERE schemaname='public'")
[ "$empty_count" = 0 ] || { echo 'restore database is not empty' >&2; exit 1; }
docker exec -i "$postgres_id" pg_restore -U carpool_restore -d sub2api_release_restore \
  --no-owner --no-privileges --exit-on-error <"$dump_path" >"$run_dir/restore.private.log" 2>&1
counts=$(docker exec "$postgres_id" psql -U carpool_restore -d sub2api_release_restore -Atqc \
  "SELECT (SELECT count(*) FROM users WHERE deleted_at IS NULL),(SELECT count(*) FROM api_keys WHERE deleted_at IS NULL),(SELECT max(substring(filename FROM '^[0-9]+')::integer) FROM schema_migrations)")
printf '%s\n' "$counts" >"$run_dir/verified-counts"
[ "$counts" = '24|29|234' ] || { echo 'restored snapshot counts differ from recorded backup metadata' >&2; exit 1; }
[ "$(sha256sum "$dump_path" | awk '{print $1}')" = "$expected_sha" ] || { echo 'backup changed during restore' >&2; exit 1; }
printf 'sha256=%s\nusers=24\nkeys=29\nmigration=234\n' "$expected_sha" >"$run_dir/verification"
