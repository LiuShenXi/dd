-- Hash-only conservation for the accepted synthetic reporter/UI/reset state.
-- No identity, credential, amount, note, or announcement content is printed.
\set ON_ERROR_STOP on

DO $guard$
BEGIN
    IF (SELECT COUNT(*) FROM users WHERE id=122 AND balance=0) <> 1
       OR (SELECT COUNT(*) FROM carpool_terms WHERE id=78 AND user_id=122 AND boost_used=2) <> 1
       OR (SELECT COUNT(*) FROM carpool_cycles WHERE id=386 AND term_id=78 AND base_balance_usd=698.00000000 AND boost_balance_usd=140.00000000) <> 1 THEN
        RAISE EXCEPTION 'accepted reporter fixture is missing or changed';
    END IF;
    IF (SELECT COUNT(*) FROM users WHERE id=127 AND balance=100.00000000) <> 1
       OR (SELECT COUNT(*) FROM api_keys WHERE id=64 AND user_id=127) <> 1
       OR (SELECT COUNT(*) FROM carpool_terms WHERE id=94 AND user_id=127) <> 1
       OR (SELECT COUNT(*) FROM carpool_cycles WHERE term_id=94) <> 5
       OR (SELECT COUNT(*) FROM carpool_ledger WHERE term_id=94 AND event_type='cycle_initial' AND delta_usd=550.00000000) <> 1
       OR (SELECT COUNT(*) FROM carpool_payments WHERE term_id=94) <> 0 THEN
        RAISE EXCEPTION 'accepted UI fixture is missing or changed';
    END IF;
    IF (SELECT COUNT(*) FROM carpool_reset_batches WHERE id=1 AND scope_id=1 AND status='scheduled' AND effective_at IS NULL AND completed_at IS NULL) <> 1
       OR (SELECT COUNT(*) FROM carpool_reset_targets WHERE batch_id=1) <> 0
       OR (SELECT COUNT(*) FROM carpool_reset_qualifications WHERE scope_id=1 AND batch_id=1) <> 3
       OR (SELECT COUNT(*) FROM announcements WHERE id=10 AND source_type='carpool_reset' AND source_id=1 AND source_event_kind='qualification' AND source_revision=0) <> 1 THEN
        RAISE EXCEPTION 'accepted pending reset fixture is missing or changed';
    END IF;
END
$guard$;

