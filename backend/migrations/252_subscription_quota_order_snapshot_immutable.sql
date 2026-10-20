SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Freeze purchased Claude rights independently of mutable plan/provider data.
-- Legacy orders with no snapshot retain their historical update behavior.
CREATE OR REPLACE FUNCTION subscription_quota_order_snapshot_immutable()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF OLD.subscription_quota_snapshot IS NOT NULL AND
    ROW(NEW.subscription_quota_snapshot, NEW.user_id, NEW.order_type,
        NEW.plan_id, NEW.subscription_group_id, NEW.subscription_days)
    IS DISTINCT FROM
    ROW(OLD.subscription_quota_snapshot, OLD.user_id, OLD.order_type,
        OLD.plan_id, OLD.subscription_group_id, OLD.subscription_days) THEN
    RAISE EXCEPTION 'subscription quota order entitlement is immutable'
      USING ERRCODE = '23514';
  END IF;
  RETURN NEW;
END;
$$;
DROP TRIGGER IF EXISTS subscription_quota_order_snapshot_immutable ON payment_orders;
CREATE TRIGGER subscription_quota_order_snapshot_immutable
  BEFORE UPDATE ON payment_orders FOR EACH ROW
  EXECUTE FUNCTION subscription_quota_order_snapshot_immutable();
