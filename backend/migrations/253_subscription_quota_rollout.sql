SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE IF NOT EXISTS subscription_quota_rollouts (
 group_id BIGINT PRIMARY KEY REFERENCES groups(id),
 maintenance BOOLEAN NOT NULL DEFAULT false,
 converted BOOLEAN NOT NULL DEFAULT false,
 original_status VARCHAR(32) NOT NULL,
 revision BIGINT NOT NULL DEFAULT 0 CHECK(revision>=0),
 preview_token VARCHAR(64),
 preview_actor_id BIGINT REFERENCES users(id),
 preview_allocations JSONB NOT NULL DEFAULT '[]'::jsonb,
 updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);
CREATE TABLE IF NOT EXISTS subscription_quota_rollout_operations (
 id BIGSERIAL PRIMARY KEY,
 group_id BIGINT NOT NULL REFERENCES groups(id),
 actor_id BIGINT NOT NULL REFERENCES users(id),
 idempotency_key VARCHAR(128) NOT NULL,
 operation_kind VARCHAR(16) NOT NULL CHECK(operation_kind IN ('maintenance','conversion')),
 request_fingerprint VARCHAR(64) NOT NULL,
 result JSONB NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(group_id,idempotency_key)
);
CREATE TABLE IF NOT EXISTS subscription_quota_runtime_replicas (
 replica_id VARCHAR(128) NOT NULL,
 instance_token VARCHAR(128) NOT NULL,
 capabilities JSONB NOT NULL CHECK(jsonb_typeof(capabilities)='object'),
 heartbeat_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(replica_id,instance_token)
);
CREATE INDEX IF NOT EXISTS subscription_quota_runtime_replicas_heartbeat ON subscription_quota_runtime_replicas(heartbeat_at);

-- Only the controlled repository transaction may change a maintained group's
-- configuration. Generic admin edits cannot accidentally reopen legacy billing.
CREATE OR REPLACE FUNCTION subscription_quota_rollout_guard_group()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.platform <> 'anthropic' AND NEW.platform <> 'anthropic' THEN RETURN NEW; END IF;
 IF EXISTS(SELECT 1 FROM subscription_quota_rollouts WHERE group_id=OLD.id AND maintenance)
 AND current_setting('sub2api.quota_rollout_group',true) IS DISTINCT FROM OLD.id::text THEN
  RAISE EXCEPTION 'subscription quota group is in controlled maintenance' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$;
DROP TRIGGER IF EXISTS subscription_quota_rollout_guard_group ON groups;
CREATE TRIGGER subscription_quota_rollout_guard_group BEFORE UPDATE ON groups
 FOR EACH ROW EXECUTE FUNCTION subscription_quota_rollout_guard_group();

-- Preserve late trusted legacy usage increments. Only entitlement/lifecycle
-- changes are blocked; accounting counters and window maintenance can drain.
CREATE OR REPLACE FUNCTION subscription_quota_rollout_guard_subscription()
RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE gid BIGINT; old_gid BIGINT; new_gid BIGINT;
BEGIN
 IF TG_OP <> 'INSERT' THEN old_gid:=OLD.group_id; END IF;
 IF TG_OP <> 'DELETE' THEN new_gid:=NEW.group_id; END IF;
 -- Moving a parent out of maintenance is also a rights change. Lock the
 -- affected Anthropic groups in numeric order, with no GPT row locks.
 FOR gid IN SELECT id FROM groups WHERE id IN(old_gid,new_gid) AND platform='anthropic' ORDER BY id FOR SHARE LOOP
  IF EXISTS(SELECT 1 FROM subscription_quota_rollouts WHERE group_id=gid AND maintenance)
  AND current_setting('sub2api.quota_rollout_group',true) IS DISTINCT FROM gid::text THEN
   IF TG_OP<>'UPDATE' THEN
    RAISE EXCEPTION 'subscription quota maintenance blocks entitlement changes' USING ERRCODE='23514';
   END IF;
   IF ROW(NEW.user_id,NEW.group_id,NEW.starts_at,NEW.expires_at,NEW.status,NEW.deleted_at,NEW.quota_revision,NEW.assigned_by,NEW.notes)
      IS DISTINCT FROM ROW(OLD.user_id,OLD.group_id,OLD.starts_at,OLD.expires_at,OLD.status,OLD.deleted_at,OLD.quota_revision,OLD.assigned_by,OLD.notes) THEN
    RAISE EXCEPTION 'subscription quota maintenance blocks entitlement changes' USING ERRCODE='23514';
   END IF;
  END IF;
 END LOOP;
 IF TG_OP='DELETE' THEN RETURN OLD; END IF;
 RETURN NEW;
END;
$$;
DROP TRIGGER IF EXISTS subscription_quota_rollout_guard_subscription ON user_subscriptions;
CREATE TRIGGER subscription_quota_rollout_guard_subscription BEFORE INSERT OR UPDATE OR DELETE ON user_subscriptions
 FOR EACH ROW EXECUTE FUNCTION subscription_quota_rollout_guard_subscription();

-- Order creation takes the same group lock as maintenance. Paid notifications
-- may still record payment facts; fulfillment is deferred by the rights guard.
CREATE OR REPLACE FUNCTION subscription_quota_rollout_guard_order()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.subscription_group_id IS NOT NULL THEN
  PERFORM id FROM groups WHERE id=NEW.subscription_group_id AND platform='anthropic' FOR SHARE;
  IF NOT FOUND THEN RETURN NEW; END IF;
  IF EXISTS(SELECT 1 FROM subscription_quota_rollouts WHERE group_id=NEW.subscription_group_id AND maintenance) THEN
   RAISE EXCEPTION 'subscription quota maintenance blocks new orders' USING ERRCODE='23514';
  END IF;
 END IF;
 RETURN NEW;
END;
$$;
DROP TRIGGER IF EXISTS subscription_quota_rollout_guard_order ON payment_orders;
CREATE TRIGGER subscription_quota_rollout_guard_order BEFORE INSERT ON payment_orders
 FOR EACH ROW EXECUTE FUNCTION subscription_quota_rollout_guard_order();
