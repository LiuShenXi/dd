-- Only hashes and counts leave PostgreSQL. No copied identity or monetary row is printed.
SELECT 'users|' || count(*) || '|' || COALESCE(md5(string_agg(md5(to_jsonb(u)::text),'' ORDER BY id)), 'empty')
FROM users u WHERE email LIKE 'local-user-%@example.invalid'
UNION ALL
SELECT 'api_keys|' || count(*) || '|' || COALESCE(md5(string_agg(md5(to_jsonb(k)::text),'' ORDER BY k.id)), 'empty')
FROM api_keys k JOIN users u ON u.id=k.user_id WHERE u.email LIKE 'local-user-%@example.invalid'
UNION ALL
SELECT 'usage_logs|' || count(*) || '|' || COALESCE(md5(string_agg(md5((to_jsonb(l) - ARRAY['carpool_term_id','carpool_cycle_id','carpool_admitted_at'])::text),'' ORDER BY l.id)), 'empty')
FROM usage_logs l JOIN users u ON u.id=l.user_id WHERE u.email LIKE 'local-user-%@example.invalid'
UNION ALL
SELECT 'dedup|' || count(*) || '|' || COALESCE(md5(string_agg(md5(to_jsonb(d)::text),'' ORDER BY d.id)), 'empty')
FROM usage_billing_dedup d JOIN api_keys k ON k.id=d.api_key_id JOIN users u ON u.id=k.user_id WHERE u.email LIKE 'local-user-%@example.invalid'
UNION ALL
SELECT 'groups|' || count(*) || '|' || COALESCE(md5(string_agg(md5(g::text),'' ORDER BY g.id)), 'empty')
FROM groups g WHERE g.id IN (SELECT k.group_id FROM api_keys k JOIN users u ON u.id=k.user_id WHERE u.email LIKE 'local-user-%@example.invalid')
UNION ALL
SELECT 'unexpected_carpool_attribution|' || count(*) FROM usage_logs l JOIN users u ON u.id=l.user_id
WHERE u.email LIKE 'local-user-%@example.invalid' AND (
    COALESCE(to_jsonb(l)->'carpool_term_id','null'::jsonb) <> 'null'::jsonb
    OR COALESCE(to_jsonb(l)->'carpool_cycle_id','null'::jsonb) <> 'null'::jsonb
    OR COALESCE(to_jsonb(l)->'carpool_admitted_at','null'::jsonb) <> 'null'::jsonb)
UNION ALL
SELECT 'protected_schema_v2|' || md5(string_agg(ROW(table_name,column_name,data_type,is_nullable,column_default)::text,'' ORDER BY table_name,ordinal_position))
FROM information_schema.columns WHERE table_schema='public' AND table_name IN ('users','api_keys','usage_logs','usage_billing_dedup','groups')
AND NOT (table_name='usage_logs' AND column_name IN ('carpool_term_id','carpool_cycle_id','carpool_admitted_at'));
