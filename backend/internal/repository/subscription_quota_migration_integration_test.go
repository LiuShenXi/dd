//go:build integration

package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// This digest binds the sorted filename + NUL + SHA256(TrimSpace(SQL)) + LF
// records of all 297 migrations in the 2026-10-19 .13 production manifest.
// It contains no production data or connection information. A changed original
// file, an omitted file, or a different baseline cannot silently pass the test.
const subscriptionQuotaOriginalMigrationDigest = "35a19130f66cce3abb9134b074a611ce52c29c4ee3f88ebd3a87744656637bb3"

func subscriptionQuotaBaselineFS(t *testing.T) fstest.MapFS {
	t.Helper()
	names, err := fs.Glob(migrations.FS, "*.sql")
	require.NoError(t, err)
	sort.Strings(names)
	baseline := make(fstest.MapFS)
	var manifest strings.Builder
	for _, name := range names {
		// Historical files include 108a/120a/154a/234z revisions. Keep their
		// complete filenames in the manifest while reading only leading digits.
		digits := strings.IndexFunc(name, func(r rune) bool { return r < '0' || r > '9' })
		require.Positive(t, digits, name)
		number, err := strconv.Atoi(name[:digits])
		require.NoError(t, err, name)
		if number >= 245 {
			continue
		}
		body, err := migrations.FS.ReadFile(name)
		require.NoError(t, err, name)
		sum := sha256.Sum256([]byte(strings.TrimSpace(string(body))))
		fmt.Fprintf(&manifest, "%s\x00%s\n", name, hex.EncodeToString(sum[:]))
		baseline[name] = &fstest.MapFile{Data: body}
	}
	require.Len(t, baseline, 297, "the pre-weekly-quota baseline must remain exact")
	digest := sha256.Sum256([]byte(manifest.String()))
	require.Equal(t, subscriptionQuotaOriginalMigrationDigest, hex.EncodeToString(digest[:]), "an original migration filename or checksum changed")
	return baseline
}

// Capture all old columns, including GPT wallets, carpool ledgers/receipts,
// native subscription windows, plan/order data, and usage. New additive columns
// are intentionally excluded from the old-row fingerprint after the upgrade.
func subscriptionQuotaLegacyColumns(t *testing.T, db *sql.DB) map[string][]string {
	t.Helper()
	rows, err := db.Query(`SELECT table_name,column_name FROM information_schema.columns
WHERE table_schema='public' AND table_name NOT IN ('schema_migrations','atlas_schema_revisions')
ORDER BY table_name,ordinal_position`)
	require.NoError(t, err)
	defer rows.Close()
	columns := make(map[string][]string)
	for rows.Next() {
		var table, column string
		require.NoError(t, rows.Scan(&table, &column))
		columns[table] = append(columns[table], column)
	}
	require.NoError(t, rows.Err())
	return columns
}

func subscriptionQuotaLegacyFingerprints(t *testing.T, db *sql.DB, columns map[string][]string) map[string]string {
	t.Helper()
	fingerprints := make(map[string]string, len(columns))
	for table, names := range columns {
		quoted := make([]string, len(names))
		for i, name := range names {
			quoted[i] = pq.QuoteIdentifier(name)
		}
		query := `SELECT md5(COALESCE(jsonb_agg(to_jsonb(r) ORDER BY to_jsonb(r)::text)::text,'[]')) FROM (SELECT ` + strings.Join(quoted, ",") + ` FROM ` + pq.QuoteIdentifier(table) + `) r`
		var fingerprint string
		require.NoError(t, db.QueryRow(query).Scan(&fingerprint), table)
		fingerprints[table] = fingerprint
	}
	return fingerprints
}

func requireSubscriptionQuotaSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	var version int
	require.NoError(t, db.QueryRow(`SELECT current_setting('server_version_num')::int`).Scan(&version))
	require.GreaterOrEqual(t, version, 180000, "this acceptance run requires PostgreSQL 18")
	for _, table := range []string{"subscription_quota_terms", "subscription_quota_requests", "subscription_quota_operations", "subscription_quota_ledger"} {
		var count int
		require.NoError(t, db.QueryRow(`SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_name=$1`, table).Scan(&count))
		require.Equal(t, 1, count, table)
	}
	var validated int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM pg_constraint WHERE conrelid='usage_logs'::regclass AND conname IN ('usage_logs_subscription_quota_pair','usage_logs_subscription_quota_request_fk') AND convalidated`).Scan(&validated))
	require.Equal(t, 2, validated, "both online-added usage constraints must be validated")
	var validIndex bool
	require.NoError(t, db.QueryRow(`SELECT i.indisvalid AND i.indisready FROM pg_index i JOIN pg_class c ON c.oid=i.indexrelid WHERE c.relname='usage_logs_subscription_term_time'`).Scan(&validIndex))
	require.True(t, validIndex, "online usage index must be ready and valid")
}

func TestSubscriptionQuotaMigrations_EmptyDatabaseAndReplay(t *testing.T) {
	// Reuse the existing helper that only creates a fresh database on TestMain's
	// disposable PostgreSQL. It never reads a DSN from user/process configuration.
	db := newUpgrade026Database(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	var tables int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM pg_tables WHERE schemaname='public'`).Scan(&tables))
	require.Zero(t, tables)
	require.NoError(t, ApplyMigrations(ctx, db))
	requireSubscriptionQuotaSchema(t, db)
	before := readUpgrade026MigrationRecords(t, db)
	require.NoError(t, ApplyMigrations(ctx, db))
	require.Equal(t, before, readUpgrade026MigrationRecords(t, db), "replay must preserve every checksum and applied_at")
	for _, table := range []string{"subscription_quota_terms", "subscription_quota_requests", "subscription_quota_operations", "subscription_quota_ledger"} {
		require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM `+pq.QuoteIdentifier(table)).Scan(&tables))
		require.Zero(t, tables, "schema installation must not grant or consume quota")
	}
}

func TestSubscriptionQuotaMigrations_Legacy297UpgradePreservesAllRows(t *testing.T) {
	db := newUpgrade026Database(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	baseline := subscriptionQuotaBaselineFS(t)
	require.NoError(t, applyMigrationsFS(ctx, db, baseline))
	oldMigrations := readUpgrade026MigrationRecords(t, db)
	require.Len(t, oldMigrations, 297)
	_, _, ordinaryID := seedUpgrade026FinancialFixture(t, db)
	var groupID, subscriptionID, planID, keyID, accountID int64
	require.NoError(t, db.QueryRow(`INSERT INTO groups(name,platform,subscription_type,daily_limit_usd,weekly_limit_usd,monthly_limit_usd) VALUES('Synthetic legacy Claude windows','anthropic','subscription',9,99,299) RETURNING id`).Scan(&groupID))
	require.NoError(t, db.QueryRow(`INSERT INTO user_subscriptions(user_id,group_id,starts_at,expires_at,daily_window_start,weekly_window_start,monthly_window_start,daily_usage_usd,weekly_usage_usd,monthly_usage_usd,notes) VALUES($1,$2,'2026-10-01T00:00:00Z','2026-11-01T00:00:00Z','2026-10-18T00:00:00Z','2026-10-12T00:00:00Z','2026-10-01T00:00:00Z',1.1234567890,8.7654321098,17.1234567890,'retain exact native windows') RETURNING id`, ordinaryID, groupID).Scan(&subscriptionID))
	require.NoError(t, db.QueryRow(`INSERT INTO subscription_plans(group_id,name,price) VALUES($1,'Synthetic legacy plan',12.34) RETURNING id`, groupID).Scan(&planID))
	require.NoError(t, db.QueryRow(`INSERT INTO api_keys(user_id,group_id,key,name) VALUES($1,$2,'synthetic-legacy-windows-no-upstream','Legacy windows key') RETURNING id`, ordinaryID, groupID).Scan(&keyID))
	require.NoError(t, db.QueryRow(`INSERT INTO accounts(name,platform,type,credentials,status) VALUES('Synthetic legacy disabled account','anthropic','apikey','{}','disabled') RETURNING id`).Scan(&accountID))
	_, err := db.Exec(`INSERT INTO usage_logs(user_id,api_key_id,account_id,group_id,subscription_id,model,total_cost,actual_cost) VALUES($1,$2,$3,$4,$5,'synthetic-legacy-model',0.12345678,0.02469136)`, ordinaryID, keyID, accountID, groupID, subscriptionID)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO payment_orders(user_id,amount,pay_amount,expires_at,out_trade_no,order_type,plan_id,subscription_group_id,subscription_days) VALUES($1,12.34,12.34,'2026-11-01T00:00:00Z','synthetic-legacy-subscription-order','subscription',$2,$3,30)`, ordinaryID, planID, groupID)
	require.NoError(t, err)
	columns := subscriptionQuotaLegacyColumns(t, db)
	before := subscriptionQuotaLegacyFingerprints(t, db, columns)
	require.NoError(t, ApplyMigrations(ctx, db))
	requireSubscriptionQuotaSchema(t, db)
	require.Equal(t, before, subscriptionQuotaLegacyFingerprints(t, db, columns), "every pre-existing column value must survive the additive migration")
	afterMigrations := readUpgrade026MigrationRecords(t, db)
	for name, original := range oldMigrations {
		require.Equal(t, original, afterMigrations[name], "original migration record changed: %s", name)
	}
	var wrongModes, wrongRevisions, planSnapshots int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM groups WHERE subscription_quota_mode <> 'windows'`).Scan(&wrongModes))
	require.Zero(t, wrongModes, "all historical groups default to windows")
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM user_subscriptions WHERE quota_revision <> 0`).Scan(&wrongRevisions))
	require.Zero(t, wrongRevisions)
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM subscription_plans WHERE term_quota_usd IS NOT NULL`).Scan(&planSnapshots))
	require.Zero(t, planSnapshots, "legacy plans must not invent allowances")
	require.NoError(t, applyMigrationsFS(ctx, db, baseline), "an R0 migration runner must not rewrite an already upgraded database")
	require.NoError(t, ApplyMigrations(ctx, db))
	require.Equal(t, afterMigrations, readUpgrade026MigrationRecords(t, db))
	require.Equal(t, before, subscriptionQuotaLegacyFingerprints(t, db, columns))
	t.Logf("verified exact 297-file baseline -> %d files; %d legacy tables preserved, then old/new migration replay", len(afterMigrations), len(columns))
}

type subscriptionQuotaMigrationFixture struct {
	db                  *sql.DB
	user, otherUser     int64
	group, otherGroup   int64
	subscription, other int64
	key, otherKey       int64
	account, plan       int64
	termSequence        int
	requestSequence     int
	starts              time.Time
}

func newSubscriptionQuotaMigrationFixture(t *testing.T) *subscriptionQuotaMigrationFixture {
	t.Helper()
	f := &subscriptionQuotaMigrationFixture{db: newUpgrade026Database(t), starts: time.Date(2026, 3, 7, 17, 0, 0, 123456000, time.UTC)}
	require.NoError(t, ApplyMigrations(context.Background(), f.db))
	require.NoError(t, f.db.QueryRow(`INSERT INTO users(email,password_hash,balance) VALUES('quota-a@example.invalid','synthetic-no-login',77.12345678) RETURNING id`).Scan(&f.user))
	require.NoError(t, f.db.QueryRow(`INSERT INTO users(email,password_hash,balance) VALUES('quota-b@example.invalid','synthetic-no-login',88.87654321) RETURNING id`).Scan(&f.otherUser))
	for i, name := range []string{"Synthetic term A", "Synthetic term B"} {
		var groupID, subID int64
		require.NoError(t, f.db.QueryRow(`INSERT INTO groups(name,platform,subscription_type,subscription_quota_mode) VALUES($1,'anthropic','subscription','term') RETURNING id`, name).Scan(&groupID))
		userID := f.user
		if i == 1 {
			userID = f.otherUser
		}
		require.NoError(t, f.db.QueryRow(`INSERT INTO user_subscriptions(user_id,group_id,starts_at,expires_at) VALUES($1,$2,$3,$4) RETURNING id`, userID, groupID, f.starts, f.starts.Add(336*time.Hour)).Scan(&subID))
		if i == 0 {
			f.group, f.subscription = groupID, subID
		} else {
			f.otherGroup, f.other = groupID, subID
		}
	}
	require.NoError(t, f.db.QueryRow(`INSERT INTO api_keys(user_id,group_id,key,name) VALUES($1,$2,'synthetic-quota-no-upstream','Synthetic quota key') RETURNING id`, f.user, f.group).Scan(&f.key))
	require.NoError(t, f.db.QueryRow(`INSERT INTO api_keys(user_id,group_id,key,name) VALUES($1,$2,'synthetic-quota-b-no-upstream','Synthetic quota key B') RETURNING id`, f.otherUser, f.otherGroup).Scan(&f.otherKey))
	require.NoError(t, f.db.QueryRow(`INSERT INTO accounts(name,platform,type,credentials,status) VALUES('Synthetic disabled account','anthropic','apikey','{}','disabled') RETURNING id`).Scan(&f.account))
	require.NoError(t, f.db.QueryRow(`INSERT INTO subscription_plans(group_id,name,price,term_quota_usd) VALUES($1,'Synthetic quota plan',1,100) RETURNING id`, f.group).Scan(&f.plan))
	return f
}

func (f *subscriptionQuotaMigrationFixture) insertTerm(quota, used string, subscription, user, group int64, ends time.Time) (int64, error) {
	f.termSequence++
	var id int64
	err := f.db.QueryRow(`INSERT INTO subscription_quota_terms(subscription_id,user_id,group_id,term_no,quota_usd,used_usd,starts_at,expires_at,lifecycle_state,source_kind,source_snapshot) VALUES($1,$2,$3,$4,$5::numeric,$6::numeric,$7,$8,'active','admin','{"synthetic":true}') RETURNING id`, subscription, user, group, f.termSequence, quota, used, f.starts, ends).Scan(&id)
	return id, err
}

func (f *subscriptionQuotaMigrationFixture) term(t *testing.T, quota, used string) int64 {
	t.Helper()
	id, err := f.insertTerm(quota, used, f.subscription, f.user, f.group, f.starts.Add(168*time.Hour))
	require.NoError(t, err)
	return id
}

func (f *subscriptionQuotaMigrationFixture) insertRequest(term, subscription, user, group int64, cost any) (int64, error) {
	f.requestSequence++
	key := f.key
	if user == f.otherUser {
		key = f.otherKey
	}
	var id int64
	err := f.db.QueryRow(`INSERT INTO subscription_quota_requests(request_id,api_key_id,subscription_id,term_id,user_id,group_id,admitted_at,pricing_at,pricing_snapshot,status,actual_cost_usd,receipt_version,request_fingerprint,billing_payload,usage_log_payload) VALUES($1,$2,$3,$4,$5,$6,$7,$7,'{}','usage_known',$8::numeric,1,$9,'{}','{}') RETURNING id`, fmt.Sprintf("synthetic-quota-%d", f.requestSequence), key, subscription, term, user, group, f.starts, cost, strings.Repeat("a", 64)).Scan(&id)
	return id, err
}

func (f *subscriptionQuotaMigrationFixture) request(t *testing.T, term int64, cost string) int64 {
	t.Helper()
	id, err := f.insertRequest(term, f.subscription, f.user, f.group, cost)
	require.NoError(t, err)
	return id
}

func requireSubscriptionQuotaSQLState(t *testing.T, err error, state string) {
	t.Helper()
	require.Error(t, err)
	var pgError *pq.Error
	require.ErrorAs(t, err, &pgError)
	require.Equal(t, state, string(pgError.Code), "unexpected PostgreSQL constraint failure: %s", pgError.Constraint)
}

func TestSubscriptionQuotaMigrations_GroupModeAndCompositeOwnership(t *testing.T) {
	f := newSubscriptionQuotaMigrationFixture(t)
	for _, values := range []struct{ platform, kind, mode string }{
		{"openai", "subscription", "term"}, {"anthropic", "standard", "term"},
		{"gemini", "subscription", "term"}, {"anthropic", "subscription", "unknown"},
	} {
		t.Run(values.platform+"_"+values.kind+"_"+values.mode, func(t *testing.T) {
			_, err := f.db.Exec(`INSERT INTO groups(name,platform,subscription_type,subscription_quota_mode) VALUES($1,$2,$3,$4)`, "invalid-"+values.platform+values.kind+values.mode, values.platform, values.kind, values.mode)
			requireSubscriptionQuotaSQLState(t, err, "23514")
		})
	}
	_, err := f.db.Exec(`UPDATE groups SET fallback_group_id=$1 WHERE id=$2`, f.otherGroup, f.group)
	requireSubscriptionQuotaSQLState(t, err, "23514")
	_, err = f.db.Exec(`UPDATE groups SET fallback_group_id_on_invalid_request=$1 WHERE id=$2`, f.otherGroup, f.group)
	requireSubscriptionQuotaSQLState(t, err, "23514")
	term := f.term(t, "100", "0")
	otherTerm, err := f.insertTerm("300", "0", f.other, f.otherUser, f.otherGroup, f.starts.Add(168*time.Hour))
	require.NoError(t, err)
	for _, tuple := range [][3]int64{{f.subscription, f.otherUser, f.group}, {f.subscription, f.user, f.otherGroup}, {f.other, f.user, f.group}} {
		_, err := f.insertTerm("100", "0", tuple[0], tuple[1], tuple[2], f.starts.Add(168*time.Hour))
		requireSubscriptionQuotaSQLState(t, err, "23503")
	}
	for _, tuple := range [][4]int64{{term, f.subscription, f.otherUser, f.group}, {term, f.subscription, f.user, f.otherGroup}, {otherTerm, f.subscription, f.user, f.group}} {
		_, err := f.insertRequest(tuple[0], tuple[1], tuple[2], tuple[3], "1")
		requireSubscriptionQuotaSQLState(t, err, "23503")
	}
	request := f.request(t, term, "1")
	_, err = f.db.Exec(`INSERT INTO subscription_quota_ledger(term_id,request_row_id,event_key,entry_kind,delta_used_usd) VALUES($1,$2,'cross-term-debit','usage_debit',1)`, otherTerm, request)
	requireSubscriptionQuotaSQLState(t, err, "23503")
	var originalDebit int64
	require.NoError(t, f.db.QueryRow(`INSERT INTO subscription_quota_ledger(term_id,request_row_id,event_key,entry_kind,delta_used_usd) VALUES($1,$2,'original-debit','usage_debit',1) RETURNING id`, term, request).Scan(&originalDebit))
	otherRequest, err := f.insertRequest(otherTerm, f.other, f.otherUser, f.otherGroup, "1")
	require.NoError(t, err)
	_, err = f.db.Exec(`INSERT INTO subscription_quota_ledger(term_id,request_row_id,event_key,entry_kind,delta_used_usd,reverses_entry_id,actor_id,reason) VALUES($1,$2,'cross-term-reversal','usage_reversal',-1,$3,$4,'synthetic')`, otherTerm, otherRequest, originalDebit, f.user)
	requireSubscriptionQuotaSQLState(t, err, "23503")
	_, err = f.db.Exec(`UPDATE groups SET subscription_quota_mode='windows' WHERE id=$1`, f.group)
	requireSubscriptionQuotaSQLState(t, err, "P0001")
}

func TestSubscriptionQuotaMigrations_Exact168HoursAndNumericBoundaries(t *testing.T) {
	f := newSubscriptionQuotaMigrationFixture(t)
	conn, err := f.db.Conn(context.Background())
	require.NoError(t, err)
	defer conn.Close()
	_, err = conn.ExecContext(context.Background(), `SET TIME ZONE 'America/New_York'`)
	require.NoError(t, err)
	var elapsedHours, elapsedDays int64
	require.NoError(t, conn.QueryRowContext(context.Background(), `SELECT extract(epoch FROM ((TIMESTAMPTZ '2026-03-07 12:00 America/New_York' + INTERVAL '168 hours') - TIMESTAMPTZ '2026-03-07 12:00 America/New_York'))::bigint, extract(epoch FROM ((TIMESTAMPTZ '2026-03-07 12:00 America/New_York' + INTERVAL '7 days') - TIMESTAMPTZ '2026-03-07 12:00 America/New_York'))::bigint`).Scan(&elapsedHours, &elapsedDays))
	require.EqualValues(t, 604800, elapsedHours)
	require.EqualValues(t, 601200, elapsedDays, "the fixture crosses DST and distinguishes 168 hours from 7 local calendar days")
	_, err = conn.ExecContext(context.Background(), `RESET TIME ZONE`)
	require.NoError(t, err)
	require.NoError(t, conn.Close())
	for _, duration := range []time.Duration{167 * time.Hour, 169 * time.Hour, 168*time.Hour + time.Microsecond} {
		_, err := f.insertTerm("100", "0", f.subscription, f.user, f.group, f.starts.Add(duration))
		requireSubscriptionQuotaSQLState(t, err, "23514")
	}
	for _, value := range []string{"0", "-0.00000001", "0.000000004", "NaN"} {
		t.Run("quota_"+value, func(t *testing.T) {
			_, err := f.insertTerm(value, "0", f.subscription, f.user, f.group, f.starts.Add(168*time.Hour))
			requireSubscriptionQuotaSQLState(t, err, "23514")
		})
	}
	for _, value := range []string{"NaN", "-0.00000001"} {
		t.Run("used_"+value, func(t *testing.T) {
			_, err := f.insertTerm("100", value, f.subscription, f.user, f.group, f.starts.Add(168*time.Hour))
			requireSubscriptionQuotaSQLState(t, err, "23514")
		})
	}
	for _, value := range []string{"Infinity", "-Infinity", "1000000000000"} {
		_, err := f.insertTerm(value, "0", f.subscription, f.user, f.group, f.starts.Add(168*time.Hour))
		requireSubscriptionQuotaSQLState(t, err, "22003")
	}
	minimum := f.term(t, "0.00000001", "0")
	maximum := f.term(t, "999999999999.99999999", "0")
	for id, expected := range map[int64]string{minimum: "0.00000001", maximum: "999999999999.99999999"} {
		var stored string
		require.NoError(t, f.db.QueryRow(`SELECT quota_usd::text FROM subscription_quota_terms WHERE id=$1`, id).Scan(&stored))
		require.Equal(t, expected, stored)
	}
	term := f.term(t, "0.000078125", "0.000078125")
	request := f.request(t, term, "0.000078125")
	var ledger int64
	require.NoError(t, f.db.QueryRow(`INSERT INTO subscription_quota_ledger(term_id,request_row_id,event_key,entry_kind,delta_used_usd) VALUES($1,$2,'rounding-debit','usage_debit',0.000078125) RETURNING id`, term, request).Scan(&ledger))
	var quota, used, actual, delta string
	require.NoError(t, f.db.QueryRow(`SELECT t.quota_usd::text,t.used_usd::text,r.actual_cost_usd::text,l.delta_used_usd::text FROM subscription_quota_terms t JOIN subscription_quota_requests r ON r.term_id=t.id JOIN subscription_quota_ledger l ON l.request_row_id=r.id WHERE l.id=$1`, ledger).Scan(&quota, &used, &actual, &delta))
	for _, amount := range []string{quota, used, actual, delta} {
		require.Equal(t, "0.00007813", amount)
	}
	var seconds int64
	require.NoError(t, f.db.QueryRow(`SELECT extract(epoch FROM expires_at-starts_at)::bigint FROM subscription_quota_terms WHERE id=$1`, term).Scan(&seconds))
	require.EqualValues(t, 604800, seconds)
	// The storage schema deliberately permits actual in-flight overshoot. It must
	// not silently cap already incurred usage at quota or move it to another term.
	f.term(t, "100", "100.00000001")
	for _, value := range []string{"NaN", "-0.00000001"} {
		t.Run("actual_"+value, func(t *testing.T) {
			_, err := f.insertRequest(term, f.subscription, f.user, f.group, value)
			requireSubscriptionQuotaSQLState(t, err, "23514")
		})
		t.Run("ledger_delta_"+value, func(t *testing.T) {
			newRequest := f.request(t, term, "0")
			_, err := f.db.Exec(`INSERT INTO subscription_quota_ledger(term_id,request_row_id,event_key,entry_kind,delta_used_usd) VALUES($1,$2,$3,'usage_debit',$4::numeric)`, term, newRequest, "invalid-delta-"+value, value)
			requireSubscriptionQuotaSQLState(t, err, "23514")
		})
	}
	for _, value := range []string{"NaN", "0", "-0.00000001"} {
		t.Run("plan_"+value, func(t *testing.T) {
			_, err := f.db.Exec(`UPDATE subscription_plans SET term_quota_usd=$1::numeric WHERE id=$2`, value, f.plan)
			requireSubscriptionQuotaSQLState(t, err, "23514")
		})
	}
	_, err = f.db.Exec(`UPDATE subscription_plans SET term_quota_usd=0.000078125 WHERE id=$1`, f.plan)
	require.NoError(t, err)
	var planAmount string
	require.NoError(t, f.db.QueryRow(`SELECT term_quota_usd::text FROM subscription_plans WHERE id=$1`, f.plan).Scan(&planAmount))
	require.Equal(t, "0.00007813", planAmount)
}

func TestSubscriptionQuotaMigrations_ImmutableTermsAndLedger(t *testing.T) {
	f := newSubscriptionQuotaMigrationFixture(t)
	term := f.term(t, "100", "0")
	request := f.request(t, term, "1")
	for field, expression := range map[string]string{
		"quota_usd": "quota_usd+1", "term_no": "term_no+1", "subscription_id": fmt.Sprint(f.other),
		"user_id": fmt.Sprint(f.otherUser), "group_id": fmt.Sprint(f.otherGroup), "source_plan_id": fmt.Sprint(f.plan),
		"source_snapshot": `'{}'::jsonb`, "source_kind": `'payment'`, "payment_order_id": "999999999",
		"starts_at": "starts_at+interval '1 microsecond'", "expires_at": "expires_at+interval '1 microsecond'",
	} {
		t.Run(field, func(t *testing.T) {
			_, err := f.db.Exec(`UPDATE subscription_quota_terms SET `+pq.QuoteIdentifier(field)+`=`+expression+` WHERE id=$1`, term)
			requireSubscriptionQuotaSQLState(t, err, "P0001")
			require.Contains(t, err.Error(), "immutable")
		})
	}
	_, err := f.db.Exec(`UPDATE subscription_quota_terms SET used_usd=1,revision=revision+1,lifecycle_state='closing',cancelled_at=clock_timestamp(),updated_at=clock_timestamp() WHERE id=$1`, term)
	require.NoError(t, err, "accounting/lifecycle columns must remain writable")
	var debit int64
	require.NoError(t, f.db.QueryRow(`INSERT INTO subscription_quota_ledger(term_id,request_row_id,event_key,entry_kind,delta_used_usd) VALUES($1,$2,'immutable-debit','usage_debit',1) RETURNING id`, term, request).Scan(&debit))
	for _, statement := range []string{`UPDATE subscription_quota_ledger SET delta_used_usd=2 WHERE id=$1`, `DELETE FROM subscription_quota_ledger WHERE id=$1`} {
		_, err := f.db.Exec(statement, debit)
		requireSubscriptionQuotaSQLState(t, err, "P0001")
		require.Contains(t, err.Error(), "append only")
	}
	_, err = f.db.Exec(`INSERT INTO subscription_quota_ledger(term_id,request_row_id,event_key,entry_kind,delta_used_usd) VALUES($1,$2,'duplicate-debit','usage_debit',1)`, term, request)
	requireSubscriptionQuotaSQLState(t, err, "23505")
	_, err = f.db.Exec(`INSERT INTO subscription_quota_ledger(term_id,request_row_id,event_key,entry_kind,delta_used_usd,reverses_entry_id,actor_id,reason) VALUES($1,$2,'audited-reversal','usage_reversal',-1,$3,$4,'synthetic correction')`, term, request, debit, f.user)
	require.NoError(t, err, "append-only enforcement must permit an audited reversal row")
	_, err = f.db.Exec(`INSERT INTO subscription_quota_ledger(term_id,request_row_id,event_key,entry_kind,delta_used_usd,reverses_entry_id,actor_id,reason) VALUES($1,$2,'duplicate-reversal','usage_reversal',-1,$3,$4,'synthetic correction')`, term, request, debit, f.user)
	requireSubscriptionQuotaSQLState(t, err, "23505")
	var net string
	require.NoError(t, f.db.QueryRow(`SELECT sum(delta_used_usd)::text FROM subscription_quota_ledger WHERE term_id=$1`, term).Scan(&net))
	require.Equal(t, "0.00000000", net)
}

func TestSubscriptionQuotaMigrations_UnknownUsageAndUsageLogLinks(t *testing.T) {
	f := newSubscriptionQuotaMigrationFixture(t)
	term := f.term(t, "100", "0")
	var admitted int64
	require.NoError(t, f.db.QueryRow(`INSERT INTO subscription_quota_requests(request_id,api_key_id,subscription_id,term_id,user_id,group_id,admitted_at,pricing_at,pricing_snapshot,status) VALUES('unknown-synthetic',$1,$2,$3,$4,$5,$6,$6,'{}','admitted') RETURNING id`, f.key, f.subscription, term, f.user, f.group, f.starts).Scan(&admitted))
	var actual sql.NullString
	require.NoError(t, f.db.QueryRow(`SELECT actual_cost_usd::text FROM subscription_quota_requests WHERE id=$1`, admitted).Scan(&actual))
	require.False(t, actual.Valid, "unknown usage must remain SQL NULL")
	for _, assignment := range []string{"status='usage_known'", "actual_cost_usd=0", "status='no_charge',actual_cost_usd=0"} {
		_, err := f.db.Exec(`UPDATE subscription_quota_requests SET `+assignment+` WHERE id=$1`, admitted)
		requireSubscriptionQuotaSQLState(t, err, "23514")
	}
	_, err := f.db.Exec(`UPDATE subscription_quota_requests SET status='no_charge',actual_cost_usd=0,resolution_evidence='{"synthetic":true}' WHERE id=$1`, admitted)
	require.NoError(t, err, "an evidenced no-charge resolution must remain possible")
	request := f.request(t, term, "1")
	for _, assignment := range []string{"receipt_version=0", "billing_payload='[]'", "usage_log_payload='[]'", "status='settled'"} {
		_, err := f.db.Exec(`UPDATE subscription_quota_requests SET `+assignment+` WHERE id=$1`, request)
		requireSubscriptionQuotaSQLState(t, err, "23514")
	}
	_, err = f.db.Exec(`UPDATE subscription_quota_requests SET status='settled',settled_at=clock_timestamp() WHERE id=$1`, request)
	require.NoError(t, err)
	var usage int64
	require.NoError(t, f.db.QueryRow(`INSERT INTO usage_logs(user_id,api_key_id,account_id,group_id,subscription_id,model,total_cost,actual_cost) VALUES($1,$2,$3,$4,$5,'synthetic-no-upstream',1,1) RETURNING id`, f.user, f.key, f.account, f.group, f.subscription).Scan(&usage))
	_, err = f.db.Exec(`UPDATE usage_logs SET subscription_term_id=$1 WHERE id=$2`, term, usage)
	requireSubscriptionQuotaSQLState(t, err, "23514")
	otherTerm, err := f.insertTerm("100", "0", f.other, f.otherUser, f.otherGroup, f.starts.Add(168*time.Hour))
	require.NoError(t, err)
	_, err = f.db.Exec(`UPDATE usage_logs SET subscription_term_id=$1,subscription_quota_request_id=$2 WHERE id=$3`, otherTerm, request, usage)
	requireSubscriptionQuotaSQLState(t, err, "23503")
	_, err = f.db.Exec(`UPDATE usage_logs SET subscription_term_id=$1,subscription_quota_request_id=$2 WHERE id=$3`, term, request, usage)
	require.NoError(t, err, "the matching request/term pair must be accepted")
}

func subscriptionQuotaBeforeOnlineIndex(t *testing.T) *sql.DB {
	t.Helper()
	db := newUpgrade026Database(t)
	beforeIndex := subscriptionQuotaBaselineFS(t)
	for _, name := range []string{"245_subscription_quota_structure.sql", "246_subscription_quota_usage_links.sql"} {
		body, err := migrations.FS.ReadFile(name)
		require.NoError(t, err)
		beforeIndex[name] = &fstest.MapFile{Data: body}
	}
	require.NoError(t, applyMigrationsFS(context.Background(), db, beforeIndex))
	return db
}

func subscriptionQuotaOnlineIndexSQL(t *testing.T) (name, content, checksum string) {
	t.Helper()
	name = "247_subscription_quota_usage_index_notx.sql"
	body, err := migrations.FS.ReadFile(name)
	require.NoError(t, err)
	content = strings.TrimSpace(string(body))
	sum := sha256.Sum256([]byte(content))
	checksum = hex.EncodeToString(sum[:])
	return name, content, checksum
}

func TestSubscriptionQuotaMigrations_OnlineIndexFailureRestoresTimeouts(t *testing.T) {
	db := subscriptionQuotaBeforeOnlineIndex(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	lockConn, err := db.Conn(ctx)
	require.NoError(t, err)
	defer lockConn.Close()
	_, err = lockConn.ExecContext(ctx, `SELECT set_config('lock_timeout','321ms',false),set_config('statement_timeout','17s',false)`)
	require.NoError(t, err)
	var originalPID int
	require.NoError(t, lockConn.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&originalPID))
	blocker, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = blocker.Rollback() }()
	_, err = blocker.ExecContext(ctx, `LOCK TABLE usage_logs IN ACCESS EXCLUSIVE MODE`)
	require.NoError(t, err)
	name, content, checksum := subscriptionQuotaOnlineIndexSQL(t)
	started := time.Now()
	err = applySubscriptionQuotaOnlineIndexMigration(ctx, lockConn, name, content, checksum)
	requireSubscriptionQuotaSQLState(t, err, "55P03")
	require.Contains(t, err.Error(), "lock timeout")
	var restoredLock, restoredStatement string
	var restoredPID, applied int
	require.NoError(t, lockConn.QueryRowContext(ctx, `SELECT current_setting('lock_timeout'),current_setting('statement_timeout'),pg_backend_pid()`).Scan(&restoredLock, &restoredStatement, &restoredPID))
	require.Equal(t, originalPID, restoredPID, "the restored settings must belong to the same physical PostgreSQL session")
	require.Equal(t, "321ms", restoredLock)
	require.Equal(t, "17s", restoredStatement)
	require.NoError(t, lockConn.QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations WHERE filename=$1`, name).Scan(&applied))
	require.Zero(t, applied, "a failed CREATE INDEX must not record migration success")
	require.NoError(t, blocker.Rollback())
	t.Logf("real CREATE INDEX lock timeout after %s; same backend PID retained both original non-default timeout settings", time.Since(started).Round(time.Millisecond))
}

