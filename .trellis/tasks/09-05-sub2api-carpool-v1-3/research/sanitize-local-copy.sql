-- Sanitizes the restored production copy for isolated local testing.
-- Review this file before use. Run it only against carpool_test while the app is stopped.
-- The transaction aborts on schema drift, accounting drift, or a failed safety assertion.

BEGIN;
SET TRANSACTION ISOLATION LEVEL SERIALIZABLE;
SET LOCAL lock_timeout = '10s';
SET LOCAL statement_timeout = '10min';

DO $guard$
BEGIN
    IF current_database() <> 'carpool_test' THEN
        RAISE EXCEPTION 'refusing to sanitize database %; expected carpool_test', current_database();
    END IF;

    IF current_user <> 'carpool_test' THEN
        RAISE EXCEPTION 'refusing to sanitize as role %; expected carpool_test', current_user;
    END IF;

    IF to_regprocedure('gen_random_uuid()') IS NULL THEN
        RAISE EXCEPTION 'gen_random_uuid() is required to rotate local secrets';
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM pg_trigger
        WHERE tgrelid = 'public.api_keys'::regclass
          AND tgname = 'trg_api_keys_auth_cache_invalidation'
          AND tgenabled = 'O'
    ) THEN
        RAISE EXCEPTION 'API-key invalidation trigger state differs from the reviewed schema';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM unnest(ARRAY[
            'users', 'api_keys', 'accounts', 'proxies', 'usage_logs',
            'usage_billing_dedup', 'settings', 'security_secrets',
            'payment_orders', 'payment_provider_instances', 'channel_monitors',
            'scheduled_test_plans', 'prompt_audit_jobs',
            'sub2api_plugin_installations', 'sub2api_plugin_bindings'
        ]) AS required_table(name)
        WHERE to_regclass('public.' || required_table.name) IS NULL
    ) THEN
        RAISE EXCEPTION 'required production-schema table is missing';
    END IF;

    IF to_regclass('public.carpool_terms') IS NOT NULL
       OR to_regclass('public.carpool_cycles') IS NOT NULL
       OR to_regclass('public.carpool_ledgers') IS NOT NULL
       OR to_regclass('public.carpool_payments') IS NOT NULL
       OR to_regclass('public.carpool_billing_requests') IS NOT NULL THEN
        RAISE EXCEPTION 'unexpected carpool migration detected; review and extend this script before use';
    END IF;
END
$guard$;

-- Prevent concurrent application writes while the baseline and sanitized state are compared.
LOCK TABLE users, api_keys, accounts, proxies, usage_logs, usage_billing_dedup,
    usage_billing_dedup_archive, billing_usage_entries, payment_orders,
    payment_provider_instances, payment_audit_logs, promo_codes, promo_code_usages,
    redeem_codes, user_affiliates, user_affiliate_ledger, user_subscriptions,
    user_platform_quotas, auth_identities, auth_identity_channels,
    auth_identity_migration_reports, pending_auth_sessions,
    identity_adoption_decisions, passkey_credentials, passkey_user_handles,
    user_attribute_values, user_avatars, channel_monitors,
    channel_monitor_request_templates, channel_monitor_histories,
    channel_monitor_v2_config, scheduled_test_plans, scheduled_test_results,
    sub2api_plugin_installations, sub2api_plugin_bindings, prompt_audit_jobs,
    prompt_audit_events, audit_logs, content_moderation_logs, ops_error_logs,
    ops_system_logs, ops_alert_events, ops_alert_rules, announcements,
    batch_image_jobs, batch_image_items, batch_image_events, idempotency_records,
    deleted_api_key_audits, usage_cleanup_tasks, scheduler_outbox,
    auth_cache_invalidation_outbox, settings, security_secrets
IN SHARE ROW EXCLUSIVE MODE;

-- Preserve every public-table row count except settings, where fail-closed keys may be inserted.
CREATE TEMP TABLE sanitize_row_counts (
    table_name text PRIMARY KEY,
    row_count bigint NOT NULL
) ON COMMIT DROP;

DO $capture_counts$
DECLARE
    table_record record;
    captured_count bigint;
BEGIN
    FOR table_record IN
        SELECT tablename
        FROM pg_catalog.pg_tables
        WHERE schemaname = 'public'
          AND tablename <> 'settings'
        ORDER BY tablename
    LOOP
        EXECUTE format('SELECT count(*) FROM public.%I', table_record.tablename)
            INTO captured_count;
        INSERT INTO sanitize_row_counts(table_name, row_count)
        VALUES (table_record.tablename, captured_count);
    END LOOP;
END
$capture_counts$;

