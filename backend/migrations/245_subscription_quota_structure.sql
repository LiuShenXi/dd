-- Additive Claude term quota structures. No group conversion or card grant.
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE groups
  ADD COLUMN IF NOT EXISTS subscription_quota_mode VARCHAR(16) NOT NULL DEFAULT 'windows';
ALTER TABLE groups ADD CONSTRAINT groups_subscription_quota_mode_check CHECK (
  subscription_quota_mode = 'windows'
  OR (subscription_quota_mode = 'term'
      AND subscription_type = 'subscription' AND platform = 'anthropic'
      AND fallback_group_id IS NULL AND fallback_group_id_on_invalid_request IS NULL)
);

ALTER TABLE user_subscriptions
  ADD COLUMN IF NOT EXISTS quota_revision BIGINT NOT NULL DEFAULT 0;
ALTER TABLE user_subscriptions
  ADD CONSTRAINT user_subscriptions_quota_identity_unique
  UNIQUE (id, user_id, group_id);

ALTER TABLE subscription_plans ADD COLUMN IF NOT EXISTS term_quota_usd NUMERIC(20,8);
ALTER TABLE subscription_plans ADD CONSTRAINT subscription_plans_term_quota_positive
  CHECK (term_quota_usd IS NULL OR (term_quota_usd > 0 AND term_quota_usd <> 'NaN'::numeric));
ALTER TABLE payment_orders ADD COLUMN IF NOT EXISTS subscription_quota_snapshot JSONB;
ALTER TABLE payment_orders ADD CONSTRAINT payment_orders_quota_snapshot_object
  CHECK (subscription_quota_snapshot IS NULL
      OR jsonb_typeof(subscription_quota_snapshot) = 'object');

CREATE TABLE IF NOT EXISTS subscription_quota_terms (
  id BIGSERIAL PRIMARY KEY,
  subscription_id BIGINT NOT NULL,
  user_id BIGINT NOT NULL,
  group_id BIGINT NOT NULL,
  term_no INTEGER NOT NULL CHECK (term_no > 0),
  quota_usd NUMERIC(20,8) NOT NULL CHECK (quota_usd > 0 AND quota_usd <> 'NaN'::numeric),
  used_usd NUMERIC(20,8) NOT NULL DEFAULT 0 CHECK (used_usd >= 0 AND used_usd <> 'NaN'::numeric),
  starts_at TIMESTAMPTZ NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL,
  lifecycle_state VARCHAR(16) NOT NULL
    CHECK (lifecycle_state IN ('scheduled','active','closing','closed')),
  cancelled_at TIMESTAMPTZ,
  closed_at TIMESTAMPTZ,
  revision BIGINT NOT NULL DEFAULT 0 CHECK (revision >= 0),
  source_kind VARCHAR(16) NOT NULL CHECK (source_kind IN ('admin','payment')),
  source_plan_id BIGINT,
  source_snapshot JSONB NOT NULL CHECK (jsonb_typeof(source_snapshot) = 'object'),
  payment_order_id BIGINT UNIQUE REFERENCES payment_orders(id),
  created_by BIGINT REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
  CHECK (expires_at = starts_at + INTERVAL '168 hours'),
  CHECK ((source_kind = 'payment') = (payment_order_id IS NOT NULL)),
  UNIQUE (subscription_id, term_no),
  UNIQUE (id, subscription_id, user_id, group_id),
  FOREIGN KEY (subscription_id, user_id, group_id)
    REFERENCES user_subscriptions(id, user_id, group_id)
);
CREATE INDEX IF NOT EXISTS subscription_quota_terms_lookup
  ON subscription_quota_terms(subscription_id, starts_at, expires_at)
  WHERE cancelled_at IS NULL;
CREATE INDEX IF NOT EXISTS subscription_quota_terms_maintenance
  ON subscription_quota_terms(lifecycle_state, expires_at, id);

