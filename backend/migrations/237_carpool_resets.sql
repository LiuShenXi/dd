-- Carpool reset observation, scheduling, execution targets, and announcement outbox.
CREATE TABLE IF NOT EXISTS carpool_reset_scope_states (
    id BIGSERIAL PRIMARY KEY,
    scope_id BIGINT NOT NULL UNIQUE,
    timezone VARCHAR(64) NOT NULL DEFAULT 'Asia/Shanghai',
    last_successful_reset_at TIMESTAMPTZ,
    pending_batch_id BIGINT,
    revision BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (timezone = 'Asia/Shanghai')
);

CREATE TABLE IF NOT EXISTS carpool_reset_batches (
    id BIGSERIAL PRIMARY KEY,
    scope_id BIGINT NOT NULL REFERENCES carpool_reset_scope_states(scope_id),
    status VARCHAR(24) NOT NULL,
    detected_at TIMESTAMPTZ NOT NULL,
    qualified_at TIMESTAMPTZ,
    slot_at TIMESTAMPTZ,
    scheduled_at TIMESTAMPTZ,
    schedule_revision INTEGER NOT NULL DEFAULT 0,
    effective_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    delay_reason TEXT,
    evidence JSONB NOT NULL DEFAULT '{}'::jsonb,
    announcement_state VARCHAR(24) NOT NULL DEFAULT 'pending',
    qualification_source VARCHAR(24) NOT NULL,
    source_event_key_hash VARCHAR(64) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (status IN ('qualified','scheduled','running','completed','cancelled','needs_review')),
    CHECK (announcement_state IN ('pending','published','correction_pending','failed')),
    CHECK (qualification_source IN ('automatic','administrator')),
    CHECK ((slot_at IS NULL) = (scheduled_at IS NULL)),
    UNIQUE (scope_id, source_event_key_hash)
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_carpool_reset_batches_pending_scope
    ON carpool_reset_batches(scope_id)
    WHERE status IN ('qualified','scheduled','running');
CREATE INDEX IF NOT EXISTS idx_carpool_reset_batches_status_schedule
    ON carpool_reset_batches(status, scheduled_at);

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'fk_carpool_reset_scope_pending_batch'
          AND conrelid = 'carpool_reset_scope_states'::regclass
    ) THEN
        ALTER TABLE carpool_reset_scope_states
            ADD CONSTRAINT fk_carpool_reset_scope_pending_batch
            FOREIGN KEY (pending_batch_id) REFERENCES carpool_reset_batches(id);
    END IF;
END $$;

CREATE TABLE IF NOT EXISTS carpool_reset_account_states (
    id BIGSERIAL PRIMARY KEY,
    upstream_identity_hash VARCHAR(64) NOT NULL UNIQUE,
    representative_account_id BIGINT REFERENCES accounts(id),
    baseline_complete BOOLEAN NOT NULL DEFAULT FALSE,
    last_observed_at TIMESTAMPTZ,
    last_complete_at TIMESTAMPTZ,
    health_status VARCHAR(24) NOT NULL DEFAULT 'unknown',
    known_credit_count INTEGER NOT NULL DEFAULT 0 CHECK (known_credit_count >= 0),
    incomplete_reason VARCHAR(64),
    revision BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (health_status IN ('unknown','healthy','incomplete','error'))
);

CREATE INDEX IF NOT EXISTS idx_carpool_reset_account_states_health
    ON carpool_reset_account_states(health_status, last_observed_at);

CREATE TABLE IF NOT EXISTS carpool_reset_credits (
    id BIGSERIAL PRIMARY KEY,
    account_state_id BIGINT NOT NULL REFERENCES carpool_reset_account_states(id),
    upstream_identity_hash VARCHAR(64) NOT NULL,
    credit_hash VARCHAR(64) NOT NULL,
    first_seen_at TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ,
    initial_stock BOOLEAN NOT NULL DEFAULT FALSE,
    assignment_status VARCHAR(24) NOT NULL DEFAULT 'pending',
    reset_batch_id BIGINT REFERENCES carpool_reset_batches(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (assignment_status IN ('baseline','pending','assigned','needs_review')),
    UNIQUE (upstream_identity_hash, credit_hash)
);

CREATE INDEX IF NOT EXISTS idx_carpool_reset_credits_assignment
    ON carpool_reset_credits(assignment_status, first_seen_at);
CREATE INDEX IF NOT EXISTS idx_carpool_reset_credits_batch
    ON carpool_reset_credits(reset_batch_id) WHERE reset_batch_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS carpool_reset_targets (
    id BIGSERIAL PRIMARY KEY,
    batch_id BIGINT NOT NULL REFERENCES carpool_reset_batches(id),
    term_id BIGINT NOT NULL REFERENCES carpool_terms(id),
    cycle_id BIGINT NOT NULL REFERENCES carpool_cycles(id),
    status VARCHAR(24) NOT NULL,
    granted_usd NUMERIC(20,8) NOT NULL DEFAULT 0 CHECK (granted_usd >= 0),
    executed_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (status IN ('succeeded','failed')),
    UNIQUE (batch_id, term_id),
    UNIQUE (batch_id, cycle_id)
);

CREATE INDEX IF NOT EXISTS idx_carpool_reset_targets_term
    ON carpool_reset_targets(term_id, executed_at DESC);

CREATE TABLE IF NOT EXISTS carpool_reset_announcement_outbox (
    id BIGSERIAL PRIMARY KEY,
    batch_id BIGINT NOT NULL REFERENCES carpool_reset_batches(id),
    scope_id BIGINT NOT NULL,
    event_kind VARCHAR(24) NOT NULL,
    schedule_revision INTEGER NOT NULL,
    status VARCHAR(24) NOT NULL DEFAULT 'pending',
    announcement_id BIGINT REFERENCES announcements(id),
    original_announcement_id BIGINT REFERENCES announcements(id),
    title VARCHAR(200) NOT NULL,
    content TEXT NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_error TEXT,
    published_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (event_kind IN ('qualification','correction','completed')),
    CHECK (status IN ('pending','publishing','published','failed')),
    UNIQUE (batch_id, event_kind, schedule_revision)
);

CREATE INDEX IF NOT EXISTS idx_carpool_reset_announcement_outbox_pending
    ON carpool_reset_announcement_outbox(status, next_attempt_at, id);

ALTER TABLE announcements ADD COLUMN IF NOT EXISTS source_type VARCHAR(32);
ALTER TABLE announcements ADD COLUMN IF NOT EXISTS source_id BIGINT;
ALTER TABLE announcements ADD COLUMN IF NOT EXISTS source_event_kind VARCHAR(32);
ALTER TABLE announcements ADD COLUMN IF NOT EXISTS source_revision INTEGER NOT NULL DEFAULT 0;

CREATE UNIQUE INDEX IF NOT EXISTS uq_announcements_source_event
    ON announcements(source_type, source_id, source_event_kind, source_revision)
    WHERE source_type IS NOT NULL AND source_id IS NOT NULL AND source_event_kind IS NOT NULL;
