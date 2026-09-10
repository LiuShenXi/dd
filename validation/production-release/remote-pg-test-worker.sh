#!/bin/sh
set -eu
umask 077

run_dir=${1:?private run directory required}
run_id=${2:?run ID required}
case "$run_id" in *[!A-Za-z0-9-]*|'') exit 64 ;; esac
if [ "$run_dir" != "/tmp/carpool-release-pg-$run_id" ] || [ ! -f "$run_dir/repository-tests" ]; then
  echo 'unexpected run directory or missing test binary' >&2
  exit 64
fi
chmod 700 "$run_dir"
test_pattern=$(cat "$run_dir/test-pattern")
case "$test_pattern" in ^*\$) ;; *) exit 64 ;; esac
task_label=production-release-20260908
network_name="carpool-release-net-$run_id"
postgres_name="carpool-release-pg-$run_id"
redis_name="carpool-release-redis-$run_id"
runner_name="carpool-release-runner-$run_id"
network_id=''
postgres_id=''
redis_id=''
runner_id=''
cleanup_failed=0

cleanup_container() {
  candidate=$1
  [ -n "$candidate" ] || return 0
  owner=$(docker inspect --format '{{ index .Config.Labels "com.codex.local-task" }}' "$candidate" 2>/dev/null || true)
  generation=$(docker inspect --format '{{ index .Config.Labels "com.codex.validation-run" }}' "$candidate" 2>/dev/null || true)
  if [ "$owner" = "$task_label" ] && [ "$generation" = "$run_id" ]; then
    docker logs "$candidate" >"$run_dir/$candidate.private.log" 2>&1 || true
    docker rm -f -v "$candidate" >>"$run_dir/cleanup.private.log" 2>&1 || cleanup_failed=1
  else
    cleanup_failed=1
  fi
}
cleanup() {
  original_exit=$?
  trap - EXIT HUP INT TERM
  cleanup_container "${runner_id:-$runner_name}"
  cleanup_container "${redis_id:-$redis_name}"
  cleanup_container "${postgres_id:-$postgres_name}"
  if [ -n "$network_id" ]; then
    owner=$(docker network inspect --format '{{ index .Labels "com.codex.local-task" }}' "$network_id" 2>/dev/null || true)
    generation=$(docker network inspect --format '{{ index .Labels "com.codex.validation-run" }}' "$network_id" 2>/dev/null || true)
    if [ "$owner" = "$task_label" ] && [ "$generation" = "$run_id" ]; then
      docker network rm "$network_id" >>"$run_dir/cleanup.private.log" 2>&1 || cleanup_failed=1
    else
      cleanup_failed=1
    fi
  fi
  rm -f "$run_dir/postgres.env" "$run_dir/runner.env"
  printf 'exit=%s\ncleanup_failed=%s\n' "$original_exit" "$cleanup_failed" >"$run_dir/result"
  if [ "$cleanup_failed" -ne 0 ]; then exit 1; fi
  exit "$original_exit"
}
trap cleanup EXIT
trap 'exit 130' HUP INT TERM

docker image inspect postgres:18-alpine redis:8-alpine >"$run_dir/images.private.json"
postgres_image=$(docker image inspect --format '{{.Id}}' postgres:18-alpine)
redis_image=$(docker image inspect --format '{{.Id}}' redis:8-alpine)
if [ "$(docker image inspect --format '{{.Architecture}}' "$postgres_image")" != amd64 ] || \
   [ "$(docker image inspect --format '{{.Architecture}}' "$redis_image")" != amd64 ]; then
  echo 'cached test images do not match Linux amd64 binary' >&2
  exit 1
