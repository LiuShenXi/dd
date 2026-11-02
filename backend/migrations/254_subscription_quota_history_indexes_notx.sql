-- Keep completed history out of admission/recovery polling. The ledger index
-- covers the exact full-sum invariant without replacing or weakening that check.
SET lock_timeout = '5s';
SET statement_timeout = '120s';
CREATE INDEX CONCURRENTLY IF NOT EXISTS subscription_quota_requests_reconciliation_hold
    ON subscription_quota_requests(term_id)
    WHERE status = 'reconcile_required' OR last_error_category = 'ledger_mismatch';
CREATE INDEX CONCURRENTLY IF NOT EXISTS subscription_quota_requests_backlog
    ON subscription_quota_requests(status, updated_at)
    INCLUDE (usage_log_recorded_at)
    WHERE status IN ('admitted','usage_known','reconcile_required')
       OR (status = 'settled' AND usage_log_recorded_at IS NULL);
CREATE INDEX CONCURRENTLY IF NOT EXISTS subscription_quota_ledger_term_total
    ON subscription_quota_ledger(term_id) INCLUDE (delta_used_usd);
RESET lock_timeout;
RESET statement_timeout;
