SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '5min';
ALTER TABLE usage_logs VALIDATE CONSTRAINT usage_logs_subscription_quota_pair;
ALTER TABLE usage_logs VALIDATE CONSTRAINT usage_logs_subscription_quota_request_fk;
