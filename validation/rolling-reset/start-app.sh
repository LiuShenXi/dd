#!/bin/sh
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repository_root=$(CDPATH= cd -- "$script_dir/../.." && pwd)
runtime_dir="$script_dir/.runtime"
credentials_file="$runtime_dir/app-credentials.private.json"
environment_file="$runtime_dir/app.private.env"
task_label='rolling-reset-20260907'
network_name='rolling-reset-app-net'
ingress_network_name='rolling-reset-ingress-net'
postgres_name='rolling-reset-app-postgres'
redis_name='rolling-reset-app-redis'
app_name='rolling-reset-app'
ingress_name='rolling-reset-ingress'
postgres_volume='rolling-reset-app-pgdata'
app_volume='rolling-reset-app-data'
image_name='sub2api-rolling-reset:current'
host_port=38100
module_cache='rolling-reset-20260907-go-mod-cache'
build_cache='rolling-reset-20260907-go-build-cache'
reuse=${ROLLING_RESET_REUSE:-0}

mkdir -p "$runtime_dir"
chmod 700 "$runtime_dir"

if lsof -nP -iTCP:"$host_port" -sTCP:LISTEN >/dev/null 2>&1; then
  echo "port $host_port is already in use" >&2
  exit 1
fi

for resource_name in "$postgres_name" "$redis_name" "$app_name" "$ingress_name"; do
  if docker container inspect "$resource_name" >/dev/null 2>&1; then
    echo "container $resource_name already exists; use stop-app.sh after inspection" >&2
    exit 1
  fi
done
if docker network inspect "$network_name" >/dev/null 2>&1; then
  echo "network $network_name already exists; use stop-app.sh after inspection" >&2
  exit 1
fi
if docker network inspect "$ingress_network_name" >/dev/null 2>&1; then
  echo "network $ingress_network_name already exists; use stop-app.sh after inspection" >&2
  exit 1
fi
if [ ! -f "$repository_root/backend/internal/web/dist/index.html" ]; then
  echo 'embedded frontend output is missing; wait for the frontend build' >&2
  exit 1
fi

ensure_task_volume() {
  volume_name=$1
  if docker volume inspect "$volume_name" >/dev/null 2>&1; then
    label=$(docker volume inspect --format '{{ index .Labels "com.codex.local-task" }}' "$volume_name")
    if [ "$label" != "$task_label" ]; then
      echo "refusing unlabeled or foreign volume $volume_name" >&2
      exit 1
    fi
    return 0
  fi
  return 1
}

for cache_volume in "$module_cache" "$build_cache"; do
  if docker volume inspect "$cache_volume" >/dev/null 2>&1; then
    cache_label=$(docker volume inspect --format '{{ index .Labels "com.codex.local-task" }}' "$cache_volume")
    if [ "$cache_label" != "$task_label" ]; then
      echo "refusing foreign Go cache volume $cache_volume" >&2
      exit 1
    fi
  else
    docker volume create --label "com.codex.local-task=$task_label" "$cache_volume" >/dev/null
  fi
done

if [ "$reuse" = 1 ]; then
  for volume_name in "$postgres_volume" "$app_volume"; do
    if ! ensure_task_volume "$volume_name"; then
      echo "reuse requested but task volume $volume_name is absent" >&2
      exit 1
    fi
  done
  if [ ! -f "$environment_file" ] || [ ! -f "$credentials_file" ]; then
    echo 'reuse requested but private environment or credentials are absent' >&2
    exit 1
  fi
else
  for volume_name in "$postgres_volume" "$app_volume"; do
    if docker volume inspect "$volume_name" >/dev/null 2>&1; then
      echo "volume $volume_name already exists; set ROLLING_RESET_REUSE=1 only after inspection" >&2
      exit 1
    fi
  done

  postgres_password=$(openssl rand -hex 32)
  admin_password=$(openssl rand -hex 24)
  jwt_secret=$(openssl rand -hex 32)
  umask 077
  printf '%s\n' \
    "POSTGRES_DB=rolling_reset" \
    "POSTGRES_USER=rolling_reset" \
    "POSTGRES_PASSWORD=$postgres_password" \
    "DATABASE_PASSWORD=$postgres_password" \
    "ADMIN_PASSWORD=$admin_password" \
    "JWT_SECRET=$jwt_secret" >"$environment_file"
  printf '{\n  "source": "synthetic-local-only",\n  "base_url": "http://127.0.0.1:%s",\n  "admin": {"email": "rolling-reset-admin@example.invalid", "password": "%s"}\n}\n' \
    "$host_port" "$admin_password" >"$credentials_file"
  chmod 600 "$environment_file" "$credentials_file"
  postgres_password=''
  admin_password=''
  jwt_secret=''
fi

