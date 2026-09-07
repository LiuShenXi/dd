#!/bin/sh
set -eu

if [ "$#" -ne 1 ]; then
  echo "usage: $0 '^(TestNameOne|TestNameTwo)$'" >&2
  exit 64
fi

test_pattern=$1
case "$test_pattern" in
  ^*\$) ;;
  *) echo 'test pattern must be anchored with ^ and $' >&2; exit 64 ;;
esac

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repository_root=$(CDPATH= cd -- "$script_dir/../.." && pwd)
runtime_root="$script_dir/.runtime/pg-tests"
mkdir -p "$runtime_root"
chmod 700 "$script_dir/.runtime" "$runtime_root"

run_id=$(date -u '+%Y%m%dT%H%M%SZ')-$$
run_dir="$runtime_root/$run_id"
mkdir -m 700 "$run_dir"

task_label='rolling-reset-20260907'
network_name="rolling-reset-pg-net-$run_id"
postgres_name="rolling-reset-pg-$run_id"
redis_name="rolling-reset-redis-$run_id"
runner_name="rolling-reset-runner-$run_id"
database_name='sub2api_carpool_integration_20260905'
database_user='carpool_integration'
module_cache='rolling-reset-20260907-go-mod-cache'
build_cache='rolling-reset-20260907-go-build-cache'
password=''

network_id=''
postgres_id=''
redis_id=''

cleanup_container() {
  expected_id=$1
  if [ -z "$expected_id" ]; then return; fi
  actual_label=$(docker inspect --format '{{ index .Config.Labels "com.codex.local-task" }}' "$expected_id" 2>/dev/null || true)
  actual_run=$(docker inspect --format '{{ index .Config.Labels "com.codex.validation-run" }}' "$expected_id" 2>/dev/null || true)
  if [ "$actual_label" = "$task_label" ] && [ "$actual_run" = "$run_id" ]; then
    docker rm -f -v "$expected_id" >/dev/null 2>&1 || true
  fi
}

cleanup() {
  cleanup_container "$redis_id"
  cleanup_container "$postgres_id"
  if [ -n "$network_id" ]; then
    actual_label=$(docker network inspect --format '{{ index .Labels "com.codex.local-task" }}' "$network_id" 2>/dev/null || true)
    actual_run=$(docker network inspect --format '{{ index .Labels "com.codex.validation-run" }}' "$network_id" 2>/dev/null || true)
    if [ "$actual_label" = "$task_label" ] && [ "$actual_run" = "$run_id" ]; then
      docker network rm "$network_id" >/dev/null 2>&1 || true
    fi
  fi
  password=''
}
trap cleanup EXIT HUP INT TERM

docker image inspect golang:1.27.0-alpine postgres:18-alpine redis:8-alpine >/dev/null

ensure_cache_volume() {
  volume_name=$1
  if docker volume inspect "$volume_name" >/dev/null 2>&1; then
    label=$(docker volume inspect --format '{{ index .Labels "com.codex.local-task" }}' "$volume_name")
    if [ "$label" != "$task_label" ]; then
      echo "refusing unlabeled or foreign Go cache volume $volume_name" >&2
      exit 1
    fi
    return
  fi
  docker volume create --label "com.codex.local-task=$task_label" "$volume_name" >/dev/null
}
ensure_cache_volume "$module_cache"
ensure_cache_volume "$build_cache"

generator_log="$run_dir/overlay-generator.private.log"
compile_log="$run_dir/compile.private.log"
test_log="$run_dir/test.private.log"

docker run --rm \
  --platform linux/arm64 \
  -e GOTOOLCHAIN=local \
  -e GOMODCACHE=/go/pkg/mod \
  -e GOCACHE=/go/build-cache \
  -v "$repository_root:/src" \
  -v "$module_cache:/go/pkg/mod" \
  -v "$build_cache:/go/build-cache" \
  -w /src/backend \
  golang:1.27.0-alpine \
  go test ../validation/source-runtime/repository-tests/prepare-overlay.go \
    ../validation/source-runtime/repository-tests/prepare_overlay_test.go \
    >"$generator_log" 2>&1

