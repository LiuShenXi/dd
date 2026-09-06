-- Carpool v1.3 core accounting. Tables are append-only/audited where noted.
CREATE TABLE IF NOT EXISTS carpool_plans (
    id BIGSERIAL PRIMARY KEY,
    code VARCHAR(32) NOT NULL,
    name VARCHAR(100) NOT NULL,
    list_price_cny NUMERIC(20,2) NOT NULL CHECK (list_price_cny >= 0),
    weekly_quota_usd NUMERIC(20,8) NOT NULL CHECK (weekly_quota_usd > 0),
    duration_days INTEGER NOT NULL DEFAULT 30 CHECK (duration_days = 30),
    cycle_days INTEGER NOT NULL DEFAULT 7 CHECK (cycle_days = 7),
    boost_ratio NUMERIC(10,8) NOT NULL DEFAULT 0.10000000 CHECK (boost_ratio = 0.10000000),
    boost_count INTEGER NOT NULL DEFAULT 2 CHECK (boost_count = 2),
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (code, version)
);

CREATE TABLE IF NOT EXISTS carpool_terms (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id),
    scope_id BIGINT NOT NULL,
    group_id BIGINT NOT NULL REFERENCES groups(id),
    plan_id BIGINT NOT NULL REFERENCES carpool_plans(id),
    plan_snapshot JSONB NOT NULL,
    starts_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    status VARCHAR(24) NOT NULL,
    boost_used INTEGER NOT NULL DEFAULT 0 CHECK (boost_used >= 0),
    source_mode VARCHAR(24) NOT NULL DEFAULT 'new',
    history_complete BOOLEAN NOT NULL DEFAULT TRUE,
	statistics_since TIMESTAMPTZ,
    created_by BIGINT NOT NULL REFERENCES users(id),
    notes TEXT,
    terminated_at TIMESTAMPTZ,
    terminated_by BIGINT REFERENCES users(id),
    termination_reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (starts_at < expires_at),
    CHECK (status IN ('pending','active','expired','terminated')),
    CHECK (source_mode IN ('new','takeover'))
);

CREATE INDEX IF NOT EXISTS idx_carpool_terms_user_scope_range ON carpool_terms(user_id, scope_id, starts_at, expires_at);
CREATE INDEX IF NOT EXISTS idx_carpool_terms_scope_status_range ON carpool_terms(scope_id, status, starts_at, expires_at);
CREATE INDEX IF NOT EXISTS idx_carpool_terms_group_id ON carpool_terms(group_id);

CREATE TABLE IF NOT EXISTS carpool_cycles (
    id BIGSERIAL PRIMARY KEY,
    term_id BIGINT NOT NULL REFERENCES carpool_terms(id),
    cycle_no INTEGER NOT NULL CHECK (cycle_no BETWEEN 1 AND 5),
    starts_at TIMESTAMPTZ NOT NULL,
    ends_at TIMESTAMPTZ NOT NULL,
    base_quota_usd NUMERIC(20,8) NOT NULL CHECK (base_quota_usd > 0),
    base_balance_usd NUMERIC(20,8) NOT NULL DEFAULT 0,
    boost_balance_usd NUMERIC(20,8) NOT NULL DEFAULT 0,
    manual_balance_usd NUMERIC(20,8) NOT NULL DEFAULT 0,
    state VARCHAR(24) NOT NULL DEFAULT 'scheduled',
    revision BIGINT NOT NULL DEFAULT 0,
    activated_at TIMESTAMPTZ,
    closed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (term_id, cycle_no),
    CHECK (starts_at < ends_at),
    CHECK (state IN ('scheduled','active','missed','closing','closed'))
);

CREATE INDEX IF NOT EXISTS idx_carpool_cycles_state_range ON carpool_cycles(state, starts_at, ends_at);

CREATE TABLE IF NOT EXISTS carpool_ledger (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id),
    term_id BIGINT NOT NULL REFERENCES carpool_terms(id),
    cycle_id BIGINT NOT NULL REFERENCES carpool_cycles(id),
    event_type VARCHAR(32) NOT NULL,
    bucket VARCHAR(16) NOT NULL CHECK (bucket IN ('base','boost','manual')),
    delta_usd NUMERIC(20,8) NOT NULL CHECK (delta_usd <> 0),
    event_key VARCHAR(180) NOT NULL UNIQUE,
    request_id VARCHAR(128),
    api_key_id BIGINT REFERENCES api_keys(id),
    reset_batch_id BIGINT,
    boost_slot INTEGER CHECK (boost_slot BETWEEN 1 AND 2),
    actor_id BIGINT REFERENCES users(id),
    reverses_ledger_id BIGINT REFERENCES carpool_ledger(id),
    reason TEXT,
    effective_at TIMESTAMPTZ NOT NULL,
    recorded_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_carpool_ledger_boost_slot ON carpool_ledger(term_id, boost_slot) WHERE boost_slot IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS uq_carpool_ledger_user_boost_request ON carpool_ledger(user_id, request_id) WHERE event_type = 'boost' AND request_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS uq_carpool_ledger_reversal ON carpool_ledger(reverses_ledger_id) WHERE reverses_ledger_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_carpool_ledger_user_recorded ON carpool_ledger(user_id, recorded_at DESC);
