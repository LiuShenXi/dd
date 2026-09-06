-- Carpool v1.4: new terms use four weekly cycles over 28 days and three boosts.
-- Legacy plan rows and term snapshots remain valid and are never rewritten.
ALTER TABLE carpool_plans
    DROP CONSTRAINT IF EXISTS carpool_plans_duration_days_check;
ALTER TABLE carpool_plans
    ADD CONSTRAINT carpool_plans_duration_days_check
    CHECK (duration_days IN (28, 30));
ALTER TABLE carpool_plans
    ALTER COLUMN duration_days SET DEFAULT 28;

ALTER TABLE carpool_plans
    DROP CONSTRAINT IF EXISTS carpool_plans_boost_count_check;
ALTER TABLE carpool_plans
    ADD CONSTRAINT carpool_plans_boost_count_check
    CHECK (boost_count IN (2, 3));
ALTER TABLE carpool_plans
    ALTER COLUMN boost_count SET DEFAULT 3;

ALTER TABLE carpool_ledger
    DROP CONSTRAINT IF EXISTS carpool_ledger_boost_slot_check;
ALTER TABLE carpool_ledger
    ADD CONSTRAINT carpool_ledger_boost_slot_check
    CHECK (boost_slot BETWEEN 1 AND 3);

WITH latest AS (
    SELECT DISTINCT ON (code)
        code,
        name,
        list_price_cny,
        weekly_quota_usd,
        enabled,
        version,
        duration_days,
        cycle_days,
        boost_ratio,
        boost_count
    FROM carpool_plans
    ORDER BY code, version DESC, id DESC
), upgrades AS (
    SELECT
        code,
        name,
        list_price_cny,
        weekly_quota_usd,
        enabled,
        version + 1 AS version
    FROM latest
    WHERE duration_days <> 28
       OR cycle_days <> 7
       OR boost_ratio <> 0.10000000
       OR boost_count <> 3
)
INSERT INTO carpool_plans(
    code,
    name,
    list_price_cny,
    weekly_quota_usd,
    duration_days,
    cycle_days,
    boost_ratio,
    boost_count,
    enabled,
    version
)
SELECT
    code,
    name,
    list_price_cny,
    weekly_quota_usd,
    28,
    7,
    0.10000000,
    3,
    enabled,
    version
FROM upgrades
ON CONFLICT (code, version) DO NOTHING;
