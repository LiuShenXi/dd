-- Internal immutable carpool attribution for ordinary usage-log persistence.
-- All columns remain nullable so historical and non-carpool rows are unchanged.
ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS carpool_term_id BIGINT,
    ADD COLUMN IF NOT EXISTS carpool_cycle_id BIGINT,
    ADD COLUMN IF NOT EXISTS carpool_admitted_at TIMESTAMPTZ;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'usage_logs_carpool_term_id_fkey'
          AND conrelid = 'usage_logs'::regclass
    ) THEN
        ALTER TABLE usage_logs
            ADD CONSTRAINT usage_logs_carpool_term_id_fkey
            FOREIGN KEY (carpool_term_id) REFERENCES carpool_terms(id) ON DELETE SET NULL;
    END IF;
END $$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'usage_logs_carpool_cycle_id_fkey'
          AND conrelid = 'usage_logs'::regclass
    ) THEN
        ALTER TABLE usage_logs
            ADD CONSTRAINT usage_logs_carpool_cycle_id_fkey
            FOREIGN KEY (carpool_cycle_id) REFERENCES carpool_cycles(id) ON DELETE SET NULL;
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_usage_logs_carpool_term_id
    ON usage_logs(carpool_term_id);

CREATE INDEX IF NOT EXISTS idx_usage_logs_carpool_cycle_id
    ON usage_logs(carpool_cycle_id);
