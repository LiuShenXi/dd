//go:build integration

package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

type releaseImportFixture struct {
	db       *sql.DB
	limited  *sql.DB
	repo     *CarpoolRepository
	manifest CarpoolReleaseManifest
}

// This acceptance fixture uses its own fully migrated database. The release
// manifest intentionally accounts for every live user, unlike shared fixtures.
func newReleaseImportFixture(t *testing.T) releaseImportFixture {
	t.Helper()
	host := os.Getenv("CARPOOL_INTEGRATION_PG_HOST")
	if host == "" {
		t.Skip("requires the isolated source-runtime PostgreSQL acceptance runner")
	}
	require.Equal(t, "carpool-integration-db", host)
	ctx := context.Background()
	name := "release_acceptance_" + uuid.NewString()[:8]
	role := name + "_importer"
	_, err := integrationDB.ExecContext(ctx, "CREATE DATABASE "+pq.QuoteIdentifier(name))
	require.NoError(t, err)
	dsn := &url.URL{Scheme: "postgres", Host: net.JoinHostPort(host, os.Getenv("CARPOOL_INTEGRATION_PG_PORT")), Path: name, User: url.UserPassword(os.Getenv("CARPOOL_INTEGRATION_PG_USER"), os.Getenv("CARPOOL_INTEGRATION_PG_PASSWORD"))}
	query := dsn.Query()
	query.Set("sslmode", "disable")
	query.Set("TimeZone", "UTC")
	dsn.RawQuery = query.Encode()
	db, err := sql.Open("postgres", dsn.String())
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = db.Close()
		_, _ = integrationDB.ExecContext(context.Background(), "DROP DATABASE "+pq.QuoteIdentifier(name)+" WITH (FORCE)")
		_, _ = integrationDB.ExecContext(context.Background(), "DROP ROLE IF EXISTS "+pq.QuoteIdentifier(role))
	})
	require.NoError(t, ApplyMigrations(ctx, db))
	var now time.Time
	require.NoError(t, db.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now))
	manifest := CarpoolReleaseManifest{BatchKey: "acceptance-" + name, CycleAnchor: now.Add(-2 * time.Hour), Members: make([]CarpoolReleaseMember, 0, 20)}
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO groups(name,platform,subscription_type,allow_live,allow_batch_image_generation) VALUES('Original production route','openai','standard',FALSE,FALSE) RETURNING id`).Scan(&manifest.GroupID))
	createUser := func(email, userRole, balance string, registered time.Time) int64 {
		var id int64
		require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO users(email,password_hash,role,balance,created_at) VALUES($1,'synthetic-password-hash',$2,$3,$4) RETURNING id`, email, userRole, balance, registered).Scan(&id))
		return id
	}
	createKey := func(userID int64, status string) int64 {
		var id int64
		require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO api_keys(user_id,key,name,group_id,status) VALUES($1,$2,'Original unchanged key',$3,$4) RETURNING id`, userID, "sk-fixture-"+uuid.NewString(), manifest.GroupID, status).Scan(&id))
		return id
	}
	manifest.AdminUserID = createUser("admin@example.test", "admin", "9876.12345678", now.Add(-30*24*time.Hour))
	manifest.AdminKeyIDs = []int64{createKey(manifest.AdminUserID, "active"), createKey(manifest.AdminUserID, "active")}
	for i := 0; i < 20; i++ {
		registered := now.Add(-4 * 24 * time.Hour).Truncate(time.Second).Add(time.Duration(123450+i) * time.Microsecond)
		email := fmt.Sprintf("member%d@example.test", i)
		balance := decimal.NewFromInt(int64(800 + i)).Add(decimal.RequireFromString("0.12345678")).StringFixed(8)
		if i == 18 {
			balance = "0.00000000"
		} else if i == 19 {
			balance = "-2.87654321"
		}
		id := createUser(email, "user", balance, registered)
		member := CarpoolReleaseMember{UserID: id, Email: email, PlanCode: "four_seat", WeeklyQuotaUSD: decimal.NewFromInt(550), DurationDays: 28}
		switch i {
		case 0:
			member.WeeklyQuotaUSD = decimal.NewFromInt(450)
		case 1:
			member.PlanCode, member.WeeklyQuotaUSD = "two_seat", decimal.NewFromInt(1000)
		case 2, 3:
			member.DurationDays = 7
		case 4:
			member.PlanCode, member.WeeklyQuotaUSD = "two_seat", decimal.NewFromInt(1100)
		case 5, 6, 7:
			member.PlanCode, member.WeeklyQuotaUSD = "three_seat", decimal.NewFromInt(700)
		}
		manifest.Members = append(manifest.Members, member)
		if i < 18 {
			keyStatus := "active"
			if i == 17 {
				keyStatus = "disabled"
			}
			createKey(id, keyStatus)
		}
	}
	for i := 0; i < 3; i++ {
		manifest.ExcludedUserIDs = append(manifest.ExcludedUserIDs, createUser(fmt.Sprintf("unactivated%d@example.test", i), "user", "0", now.Add(-time.Hour)))
	}
	_, err = db.ExecContext(ctx, "CREATE ROLE "+pq.QuoteIdentifier(role)+" NOLOGIN")
	require.NoError(t, err)
	for _, grant := range []string{
		"GRANT USAGE ON SCHEMA public TO ",
		"GRANT SELECT ON ALL TABLES IN SCHEMA public TO ",
		"GRANT USAGE ON SEQUENCE carpool_terms_id_seq,carpool_cycles_id_seq,carpool_ledger_id_seq,auth_cache_invalidation_outbox_id_seq TO ",
		"GRANT INSERT ON carpool_billing_bindings, carpool_terms, carpool_cycles, carpool_ledger, carpool_release_imports, user_allowed_groups, auth_cache_invalidation_outbox TO ",
		"GRANT DELETE ON user_allowed_groups TO ",
		"GRANT UPDATE(restrict_public_groups,updated_at) ON users TO ",
		"GRANT UPDATE(updated_at) ON groups,api_keys TO ",
	} {
		_, err = db.ExecContext(ctx, grant+pq.QuoteIdentifier(role))
		require.NoError(t, err)
	}
	limited, err := sql.Open("postgres", dsn.String())
	require.NoError(t, err)
	limited.SetMaxOpenConns(1)
	limited.SetMaxIdleConns(1)
	t.Cleanup(func() { _ = limited.Close() })
	_, err = limited.ExecContext(ctx, "SET ROLE "+pq.QuoteIdentifier(role))
	require.NoError(t, err)
	return releaseImportFixture{db: db, limited: limited, repo: NewCarpoolRepository(limited), manifest: manifest}
}

func releaseProtectedRows(t *testing.T, db *sql.DB) string {
	t.Helper()
	var result string
	require.NoError(t, db.QueryRowContext(context.Background(), `SELECT jsonb_build_object(
		'groups',(SELECT jsonb_agg(to_jsonb(g) ORDER BY id) FROM groups g),
		'keys',(SELECT jsonb_agg(to_jsonb(k) ORDER BY id) FROM api_keys k),
		'users',(SELECT jsonb_agg(jsonb_build_array(id,email,role,status,balance,frozen_balance,created_at) ORDER BY id) FROM users),
		'admin',(SELECT jsonb_agg(to_jsonb(u) ORDER BY id) FROM users u WHERE role='admin'),
		'unactivated',(SELECT jsonb_agg(to_jsonb(u) ORDER BY id) FROM users u WHERE email LIKE 'unactivated%')
	)::text`).Scan(&result))
	return result
}

func assertReleaseEmpty(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, table := range []string{"carpool_terms", "carpool_cycles", "carpool_ledger", "carpool_billing_bindings", "carpool_release_imports", "user_allowed_groups"} {
		var count int
		require.NoError(t, db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM "+pq.QuoteIdentifier(table)).Scan(&count))
		require.Zero(t, count, table)
	}
	var restricted int
	require.NoError(t, db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM users WHERE restrict_public_groups`).Scan(&restricted))
	require.Zero(t, restricted)
}