app_build_dir="$runtime_dir/app-build"
mkdir -p "$app_build_dir"
chmod 700 "$app_build_dir"
docker run --rm \
  --platform linux/arm64 \
  -e GOTOOLCHAIN=local \
  -e GOMODCACHE=/go/pkg/mod \
  -e GOCACHE=/go/build-cache \
  -e CGO_ENABLED=0 \
  -v "$repository_root:/src" \
  -v "$module_cache:/go/pkg/mod" \
  -v "$build_cache:/go/build-cache" \
  -w /src/backend \
  golang:1.27.0-alpine \
  go build -tags embed -trimpath \
    -ldflags '-s -w -X main.Commit=rolling-reset-local -X main.BuildType=source' \
    -o /src/validation/rolling-reset/.runtime/app-build/sub2api \
    ./cmd/server

docker build \
  --platform linux/arm64 \
  -f validation/rolling-reset/Dockerfile.app \
  -t "$image_name" \
  "$repository_root"

docker network create \
  --internal \
  --label "com.codex.local-task=$task_label" \
  "$network_name" >/dev/null
docker network create \
  --label "com.codex.local-task=$task_label" \
  "$ingress_network_name" >/dev/null
if [ "$reuse" != 1 ]; then
  docker volume create --label "com.codex.local-task=$task_label" "$postgres_volume" >/dev/null
  docker volume create --label "com.codex.local-task=$task_label" "$app_volume" >/dev/null
  docker run --rm -v "$app_volume:/data" postgres:18-alpine chown 1000:1000 /data
fi

docker run -d \
  --name "$postgres_name" \
  --network "$network_name" \
  --network-alias rolling-reset-postgres \
  --label "com.codex.local-task=$task_label" \
  --env-file "$environment_file" \
  -v "$postgres_volume:/var/lib/postgresql" \
  postgres:18-alpine >/dev/null

docker run -d \
  --name "$redis_name" \
  --network "$network_name" \
  --network-alias rolling-reset-redis \
  --label "com.codex.local-task=$task_label" \
  --read-only \
  --tmpfs /data:rw,nosuid,noexec,size=64m \
  redis:8-alpine redis-server --save '' --appendonly no >/dev/null

ready=0
attempt=0
while [ "$attempt" -lt 60 ]; do
  if docker exec "$postgres_name" pg_isready -U rolling_reset -d rolling_reset >/dev/null 2>&1; then
    ready=1
    break
  fi
  attempt=$((attempt + 1))
  sleep 1
done
if [ "$ready" -ne 1 ]; then
  echo 'isolated application PostgreSQL did not become ready' >&2
  exit 1
fi

docker run -d \
  --name "$app_name" \
  --network "$network_name" \
  --network-alias carpool-app \
  --label "com.codex.local-task=$task_label" \
  --env-file "$environment_file" \
  -e AUTO_SETUP=true \
  -e DATABASE_HOST=rolling-reset-postgres \
  -e DATABASE_PORT=5432 \
  -e DATABASE_USER=rolling_reset \
  -e DATABASE_DBNAME=rolling_reset \
  -e DATABASE_SSLMODE=disable \
  -e REDIS_HOST=rolling-reset-redis \
  -e REDIS_PORT=6379 \
  -e ADMIN_EMAIL=rolling-reset-admin@example.invalid \
  -e SERVER_HOST=0.0.0.0 \
  -e SERVER_PORT=8080 \
  -e SERVER_MODE=release \
  -e TZ=Asia/Shanghai \
  -e TOKEN_REFRESH_ENABLED=false \
  -e OPS_ENABLED=false \
  -e BATCH_IMAGE_ENABLED=false \
  -e IMAGE_STORAGE_ENABLED=false \
  -e SECURITY_URL_ALLOWLIST_ENABLED=true \
  -e SECURITY_URL_ALLOWLIST_UPSTREAM_HOSTS=rolling-reset.invalid \
  -e SECURITY_URL_ALLOWLIST_PRICING_HOSTS=rolling-reset.invalid \
  -e SECURITY_URL_ALLOWLIST_CRS_HOSTS=rolling-reset.invalid \
  -v "$app_volume:/app/data" \
  --read-only \
  --tmpfs /tmp:rw,nosuid,noexec,size=64m \
  "$image_name" >/dev/null

docker create \
  --name "$ingress_name" \
  --network "$ingress_network_name" \
  --label "com.codex.local-task=$task_label" \
  -p "127.0.0.1:$host_port:8080" \
  -v "$script_dir/nginx.conf:/etc/nginx/conf.d/default.conf:ro" \
  nginx:1.27-alpine >/dev/null
docker network connect "$network_name" "$ingress_name"
docker start "$ingress_name" >/dev/null

health=0
attempt=0
while [ "$attempt" -lt 90 ]; do
  if curl -fsS --max-time 2 "http://127.0.0.1:$host_port/health" >/dev/null 2>&1; then
    health=1
    break
  fi
  attempt=$((attempt + 1))
  sleep 1
done
if [ "$health" -ne 1 ]; then
  echo "application health check failed; inspect: docker logs $app_name" >&2
  exit 1
fi

echo "ROLLING_RESET_APP_READY http://127.0.0.1:$host_port"
echo "Synthetic credentials remain private at $credentials_file"