CREATE INDEX IF NOT EXISTS idx_carpool_ledger_term_cycle_recorded ON carpool_ledger(term_id, cycle_id, recorded_at DESC);
CREATE INDEX IF NOT EXISTS idx_carpool_ledger_request_api_key ON carpool_ledger(request_id, api_key_id);

CREATE TABLE IF NOT EXISTS carpool_payments (
    id BIGSERIAL PRIMARY KEY,
    term_id BIGINT NOT NULL REFERENCES carpool_terms(id),
    amount_cny NUMERIC(20,2) NOT NULL CHECK (amount_cny > 0),
    payment_kind VARCHAR(16) NOT NULL CHECK (payment_kind IN ('payment','refund')),
    paid_at TIMESTAMPTZ NOT NULL,
    channel VARCHAR(64) NOT NULL,
    external_order_no VARCHAR(128),
    request_id VARCHAR(128) NOT NULL,
    request_fingerprint VARCHAR(64) NOT NULL,
    recorded_by BIGINT NOT NULL REFERENCES users(id),
    notes TEXT,
    recorded_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (term_id, request_id)
);

CREATE INDEX IF NOT EXISTS idx_carpool_payments_term_paid ON carpool_payments(term_id, paid_at DESC);
CREATE INDEX IF NOT EXISTS idx_carpool_payments_external_order ON carpool_payments(external_order_no) WHERE external_order_no IS NOT NULL;

CREATE TABLE IF NOT EXISTS carpool_billing_requests (
    id BIGSERIAL PRIMARY KEY,
    request_id VARCHAR(128) NOT NULL,
    api_key_id BIGINT NOT NULL REFERENCES api_keys(id),
    user_id BIGINT NOT NULL REFERENCES users(id),
    group_id BIGINT NOT NULL REFERENCES groups(id),
    term_id BIGINT NOT NULL REFERENCES carpool_terms(id),
    cycle_id BIGINT NOT NULL REFERENCES carpool_cycles(id),
    admitted_at TIMESTAMPTZ NOT NULL,
    status VARCHAR(32) NOT NULL,
    request_fingerprint VARCHAR(64) NOT NULL,
    billing_payload JSONB,
    actual_cost_usd NUMERIC(20,8),
    retry_count INTEGER NOT NULL DEFAULT 0,
    last_error TEXT,
    receipt_recorded_at TIMESTAMPTZ,
    settled_at TIMESTAMPTZ,
    resolution VARCHAR(32),
    resolved_at TIMESTAMPTZ,
    resolved_by BIGINT REFERENCES users(id),
    resolution_reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (request_id, api_key_id),
    CHECK (status IN ('admitted','usage_known','settling','settled','reconcile_required')),
    CHECK (actual_cost_usd IS NULL OR actual_cost_usd >= 0),
    CHECK (resolution IS NULL OR resolution IN ('no_cost','actual_cost')),
    CHECK ((resolution IS NULL AND resolved_at IS NULL AND resolved_by IS NULL AND resolution_reason IS NULL) OR (resolution IS NOT NULL AND resolved_at IS NOT NULL AND resolved_by IS NOT NULL AND resolution_reason IS NOT NULL))
);

CREATE INDEX IF NOT EXISTS idx_carpool_billing_requests_status_updated ON carpool_billing_requests(status, updated_at);
CREATE INDEX IF NOT EXISTS idx_carpool_billing_requests_cycle_status ON carpool_billing_requests(cycle_id, status);

CREATE TABLE IF NOT EXISTS carpool_operations (
    id BIGSERIAL PRIMARY KEY,
    kind VARCHAR(32) NOT NULL,
    actor_id BIGINT NOT NULL REFERENCES users(id),
    key_hash VARCHAR(64) NOT NULL,
    request_fingerprint VARCHAR(64) NOT NULL,
    resource_type VARCHAR(32) NOT NULL,
    resource_id BIGINT NOT NULL,
    response JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (kind, actor_id, key_hash)
);

CREATE INDEX IF NOT EXISTS idx_carpool_operations_resource ON carpool_operations(resource_type, resource_id);

INSERT INTO carpool_plans(code, name, list_price_cny, weekly_quota_usd, duration_days, cycle_days, boost_ratio, boost_count, enabled, version)
VALUES
    ('four_seat', 'Four-seat', 330.00, 550.00000000, 30, 7, 0.10000000, 2, TRUE, 1),
    ('three_seat', 'Three-seat', 420.00, 700.00000000, 30, 7, 0.10000000, 2, TRUE, 1),
    ('two_seat', 'Two-seat', 655.00, 1100.00000000, 30, 7, 0.10000000, 2, TRUE, 1)
ON CONFLICT (code, version) DO NOTHING;
