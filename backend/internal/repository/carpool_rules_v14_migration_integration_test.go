//go:build integration

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestCarpoolRulesV14Migration_ReplayPreservesLegacyRowsAndCustomPlanState(t *testing.T) {
	ctx := context.Background()
	schema := "carpool_rules_v14_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quotedSchema := pq.QuoteIdentifier(schema)
	_, err := integrationDB.ExecContext(ctx, "CREATE SCHEMA "+quotedSchema)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), "DROP SCHEMA IF EXISTS "+quotedSchema+" CASCADE")
	})

	tx, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	_, err = tx.ExecContext(ctx, "SET LOCAL search_path TO "+quotedSchema)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `
		CREATE TABLE carpool_plans (
			id BIGSERIAL PRIMARY KEY,
			code VARCHAR(32) NOT NULL,
			name VARCHAR(100) NOT NULL,
			list_price_cny NUMERIC(20,2) NOT NULL,
			weekly_quota_usd NUMERIC(20,8) NOT NULL,
			duration_days INTEGER NOT NULL DEFAULT 30 CHECK (duration_days = 30),
			cycle_days INTEGER NOT NULL DEFAULT 7 CHECK (cycle_days = 7),
			boost_ratio NUMERIC(10,8) NOT NULL DEFAULT 0.10000000 CHECK (boost_ratio = 0.10000000),
			boost_count INTEGER NOT NULL DEFAULT 2 CHECK (boost_count = 2),
			enabled BOOLEAN NOT NULL DEFAULT TRUE,
			version INTEGER NOT NULL,
			UNIQUE (code, version)
		);
		CREATE TABLE carpool_terms (
			id BIGSERIAL PRIMARY KEY,
			plan_id BIGINT NOT NULL,
			plan_snapshot JSONB NOT NULL,
			starts_at TIMESTAMPTZ NOT NULL,
			expires_at TIMESTAMPTZ NOT NULL,
			status VARCHAR(24) NOT NULL,
			boost_used INTEGER NOT NULL
		);
		CREATE TABLE carpool_cycles (
			id BIGSERIAL PRIMARY KEY,
			term_id BIGINT NOT NULL,
			cycle_no INTEGER NOT NULL,
			starts_at TIMESTAMPTZ NOT NULL,
			ends_at TIMESTAMPTZ NOT NULL,
			base_quota_usd NUMERIC(20,8) NOT NULL,
			base_balance_usd NUMERIC(20,8) NOT NULL,
			state VARCHAR(24) NOT NULL
		);
		CREATE TABLE carpool_ledger (
			id BIGSERIAL PRIMARY KEY,
			term_id BIGINT NOT NULL,
			cycle_id BIGINT NOT NULL,
			event_type VARCHAR(32) NOT NULL,
			delta_usd NUMERIC(20,8) NOT NULL,
			event_key VARCHAR(180) NOT NULL UNIQUE,
			boost_slot INTEGER CHECK (boost_slot BETWEEN 1 AND 2)
		);
	`)
	require.NoError(t, err)

	var fourSeatPlanID int64
	require.NoError(t, tx.QueryRowContext(ctx, `
		INSERT INTO carpool_plans(code,name,list_price_cny,weekly_quota_usd,duration_days,cycle_days,boost_ratio,boost_count,enabled,version)
		VALUES('four_seat','Custom four-seat',345.67,575.12500000,30,7,0.10000000,2,TRUE,4)
		RETURNING id
	`).Scan(&fourSeatPlanID))
	_, err = tx.ExecContext(ctx, `
		INSERT INTO carpool_plans(code,name,list_price_cny,weekly_quota_usd,duration_days,cycle_days,boost_ratio,boost_count,enabled,version)
		VALUES
			('three_seat','Paused custom three-seat',432.10,725.50000000,30,7,0.10000000,2,FALSE,7),
			('two_seat','Custom two-seat',688.00,1175.00000000,30,7,0.10000000,2,TRUE,2)
	`)
	require.NoError(t, err)

	legacySnapshot := fmt.Sprintf(`{"plan_id":%d,"code":"four_seat","name":"Custom four-seat","version":4,"list_price_cny":"345.67","weekly_quota_usd":"575.12500000","cycle_5_quota_usd":"164.00000000","duration_days":30,"cycle_days":7,"boost_ratio":"0.10000000","boost_amount_usd":"57.51250000","boost_count":2,"rounding_mode":"half_up","enabled":true}`, fourSeatPlanID)
	var termID int64
	require.NoError(t, tx.QueryRowContext(ctx, `
		INSERT INTO carpool_terms(plan_id,plan_snapshot,starts_at,expires_at,status,boost_used)
		VALUES($1,$2::jsonb,'2026-01-01T00:00:00Z','2026-01-31T00:00:00Z','active',2)
		RETURNING id
	`, fourSeatPlanID, legacySnapshot).Scan(&termID))
	var firstCycleID int64
	require.NoError(t, tx.QueryRowContext(ctx, `
		INSERT INTO carpool_cycles(term_id,cycle_no,starts_at,ends_at,base_quota_usd,base_balance_usd,state)
		VALUES($1,1,'2026-01-01T00:00:00Z','2026-01-08T00:00:00Z',575.12500000,500.00000000,'closed')
		RETURNING id
	`, termID).Scan(&firstCycleID))
	_, err = tx.ExecContext(ctx, `
		INSERT INTO carpool_cycles(term_id,cycle_no,starts_at,ends_at,base_quota_usd,base_balance_usd,state)
		VALUES($1,5,'2026-01-29T00:00:00Z','2026-01-31T00:00:00Z',164.00000000,164.00000000,'active')
	`, termID)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `
		INSERT INTO carpool_ledger(term_id,cycle_id,event_type,delta_usd,event_key,boost_slot)
		VALUES($1,$2,'boost',57.51250000,'legacy-boost-slot-2',2)
	`, termID, firstCycleID)
	require.NoError(t, err)

	legacyBefore := carpoolRulesV14LegacyFingerprint(t, ctx, tx)
	migrationSQL, err := migrations.FS.ReadFile("239_carpool_rules_v14.sql")
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, string(migrationSQL))
	require.NoError(t, err)

	type planState struct {
		name       string
		price      string
		weekly     string
		duration   int
		cycleDays  int
		ratio      string
		boostCount int
		enabled    bool
		version    int
	}
	readLatest := func(code string) planState {
		t.Helper()
		var state planState
		require.NoError(t, tx.QueryRowContext(ctx, `
			SELECT name,list_price_cny::text,weekly_quota_usd::text,duration_days,cycle_days,
				boost_ratio::text,boost_count,enabled,version
			FROM carpool_plans WHERE code=$1 ORDER BY version DESC,id DESC LIMIT 1
		`, code).Scan(&state.name, &state.price, &state.weekly, &state.duration, &state.cycleDays, &state.ratio, &state.boostCount, &state.enabled, &state.version))
		return state
	}
	require.Equal(t, planState{"Custom four-seat", "345.67", "575.12500000", 28, 7, "0.10000000", 3, true, 5}, readLatest("four_seat"))
	require.Equal(t, planState{"Paused custom three-seat", "432.10", "725.50000000", 28, 7, "0.10000000", 3, false, 8}, readLatest("three_seat"))
	require.Equal(t, planState{"Custom two-seat", "688.00", "1175.00000000", 28, 7, "0.10000000", 3, true, 3}, readLatest("two_seat"))
	require.Equal(t, legacyBefore, carpoolRulesV14LegacyFingerprint(t, ctx, tx))

	_, err = tx.ExecContext(ctx, `
		INSERT INTO carpool_plans(code,name,list_price_cny,weekly_quota_usd,duration_days,cycle_days,boost_ratio,boost_count,enabled,version)
		VALUES('already_current','Already current',99.00,125.00000000,28,7,0.10000000,3,TRUE,1)
	`)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `
		INSERT INTO carpool_ledger(term_id,cycle_id,event_type,delta_usd,event_key,boost_slot)
		VALUES($1,$2,'boost',57.51250000,'new-boost-slot-3',3)
	`, termID, firstCycleID)
	require.NoError(t, err)
	var planCountBeforeReplay int
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM carpool_plans`).Scan(&planCountBeforeReplay))
	postUpgradeBeforeReplay := carpoolRulesV14LegacyFingerprint(t, ctx, tx)

	_, err = tx.ExecContext(ctx, string(migrationSQL))
	require.NoError(t, err)
	var planCountAfterReplay int
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM carpool_plans`).Scan(&planCountAfterReplay))
	require.Equal(t, planCountBeforeReplay, planCountAfterReplay)
	require.Equal(t, planState{"Already current", "99.00", "125.00000000", 28, 7, "0.10000000", 3, true, 1}, readLatest("already_current"))
	require.Equal(t, postUpgradeBeforeReplay, carpoolRulesV14LegacyFingerprint(t, ctx, tx))
}

func carpoolRulesV14LegacyFingerprint(t *testing.T, ctx context.Context, tx *sql.Tx) string {
	t.Helper()
	var fingerprint string
	require.NoError(t, tx.QueryRowContext(ctx, `
		SELECT md5(
			(SELECT string_agg(to_jsonb(p)::text,'|' ORDER BY p.id) FROM carpool_plans p WHERE p.duration_days=30) || '#' ||
			(SELECT string_agg(to_jsonb(t)::text,'|' ORDER BY t.id) FROM carpool_terms t) || '#' ||
			(SELECT string_agg(to_jsonb(c)::text,'|' ORDER BY c.id) FROM carpool_cycles c) || '#' ||
			(SELECT string_agg(to_jsonb(l)::text,'|' ORDER BY l.id) FROM carpool_ledger l)
		)
	`).Scan(&fingerprint))
	return fingerprint
}