func TestCarpoolReleaseImportRestrictedRoleAndReplay(t *testing.T) {
	f := newReleaseImportFixture(t)
	ctx := context.Background()
	before := releaseProtectedRows(t, f.db)
	_, err := f.limited.ExecContext(ctx, `UPDATE users SET balance=balance+1 WHERE id=$1`, f.manifest.AdminUserID)
	require.ErrorContains(t, err, "permission denied")
	_, err = NewCarpoolRepository(f.db).ImportRelease(ctx, f.manifest, true)
	require.ErrorContains(t, err, "must not have UPDATE privilege")
	preview, err := f.repo.ImportRelease(ctx, f.manifest, false)
	require.NoError(t, err)
	require.False(t, preview.Committed)
	require.Len(t, preview.Members, 20)
	assertReleaseEmpty(t, f.db)
	require.Equal(t, before, releaseProtectedRows(t, f.db))

	// A real debit after preview must be reflected in the final captured balance.
	_, err = f.db.ExecContext(ctx, `UPDATE users SET balance=balance-3.12345678 WHERE id=$1`, f.manifest.Members[0].UserID)
	require.NoError(t, err)
	before = releaseProtectedRows(t, f.db)
	result, err := f.repo.ImportRelease(ctx, f.manifest, true)
	require.NoError(t, err)
	require.True(t, result.Committed)
	require.False(t, result.Replayed)
	require.Equal(t, before, releaseProtectedRows(t, f.db))
	require.True(t, preview.Members[0].OpeningBalanceUSD.Sub(decimal.RequireFromString("3.12345678")).Equal(result.Members[0].OpeningBalanceUSD))
	for i, member := range result.Members {
		approved := f.manifest.Members[i]
		var registered time.Time
		var balance, cycleBalance, cycleQuota, ledgerNet string
		var used, bindings, restrictions int
		var complete bool
		require.NoError(t, f.db.QueryRowContext(ctx, `SELECT created_at,balance::text FROM users WHERE id=$1`, member.UserID).Scan(&registered, &balance))
		require.Equal(t, registered, member.StartsAt)
		require.NotZero(t, registered.Nanosecond())
		require.Equal(t, registered.Add(time.Duration(approved.DurationDays)*24*time.Hour), member.ExpiresAt)
		require.Equal(t, f.manifest.CycleAnchor, member.CycleStartsAt)
		expectedEnd := f.manifest.CycleAnchor.Add(7 * 24 * time.Hour)
		if member.ExpiresAt.Before(expectedEnd) {
			expectedEnd = member.ExpiresAt
		}
		require.Equal(t, expectedEnd, member.CycleEndsAt)
		require.Equal(t, approved.PlanCode, member.Snapshot.Code)
		require.True(t, approved.WeeklyQuotaUSD.Equal(member.Snapshot.WeeklyQuotaUSD))
		require.Equal(t, i < 2, member.Snapshot.WeeklyQuotaCustomized)
		require.Equal(t, approved.DurationDays == 7, member.Snapshot.DurationCustomized)
		require.NoError(t, f.db.QueryRowContext(ctx, `SELECT base_balance_usd::text,base_quota_usd::text FROM carpool_cycles WHERE id=$1`, member.CycleID).Scan(&cycleBalance, &cycleQuota))
		require.True(t, decimal.RequireFromString(balance).Equal(decimal.RequireFromString(cycleBalance)))
		require.True(t, approved.WeeklyQuotaUSD.Equal(decimal.RequireFromString(cycleQuota)))
		require.NoError(t, f.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(delta_usd),0)::text FROM carpool_ledger WHERE term_id=$1`, member.TermID).Scan(&ledgerNet))
		require.True(t, decimal.RequireFromString(balance).Equal(decimal.RequireFromString(ledgerNet)))
		require.NoError(t, f.db.QueryRowContext(ctx, `SELECT boost_used,history_complete FROM carpool_terms WHERE id=$1`, member.TermID).Scan(&used, &complete))
		require.Zero(t, used)
		require.False(t, complete)
		require.NoError(t, f.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM carpool_billing_bindings WHERE user_id=$1 AND group_id=$2`, member.UserID, f.manifest.GroupID).Scan(&bindings))
		require.Equal(t, 1, bindings)
		require.NoError(t, f.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users u JOIN user_allowed_groups a ON a.user_id=u.id WHERE u.id=$1 AND u.restrict_public_groups AND a.group_id=$2`, member.UserID, f.manifest.GroupID).Scan(&restrictions))
		require.Equal(t, 1, restrictions)
	}
	verified, err := f.repo.VerifyRelease(ctx, f.manifest)
	require.NoError(t, err)
	require.Equal(t, result.Fingerprint, verified.Fingerprint)
	replayed, err := f.repo.ImportRelease(ctx, f.manifest, true)
	require.NoError(t, err)
	require.True(t, replayed.Replayed)
	originalMembers, err := json.Marshal(result.Members)
	require.NoError(t, err)
	replayedMembers, err := json.Marshal(replayed.Members)
	require.NoError(t, err)
	require.JSONEq(t, string(originalMembers), string(replayedMembers))
	var terms, cycles, bindings, receipts, ledgers int
	require.NoError(t, f.db.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM carpool_terms),(SELECT COUNT(*) FROM carpool_cycles),(SELECT COUNT(*) FROM carpool_billing_bindings),(SELECT COUNT(*) FROM carpool_release_imports),(SELECT COUNT(*) FROM carpool_ledger)`).Scan(&terms, &cycles, &bindings, &receipts, &ledgers))
	require.Equal(t, 20, terms)
	require.Equal(t, 20, cycles)
	require.Equal(t, 20, bindings)
	require.Equal(t, 1, receipts)
	require.Equal(t, 19, ledgers, "zero opening balance has no invented grant")
	changed := f.manifest
	changed.Members = append([]CarpoolReleaseMember(nil), f.manifest.Members...)
	changed.Members[0].WeeklyQuotaUSD = decimal.NewFromInt(451)
	_, err = f.repo.ImportRelease(ctx, changed, true)
	require.ErrorContains(t, err, "conflicts")
	_, err = f.db.ExecContext(ctx, `UPDATE api_keys SET name='Unexpected mutation' WHERE id=$1`, f.manifest.AdminKeyIDs[0])
	require.NoError(t, err)
	_, err = f.repo.VerifyRelease(ctx, f.manifest)
	require.Error(t, err, "pre-resume verification must detect changed administrator keys")
}