-- These aggregates cover the accounting values that the sanitizer must not change.
CREATE TEMP VIEW sanitize_accounting_metrics AS
SELECT 'users'::text AS metric, jsonb_build_object(
    'rows', count(*),
    'balance', COALESCE(sum(balance), 0),
    'frozen_balance', COALESCE(sum(frozen_balance), 0),
    'total_recharged', COALESCE(sum(total_recharged), 0),
    'concurrency', COALESCE(sum(concurrency), 0),
    'rpm_limit', COALESCE(sum(rpm_limit), 0)
) AS value FROM users
UNION ALL
SELECT 'api_keys', jsonb_build_object(
    'rows', count(*),
    'quota', COALESCE(sum(quota), 0),
    'quota_used', COALESCE(sum(quota_used), 0),
    'rate_limit_5h', COALESCE(sum(rate_limit_5h), 0),
    'rate_limit_1d', COALESCE(sum(rate_limit_1d), 0),
    'rate_limit_7d', COALESCE(sum(rate_limit_7d), 0),
    'usage_5h', COALESCE(sum(usage_5h), 0),
    'usage_1d', COALESCE(sum(usage_1d), 0),
    'usage_7d', COALESCE(sum(usage_7d), 0)
) FROM api_keys
UNION ALL
SELECT 'accounts', jsonb_build_object(
    'rows', count(*),
    'concurrency', COALESCE(sum(concurrency), 0),
    'priority', COALESCE(sum(priority), 0),
    'rate_multiplier', COALESCE(sum(rate_multiplier), 0),
    'load_factor', COALESCE(sum(load_factor), 0)
) FROM accounts
UNION ALL
SELECT 'usage_logs', jsonb_build_object(
    'rows', count(*),
    'input_tokens', COALESCE(sum(input_tokens), 0),
    'output_tokens', COALESCE(sum(output_tokens), 0),
    'cache_creation_tokens', COALESCE(sum(cache_creation_tokens), 0),
    'cache_read_tokens', COALESCE(sum(cache_read_tokens), 0),
    'cache_creation_5m_tokens', COALESCE(sum(cache_creation_5m_tokens), 0),
    'cache_creation_1h_tokens', COALESCE(sum(cache_creation_1h_tokens), 0),
    'image_input_tokens', COALESCE(sum(image_input_tokens), 0),
    'image_output_tokens', COALESCE(sum(image_output_tokens), 0),
    'image_count', COALESCE(sum(image_count), 0),
    'video_count', COALESCE(sum(video_count), 0),
    'video_duration_seconds', COALESCE(sum(video_duration_seconds), 0),
    'input_cost', COALESCE(sum(input_cost), 0),
    'output_cost', COALESCE(sum(output_cost), 0),
    'cache_creation_cost', COALESCE(sum(cache_creation_cost), 0),
    'cache_read_cost', COALESCE(sum(cache_read_cost), 0),
    'image_input_cost', COALESCE(sum(image_input_cost), 0),
    'image_output_cost', COALESCE(sum(image_output_cost), 0),
    'total_cost', COALESCE(sum(total_cost), 0),
    'actual_cost', COALESCE(sum(actual_cost), 0),
    'account_stats_cost', COALESCE(sum(account_stats_cost), 0),
    'rate_multiplier', COALESCE(sum(rate_multiplier), 0),
    'account_rate_multiplier', COALESCE(sum(account_rate_multiplier), 0)
) FROM usage_logs
UNION ALL
SELECT 'usage_billing_dedup', jsonb_build_object('rows', count(*))
FROM usage_billing_dedup
UNION ALL
SELECT 'usage_billing_dedup_archive', jsonb_build_object('rows', count(*))
FROM usage_billing_dedup_archive
UNION ALL
SELECT 'billing_usage_entries', jsonb_build_object(
    'rows', count(*),
    'delta_usd', COALESCE(sum(delta_usd), 0),
    'applied', count(*) FILTER (WHERE applied)
) FROM billing_usage_entries
UNION ALL
SELECT 'payment_orders', jsonb_build_object(
    'rows', count(*),
    'amount', COALESCE(sum(amount), 0),
    'pay_amount', COALESCE(sum(pay_amount), 0),
    'fee_rate', COALESCE(sum(fee_rate), 0),
    'refund_amount', COALESCE(sum(refund_amount), 0),
    'subscription_days', COALESCE(sum(subscription_days), 0)
) FROM payment_orders
UNION ALL
SELECT 'promo_codes', jsonb_build_object(
    'rows', count(*),
    'bonus_amount', COALESCE(sum(bonus_amount), 0),
    'max_uses', COALESCE(sum(max_uses), 0),
    'used_count', COALESCE(sum(used_count), 0)
) FROM promo_codes
UNION ALL
SELECT 'promo_code_usages', jsonb_build_object(
    'rows', count(*),
    'bonus_amount', COALESCE(sum(bonus_amount), 0)
) FROM promo_code_usages
UNION ALL
SELECT 'redeem_codes', jsonb_build_object(
    'rows', count(*),
    'value', COALESCE(sum(value), 0),
    'validity_days', COALESCE(sum(validity_days), 0)
) FROM redeem_codes
UNION ALL
SELECT 'user_affiliates', jsonb_build_object(
    'rows', count(*),
    'aff_count', COALESCE(sum(aff_count), 0),
    'aff_quota', COALESCE(sum(aff_quota), 0),
    'aff_history_quota', COALESCE(sum(aff_history_quota), 0),
    'aff_frozen_quota', COALESCE(sum(aff_frozen_quota), 0),
    'aff_rebate_rate_percent', COALESCE(sum(aff_rebate_rate_percent), 0)
) FROM user_affiliates
UNION ALL
SELECT 'user_affiliate_ledger', jsonb_build_object(
    'rows', count(*),
    'amount', COALESCE(sum(amount), 0),
    'balance_after', COALESCE(sum(balance_after), 0),
    'aff_quota_after', COALESCE(sum(aff_quota_after), 0),
    'aff_frozen_quota_after', COALESCE(sum(aff_frozen_quota_after), 0),
    'aff_history_quota_after', COALESCE(sum(aff_history_quota_after), 0)
) FROM user_affiliate_ledger
UNION ALL
SELECT 'user_subscriptions', jsonb_build_object(
    'rows', count(*),
    'daily_usage_usd', COALESCE(sum(daily_usage_usd), 0),
    'weekly_usage_usd', COALESCE(sum(weekly_usage_usd), 0),
    'monthly_usage_usd', COALESCE(sum(monthly_usage_usd), 0)
) FROM user_subscriptions
UNION ALL
SELECT 'user_platform_quotas', jsonb_build_object(
    'rows', count(*),
    'daily_limit_usd', COALESCE(sum(daily_limit_usd), 0),
    'weekly_limit_usd', COALESCE(sum(weekly_limit_usd), 0),
    'monthly_limit_usd', COALESCE(sum(monthly_limit_usd), 0),
    'daily_usage_usd', COALESCE(sum(daily_usage_usd), 0),
    'weekly_usage_usd', COALESCE(sum(weekly_usage_usd), 0),
    'monthly_usage_usd', COALESCE(sum(monthly_usage_usd), 0)
) FROM user_platform_quotas
UNION ALL
SELECT 'batch_image_jobs', jsonb_build_object(
    'rows', count(*),
    'item_count', COALESCE(sum(item_count), 0),
    'success_count', COALESCE(sum(success_count), 0),
    'fail_count', COALESCE(sum(fail_count), 0),
    'cancelled_count', COALESCE(sum(cancelled_count), 0),
    'estimated_cost', COALESCE(sum(estimated_cost), 0),
    'hold_amount', COALESCE(sum(hold_amount), 0),
    'actual_cost', COALESCE(sum(actual_cost), 0),
    'base_unit_price', COALESCE(sum(base_unit_price), 0),
    'group_rate_multiplier', COALESCE(sum(group_rate_multiplier), 0),
    'account_rate_multiplier', COALESCE(sum(account_rate_multiplier), 0),
    'batch_discount_multiplier', COALESCE(sum(batch_discount_multiplier), 0),
    'hold_multiplier', COALESCE(sum(hold_multiplier), 0),
    'billable_unit_price', COALESCE(sum(billable_unit_price), 0),
    'hold_unit_price', COALESCE(sum(hold_unit_price), 0)
) FROM batch_image_jobs
UNION ALL
SELECT 'batch_image_items', jsonb_build_object(
    'rows', count(*),
    'image_count', COALESCE(sum(image_count), 0),
    'billed_amount', COALESCE(sum(billed_amount), 0)
) FROM batch_image_items;

