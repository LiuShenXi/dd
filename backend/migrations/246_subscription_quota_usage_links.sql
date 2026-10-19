-- Additive Claude term quota structures. No group conversion or card grant.
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE usage_logs
  ADD COLUMN IF NOT EXISTS subscription_term_id BIGINT,
  ADD COLUMN IF NOT EXISTS subscription_quota_request_id BIGINT;
ALTER TABLE usage_logs ADD CONSTRAINT usage_logs_subscription_quota_pair
  CHECK ((subscription_term_id IS NULL) = (subscription_quota_request_id IS NULL))
  NOT VALID;
ALTER TABLE usage_logs ADD CONSTRAINT usage_logs_subscription_quota_request_fk
  FOREIGN KEY (subscription_quota_request_id, subscription_term_id)
  REFERENCES subscription_quota_requests(id, term_id) NOT VALID;
