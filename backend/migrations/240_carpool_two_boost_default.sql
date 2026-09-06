-- Default new memberships to two boosts; retain all existing plan/term snapshots.
ALTER TABLE carpool_plans ALTER COLUMN boost_count SET DEFAULT 2;

WITH latest AS (
    SELECT DISTINCT ON (code) *
    FROM carpool_plans
    ORDER BY code, version DESC, id DESC
)
INSERT INTO carpool_plans (
    code, name, list_price_cny, weekly_quota_usd, duration_days, cycle_days,
    boost_ratio, boost_count, enabled, version
)
SELECT code, name, list_price_cny, weekly_quota_usd, duration_days, cycle_days,
    boost_ratio, 2, enabled, version + 1
FROM latest
WHERE boost_count <> 2
ON CONFLICT (code, version) DO NOTHING;