docker run --rm \
  --platform linux/arm64 \
  -e GOTOOLCHAIN=local \
  -e GOMODCACHE=/go/pkg/mod \
  -e GOCACHE=/go/build-cache \
  -v "$repository_root:/src" \
  -v "$module_cache:/go/pkg/mod" \
  -v "$build_cache:/go/build-cache" \
  -w /src/backend \
  golang:1.27.0-alpine \
  go run ../validation/source-runtime/repository-tests/prepare-overlay.go \
    -source /src/backend/internal/repository/integration_harness_test.go \
    -output "/src/validation/rolling-reset/.runtime/pg-tests/$run_id/integration_harness_test.go" \
    -overlay "/src/validation/rolling-reset/.runtime/pg-tests/$run_id/overlay.json" \
    -helper /src/validation/source-runtime/repository-tests/source_runtime_testmain_test.go \
    -virtual /src/backend/internal/repository/source_runtime_isolated_testmain_test.go \
    >>"$generator_log" 2>&1

docker run --rm \
  --platform linux/arm64 \
  -e GOTOOLCHAIN=local \
  -e CGO_ENABLED=0 \
  -e GOMODCACHE=/go/pkg/mod \
  -e GOCACHE=/go/build-cache \
  -v "$repository_root:/src" \
  -v "$module_cache:/go/pkg/mod" \
  -v "$build_cache:/go/build-cache" \
  -w /src/backend \
  golang:1.27.0-alpine \
  go test -c -tags=integration \
    -overlay="/src/validation/rolling-reset/.runtime/pg-tests/$run_id/overlay.json" \
    -o "/src/validation/rolling-reset/.runtime/pg-tests/$run_id/repository-tests" \
    ./internal/repository >"$compile_log" 2>&1

password=$(openssl rand -hex 32)
network_id=$(docker network create \
  --internal \
  --label "com.codex.local-task=$task_label" \
  --label "com.codex.validation-run=$run_id" \
  "$network_name")

postgres_id=$(docker run -d \
  --name "$postgres_name" \
  --network "$network_name" \
  --network-alias carpool-integration-db \
  --label "com.codex.local-task=$task_label" \
  --label "com.codex.validation-run=$run_id" \
  --tmpfs /var/lib/postgresql:rw,nosuid,noexec,size=1g \
  -e "POSTGRES_DB=$database_name" \
  -e "POSTGRES_USER=$database_user" \
  -e "POSTGRES_PASSWORD=$password" \
  postgres:18-alpine)

redis_id=$(docker run -d \
  --name "$redis_name" \
  --network "$network_name" \
  --network-alias carpool-integration-redis \
  --label "com.codex.local-task=$task_label" \
  --label "com.codex.validation-run=$run_id" \
  --read-only \
  --tmpfs /data:rw,nosuid,noexec,size=64m \
  redis:8-alpine redis-server --save '' --appendonly no)

ready=0
attempt=0
while [ "$attempt" -lt 60 ]; do
  if docker exec "$postgres_id" pg_isready -U "$database_user" -d "$database_name" >/dev/null 2>&1; then
    ready=1
    break
  fi
  attempt=$((attempt + 1))
  sleep 1
done
if [ "$ready" -ne 1 ]; then
  echo 'isolated PostgreSQL did not become ready' >&2
  exit 1
fi

set +e
docker run --rm \
  --name "$runner_name" \
  --network "$network_name" \
  --label "com.codex.local-task=$task_label" \
  --label "com.codex.validation-run=$run_id" \
  --read-only \
  --tmpfs /tmp:rw,nosuid,noexec,size=64m \
  -e CARPOOL_INTEGRATION_PG_HOST=carpool-integration-db \
  -e CARPOOL_INTEGRATION_PG_PORT=5432 \
  -e "CARPOOL_INTEGRATION_PG_DATABASE=$database_name" \
  -e "CARPOOL_INTEGRATION_PG_USER=$database_user" \
  -e "CARPOOL_INTEGRATION_PG_PASSWORD=$password" \
  -e CARPOOL_INTEGRATION_REDIS_HOST=carpool-integration-redis \
  -e CARPOOL_INTEGRATION_REDIS_PORT=6379 \
  -e "CARPOOL_INTEGRATION_RUN_ID=$run_id" \
  -v "$run_dir/repository-tests:/work/repository-tests:ro" \
  golang:1.27.0-alpine \
  /work/repository-tests -test.v -test.timeout=15m -test.run "$test_pattern" \
  >"$test_log" 2>&1
test_exit=$?
set -e

run_count=$(awk '/^=== RUN / { count++ } END { print count + 0 }' "$test_log")
if [ "$test_exit" -ne 0 ] || [ "$run_count" -eq 0 ]; then
  echo "ROLLING_RESET_PG_FAILED run=$run_id exit=$test_exit tests=$run_count log=$test_log" >&2
  exit 1
fi

passed_count=$(awk '/^--- PASS: / { count++ } END { print count + 0 }' "$test_log")
echo "ROLLING_RESET_PG_PASSED run=$run_id tests=$run_count passed=$passed_count"