CREATE TEMP TABLE sanitize_accounting_baseline ON COMMIT DROP AS
SELECT metric, value FROM sanitize_accounting_metrics;

-- Disable login and user-addressed notifications without touching balances or timestamps.
UPDATE users
SET email = 'local-user-' || id::text || '@example.invalid',
    username = 'local-user-' || id::text,
    password_hash = '!local-copy-login-disabled!',
    notes = '',
    wechat = NULL,
    totp_secret_encrypted = NULL,
    totp_enabled = false,
    totp_enabled_at = NULL,
    balance_notify_enabled = false,
    balance_notify_threshold = NULL,
    balance_notify_extra_emails = '[]';

-- No application or old Redis cache exists. Suppress only this trigger while rotating keys;
-- restore it in the same transaction, retaining all constraints and other triggers.
ALTER TABLE api_keys DISABLE TRIGGER trg_api_keys_auth_cache_invalidation;
UPDATE api_keys
SET key = 'sk-local-disabled-' || replace(gen_random_uuid()::text, '-', ''),
    name = 'local-key-' || id::text,
    status = 'disabled',
    ip_whitelist = '[]'::jsonb,
    ip_blacklist = '[]'::jsonb;
ALTER TABLE api_keys ENABLE TRIGGER trg_api_keys_auth_cache_invalidation;

UPDATE deleted_api_key_audits
SET key = 'sk-local-deleted-' || id::text,
    key_name = 'local-deleted-key-' || id::text;

UPDATE accounts
SET name = 'local-account-' || id::text,
    credentials = '{}'::jsonb,
    extra = '{}'::jsonb,
    status = 'disabled',
    schedulable = false,
    error_message = NULL,
    temp_unschedulable_reason = NULL,
    notes = NULL;

UPDATE proxies
SET name = 'local-proxy-' || id::text,
    protocol = 'http',
    host = '127.0.0.1',
    port = 9,
    username = NULL,
    password = NULL,
    status = 'disabled',
    fallback_mode = 'none',
    backup_proxy_id = NULL;

-- Invalidate copied OAuth, pending-login, passkey, profile, and avatar material in place.
UPDATE auth_identities
SET provider_key = 'local-copy-' || id::text,
    provider_subject = 'local-subject-' || id::text,
    issuer = NULL,
    metadata = '{}'::jsonb;

UPDATE auth_identity_channels
SET provider_key = 'local-copy-' || id::text,
    channel_app_id = 'local-app-' || id::text,
    channel_subject = 'local-subject-' || id::text,
    metadata = '{}'::jsonb;

UPDATE auth_identity_migration_reports
SET report_key = 'local-report-' || id::text,
    details = '{}'::jsonb,
    resolution_note = '';

UPDATE pending_auth_sessions
SET session_token = 'local-invalid-session-' || id::text,
    provider_key = 'local-copy-' || id::text,
    provider_subject = 'local-subject-' || id::text,
    redirect_to = '/',
    resolved_email = 'local-pending-' || id::text || '@example.invalid',
    registration_password_hash = '!local-copy-login-disabled!',
    upstream_identity_claims = '{}'::jsonb,
    local_flow_state = '{}'::jsonb,
    browser_session_key = 'local-invalid-browser-' || id::text,
    completion_code_hash = md5('local-copy-completion:' || id::text);