WITH protected_rows AS (
    SELECT 1 AS ordinal,'reporter_user_122' AS label,count(*) AS row_count,COALESCE(md5(string_agg(md5(to_jsonb(u)::text),'' ORDER BY id)),'empty') AS row_hash
    FROM users u WHERE id=122
    UNION ALL
    SELECT 2,'reporter_term_78',count(*),COALESCE(md5(string_agg(md5(to_jsonb(t)::text),'' ORDER BY id)),'empty')
    FROM carpool_terms t WHERE id=78
    UNION ALL
    SELECT 3,'reporter_cycles_78',count(*),COALESCE(md5(string_agg(md5(to_jsonb(c)::text),'' ORDER BY id)),'empty')
    FROM carpool_cycles c WHERE term_id=78
    UNION ALL
    SELECT 4,'reporter_ledger_78',count(*),COALESCE(md5(string_agg(md5(to_jsonb(l)::text),'' ORDER BY id)),'empty')
    FROM carpool_ledger l WHERE term_id=78
    UNION ALL
    SELECT 5,'reporter_operations_78',count(*),COALESCE(md5(string_agg(md5(to_jsonb(o)::text),'' ORDER BY id)),'empty')
    FROM carpool_operations o WHERE resource_type='term' AND resource_id=78
    UNION ALL
    SELECT 6,'reporter_keys_122',count(*),COALESCE(md5(string_agg(md5(to_jsonb(k)::text),'' ORDER BY id)),'empty')
    FROM api_keys k WHERE user_id=122
    UNION ALL
    SELECT 7,'ui_user_127',count(*),COALESCE(md5(string_agg(md5(to_jsonb(u)::text),'' ORDER BY id)),'empty')
    FROM users u WHERE id=127
    UNION ALL
    SELECT 8,'ui_key_64',count(*),COALESCE(md5(string_agg(md5(to_jsonb(k)::text),'' ORDER BY id)),'empty')
    FROM api_keys k WHERE id=64
    UNION ALL
    SELECT 9,'ui_term_94',count(*),COALESCE(md5(string_agg(md5(to_jsonb(t)::text),'' ORDER BY id)),'empty')
    FROM carpool_terms t WHERE id=94
    UNION ALL
    SELECT 10,'ui_cycles_94',count(*),COALESCE(md5(string_agg(md5(to_jsonb(c)::text),'' ORDER BY id)),'empty')
    FROM carpool_cycles c WHERE term_id=94
    UNION ALL
    SELECT 11,'ui_ledger_94',count(*),COALESCE(md5(string_agg(md5(to_jsonb(l)::text),'' ORDER BY id)),'empty')
    FROM carpool_ledger l WHERE term_id=94
    UNION ALL
    SELECT 12,'ui_payments_94',count(*),COALESCE(md5(string_agg(md5(to_jsonb(p)::text),'' ORDER BY id)),'empty')
    FROM carpool_payments p WHERE term_id=94
    UNION ALL
    SELECT 13,'fixture_groups',count(*),COALESCE(md5(string_agg(md5(to_jsonb(g)::text),'' ORDER BY id)),'empty')
    FROM groups g WHERE id IN (
        SELECT group_id FROM carpool_terms WHERE id IN (78,94)
        UNION SELECT group_id FROM api_keys WHERE id=64 OR user_id=122
    )
    UNION ALL
    SELECT 14,'reset_scope_1',count(*),COALESCE(md5(string_agg(md5(to_jsonb(s)::text),'' ORDER BY id)),'empty')
    FROM carpool_reset_scope_states s WHERE scope_id=1
    UNION ALL
    SELECT 15,'reset_batch_1',count(*),COALESCE(md5(string_agg(md5(to_jsonb(b)::text),'' ORDER BY id)),'empty')
    FROM carpool_reset_batches b WHERE id=1
    UNION ALL
    SELECT 16,'reset_targets_batch_1',count(*),COALESCE(md5(string_agg(md5(to_jsonb(t)::text),'' ORDER BY id)),'empty')
    FROM carpool_reset_targets t WHERE batch_id=1
    UNION ALL
    SELECT 17,'reset_qualifications_scope_1',count(*),COALESCE(md5(string_agg(md5(to_jsonb(q)::text),'' ORDER BY id)),'empty')
    FROM carpool_reset_qualifications q WHERE scope_id=1
    UNION ALL
    SELECT 18,'reset_credits_batch_1',count(*),COALESCE(md5(string_agg(md5(to_jsonb(c)::text),'' ORDER BY id)),'empty')
    FROM carpool_reset_credits c WHERE reset_batch_id=1
    UNION ALL
    SELECT 19,'reset_outbox_batch_1',count(*),COALESCE(md5(string_agg(md5(to_jsonb(o)::text),'' ORDER BY id)),'empty')
    FROM carpool_reset_announcement_outbox o WHERE batch_id=1
    UNION ALL
    SELECT 20,'announcement_10',count(*),COALESCE(md5(string_agg(md5(to_jsonb(a)::text),'' ORDER BY id)),'empty')
    FROM announcements a WHERE id=10
    UNION ALL
    SELECT 21,'announcement_reads_10',count(*),COALESCE(md5(string_agg(md5(to_jsonb(r)::text),'' ORDER BY id)),'empty')
    FROM announcement_reads r WHERE announcement_id=10
)
SELECT label || '|' || row_count || '|' || row_hash FROM protected_rows ORDER BY ordinal;
