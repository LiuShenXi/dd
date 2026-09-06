BEGIN;
DO $guard$
BEGIN
    IF current_database() <> 'carpool_test' OR current_user <> 'carpool_test'
       OR to_regclass('public.carpool_terms') IS NOT NULL
       OR EXISTS (SELECT 1 FROM users WHERE password_hash <> '!local-copy-login-disabled!') THEN
        RAISE EXCEPTION 'expected isolated, sanitized, unmigrated local copy';
    END IF;
END
$guard$;
UPDATE auth_cache_invalidation_outbox
SET cache_key = encode(sha256(convert_to('local-copy-invalidation:' || id::text, 'UTF8')), 'hex'),
    last_error = NULL,
    claimed_at = NULL,
    claimed_by = NULL;
DO $assert$
BEGIN
    IF EXISTS (
        SELECT 1 FROM auth_cache_invalidation_outbox
        WHERE cache_key <> encode(sha256(convert_to('local-copy-invalidation:' || id::text, 'UTF8')), 'hex')
           OR claimed_at IS NOT NULL OR claimed_by IS NOT NULL OR last_error IS NOT NULL
    ) THEN
        RAISE EXCEPTION 'local outbox sanitization failed';
    END IF;
END
$assert$;
COMMIT;
