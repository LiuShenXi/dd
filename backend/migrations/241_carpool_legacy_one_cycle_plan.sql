-- Allow operator-confirmed legacy one-cycle terms without changing standard
-- 28-day plan defaults or the administrator plan-version API.
ALTER TABLE carpool_plans
    DROP CONSTRAINT IF EXISTS carpool_plans_duration_days_check;
ALTER TABLE carpool_plans
    ADD CONSTRAINT carpool_plans_duration_days_check
    CHECK (duration_days IN (7, 28, 30));

ALTER TABLE carpool_plans
    DROP CONSTRAINT IF EXISTS carpool_plans_boost_count_check;
ALTER TABLE carpool_plans
    ADD CONSTRAINT carpool_plans_boost_count_check
    CHECK (boost_count IN (0, 2, 3));