UPDATE identity_adoption_decisions
SET adopt_display_name = false,
    adopt_avatar = false;

UPDATE passkey_credentials
SET credential_id = decode(md5('local-copy-passkey:' || id::text), 'hex'),
    name = 'local-passkey-' || id::text,
    credential_data = '{}'::jsonb;

UPDATE passkey_user_handles
SET user_handle = decode(md5('local-copy-handle:' || user_id::text), 'hex');

UPDATE user_attribute_values
SET value = NULL;

UPDATE user_avatars
SET storage_provider = 'local',
    storage_key = 'local-copy-disabled/' || id::text,
    url = '',
    content_type = 'application/octet-stream',
    sha256 = md5('local-copy-avatar:' || id::text);

-- Disable payment providers and detach copied provider/network material from orders.
UPDATE payment_provider_instances
SET config = '',
    limits = '{}',
    enabled = false,
    refund_enabled = false,
    allow_user_refund = false;

UPDATE payment_audit_logs AS audit
SET order_id = CASE
        WHEN EXISTS (
            SELECT 1
            FROM payment_orders AS payment_order
            WHERE payment_order.out_trade_no <> ''
              AND payment_order.out_trade_no = audit.order_id
        ) THEN 'local-' || md5('local-copy-order:' || audit.order_id)
        ELSE audit.order_id
    END,
    detail = '',
    operator = 'local-copy';

UPDATE payment_orders
SET user_email = 'local-user-' || user_id::text || '@example.invalid',
    user_name = 'local-user-' || user_id::text,
    user_notes = NULL,
    recharge_code = CASE
        WHEN recharge_code = '' THEN ''
        ELSE 'local-' || md5('local-copy-recharge:' || recharge_code)
    END,
    payment_trade_no = CASE
        WHEN payment_trade_no = '' THEN ''
        ELSE 'local-' || md5('local-copy-trade:' || payment_trade_no)
    END,
    out_trade_no = CASE
        WHEN out_trade_no = '' THEN ''
        ELSE 'local-' || md5('local-copy-order:' || out_trade_no)
    END,
    pay_url = NULL,
    qr_code = NULL,
    qr_code_img = NULL,
    provider_snapshot = '{}'::jsonb,
    client_ip = '',
    src_host = '',
    src_url = NULL,
    refund_reason = NULL,
    refund_request_reason = NULL,
    refund_requested_by = NULL,
    failed_reason = NULL,
    status = CASE WHEN status = 'PENDING' THEN 'EXPIRED' ELSE status END;

-- Disable monitor, scheduled-test, plugin, prompt-audit, and other background work.
UPDATE channel_monitors
SET name = 'local-monitor-' || id::text,
    endpoint = '',
    api_key_encrypted = '',
    enabled = false,
    extra_headers = '{}'::jsonb,
    body_override_mode = 'off',
    body_override = NULL;

UPDATE channel_monitor_request_templates
SET name = 'local-monitor-template-' || id::text,
    description = '',
    extra_headers = '{}'::jsonb,
    body_override_mode = 'off',
    body_override = NULL;

UPDATE channel_monitor_histories
SET message = '',
    quota = NULL;

UPDATE channel_monitor_v2_config
SET enabled = false;

UPDATE scheduled_test_plans
SET enabled = false,
    auto_recover = false;

UPDATE scheduled_test_results
SET response_text = '',
    error_message = '';

UPDATE sub2api_plugin_bindings
SET enabled = false,
    rollout_percent = 0;

UPDATE sub2api_plugin_installations
SET name = 'local-plugin-' || id::text,
    description = '',
    author = '',
    manifest = '{}'::jsonb,
    artifact_path = '',
    install_path = '',
    binary_path = '',
    binary_sha256 = md5('local-copy-plugin:' || id::text),
    state = 'disabled',
    config_encrypted = '',
    last_error = '',
    artifact_data = NULL;

UPDATE prompt_audit_jobs
SET request_id = 'local-' || md5('local-copy-prompt-job:' || id::text),
    username_snapshot = CASE WHEN user_id IS NULL THEN '' ELSE 'local-user-' || user_id::text END,
    user_email_snapshot = CASE WHEN user_id IS NULL THEN '' ELSE 'local-user-' || user_id::text || '@example.invalid' END,
    api_key_name_snapshot = CASE WHEN api_key_id IS NULL THEN '' ELSE 'local-key-' || api_key_id::text END,
    group_name = '',
    endpoint = '',
    prompt_hash = md5('local-copy-prompt-hash:' || id::text),
    redacted_preview = '',
    status = CASE
        WHEN status IN ('staging', 'queued', 'processing', 'retry') THEN 'failed'
        ELSE status
    END,
    last_error_code = '',
    last_error_message = '';

UPDATE prompt_audit_events
SET request_id = 'local-' || md5('local-copy-prompt-event:' || id::text),
    username_snapshot = CASE WHEN user_id IS NULL THEN '' ELSE 'local-user-' || user_id::text END,
    user_email_snapshot = CASE WHEN user_id IS NULL THEN '' ELSE 'local-user-' || user_id::text || '@example.invalid' END,
    api_key_name_snapshot = CASE WHEN api_key_id IS NULL THEN '' ELSE 'local-key-' || api_key_id::text END,
    group_name = '',
    endpoint = '',
    prompt_hash = md5('local-copy-prompt-event-hash:' || id::text),
    redacted_preview = '',
    categories = '[]'::jsonb,
    matched_scanners = '[]'::jsonb,
    scanner_scores = '{}'::jsonb,
    scanner_evidence = '{}'::jsonb,
    guard_endpoint_id = '',
    policy_id = '',
    full_prompt = '';