CREATE TABLE IF NOT EXISTS subscription_quota_requests (
  id BIGSERIAL PRIMARY KEY,
  request_id VARCHAR(255) NOT NULL,
  api_key_id BIGINT NOT NULL REFERENCES api_keys(id),
  subscription_id BIGINT NOT NULL,
  term_id BIGINT NOT NULL,
  user_id BIGINT NOT NULL,
  group_id BIGINT NOT NULL,
  admitted_at TIMESTAMPTZ NOT NULL,
  pricing_at TIMESTAMPTZ NOT NULL,
  pricing_snapshot JSONB NOT NULL CHECK (jsonb_typeof(pricing_snapshot) = 'object'),
  revision BIGINT NOT NULL DEFAULT 0 CHECK (revision >= 0),
  status VARCHAR(24) NOT NULL CHECK (status IN
    ('admitted','usage_known','settled','no_charge','reconcile_required')),
  actual_cost_usd NUMERIC(20,8) CHECK (actual_cost_usd >= 0 AND actual_cost_usd <> 'NaN'::numeric),
  receipt_version INTEGER CHECK (receipt_version > 0),
  request_fingerprint VARCHAR(64),
  billing_payload JSONB CHECK (billing_payload IS NULL OR jsonb_typeof(billing_payload) = 'object'),
  usage_log_payload JSONB CHECK (usage_log_payload IS NULL OR jsonb_typeof(usage_log_payload) = 'object'),
  usage_log_recorded_at TIMESTAMPTZ,
  resolution_evidence JSONB,
  recovery_attempts INTEGER NOT NULL DEFAULT 0 CHECK (recovery_attempts >= 0),
  next_retry_at TIMESTAMPTZ,
  lease_until TIMESTAMPTZ,
  lease_owner VARCHAR(64),
  last_error_category VARCHAR(64),
  created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
  settled_at TIMESTAMPTZ,
  UNIQUE (request_id, api_key_id),
  UNIQUE (id, term_id),
  FOREIGN KEY (term_id, subscription_id, user_id, group_id)
    REFERENCES subscription_quota_terms(id, subscription_id, user_id, group_id),
  CHECK (status <> 'admitted' OR actual_cost_usd IS NULL),
  CHECK (status NOT IN ('usage_known','settled')
    OR (actual_cost_usd IS NOT NULL AND receipt_version IS NOT NULL
        AND request_fingerprint IS NOT NULL AND billing_payload IS NOT NULL
        AND usage_log_payload IS NOT NULL)),
  CHECK (status <> 'settled' OR settled_at IS NOT NULL),
  CHECK (status <> 'no_charge'
    OR (actual_cost_usd IS NOT NULL AND actual_cost_usd = 0
        AND resolution_evidence IS NOT NULL))
);
CREATE INDEX IF NOT EXISTS subscription_quota_requests_recovery
  ON subscription_quota_requests(next_retry_at, id)
  WHERE status = 'usage_known';
CREATE INDEX IF NOT EXISTS subscription_quota_requests_stale
  ON subscription_quota_requests(updated_at, id)
  WHERE status = 'admitted';
CREATE INDEX IF NOT EXISTS subscription_quota_requests_usage_log_pending
  ON subscription_quota_requests(id)
  WHERE status = 'settled' AND usage_log_recorded_at IS NULL;
CREATE INDEX IF NOT EXISTS subscription_quota_requests_term_status
  ON subscription_quota_requests(term_id, status, id);

CREATE TABLE IF NOT EXISTS subscription_quota_operations (
  id BIGSERIAL PRIMARY KEY,
  idempotency_scope VARCHAR(200) NOT NULL,
  idempotency_key VARCHAR(128) NOT NULL,
  operation_kind VARCHAR(32) NOT NULL
    CHECK (operation_kind IN ('open_term','renew_term','cancel_term','resolve_request')),
  actor_id BIGINT REFERENCES users(id),
  request_fingerprint VARCHAR(64) NOT NULL,
  result JSONB NOT NULL CHECK (jsonb_typeof(result) = 'object'),
  created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
  UNIQUE (idempotency_scope, idempotency_key)
);

