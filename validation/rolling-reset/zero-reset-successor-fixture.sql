-- Synthetic display fixture only. Actual execution is covered by PostgreSQL tests.
BEGIN;
SELECT set_config('rolling_fixture.target_email', :'target_email', TRUE) IS NOT NULL AS fixture_selected;

DO $$
DECLARE
    member RECORD;
    source_cycle RECORD;
    batch carpool_reset_batches%ROWTYPE;
    target carpool_reset_targets%ROWTYPE;
    reset_at TIMESTAMPTZ;
    next_end TIMESTAMPTZ;
    successor_id BIGINT;
    ordinary_before NUMERIC;
BEGIN
    SELECT u.id AS user_id,u.balance,t.id AS term_id,t.scope_id,t.starts_at,t.expires_at,
           t.plan_snapshot->>'reset_mode' AS reset_mode
      INTO STRICT member
      FROM users u JOIN carpool_terms t ON t.user_id=u.id
     WHERE u.email=current_setting('rolling_fixture.target_email')
       AND u.email LIKE 'rolling-reset-active_reset_marker-%@example.invalid'
       AND t.status='active' AND t.starts_at<=clock_timestamp() AND t.expires_at>clock_timestamp()
     FOR UPDATE OF u,t;
    ordinary_before := member.balance;
    IF member.reset_mode IS DISTINCT FROM 'rolling' OR member.expires_at-member.starts_at <> INTERVAL '28 days' THEN
        RAISE EXCEPTION 'fixture must be an active synthetic 28-day rolling term';
    END IF;

    SELECT * INTO batch FROM carpool_reset_batches
     WHERE source_event_key_hash=repeat('0',63)||'7' FOR UPDATE;
    IF FOUND THEN
        IF batch.evidence->>'source' IS DISTINCT FROM 'synthetic-browser-fixture' OR batch.status <> 'completed' THEN
            RAISE EXCEPTION 'refusing to alter a non-fixture reset';
        END IF;
        SELECT * INTO STRICT target FROM carpool_reset_targets WHERE batch_id=batch.id;
        IF target.term_id <> member.term_id OR target.status <> 'succeeded' OR target.granted_usd <> 0 THEN
            RAISE EXCEPTION 'fixture target does not match selected synthetic term';
        END IF;
        IF batch.evidence->>'period_representation' = 'successor' THEN
            IF NOT EXISTS (
                SELECT 1 FROM carpool_cycles previous JOIN carpool_cycles successor
                  ON successor.term_id=previous.term_id AND successor.cycle_no=previous.cycle_no+1
                 WHERE successor.id=target.cycle_id AND previous.ends_at=target.executed_at
                   AND successor.starts_at=target.executed_at AND previous.state IN ('closing','closed')
                   AND successor.ends_at-successor.starts_at<=INTERVAL '7 days'
            ) THEN
                RAISE EXCEPTION 'existing successor fixture is inconsistent';
            END IF;
            RAISE NOTICE 'successor fixture already consistent; no mutation';
            RETURN;
        END IF;
        reset_at := target.executed_at;
    ELSE
        reset_at := clock_timestamp();
    END IF;

    SELECT * INTO STRICT source_cycle FROM carpool_cycles
     WHERE term_id=member.term_id AND state='active' FOR UPDATE;
    IF source_cycle.cycle_no <> 1 OR source_cycle.base_balance_usd <> source_cycle.base_quota_usd
       OR source_cycle.boost_balance_usd <> 0 OR source_cycle.manual_balance_usd <> 0
       OR (SELECT count(*) FROM carpool_cycles WHERE term_id=member.term_id) <> 1
       OR EXISTS (SELECT 1 FROM carpool_billing_requests WHERE term_id=member.term_id)
       OR (SELECT count(*) FROM carpool_ledger WHERE term_id=member.term_id) <> 1
       OR NOT EXISTS (SELECT 1 FROM carpool_ledger WHERE cycle_id=source_cycle.id
                       AND event_type='cycle_initial' AND bucket='base' AND delta_usd=source_cycle.base_quota_usd)
       OR (batch.id IS NOT NULL AND target.cycle_id <> source_cycle.id)
       OR reset_at <= source_cycle.starts_at OR reset_at >= member.expires_at THEN
        RAISE EXCEPTION 'fixture has real activity or unexpected history; refusing normalization';
    END IF;
    next_end := LEAST(reset_at+INTERVAL '7 days',member.expires_at);
    IF clock_timestamp() >= next_end THEN
        RAISE EXCEPTION 'fixture is stale; create a fresh scenario instead';
    END IF;

    IF batch.id IS NULL THEN
        INSERT INTO carpool_reset_scope_states(scope_id,timezone,revision)
        VALUES(member.scope_id,'Asia/Shanghai',0) ON CONFLICT(scope_id) DO NOTHING;
        INSERT INTO carpool_reset_batches(scope_id,status,detected_at,qualified_at,slot_at,scheduled_at,
            schedule_revision,effective_at,completed_at,evidence,announcement_state,qualification_source,source_event_key_hash)
        VALUES(member.scope_id,'completed',reset_at,reset_at,reset_at,reset_at,1,reset_at,reset_at,
            '{"source":"synthetic-browser-fixture","proves_scheduling":false,"grant":"zero"}'::jsonb,
            'published','administrator',repeat('0',63)||'7') RETURNING * INTO batch;
        UPDATE carpool_reset_scope_states SET last_successful_reset_at=GREATEST(last_successful_reset_at,reset_at),
            revision=revision+1,updated_at=clock_timestamp() WHERE scope_id=member.scope_id;
    END IF;

    UPDATE carpool_cycles SET ends_at=reset_at,base_balance_usd=0,state='closed',closed_at=reset_at,
        updated_at=clock_timestamp(),revision=revision+1 WHERE id=source_cycle.id;
    INSERT INTO carpool_ledger(user_id,term_id,cycle_id,event_type,bucket,delta_usd,event_key,reason,effective_at)
    VALUES(member.user_id,member.term_id,source_cycle.id,'expiry','base',-source_cycle.base_quota_usd,
        'cycle_expiry:'||source_cycle.id||':base','synthetic zero-reset predecessor expiry',reset_at);
    INSERT INTO carpool_cycles(term_id,cycle_no,starts_at,ends_at,base_quota_usd,base_balance_usd,
        boost_balance_usd,manual_balance_usd,state,activated_at)
    VALUES(member.term_id,source_cycle.cycle_no+1,reset_at,next_end,source_cycle.base_quota_usd,
        source_cycle.base_quota_usd,0,0,'active',reset_at) RETURNING id INTO successor_id;
    INSERT INTO carpool_ledger(user_id,term_id,cycle_id,event_type,bucket,delta_usd,event_key,reset_batch_id,reason,effective_at)
    VALUES(member.user_id,member.term_id,successor_id,'reset','base',source_cycle.base_quota_usd,
        'reset:'||batch.id||':'||successor_id,batch.id,'synthetic zero-reset successor grant',reset_at);
    INSERT INTO carpool_cycle_carryovers(source_cycle_id,initial_target_cycle_id,reset_batch_id,expires_at,status,completed_at)
    VALUES(source_cycle.id,successor_id,batch.id,next_end,'completed',reset_at);
    IF target.id IS NULL THEN
        INSERT INTO carpool_reset_targets(batch_id,term_id,cycle_id,status,granted_usd,executed_at)
        VALUES(batch.id,member.term_id,successor_id,'succeeded',0,reset_at);
    ELSE
        UPDATE carpool_reset_targets SET cycle_id=successor_id WHERE id=target.id;
    END IF;
    UPDATE carpool_reset_batches SET evidence=evidence||'{"period_representation":"successor","proves_execution":false}'::jsonb
     WHERE id=batch.id;

    IF EXISTS (
        SELECT 1 FROM carpool_cycles c WHERE c.term_id=member.term_id
         AND c.base_balance_usd+c.boost_balance_usd+c.manual_balance_usd <>
             (SELECT COALESCE(SUM(delta_usd),0) FROM carpool_ledger WHERE cycle_id=c.id)
    ) OR (SELECT balance FROM users WHERE id=member.user_id) <> ordinary_before THEN
        RAISE EXCEPTION 'synthetic fixture ledger or ordinary balance invariant failed';
    END IF;
END $$;
COMMIT;

SELECT 'synthetic-display-only' AS evidence_kind,c.cycle_no,c.starts_at,c.ends_at,c.state
  FROM users u JOIN carpool_terms t ON t.user_id=u.id JOIN carpool_cycles c ON c.term_id=t.id
 WHERE u.email=:'target_email' AND t.status='active'
 ORDER BY c.cycle_no;
