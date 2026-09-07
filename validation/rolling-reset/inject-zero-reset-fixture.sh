#!/bin/sh
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
private_fixtures="$script_dir/.runtime/browser-fixtures.private.json"
evidence_dir="$script_dir/evidence"
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

target_email=$(node -e "const f=require(process.argv[1]);const u=f.users.find(x=>x.name==='active_reset_marker');if(!u)process.exit(1);process.stdout.write(u.email)" "$private_fixtures")
case "$target_email" in
  rolling-reset-active_reset_marker-*@example.invalid) ;;
  *) echo 'unexpected synthetic target email' >&2; exit 1 ;;
esac

mkdir -p "$evidence_dir"
docker exec -i -e "ROLLING_RESET_TARGET_EMAIL=$target_email" "$postgres_name" \
  sh -c 'PGPASSWORD=$POSTGRES_PASSWORD exec psql -v ON_ERROR_STOP=1 -v target_email="$ROLLING_RESET_TARGET_EMAIL" -U "$POSTGRES_USER" -d "$POSTGRES_DB"' \
  >"$evidence_dir/zero-reset-fixture.txt" <<'SQL'
BEGIN;

DO $$
BEGIN
  IF EXISTS (
    SELECT 1 FROM carpool_reset_batches
    WHERE source_event_key_hash = repeat('0', 63) || '7'
  ) THEN
    RAISE EXCEPTION 'synthetic zero reset fixture already exists';
  END IF;
END $$;

INSERT INTO carpool_reset_scope_states(scope_id, timezone, revision)
SELECT t.scope_id, 'Asia/Shanghai', 0
FROM carpool_terms t
JOIN users u ON u.id = t.user_id
WHERE u.email = :'target_email'
ON CONFLICT (scope_id) DO NOTHING;

WITH selected AS MATERIALIZED (
  SELECT t.id AS term_id, t.scope_id, t.expires_at, c.id AS cycle_id
  FROM users u
  JOIN carpool_terms t ON t.user_id = u.id
  JOIN carpool_cycles c ON c.term_id = t.id AND c.state = 'active'
  WHERE u.email = :'target_email'
    AND t.status = 'active'
  ORDER BY c.cycle_no DESC
  LIMIT 1
  FOR UPDATE OF t, c
), shifted AS (
  UPDATE carpool_cycles c
  SET ends_at = LEAST(date_trunc('second', NOW()) + INTERVAL '7 days', selected.expires_at),
      revision = c.revision + 1,
      updated_at = NOW()
  FROM selected
  WHERE c.id = selected.cycle_id
  RETURNING selected.term_id, selected.scope_id, selected.cycle_id, c.ends_at
), batch AS (
  INSERT INTO carpool_reset_batches(
    scope_id, status, detected_at, qualified_at, slot_at, scheduled_at,
    schedule_revision, effective_at, completed_at, evidence,
    announcement_state, qualification_source, source_event_key_hash
  )
  SELECT scope_id, 'completed', NOW(), NOW(), NOW(), NOW(),
    1, NOW(), NOW(),
    '{"source":"synthetic-browser-fixture","proves_scheduling":false,"grant":"zero"}'::jsonb,
    'published', 'administrator', repeat('0', 63) || '7'
  FROM shifted
  RETURNING id, scope_id, effective_at
), target AS (
  INSERT INTO carpool_reset_targets(batch_id, term_id, cycle_id, status, granted_usd, executed_at)
  SELECT batch.id, shifted.term_id, shifted.cycle_id, 'succeeded', 0, batch.effective_at
  FROM batch
  JOIN shifted USING (scope_id)
  RETURNING batch_id, term_id, cycle_id, executed_at
)
UPDATE carpool_reset_scope_states state
SET last_successful_reset_at = target.executed_at,
    revision = state.revision + 1,
    updated_at = NOW()
FROM target
JOIN carpool_terms term ON term.id = target.term_id
WHERE state.scope_id = term.scope_id;

COMMIT;

SELECT
  'synthetic-browser-fixture' AS evidence_kind,
  false AS proves_scheduling,
  u.email,
  t.starts_at,
  t.expires_at,
  c.ends_at AS next_natural_reset_at,
  rt.granted_usd,
  rt.executed_at
FROM users u
JOIN carpool_terms t ON t.user_id = u.id
JOIN carpool_cycles c ON c.term_id = t.id AND c.state = 'active'
JOIN carpool_reset_targets rt ON rt.term_id = t.id AND rt.cycle_id = c.id
JOIN carpool_reset_batches rb ON rb.id = rt.batch_id
WHERE u.email = :'target_email'
  AND rb.source_event_key_hash = repeat('0', 63) || '7';
SQL

echo "ROLLING_RESET_ZERO_FIXTURE_READY evidence=$evidence_dir/zero-reset-fixture.txt"