CREATE TABLE IF NOT EXISTS subscription_quota_ledger (
  id BIGSERIAL PRIMARY KEY,
  term_id BIGINT NOT NULL REFERENCES subscription_quota_terms(id),
  request_row_id BIGINT NOT NULL,
  event_key VARCHAR(255) NOT NULL UNIQUE,
  entry_kind VARCHAR(24) NOT NULL
    CHECK (entry_kind IN ('usage_debit','usage_reversal')),
  delta_used_usd NUMERIC(20,8) NOT NULL CHECK (delta_used_usd <> 'NaN'::numeric),
  reverses_entry_id BIGINT,
  actor_id BIGINT REFERENCES users(id),
  reason VARCHAR(500),
  created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
  UNIQUE (id, term_id),
  FOREIGN KEY (request_row_id, term_id)
    REFERENCES subscription_quota_requests(id, term_id),
  FOREIGN KEY (reverses_entry_id, term_id)
    REFERENCES subscription_quota_ledger(id, term_id),
  CHECK (
    (entry_kind = 'usage_debit' AND delta_used_usd >= 0 AND reverses_entry_id IS NULL)
    OR
    (entry_kind = 'usage_reversal' AND delta_used_usd < 0
      AND reverses_entry_id IS NOT NULL AND actor_id IS NOT NULL AND reason IS NOT NULL)
  )
);
CREATE UNIQUE INDEX IF NOT EXISTS subscription_quota_ledger_one_debit
  ON subscription_quota_ledger(request_row_id)
  WHERE entry_kind = 'usage_debit';
CREATE UNIQUE INDEX IF NOT EXISTS subscription_quota_ledger_one_reversal
  ON subscription_quota_ledger(reverses_entry_id)
  WHERE reverses_entry_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS subscription_quota_ledger_term_time
  ON subscription_quota_ledger(term_id, created_at, id);

CREATE FUNCTION subscription_quota_ledger_append_only() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'subscription quota ledger is append only';
END;
$$;
CREATE TRIGGER subscription_quota_ledger_immutable
  BEFORE UPDATE OR DELETE ON subscription_quota_ledger
  FOR EACH ROW EXECUTE FUNCTION subscription_quota_ledger_append_only();

CREATE FUNCTION subscription_quota_term_identity_immutable() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF ROW(NEW.subscription_id, NEW.user_id, NEW.group_id, NEW.term_no,
         NEW.quota_usd, NEW.starts_at, NEW.expires_at, NEW.source_kind,
         NEW.source_plan_id, NEW.source_snapshot, NEW.payment_order_id)
     IS DISTINCT FROM
     ROW(OLD.subscription_id, OLD.user_id, OLD.group_id, OLD.term_no,
         OLD.quota_usd, OLD.starts_at, OLD.expires_at, OLD.source_kind,
         OLD.source_plan_id, OLD.source_snapshot, OLD.payment_order_id) THEN
    RAISE EXCEPTION 'subscription quota term identity and allowance are immutable';
  END IF;
  RETURN NEW;
END;
$$;
CREATE TRIGGER subscription_quota_term_immutable_fields
  BEFORE UPDATE ON subscription_quota_terms
  FOR EACH ROW EXECUTE FUNCTION subscription_quota_term_identity_immutable();


-- Preserve issued terms when stale or legacy administrative clients edit groups.
CREATE FUNCTION subscription_quota_group_identity_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF OLD.subscription_quota_mode = 'term'
     AND ROW(NEW.subscription_quota_mode, NEW.subscription_type, NEW.platform)
       IS DISTINCT FROM ROW(OLD.subscription_quota_mode, OLD.subscription_type, OLD.platform)
     AND EXISTS (SELECT 1 FROM subscription_quota_terms WHERE group_id = OLD.id) THEN
    RAISE EXCEPTION 'issued subscription quota group identity is immutable';
  END IF;
  RETURN NEW;
END;
$$;
CREATE TRIGGER subscription_quota_group_identity_immutable
  BEFORE UPDATE ON groups FOR EACH ROW
  EXECUTE FUNCTION subscription_quota_group_identity_guard();
