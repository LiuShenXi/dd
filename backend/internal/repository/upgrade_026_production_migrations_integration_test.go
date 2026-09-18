//go:build integration

package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

// Only filenames/checksums from the read-only 2026-09-18 production manifest.
// There are no database rows, user identities, credentials or connection data.
//
//go:embed testdata/upgrade_026_production_migrations.json
var upgrade026ProductionMigrationManifest []byte

type upgrade026MigrationRecord struct {
	Filename  string    `json:"filename"`
	Checksum  string    `json:"checksum"`
	AppliedAt time.Time `json:"-"`
}

func upgrade026BaselineFS(t *testing.T) fstest.MapFS {
	t.Helper()
	var manifest []upgrade026MigrationRecord
	require.NoError(t, json.Unmarshal(upgrade026ProductionMigrationManifest, &manifest))
	require.Len(t, manifest, 291, "the recovered production manifest is the exact baseline")
	baseline := make(fstest.MapFS, len(manifest))
	for _, record := range manifest {
		require.NotContains(t, baseline, record.Filename)
		body, err := migrations.FS.ReadFile(record.Filename)
		require.NoError(t, err, record.Filename)
		sum := sha256.Sum256([]byte(strings.TrimSpace(string(body))))
		require.Equal(t, record.Checksum, hex.EncodeToString(sum[:]), "deployed migration changed: %s", record.Filename)
		baseline[record.Filename] = &fstest.MapFile{Data: body}
	}
	return baseline
}

func newUpgrade026Database(t *testing.T) *sql.DB {
	t.Helper()
	// Several historical migrations explicitly address public, so a schema-only
	// fixture would not establish isolation. Use a fresh database on TestMain's
	// disposable PostgreSQL container, never a connection from the environment.
	require.NotEmpty(t, integrationDSN, "the integration harness must expose its synthetic DSN")
	parsed, err := url.Parse(integrationDSN)
	require.NoError(t, err)
	require.Contains(t, []string{"postgres", "postgresql"}, parsed.Scheme)
	databaseName := "upgrade026_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quotedName := pq.QuoteIdentifier(databaseName)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, err = integrationDB.ExecContext(ctx, "CREATE DATABASE "+quotedName)
	require.NoError(t, err)
	var database *sql.DB
	t.Cleanup(func() {
		if database != nil {
			require.NoError(t, database.Close())
		}
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		_, err := integrationDB.ExecContext(cleanupCtx, "DROP DATABASE "+quotedName+" WITH (FORCE)")
		require.NoError(t, err)
	})
	parsed.Path = "/" + databaseName
	parsed.RawPath = ""
	database, err = sql.Open("postgres", parsed.String())
	require.NoError(t, err)
	require.NoError(t, database.PingContext(ctx))
	return database
}

func readUpgrade026MigrationRecords(t *testing.T, db *sql.DB) map[string]upgrade026MigrationRecord {
	t.Helper()
	rows, err := db.QueryContext(context.Background(), "SELECT filename,checksum,applied_at FROM schema_migrations ORDER BY filename")
	require.NoError(t, err)
	defer rows.Close()
	result := make(map[string]upgrade026MigrationRecord)
	for rows.Next() {
		var record upgrade026MigrationRecord
		require.NoError(t, rows.Scan(&record.Filename, &record.Checksum, &record.AppliedAt))
		result[record.Filename] = record
	}
	require.NoError(t, rows.Err())
	return result
}

