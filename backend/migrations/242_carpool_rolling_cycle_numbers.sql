-- Rolling reset terms create actual periods on every natural or special reset,
-- so their cycle sequence is not bounded by the legacy five-cycle schedule.
ALTER TABLE carpool_cycles
    DROP CONSTRAINT IF EXISTS carpool_cycles_cycle_no_check;
ALTER TABLE carpool_cycles
    ADD CONSTRAINT carpool_cycles_cycle_no_check CHECK (cycle_no > 0);

-- Natural and special advancement may serialize at the exact same database
-- timestamp. The superseded period is retained as zero-duration history while
-- the special-reset successor is the sole active period.
ALTER TABLE carpool_cycles
    DROP CONSTRAINT IF EXISTS carpool_cycles_check;
ALTER TABLE carpool_cycles
    ADD CONSTRAINT carpool_cycles_window_check
    CHECK (starts_at < ends_at OR (starts_at = ends_at AND state IN ('closing','closed')));

CREATE TABLE IF NOT EXISTS carpool_cycle_carryovers (
    source_cycle_id BIGINT PRIMARY KEY REFERENCES carpool_cycles(id),
    initial_target_cycle_id BIGINT NOT NULL REFERENCES carpool_cycles(id),
    reset_batch_id BIGINT NOT NULL REFERENCES carpool_reset_batches(id),
    expires_at TIMESTAMPTZ NOT NULL,
    status VARCHAR(16) NOT NULL CHECK (status IN ('pending','completed','expired')),
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
