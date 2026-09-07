#!/bin/sh
set -eu

postgres_name='rolling-reset-app-postgres'
task_label='rolling-reset-20260907'
label=$(docker inspect --format '{{ index .Config.Labels "com.codex.local-task" }}' "$postgres_name" 2>/dev/null || true)
if [ "$label" != "$task_label" ]; then
  echo 'rolling reset PostgreSQL container is absent or relabeled' >&2
  exit 1
fi

docker exec -i "$postgres_name" \
  sh -c 'PGPASSWORD=$POSTGRES_PASSWORD exec psql -X -q -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB"' <<'SQL'
BEGIN;

WITH synthetic_admin AS (
  SELECT id
  FROM users
  WHERE email = 'rolling-reset-admin@example.invalid'
    AND role = 'admin'
    AND status = 'active'
    AND deleted_at IS NULL
), fixture AS (
  SELECT
    'admin_compliance_acknowledgement:' || id AS setting_key,
    jsonb_build_object(
      'version', 'v2026.06.10',
      'document_zh', 'docs/legal/admin-compliance.zh.md',
      'document_en', 'docs/legal/admin-compliance.en.md',
      'admin_user_id', id,
      'ip_address', '127.0.0.1',
      'user_agent', 'synthetic-local-only test fixture; not operator consent',
      'accepted_at', clock_timestamp()
    )::text AS setting_value
  FROM synthetic_admin
)
INSERT INTO settings("key", value, updated_at)
SELECT setting_key, setting_value, NOW()
FROM fixture
ON CONFLICT ("key") DO NOTHING;

DO $$
DECLARE
  matches INTEGER;
BEGIN
  SELECT COUNT(*) INTO matches
  FROM settings s
  JOIN users u ON s."key" = 'admin_compliance_acknowledgement:' || u.id
  WHERE u.email = 'rolling-reset-admin@example.invalid'
    AND s.value::jsonb->>'user_agent' = 'synthetic-local-only test fixture; not operator consent'
    AND s.value::jsonb->>'version' = 'v2026.06.10';
  IF matches <> 1 THEN
    RAISE EXCEPTION 'synthetic admin compliance fixture was not established';
  END IF;
END $$;

COMMIT;
SELECT 'SYNTHETIC_TEST_ADMIN_UNLOCK_READY';
SQL

echo 'Synthetic test admin unlock applied; this is not operator consent.'