UPDATE ops_alert_rules
SET enabled = false,
    notify_email = false,
    description = NULL,
    filters = '{}'::jsonb;

UPDATE ops_alert_events
SET title = NULL,
    description = NULL,
    dimensions = '{}'::jsonb,
    email_sent = false;

UPDATE usage_cleanup_tasks
SET status = CASE WHEN status IN ('pending', 'running') THEN 'canceled' ELSE status END,
    filters = '{}'::jsonb,
    error_message = NULL;

UPDATE batch_image_jobs
SET status = CASE
        WHEN status IN ('created', 'uploading', 'submitted', 'running', 'indexing', 'settling') THEN 'failed'
        ELSE status
    END,
    provider_job_name = NULL,
    gcs_input_uri = NULL,
    gcs_output_uri = NULL,
    hold_id = NULL,
    idempotency_key = CASE
        WHEN idempotency_key IS NULL THEN NULL
        ELSE md5('local-copy-batch-idempotency:' || idempotency_key)
    END,
    request_hash = CASE
        WHEN request_hash IS NULL THEN NULL
        ELSE md5('local-copy-batch-request:' || request_hash)
    END,
    provider_input_ref = NULL,
    provider_output_ref = NULL,
    last_error_code = NULL,
    last_error_message = NULL,
    session_id = NULL;

UPDATE batch_image_items
SET custom_id = 'local-item-' || id::text,
    request_hash = CASE
        WHEN request_hash IS NULL THEN NULL
        ELSE md5('local-copy-batch-item:' || request_hash)
    END,
    prompt_preview = NULL,
    provider_source_object = NULL,
    error_message = NULL;

UPDATE batch_image_events
SET payload = '{}'::jsonb;

UPDATE scheduler_outbox
SET payload = '{}'::jsonb,
    dedup_key = CASE
        WHEN dedup_key IS NULL THEN NULL
        ELSE md5('local-copy-scheduler:' || dedup_key)
    END;

UPDATE auth_cache_invalidation_outbox
SET cache_key = encode(sha256(convert_to('local-copy-invalidation:' || id::text, 'UTF8')), 'hex'),
    last_error = NULL,
    claimed_at = NULL,
    claimed_by = NULL;

-- Remove copied request content, network identifiers, and correlation identifiers.
UPDATE usage_logs
SET request_id = CASE
        WHEN request_id IS NULL THEN NULL
        ELSE 'local-' || md5('local-copy-request:' || request_id)
    END,
    user_agent = NULL,
    ip_address = NULL,
    upstream_endpoint = NULL,
    session_id = NULL,
    upstream_request_id = NULL;

UPDATE usage_billing_dedup
SET request_id = 'local-' || md5('local-copy-request:' || request_id),
    request_fingerprint = md5('local-copy-fingerprint:' || request_fingerprint);

UPDATE usage_billing_dedup_archive
SET request_id = 'local-' || md5('local-copy-request:' || request_id),
    request_fingerprint = md5('local-copy-fingerprint:' || request_fingerprint);

UPDATE idempotency_records
SET idempotency_key_hash = md5('local-copy-idempotency:' || id::text),
    request_fingerprint = md5('local-copy-idempotency-fingerprint:' || id::text),
    response_body = NULL,
    error_reason = NULL;

UPDATE audit_logs
SET actor_email = CASE
        WHEN actor_user_id IS NULL THEN '' ELSE 'local-user-' || actor_user_id::text || '@example.invalid'
    END,
    credential_masked = '',
    path = '/local-copy',
    request_id = 'local-' || md5('local-copy-audit:' || id::text),
    client_ip = '',
    user_agent = '',
    request_body = '',
    extra = '{}'::jsonb;

UPDATE content_moderation_logs
SET request_id = 'local-' || md5('local-copy-moderation:' || id::text),
    user_email = CASE WHEN user_id IS NULL THEN '' ELSE 'local-user-' || user_id::text || '@example.invalid' END,
    api_key_name = CASE WHEN api_key_id IS NULL THEN '' ELSE 'local-key-' || api_key_id::text END,
    group_name = '',
    endpoint = '',
    category_scores = '{}'::jsonb,
    threshold_snapshot = '{}'::jsonb,
    input_excerpt = '',
    error = '',
    matched_keyword = '',
    email_sent = false;

UPDATE ops_error_logs
SET request_id = NULL,
    client_request_id = NULL,
    client_ip = NULL,
    request_path = NULL,
    user_agent = NULL,
    error_message = NULL,
    error_body = NULL,
    upstream_error_message = NULL,
    upstream_error_detail = NULL,
    upstream_errors = NULL,
    inbound_endpoint = NULL,
    upstream_endpoint = NULL,
    attempted_key_prefix = NULL,
    deleted_key_name = NULL,
    api_key_prefix = NULL;

UPDATE ops_system_logs
SET message = '',
    request_id = NULL,
    client_request_id = NULL,
    extra = '{}'::jsonb,
    host = NULL;

