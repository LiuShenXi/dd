-- Preserve historical ten-digit fractional precision while admitting every
-- valid NUMERIC(20,8) term amount in its durable statistics projection.
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';
-- The existing UPDATE trigger compares actual_cost in its WHEN expression.
-- Recreate that exact guard inside this transaction after changing the type.
DROP TRIGGER IF EXISTS usage_logs_group_rollup_invalidate_update ON usage_logs;
ALTER TABLE usage_logs
 ALTER COLUMN input_cost TYPE NUMERIC(22,10),
 ALTER COLUMN output_cost TYPE NUMERIC(22,10),
 ALTER COLUMN cache_creation_cost TYPE NUMERIC(22,10),
 ALTER COLUMN cache_read_cost TYPE NUMERIC(22,10),
 ALTER COLUMN total_cost TYPE NUMERIC(22,10),
 ALTER COLUMN actual_cost TYPE NUMERIC(22,10),
 ALTER COLUMN account_stats_cost TYPE NUMERIC(22,10);

CREATE TRIGGER usage_logs_group_rollup_invalidate_update
AFTER UPDATE OF created_at, group_id, actual_cost ON usage_logs
FOR EACH ROW
WHEN (
 (OLD.created_at IS DISTINCT FROM NEW.created_at
  OR OLD.group_id IS DISTINCT FROM NEW.group_id
  OR OLD.actual_cost IS DISTINCT FROM NEW.actual_cost)
 AND (OLD.group_id IS NOT NULL OR NEW.group_id IS NOT NULL)
)
EXECUTE FUNCTION invalidate_group_usage_rollup_state();

-- Aggregates may contain many independently valid term charges.
ALTER TABLE usage_group_daily_rollups ALTER COLUMN actual_cost TYPE NUMERIC(30,10);
ALTER TABLE usage_dashboard_hourly
 ALTER COLUMN total_cost TYPE NUMERIC(30,10),
 ALTER COLUMN actual_cost TYPE NUMERIC(30,10),
 ALTER COLUMN account_cost TYPE NUMERIC(30,10);
ALTER TABLE usage_dashboard_daily
 ALTER COLUMN total_cost TYPE NUMERIC(30,10),
 ALTER COLUMN actual_cost TYPE NUMERIC(30,10),
 ALTER COLUMN account_cost TYPE NUMERIC(30,10);