func TestCarpoolReleaseImportRollbackAndRosterGuards(t *testing.T) {
	f := newReleaseImportFixture(t)
	ctx := context.Background()
	before := releaseProtectedRows(t, f.db)
	bad := f.manifest
	bad.Members = append([]CarpoolReleaseMember(nil), f.manifest.Members...)
	bad.Members[19].Email = "wrong-account@example.test"
	_, err := f.repo.ImportRelease(ctx, bad, true)
	require.ErrorContains(t, err, "identity")
	assertReleaseEmpty(t, f.db)
	require.Equal(t, before, releaseProtectedRows(t, f.db))
	var attempted int
	require.NoError(t, f.db.QueryRowContext(ctx, `SELECT last_value FROM carpool_terms_id_seq`).Scan(&attempted))
	require.GreaterOrEqual(t, attempted, 19, "first 19 insertions occurred and were rolled back with the last member failure")
	_, err = f.db.ExecContext(ctx, `INSERT INTO users(email,password_hash) VALUES('unapproved@example.test','synthetic')`)
	require.NoError(t, err)
	_, err = f.repo.ImportRelease(ctx, f.manifest, true)
	require.ErrorContains(t, err, "user count")
	_, err = f.db.ExecContext(ctx, `DELETE FROM users WHERE email='unapproved@example.test'`)
	require.NoError(t, err)
	_, err = f.db.ExecContext(ctx, `UPDATE users SET balance=1 WHERE id=$1`, f.manifest.ExcludedUserIDs[0])
	require.NoError(t, err)
	_, err = f.repo.ImportRelease(ctx, f.manifest, true)
	require.ErrorContains(t, err, "zero-balance")
	_, err = f.db.ExecContext(ctx, `UPDATE users SET balance=0 WHERE id=$1`, f.manifest.ExcludedUserIDs[0])
	require.NoError(t, err)
	assertReleaseEmpty(t, f.db)
	result, err := f.repo.ImportRelease(ctx, f.manifest, true)
	require.NoError(t, err, "a corrected retry can commit after full rollback")
	require.Len(t, result.Members, 20)
}