func seedUpgrade026FinancialFixture(t *testing.T, db *sql.DB) (groupID, memberID, ordinaryID int64) {
	t.Helper()
	ctx := context.Background()
	var adminID, keyID, planID, termID, cycleID int64
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO users(email,password_hash,role,balance) VALUES('upgrade-admin@example.invalid','synthetic-no-login','admin',99.12345678) RETURNING id`).Scan(&adminID))
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO users(email,password_hash,balance) VALUES('upgrade-member@example.invalid','synthetic-no-login',550.76543210) RETURNING id`).Scan(&memberID))
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO users(email,password_hash,balance) VALUES('upgrade-ordinary@example.invalid','synthetic-no-login',77.13000000) RETURNING id`).Scan(&ordinaryID))
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO groups(name,platform,subscription_type,models_list_config) VALUES('Synthetic upgrade group','openai','standard','{"enabled":true,"models":["gpt-5.5","gpt-image-2"]}') RETURNING id`).Scan(&groupID))
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO api_keys(user_id,group_id,key,name) VALUES($1,$2,'sk-upgrade-synthetic-no-upstream','Synthetic upgrade key') RETURNING id`, memberID, groupID).Scan(&keyID))
	require.NoError(t, db.QueryRowContext(ctx, `SELECT id FROM carpool_plans WHERE code='four_seat' AND enabled=TRUE ORDER BY version DESC,id DESC LIMIT 1`).Scan(&planID))
	plan, err := NewCarpoolRepository(db).GetPlan(ctx, planID)
	require.NoError(t, err)
	snapshot := plan.Snapshot()
	snapshotBytes, err := json.Marshal(snapshot)
	require.NoError(t, err)
	anchor := time.Date(2026, 9, 1, 14, 0, 20, 123000, time.UTC)
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO carpool_terms(user_id,scope_id,group_id,plan_id,plan_snapshot,starts_at,expires_at,status,boost_used,created_by) VALUES($1,1,$2,$3,$4::jsonb,$5,$6,'active',1,$7) RETURNING id`, memberID, groupID, planID, string(snapshotBytes), anchor, anchor.AddDate(0, 0, snapshot.DurationDays), adminID).Scan(&termID))
	baseBalance := snapshot.WeeklyQuotaUSD.Sub(decimal.RequireFromString("17.25"))
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO carpool_cycles(term_id,cycle_no,starts_at,ends_at,base_quota_usd,base_balance_usd,boost_balance_usd,manual_balance_usd,state,activated_at) VALUES($1,1,$2,$3,$4,$5,$6,-3,'active',$2) RETURNING id`, termID, anchor, anchor.AddDate(0, 0, 7), snapshot.WeeklyQuotaUSD.StringFixed(8), baseBalance.StringFixed(8), snapshot.BoostAmountUSD.StringFixed(8)).Scan(&cycleID))
	_, err = db.ExecContext(ctx, `INSERT INTO carpool_ledger(user_id,term_id,cycle_id,event_type,bucket,delta_usd,event_key,effective_at) VALUES($1,$2,$3,'cycle_initial','base',$4,'upgrade-initial',$6),($1,$2,$3,'usage','base',-17.25,'upgrade-usage',$6),($1,$2,$3,'boost','boost',$5,'upgrade-boost',$6),($1,$2,$3,'adjustment','manual',-3,'upgrade-adjustment',$6)`, memberID, termID, cycleID, snapshot.WeeklyQuotaUSD.StringFixed(8), snapshot.BoostAmountUSD.StringFixed(8), anchor)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO carpool_billing_bindings(user_id,group_id) VALUES($1,$2)`, memberID, groupID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO carpool_billing_requests(request_id,api_key_id,user_id,group_id,term_id,cycle_id,admitted_at,status,request_fingerprint,billing_payload,actual_cost_usd) VALUES('upgrade-pending-known',$1,$2,$3,$4,$5,$6,'usage_known','synthetic-fingerprint','{"synthetic":true}',2.5)`, keyID, memberID, groupID, termID, cycleID, anchor)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO carpool_payments(term_id,amount_cny,payment_kind,paid_at,channel,request_id,request_fingerprint,recorded_by) VALUES($1,330,'payment',$2,'synthetic','upgrade-payment','synthetic-fingerprint',$3)`, termID, anchor, adminID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO carpool_release_imports(batch_key,request_fingerprint,actor_id,group_id,cycle_anchor,result) VALUES('synthetic-upgrade-import','synthetic-fingerprint',$1,$2,$3,'{"synthetic":true}')`, adminID, groupID, anchor)
	require.NoError(t, err)
	// New upstream 238 intentionally deletes the unrestricted row only.
	_, err = db.ExecContext(ctx, `INSERT INTO user_platform_quotas(user_id,platform,daily_limit_usd,daily_usage_usd) VALUES($1,'openai',NULL,0),($2,'openai',100,8.75)`, memberID, ordinaryID)
	require.NoError(t, err)
	return groupID, memberID, ordinaryID
}

func upgrade026ProtectedFingerprints(t *testing.T, db *sql.DB) map[string]string {
	t.Helper()
	result := make(map[string]string)
	for _, table := range []string{
		"users", "api_keys", "settings", "carpool_plans", "carpool_terms", "carpool_cycles", "carpool_ledger",
		"carpool_payments", "carpool_billing_requests", "carpool_operations", "carpool_billing_bindings", "carpool_release_imports",
		"carpool_reset_scope_states", "carpool_reset_batches", "carpool_reset_account_states", "carpool_reset_credits",
		"carpool_reset_targets", "carpool_reset_announcement_outbox", "carpool_reset_qualifications", "carpool_cycle_carryovers",
	} {
		var fingerprint string
		err := db.QueryRowContext(context.Background(), "SELECT md5(COALESCE(jsonb_agg(to_jsonb(r) ORDER BY to_jsonb(r)::text)::text,'[]')) FROM "+pq.QuoteIdentifier(table)+" r").Scan(&fingerprint)
		require.NoError(t, err, table)
		result[table] = fingerprint
	}
	var groupsFingerprint string
	require.NoError(t, db.QueryRowContext(context.Background(), `SELECT md5(COALESCE(jsonb_agg(to_jsonb(g)-'models_list_config'-'model_allowlist' ORDER BY id)::text,'[]')) FROM groups g`).Scan(&groupsFingerprint))
	result["groups_except_model_column_names"] = groupsFingerprint
	return result
}

func TestUpgrade026ProductionMigrations_ExactBaselineUpgradeReplayAndRollback(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	baseline := upgrade026BaselineFS(t)
	db := newUpgrade026Database(t)
	require.NoError(t, applyMigrationsFS(ctx, db, baseline))
	beforeMigrations := readUpgrade026MigrationRecords(t, db)
	require.Len(t, beforeMigrations, 291)
	groupID, memberID, ordinaryID := seedUpgrade026FinancialFixture(t, db)
	protectedBefore := upgrade026ProtectedFingerprints(t, db)

	require.NoError(t, applyMigrationsFS(ctx, db, migrations.FS))
	allFiles, err := fs.Glob(migrations.FS, "*.sql")
	require.NoError(t, err)
	afterMigrations := readUpgrade026MigrationRecords(t, db)
	require.Len(t, afterMigrations, len(allFiles))
	require.Greater(t, len(afterMigrations), len(beforeMigrations))
	for name, record := range beforeMigrations {
		require.Equal(t, record, afterMigrations[name], "old filename/checksum/applied_at must stay immutable: %s", name)
	}
	require.Equal(t, protectedBefore, upgrade026ProtectedFingerprints(t, db))
	var unrestricted, configured int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM user_platform_quotas WHERE user_id=$1`, memberID).Scan(&unrestricted))
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM user_platform_quotas WHERE user_id=$1 AND daily_limit_usd=100 AND daily_usage_usd=8.75`, ordinaryID).Scan(&configured))
	require.Zero(t, unrestricted)
	require.Equal(t, 1, configured)

	readBoth := func(expected string) {
		t.Helper()
		var legacy, current string
		require.NoError(t, db.QueryRowContext(ctx, `SELECT models_list_config::text,model_allowlist::text FROM groups WHERE id=$1`, groupID).Scan(&legacy, &current))
		require.JSONEq(t, expected, legacy)
		require.JSONEq(t, expected, current)
	}
	readBoth(`{"enabled":true,"models":["gpt-5.5","gpt-image-2"]}`)
	const rollbackWrite = `{"enabled":true,"models":["gpt-rollback"]}`
	_, err = db.ExecContext(ctx, `UPDATE groups SET models_list_config=$1::jsonb WHERE id=$2`, rollbackWrite, groupID)
	require.NoError(t, err)
	readBoth(rollbackWrite)
	const candidateWrite = `{"enabled":true,"models":["gpt-candidate"]}`
	_, err = db.ExecContext(ctx, `UPDATE groups SET model_allowlist=$1::jsonb WHERE id=$2`, candidateWrite, groupID)
	require.NoError(t, err)
	readBoth(candidateWrite)
	// A rollback binary checks only its original 291 files; a subsequent
	// candidate startup checks the complete set. Neither may replay old grants.
	require.NoError(t, applyMigrationsFS(ctx, db, baseline))
	require.NoError(t, applyMigrationsFS(ctx, db, migrations.FS))
	readBoth(candidateWrite)
	require.Equal(t, afterMigrations, readUpgrade026MigrationRecords(t, db))
	require.Equal(t, protectedBefore, upgrade026ProtectedFingerprints(t, db))
	t.Logf("verified migration sequence %d -> %d, replay and old/new writer compatibility", len(beforeMigrations), len(afterMigrations))
}
