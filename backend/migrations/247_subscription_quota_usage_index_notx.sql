-- Separate nontransactional online index. Retain all historical rows.
SET lock_timeout = '5s';
SET statement_timeout = '5min';
CREATE INDEX CONCURRENTLY IF NOT EXISTS usage_logs_subscription_term_time
 ON usage_logs(subscription_term_id, created_at) WHERE subscription_term_id IS NOT NULL;
RESET lock_timeout;
RESET statement_timeout;