UPDATE announcements
SET title = 'Local test announcement ' || id::text,
    content = '',
    targeting = '{}'::jsonb,
    notify_mode = 'silent';

UPDATE promo_codes
SET code = 'local-promo-' || id::text,
    status = 'disabled',
    notes = NULL;

UPDATE redeem_codes
SET code = 'local-redeem-' || id::text,
    status = CASE WHEN status = 'unused' THEN 'disabled' ELSE status END,
    notes = NULL;

UPDATE user_affiliates
SET aff_code = 'local-aff-' || user_id::text,
    aff_code_custom = false;

-- Rotate every DB-managed secret. The copied values are never selected or emitted.
UPDATE security_secrets
SET value = replace(gen_random_uuid()::text, '-', '') || replace(gen_random_uuid()::text, '-', '');

-- Clear credential-bearing settings without changing billing, quota, or pricing settings.
UPDATE settings
SET value = ''
WHERE key IN (
    'smtp_host', 'smtp_port', 'smtp_username', 'smtp_password', 'smtp_from', 'smtp_from_name',
    'turnstile_site_key', 'turnstile_secret_key',
    'tencent_captcha_app_id', 'tencent_captcha_app_secret_key',
    'tencent_captcha_cloud_secret_id', 'tencent_captcha_cloud_secret_key',
    'aliyun_captcha_access_key_id', 'aliyun_captcha_access_key_secret',
    'linuxdo_connect_client_id', 'linuxdo_connect_client_secret', 'linuxdo_connect_redirect_url',
    'dingtalk_connect_client_id', 'dingtalk_connect_client_secret',
    'dingtalk_connect_redirect_url', 'dingtalk_connect_internal_corp_id',
    'wechat_connect_app_id', 'wechat_connect_app_secret',
    'wechat_connect_open_app_id', 'wechat_connect_open_app_secret',
    'wechat_connect_mp_app_id', 'wechat_connect_mp_app_secret',
    'wechat_connect_mobile_app_id', 'wechat_connect_mobile_app_secret',
    'wechat_connect_redirect_url', 'wechat_connect_frontend_redirect_url',
    'oidc_connect_provider_name', 'oidc_connect_client_id', 'oidc_connect_client_secret',
    'oidc_connect_issuer_url', 'oidc_connect_discovery_url', 'oidc_connect_authorize_url',
    'oidc_connect_token_url', 'oidc_connect_userinfo_url', 'oidc_connect_jwks_url',
    'oidc_connect_scopes', 'oidc_connect_redirect_url', 'oidc_connect_frontend_redirect_url',
    'github_oauth_client_id', 'github_oauth_client_secret',
    'github_oauth_redirect_url', 'github_oauth_frontend_redirect_url',
    'google_oauth_client_id', 'google_oauth_client_secret',
    'google_oauth_redirect_url', 'google_oauth_frontend_redirect_url',
    'admin_api_key', 'contact_info', 'doc_url', 'home_content',
    'purchase_subscription_url', 'identity_patch_prompt',
    'claude_oauth_system_prompt', 'balance_low_notify_recharge_url',
    'PAYMENT_HELP_IMAGE_URL', 'PAYMENT_HELP_TEXT',
    'PRODUCT_NAME_PREFIX', 'PRODUCT_NAME_SUFFIX'
);

INSERT INTO settings(key, value, updated_at) VALUES
    ('registration_enabled', 'false', now()),
    ('email_verify_enabled', 'false', now()),
    ('password_reset_enabled', 'false', now()),
    ('invitation_code_enabled', 'false', now()),
    ('affiliate_enabled', 'false', now()),
    ('promo_code_enabled', 'false', now()),
    ('risk_control_enabled', 'false', now()),
    ('cyber_session_block_enabled', 'false', now()),
    ('turnstile_enabled', 'false', now()),
    ('tencent_captcha_enabled', 'false', now()),
    ('aliyun_captcha_enabled', 'false', now()),
    ('totp_enabled', 'false', now()),
    ('passkey_enabled', 'false', now()),
    ('linuxdo_connect_enabled', 'false', now()),
    ('dingtalk_connect_enabled', 'false', now()),
    ('dingtalk_connect_bypass_registration', 'false', now()),
    ('dingtalk_connect_sync_corp_email', 'false', now()),
    ('dingtalk_connect_sync_display_name', 'false', now()),
    ('dingtalk_connect_sync_dept', 'false', now()),
    ('wechat_connect_enabled', 'false', now()),
    ('wechat_connect_open_enabled', 'false', now()),
    ('wechat_connect_mp_enabled', 'false', now()),
    ('wechat_connect_mobile_enabled', 'false', now()),
    ('oidc_connect_enabled', 'false', now()),
    ('github_oauth_enabled', 'false', now()),
    ('google_oauth_enabled', 'false', now()),
    ('purchase_subscription_enabled', 'false', now()),
    ('payment_enabled', 'false', now()),
    ('payment_visible_method_alipay_enabled', 'false', now()),
    ('payment_visible_method_wxpay_enabled', 'false', now()),
    ('ops_monitoring_enabled', 'false', now()),
    ('ops_realtime_monitoring_enabled', 'false', now()),
    ('channel_monitor_enabled', 'false', now()),
    ('plugin_management_enabled', 'false', now()),
    ('openai_codex_version_auto_sync_enabled', 'false', now()),
    ('balance_low_notify_enabled', 'false', now()),
    ('subscription_expiry_notify_enabled', 'false', now()),
    ('account_quota_notify_enabled', 'false', now()),
    ('smtp_use_tls', 'false', now()),
    ('registration_email_suffix_whitelist', '[]', now()),
    ('account_quota_notify_emails', '[]', now()),
    ('custom_menu_items', '[]', now()),
    ('custom_endpoints', '[]', now()),
    ('login_agreement_documents', '[]', now()),
    ('claude_oauth_system_prompt_blocks', '[]', now()),
    ('content_moderation_config', '{"enabled":false}', now()),
    ('prompt_audit_config', '{"enabled":false,"blocking_enabled":false,"endpoints":[],"config_version":1}', now()),
    ('ops_email_notification_config', '{"enabled":false,"recipients":[]}', now()),
    ('ops_alert_runtime_settings', '{"enabled":false}', now()),
    ('upstream_billing_probe_settings', '{"enabled":false,"interval_minutes":5}', now()),
    ('ollama_cloud_usage_settings', '{"enabled":false,"interval_minutes":60,"debounce_minutes":5}', now()),
    ('backup_s3_config', '{}', now()),
    ('backup_schedule', '{"enabled":false,"cron_expr":"","retain_days":0,"retain_count":0}', now()),
    ('backup_records', '[]', now()),
    ('web_search_emulation_config', '{"enabled":false}', now()),
    ('ENABLED_PAYMENT_TYPES', '[]', now()),
    ('BALANCE_PAYMENT_DISABLED', 'true', now()),
    ('frontend_url', 'http://127.0.0.1:38088', now()),
    ('api_base_url', 'http://127.0.0.1:38088', now())