func TestSubscriptionQuotaMigrations_OnlineInvalidIndexRecovery(t *testing.T) {
	db := subscriptionQuotaBeforeOnlineIndex(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var user, key, account int64
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO users(email,password_hash) VALUES('online-index@example.invalid','synthetic-no-login') RETURNING id`).Scan(&user))
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO api_keys(user_id,key,name) VALUES($1,'synthetic-index-no-upstream','Synthetic index key') RETURNING id`, user).Scan(&key))
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO accounts(name,platform,type,credentials,status) VALUES('Synthetic index disabled account','anthropic','apikey','{}','disabled') RETURNING id`).Scan(&account))
	_, err := db.ExecContext(ctx, `INSERT INTO usage_logs(user_id,api_key_id,account_id,model) VALUES($1,$2,$3,'duplicate-synthetic-model'),($1,$2,$3,'duplicate-synthetic-model')`, user, key, account)
	require.NoError(t, err)
	// A real failed concurrent UNIQUE build leaves a catalog entry with the same
	// name as migration 247's nonunique index. Do not fake pg_index state.
	_, err = db.ExecContext(ctx, `CREATE UNIQUE INDEX CONCURRENTLY usage_logs_subscription_term_time ON usage_logs(model)`)
	requireSubscriptionQuotaSQLState(t, err, "23505")
	var oldOID, newOID int64
	var valid, ready, unique bool
	require.NoError(t, db.QueryRowContext(ctx, `SELECT c.oid::bigint,i.indisvalid FROM pg_class c JOIN pg_index i ON i.indexrelid=c.oid WHERE c.relname='usage_logs_subscription_term_time'`).Scan(&oldOID, &valid))
	require.False(t, valid, "the failed CONCURRENTLY build must leave a real invalid index")
	name, _, checksum := subscriptionQuotaOnlineIndexSQL(t)
	var applied int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations WHERE filename=$1`, name).Scan(&applied))
	require.Zero(t, applied)
	require.NoError(t, ApplyMigrations(ctx, db), "retry must remove the invalid namesake before CREATE INDEX IF NOT EXISTS")
	var definition, storedChecksum string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT c.oid::bigint,i.indisvalid,i.indisready,i.indisunique,pg_get_indexdef(c.oid) FROM pg_class c JOIN pg_index i ON i.indexrelid=c.oid WHERE c.relname='usage_logs_subscription_term_time'`).Scan(&newOID, &valid, &ready, &unique, &definition))
	require.NotEqual(t, oldOID, newOID, "the invalid index must be dropped and replaced, not merely skipped by IF NOT EXISTS")
	require.True(t, valid)
	require.True(t, ready)
	require.False(t, unique)
	require.Contains(t, definition, "(subscription_term_id, created_at)")
	require.Contains(t, definition, "WHERE (subscription_term_id IS NOT NULL)")
	require.NoError(t, db.QueryRowContext(ctx, `SELECT checksum FROM schema_migrations WHERE filename=$1`, name).Scan(&storedChecksum))
	require.Equal(t, checksum, storedChecksum)
	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM usage_logs`).Scan(&applied))
	require.Equal(t, 2, applied, "index recovery must retain existing usage rows")
	requireSubscriptionQuotaSchema(t, db)
}
