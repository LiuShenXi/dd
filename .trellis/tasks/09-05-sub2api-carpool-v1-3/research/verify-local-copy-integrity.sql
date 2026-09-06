CREATE TEMP TABLE local_integrity_metrics (name text PRIMARY KEY, value text NOT NULL);
DO $counts$
DECLARE item record; row_count bigint;
BEGIN
    FOR item IN SELECT tablename FROM pg_catalog.pg_tables WHERE schemaname = 'public' AND tablename <> 'settings' ORDER BY tablename LOOP
        EXECUTE format('SELECT count(*) FROM public.%I', item.tablename) INTO row_count;
        INSERT INTO local_integrity_metrics VALUES ('rows.' || item.tablename, row_count::text);
    END LOOP;
END
$counts$;
INSERT INTO local_integrity_metrics
SELECT 'hash.users', COALESCE(md5(string_agg(md5(ROW(id,created_at,updated_at,deleted_at,balance,frozen_balance,total_recharged,concurrency,status)::text),'' ORDER BY id)), 'empty') FROM users
UNION ALL SELECT 'hash.api_keys', COALESCE(md5(string_agg(md5(ROW(id,user_id,group_id,created_at,updated_at,deleted_at,quota,quota_used,rate_limit_5h,rate_limit_1d,rate_limit_7d,usage_5h,usage_1d,usage_7d,window_5h_start,window_1d_start,window_7d_start)::text),'' ORDER BY id)), 'empty') FROM api_keys
UNION ALL SELECT 'hash.usage_logs', COALESCE(md5(string_agg(md5(ROW(id,user_id,api_key_id,account_id,group_id,subscription_id,created_at,input_tokens,output_tokens,cache_creation_tokens,cache_read_tokens,input_cost,output_cost,cache_creation_cost,cache_read_cost,total_cost,actual_cost,rate_multiplier,account_rate_multiplier,image_count,video_count,video_duration_seconds)::text),'' ORDER BY id)), 'empty') FROM usage_logs
UNION ALL SELECT 'hash.usage_billing_dedup', COALESCE(md5(string_agg(md5(ROW(id,api_key_id,created_at)::text),'' ORDER BY id)), 'empty') FROM usage_billing_dedup
UNION ALL SELECT 'hash.payment_orders', COALESCE(md5(string_agg(md5(ROW(id,user_id,amount,pay_amount,fee_rate,order_type,plan_id,subscription_group_id,subscription_days,provider_instance_id,refund_amount,refund_at,force_refund,refund_requested_at,expires_at,paid_at,completed_at,failed_at,created_at,updated_at)::text),'' ORDER BY id)), 'empty') FROM payment_orders
UNION ALL SELECT 'hash.payment_audit_logs', COALESCE(md5(string_agg(md5(ROW(id,action,created_at)::text),'' ORDER BY id)), 'empty') FROM payment_audit_logs
UNION ALL SELECT 'relations.usage_dedup', count(*)::text FROM usage_logs u JOIN usage_billing_dedup d ON d.request_id = u.request_id AND d.api_key_id = u.api_key_id
UNION ALL SELECT 'relations.payment_audit_internal_id', count(*)::text FROM payment_audit_logs a JOIN payment_orders p ON a.order_id = p.id::text
UNION ALL SELECT 'relations.payment_audit_trade_no', count(*)::text FROM payment_audit_logs a JOIN payment_orders p ON p.out_trade_no <> '' AND a.order_id = p.out_trade_no
UNION ALL SELECT 'dates.registered_active', count(*)::text FROM users WHERE deleted_at IS NULL AND created_at <= now() AND created_at + interval '30 days' > now();
SELECT name || '=' || value FROM local_integrity_metrics ORDER BY name;