ON CONFLICT (key) DO UPDATE
SET value = EXCLUDED.value,
    updated_at = EXCLUDED.updated_at;

-- Fail closed if row counts or accounting totals changed unexpectedly.
DO $assert_integrity$
DECLARE
    table_record record;
    current_count bigint;
    changed_metric text;
BEGIN
    FOR table_record IN SELECT table_name, row_count FROM sanitize_row_counts ORDER BY table_name LOOP
        EXECUTE format('SELECT count(*) FROM public.%I', table_record.table_name)
            INTO current_count;
        IF current_count <> table_record.row_count THEN
            RAISE EXCEPTION 'row count changed for table %', table_record.table_name;
        END IF;
    END LOOP;

    SELECT COALESCE(b.metric, c.metric)
    INTO changed_metric
    FROM sanitize_accounting_baseline b
    FULL JOIN sanitize_accounting_metrics c USING (metric)
    WHERE b.value IS DISTINCT FROM c.value
    ORDER BY COALESCE(b.metric, c.metric)
    LIMIT 1;

    IF changed_metric IS NOT NULL THEN
        RAISE EXCEPTION 'accounting aggregate changed for metric %', changed_metric;
    END IF;
END
$assert_integrity$;

-- Assert that copied credentials cannot authenticate and no DB-backed worker can call outward.
DO $assert_safety$
DECLARE
    expected_false_keys text[] := ARRAY[
        'registration_enabled', 'email_verify_enabled', 'password_reset_enabled',
        'invitation_code_enabled', 'affiliate_enabled', 'promo_code_enabled',
        'risk_control_enabled', 'cyber_session_block_enabled', 'turnstile_enabled',
        'tencent_captcha_enabled', 'aliyun_captcha_enabled', 'totp_enabled',
        'passkey_enabled', 'linuxdo_connect_enabled', 'dingtalk_connect_enabled',
        'dingtalk_connect_bypass_registration', 'dingtalk_connect_sync_corp_email',
        'dingtalk_connect_sync_display_name', 'dingtalk_connect_sync_dept',
        'wechat_connect_enabled', 'wechat_connect_open_enabled',
        'wechat_connect_mp_enabled', 'wechat_connect_mobile_enabled',
        'oidc_connect_enabled', 'github_oauth_enabled', 'google_oauth_enabled',
        'purchase_subscription_enabled', 'payment_enabled',
        'payment_visible_method_alipay_enabled', 'payment_visible_method_wxpay_enabled',
        'ops_monitoring_enabled', 'ops_realtime_monitoring_enabled',
        'channel_monitor_enabled', 'plugin_management_enabled',
        'openai_codex_version_auto_sync_enabled', 'balance_low_notify_enabled',
        'subscription_expiry_notify_enabled', 'account_quota_notify_enabled'
    ];
