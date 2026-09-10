#!/bin/sh
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
private_fixtures="$script_dir/.runtime/browser-fixtures.private.json"
evidence_dir="$script_dir/evidence/successor-20260908"
postgres_name='rolling-reset-app-postgres'
task_label='rolling-reset-20260907'

if [ ! -f "$private_fixtures" ]; then
  echo 'private browser fixtures are missing; run seed-fixtures.mjs first' >&2
  exit 1
fi
label=$(docker inspect --format '{{ index .Config.Labels "com.codex.local-task" }}' "$postgres_name" 2>/dev/null || true)
if [ "$label" != "$task_label" ]; then
  echo 'rolling reset PostgreSQL container is absent or relabeled' >&2
  exit 1
fi

target_email=$(node -e "const f=require(process.argv[1]);if(f.source!=='synthetic-local-only'||f.base_url!=='http://127.0.0.1:38100')process.exit(1);const u=f.users.find(x=>x.name==='active_reset_marker');if(!u)process.exit(1);process.stdout.write(u.email)" "$private_fixtures")
case "$target_email" in
  rolling-reset-active_reset_marker-*@example.invalid) ;;
  *) echo 'unexpected synthetic target email' >&2; exit 1 ;;
esac

mkdir -p "$evidence_dir"
docker exec -i -e "ROLLING_RESET_TARGET_EMAIL=$target_email" "$postgres_name" \
  sh -c 'PGPASSWORD=$POSTGRES_PASSWORD exec psql -X -qAt -v ON_ERROR_STOP=1 -v target_email="$ROLLING_RESET_TARGET_EMAIL" -U "$POSTGRES_USER" -d "$POSTGRES_DB"' \
  < "$script_dir/zero-reset-successor-fixture.sql" \
  > "$evidence_dir/zero-reset-successor-fixture.txt"

echo "ROLLING_RESET_ZERO_FIXTURE_READY evidence=$evidence_dir/zero-reset-successor-fixture.txt"
