-- Every merged reset qualification remains independently deduplicated.
CREATE TABLE IF NOT EXISTS carpool_reset_qualifications (
    id BIGSERIAL PRIMARY KEY,
    scope_id BIGINT NOT NULL,
    batch_id BIGINT NOT NULL REFERENCES carpool_reset_batches(id),
    source VARCHAR(24) NOT NULL,
    source_event_key_hash VARCHAR(64) NOT NULL,
    reason TEXT NOT NULL,
    confirmed_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (source IN ('automatic','administrator')),
    UNIQUE (scope_id, source_event_key_hash)
);

CREATE INDEX IF NOT EXISTS idx_carpool_reset_qualifications_batch
    ON carpool_reset_qualifications(batch_id, id);