BEGIN
    IF EXISTS (
        SELECT 1 FROM users
        WHERE password_hash <> '!local-copy-login-disabled!'
           OR totp_enabled
           OR totp_secret_encrypted IS NOT NULL
           OR balance_notify_enabled
           OR email <> 'local-user-' || id::text || '@example.invalid'
    ) THEN
        RAISE EXCEPTION 'user login or notification material remains';
    END IF;

    IF EXISTS (SELECT 1 FROM api_keys WHERE status <> 'disabled' OR key NOT LIKE 'sk-local-disabled-%')
       OR (SELECT count(*) FROM api_keys) <> (SELECT count(DISTINCT key) FROM api_keys) THEN
        RAISE EXCEPTION 'active or unsanitized API key remains';
    END IF;

    IF EXISTS (
        SELECT 1 FROM accounts
        WHERE status <> 'disabled' OR schedulable OR credentials <> '{}'::jsonb
           OR (extra - 'openai_long_context_billing_enabled') <> '{}'::jsonb
           OR (extra ? 'openai_long_context_billing_enabled'
               AND jsonb_typeof(extra->'openai_long_context_billing_enabled') <> 'boolean')
    ) THEN
        RAISE EXCEPTION 'active account or account credential remains';
    END IF;

    IF EXISTS (
        SELECT 1 FROM proxies
        WHERE status <> 'disabled' OR host <> '127.0.0.1'
           OR username IS NOT NULL OR password IS NOT NULL OR fallback_mode <> 'none'
    ) THEN
        RAISE EXCEPTION 'active or externally addressed proxy remains';
    END IF;

    IF EXISTS (
        SELECT 1 FROM channel_monitors
        WHERE enabled OR endpoint <> '' OR api_key_encrypted <> ''
           OR extra_headers <> '{}'::jsonb OR body_override IS NOT NULL
    ) THEN
        RAISE EXCEPTION 'active channel monitor or monitor credential remains';
    END IF;

    IF EXISTS (SELECT 1 FROM channel_monitor_v2_config WHERE enabled)
       OR EXISTS (SELECT 1 FROM scheduled_test_plans WHERE enabled OR auto_recover)
       OR EXISTS (SELECT 1 FROM ops_alert_rules WHERE enabled OR notify_email) THEN
        RAISE EXCEPTION 'active monitor, scheduled test, or alert rule remains';
    END IF;

    IF EXISTS (
        SELECT 1 FROM payment_provider_instances
        WHERE enabled OR refund_enabled OR allow_user_refund OR config <> ''
    ) OR EXISTS (SELECT 1 FROM payment_orders WHERE status = 'PENDING') THEN
        RAISE EXCEPTION 'active payment capability or pending payment remains';
    END IF;

    IF EXISTS (SELECT 1 FROM sub2api_plugin_bindings WHERE enabled OR rollout_percent <> 0)
       OR EXISTS (
           SELECT 1 FROM sub2api_plugin_installations
           WHERE state <> 'disabled' OR config_encrypted <> '' OR binary_path <> '' OR artifact_data IS NOT NULL
       ) THEN
        RAISE EXCEPTION 'active plugin capability remains';
    END IF;

    IF EXISTS (SELECT 1 FROM prompt_audit_jobs WHERE status IN ('staging', 'queued', 'processing', 'retry'))
       OR EXISTS (SELECT 1 FROM usage_cleanup_tasks WHERE status IN ('pending', 'running'))
       OR EXISTS (
           SELECT 1 FROM batch_image_jobs
           WHERE status IN ('created', 'uploading', 'submitted', 'running', 'indexing', 'settling')
       ) THEN
        RAISE EXCEPTION 'non-terminal background task remains';
    END IF;

    IF EXISTS (
        SELECT required_key
        FROM unnest(expected_false_keys) AS required_key
        LEFT JOIN settings s ON s.key = required_key
        WHERE s.key IS NULL OR lower(btrim(s.value)) <> 'false'
    ) THEN
        RAISE EXCEPTION 'required fail-closed boolean setting is missing or enabled';
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM settings
        WHERE key = 'upstream_billing_probe_settings'
          AND value::jsonb @> '{"enabled":false}'::jsonb
    ) OR NOT EXISTS (
        SELECT 1 FROM settings
        WHERE key = 'ollama_cloud_usage_settings'
          AND value::jsonb @> '{"enabled":false}'::jsonb
    ) OR NOT EXISTS (
        SELECT 1 FROM settings
        WHERE key = 'prompt_audit_config'
          AND value::jsonb @> '{"enabled":false,"blocking_enabled":false}'::jsonb
    ) OR NOT EXISTS (
        SELECT 1 FROM settings
        WHERE key = 'backup_schedule'
          AND value::jsonb @> '{"enabled":false}'::jsonb
    ) THEN
        RAISE EXCEPTION 'required JSON worker setting is missing or enabled';
    END IF;

    IF EXISTS (
        SELECT 1 FROM settings
        WHERE key IN (
            'smtp_host', 'smtp_username', 'smtp_password', 'smtp_from',
            'turnstile_site_key', 'turnstile_secret_key',
            'tencent_captcha_app_id', 'tencent_captcha_app_secret_key',
            'tencent_captcha_cloud_secret_id', 'tencent_captcha_cloud_secret_key',
            'aliyun_captcha_access_key_id', 'aliyun_captcha_access_key_secret',
            'linuxdo_connect_client_id', 'linuxdo_connect_client_secret',
            'dingtalk_connect_client_id', 'dingtalk_connect_client_secret',
            'wechat_connect_app_id', 'wechat_connect_app_secret',
            'wechat_connect_open_app_id', 'wechat_connect_open_app_secret',
            'wechat_connect_mp_app_id', 'wechat_connect_mp_app_secret',
            'wechat_connect_mobile_app_id', 'wechat_connect_mobile_app_secret',
            'oidc_connect_client_id', 'oidc_connect_client_secret',
            'github_oauth_client_id', 'github_oauth_client_secret',
            'google_oauth_client_id', 'google_oauth_client_secret',
            'admin_api_key'
        ) AND btrim(value) <> ''
    ) THEN
        RAISE EXCEPTION 'credential-bearing setting remains populated';
    END IF;

    IF EXISTS (SELECT 1 FROM security_secrets WHERE length(value) < 64)
       OR (SELECT count(*) FROM security_secrets) <> (SELECT count(DISTINCT value) FROM security_secrets) THEN
        RAISE EXCEPTION 'DB-managed secrets were not rotated safely';
    END IF;
END
$assert_safety$;

COMMIT;