fi
password=$(openssl rand -hex 32)
printf 'POSTGRES_DB=sub2api_carpool_integration_20260905\nPOSTGRES_USER=carpool_integration\nPOSTGRES_PASSWORD=%s\n' "$password" >"$run_dir/postgres.env"
printf 'CARPOOL_INTEGRATION_PG_HOST=carpool-integration-db\nCARPOOL_INTEGRATION_PG_PORT=5432\nCARPOOL_INTEGRATION_PG_DATABASE=sub2api_carpool_integration_20260905\nCARPOOL_INTEGRATION_PG_USER=carpool_integration\nCARPOOL_INTEGRATION_PG_PASSWORD=%s\nCARPOOL_INTEGRATION_REDIS_HOST=carpool-integration-redis\nCARPOOL_INTEGRATION_REDIS_PORT=6379\nCARPOOL_INTEGRATION_RUN_ID=%s\nGOMAXPROCS=2\n' "$password" "$run_id" >"$run_dir/runner.env"
password=''

network_id=$(docker network create --internal \
  --label "com.codex.local-task=$task_label" --label "com.codex.validation-run=$run_id" "$network_name")
printf 'network=%s\n' "$network_id" >"$run_dir/resources"
postgres_id=$(docker run -d --name "$postgres_name" --network "$network_id" \
  --network-alias carpool-integration-db --cpus 0.5 --memory 384m --memory-swap 384m --pids-limit 96 \
  --label "com.codex.local-task=$task_label" --label "com.codex.validation-run=$run_id" \
  --tmpfs /var/lib/postgresql:rw,nosuid,noexec,size=256m \
  --env-file "$run_dir/postgres.env" "$postgres_image" \
  postgres -c shared_buffers=32MB -c work_mem=1MB -c max_connections=40)
printf 'postgres=%s\n' "$postgres_id" >>"$run_dir/resources"
redis_id=$(docker run -d --name "$redis_name" --network "$network_id" \
  --network-alias carpool-integration-redis --cpus 0.25 --memory 64m --memory-swap 64m --pids-limit 32 \
  --label "com.codex.local-task=$task_label" --label "com.codex.validation-run=$run_id" \
  --read-only --tmpfs /data:rw,nosuid,noexec,size=16m \
  "$redis_image" redis-server --save '' --appendonly no --maxmemory 32mb --maxmemory-policy noeviction)
printf 'redis=%s\n' "$redis_id" >>"$run_dir/resources"

attempt=0
while [ "$attempt" -lt 60 ]; do
  if docker exec "$postgres_id" pg_isready -U carpool_integration -d sub2api_carpool_integration_20260905 >/dev/null 2>&1 && \
     docker exec "$redis_id" redis-cli ping >/dev/null 2>&1; then break; fi
  attempt=$((attempt + 1))
  sleep 1
done
if [ "$attempt" -eq 60 ]; then echo 'isolated database readiness timed out' >&2; exit 1; fi

runner_id=$(docker run -d --name "$runner_name" --network "$network_id" \
  --cpus 0.5 --memory 384m --memory-swap 384m --pids-limit 64 \
  --label "com.codex.local-task=$task_label" --label "com.codex.validation-run=$run_id" \
  --read-only --cap-drop ALL --security-opt no-new-privileges --user 65534:65534 \
  --tmpfs /tmp:rw,nosuid,noexec,size=32m,mode=1777 --tmpfs /work:rw,nosuid,exec,size=192m,mode=1777 \
  --env-file "$run_dir/runner.env" --entrypoint /bin/sh "$redis_image" -c 'sleep 1450')
printf 'runner=%s\n' "$runner_id" >>"$run_dir/resources"
docker exec -i "$runner_id" /bin/sh -c 'umask 077; cat > /work/repository-tests; chmod 500 /work/repository-tests' \
  <"$run_dir/repository-tests"
docker inspect "$postgres_id" "$redis_id" "$runner_id" >"$run_dir/containers.private.json"
docker network inspect "$network_id" >"$run_dir/network.private.json"
sha256sum "$run_dir/repository-tests" >"$run_dir/binary.sha256"
docker exec "$runner_id" /work/repository-tests -test.v -test.timeout=20m -test.run "$test_pattern" \
  >"$run_dir/test.private.log" 2>&1
