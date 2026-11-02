SET LOCAL lock_timeout = '3s';
SET LOCAL statement_timeout = '30s';

-- Durable conversion barrier for old-source requests. A process crash or a
-- delayed usage worker must never make a group appear drained. These rows do
-- not grant term quota and are not removed by TTL or normal usage-log cleanup.
CREATE TABLE IF NOT EXISTS subscription_quota_protected_requests (
    request_id VARCHAR(128) PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id),
    api_key_id BIGINT NOT NULL REFERENCES api_keys(id),
    group_id BIGINT NOT NULL REFERENCES groups(id),
    subscription_type VARCHAR(32) NOT NULL CHECK (subscription_type IN ('standard','subscription')),
    state VARCHAR(32) NOT NULL CHECK (state IN ('admitted','usage_pending','completed','no_charge','reconcile_required')),
    usage_payload JSONB CHECK (usage_payload IS NULL OR jsonb_typeof(usage_payload) = 'object'),
    reason VARCHAR(64),
    admitted_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    finished_at TIMESTAMPTZ,
    CHECK ((state IN ('completed','no_charge')) = (finished_at IS NOT NULL)),
    CHECK (state NOT IN ('usage_pending','completed') OR usage_payload IS NOT NULL)
);
CREATE INDEX IF NOT EXISTS subscription_quota_protected_requests_unfinished
    ON subscription_quota_protected_requests(group_id, admitted_at, request_id)
    WHERE state NOT IN ('completed','no_charge');
