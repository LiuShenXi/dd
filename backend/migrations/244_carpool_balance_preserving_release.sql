-- A binding selects the ledger for a user without changing shared routing.
-- Retain it after expiry: an expired term must never expose the old balance.
CREATE TABLE IF NOT EXISTS carpool_billing_bindings (
    user_id BIGINT PRIMARY KEY REFERENCES users(id),
    group_id BIGINT NOT NULL REFERENCES groups(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS carpool_release_imports (
    batch_key VARCHAR(128) PRIMARY KEY,
    request_fingerprint VARCHAR(64) NOT NULL,
    actor_id BIGINT NOT NULL REFERENCES users(id),
    group_id BIGINT NOT NULL REFERENCES groups(id),
    cycle_anchor TIMESTAMPTZ NOT NULL,
    result JSONB NOT NULL,
    committed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
