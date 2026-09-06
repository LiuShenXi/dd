-- Synthetic local acceptance fixture only. This row is test state, not legal
-- consent or an acknowledgement made by the operator or any real user.
\set ON_ERROR_STOP on

BEGIN ISOLATION LEVEL SERIALIZABLE;

SELECT pg_advisory_xact_lock(hashtextextended(
    'synthetic-admin-compliance-fixture:admin_compliance_acknowledgement:14',
    0
));

CREATE TEMP TABLE synthetic_admin_compliance_fixture (
    setting_key TEXT NOT NULL,
    fixture_name TEXT NOT NULL,
    admin_id BIGINT NOT NULL,
    admin_email TEXT NOT NULL,
    admin_username TEXT NOT NULL,
    version TEXT NOT NULL,
    document_zh TEXT NOT NULL,
    document_en TEXT NOT NULL,
    ip_address TEXT NOT NULL,
    user_agent TEXT NOT NULL,
    accepted_at TIMESTAMPTZ NOT NULL
) ON COMMIT DROP;

INSERT INTO synthetic_admin_compliance_fixture VALUES (
    :'expected_setting_key',
    :'expected_fixture_name',
    CAST(:'expected_admin_id' AS BIGINT),
    :'expected_admin_email',
    :'expected_admin_username',
    :'expected_version',
    :'expected_document_zh',
    :'expected_document_en',
    :'expected_ip_address',
    :'expected_user_agent',
    clock_timestamp()
);

DO $fixture$
DECLARE
    fixture RECORD;
    stored_value TEXT;
    acknowledgement JSONB;
    stored_accepted_at TIMESTAMPTZ;
BEGIN
    SELECT * INTO STRICT fixture FROM synthetic_admin_compliance_fixture;

    IF current_database() <> 'carpool_test' OR current_user <> 'carpool_test' THEN
        RAISE EXCEPTION 'synthetic compliance fixture database scope mismatch';
    END IF;
    IF fixture.setting_key <> 'admin_compliance_acknowledgement:14'
       OR fixture.fixture_name <> 'admin'
       OR fixture.admin_id <> 14
       OR fixture.admin_email <> 'carpool-test-admin@example.invalid'
       OR fixture.admin_username <> 'Carpool Test admin'
       OR fixture.version <> 'v2026.06.10'
       OR fixture.document_zh <> 'docs/legal/admin-compliance.zh.md'
       OR fixture.document_en <> 'docs/legal/admin-compliance.en.md'
       OR fixture.ip_address <> '127.0.0.1'
       OR fixture.user_agent <> 'synthetic-local-only test fixture; not operator consent' THEN
        RAISE EXCEPTION 'synthetic compliance fixture constants mismatch';
    END IF;

    PERFORM 1
    FROM users
    WHERE id = fixture.admin_id
      AND email = fixture.admin_email
      AND username = fixture.admin_username
      AND role = 'admin'
      AND notes = 'Synthetic local carpool acceptance fixture';
    IF NOT FOUND THEN
        RAISE EXCEPTION 'synthetic compliance fixture admin mismatch';
    END IF;

    SELECT value INTO stored_value
    FROM settings
    WHERE "key" = fixture.setting_key
    FOR UPDATE;

    IF NOT FOUND THEN
        acknowledgement := jsonb_build_object(
            'version', fixture.version,
            'document_zh', fixture.document_zh,
            'document_en', fixture.document_en,
            'admin_user_id', fixture.admin_id,
            'ip_address', fixture.ip_address,
            'user_agent', fixture.user_agent,
            'accepted_at', fixture.accepted_at
        );
        INSERT INTO settings ("key", value, updated_at)
        VALUES (fixture.setting_key, acknowledgement::TEXT, fixture.accepted_at)
        ON CONFLICT ("key") DO NOTHING;

        SELECT value INTO STRICT stored_value
        FROM settings
        WHERE "key" = fixture.setting_key
        FOR UPDATE;
    END IF;

    BEGIN
        acknowledgement := stored_value::JSONB;
    EXCEPTION WHEN OTHERS THEN
        RAISE EXCEPTION 'existing synthetic compliance setting is not valid JSON';
    END;

    IF jsonb_typeof(acknowledgement) IS DISTINCT FROM 'object'
       OR (SELECT COUNT(*) FROM jsonb_object_keys(acknowledgement)) <> 7
       OR acknowledgement->>'version' IS DISTINCT FROM fixture.version
       OR acknowledgement->>'document_zh' IS DISTINCT FROM fixture.document_zh
       OR acknowledgement->>'document_en' IS DISTINCT FROM fixture.document_en
       OR jsonb_typeof(acknowledgement->'admin_user_id') IS DISTINCT FROM 'number'
       OR acknowledgement->>'admin_user_id' IS DISTINCT FROM fixture.admin_id::TEXT
       OR acknowledgement->>'ip_address' IS DISTINCT FROM fixture.ip_address
       OR acknowledgement->>'user_agent' IS DISTINCT FROM fixture.user_agent
       OR jsonb_typeof(acknowledgement->'accepted_at') IS DISTINCT FROM 'string' THEN
        RAISE EXCEPTION 'existing synthetic compliance setting does not match the exact test marker';
    END IF;

    BEGIN
        stored_accepted_at := (acknowledgement->>'accepted_at')::TIMESTAMPTZ;
    EXCEPTION WHEN OTHERS THEN
        RAISE EXCEPTION 'existing synthetic compliance accepted_at is invalid';
    END;
    IF stored_accepted_at IS NULL
       OR NOT isfinite(stored_accepted_at)
       OR stored_accepted_at > clock_timestamp() + INTERVAL '1 minute' THEN
        RAISE EXCEPTION 'existing synthetic compliance accepted_at is not a valid test time';
    END IF;
END
$fixture$;

COMMIT;

SELECT 'SYNTHETIC_TEST_ACK_FIXTURE_READY';
