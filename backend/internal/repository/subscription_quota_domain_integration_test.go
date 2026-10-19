//go:build integration

package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type subscriptionQuotaDomainFixture struct {
	db                                   *sql.DB
	repo                                 service.SubscriptionQuotaRepository
	api                                  *service.SubscriptionQuotaService
	cfg                                  *config.Config
	admin, otherAdmin, userA, userB      int64
	group, otherGroup, nativeGroup, plan int64
	financialColumns                     map[string][]string
	financialBefore                      map[string]string
	nativeBefore                         string
}

func newSubscriptionQuotaDomainFixture(t *testing.T) *subscriptionQuotaDomainFixture {
	t.Helper()
	f := &subscriptionQuotaDomainFixture{db: newUpgrade026Database(t)}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	require.NoError(t, ApplyMigrations(ctx, f.db))
	_, f.userA, f.userB = seedUpgrade026FinancialFixture(t, f.db)
	require.NoError(t, f.db.QueryRowContext(ctx, `SELECT id FROM users WHERE email='upgrade-admin@example.invalid'`).Scan(&f.admin))
	require.NoError(t, f.db.QueryRowContext(ctx, `INSERT INTO users(email,password_hash,role,balance) VALUES('quota-other-admin@example.invalid','synthetic-no-login','admin',19.12345678) RETURNING id`).Scan(&f.otherAdmin))
	for _, target := range []struct {
		name string
		id   *int64
	}{{"Synthetic quota domain", &f.group}, {"Synthetic other quota domain", &f.otherGroup}} {
		require.NoError(t, f.db.QueryRowContext(ctx, `INSERT INTO groups(name,platform,subscription_type,subscription_quota_mode) VALUES($1,'anthropic','subscription','term') RETURNING id`, target.name).Scan(target.id))
	}
	require.NoError(t, f.db.QueryRowContext(ctx, `INSERT INTO groups(name,platform,subscription_type) VALUES('Synthetic native windows guard','anthropic','subscription') RETURNING id`).Scan(&f.nativeGroup))
	_, err := f.db.ExecContext(ctx, `INSERT INTO user_subscriptions(user_id,group_id,starts_at,expires_at,daily_usage_usd,weekly_usage_usd,monthly_usage_usd) VALUES($1,$2,'2026-01-01T00:00:00Z','2027-01-01T00:00:00Z',1.1234567890,7.8765432109,19.9999999999)`, f.userA, f.nativeGroup)
	require.NoError(t, err)
	_, err = f.db.ExecContext(ctx, `INSERT INTO api_keys(user_id,group_id,key,name) VALUES($1,$2,'synthetic-domain-claude-key-one','Synthetic Claude identity one'),($1,$2,'synthetic-domain-claude-key-two','Synthetic Claude identity two')`, f.userA, f.group)
	require.NoError(t, err)
	require.NoError(t, f.db.QueryRowContext(ctx, `INSERT INTO subscription_plans(group_id,name,price,term_quota_usd,validity_days,validity_unit,for_sale) VALUES($1,'Synthetic default plan',12.34,125.12345678,7,'day',false) RETURNING id`, f.group).Scan(&f.plan))
	f.cfg = &config.Config{RunMode: config.RunModeStandard, SubscriptionQuota: config.SubscriptionQuotaConfig{IssueEnabled: true, EnabledGroupIDs: []int64{f.group, f.otherGroup}}}
	f.repo = NewSubscriptionQuotaRepository(f.db)
	f.api = service.NewSubscriptionQuotaService(f.repo, f.cfg)
	f.financialColumns = make(map[string][]string)
	for table, columns := range subscriptionQuotaLegacyColumns(t, f.db) {
		// Only rights rows, their parent summary, their outbox and explicit
		// administrator plan-fixture edits may change during these scenarios.
		if table != "subscription_quota_terms" && table != "subscription_quota_operations" && table != "user_subscriptions" && table != "auth_cache_invalidation_outbox" && table != "subscription_plans" {
			f.financialColumns[table] = columns
		}
	}
	f.financialBefore = subscriptionQuotaLegacyFingerprints(t, f.db, f.financialColumns)
	f.nativeBefore = f.nativeFingerprint(t)
	return f
}

func (f *subscriptionQuotaDomainFixture) nativeFingerprint(t *testing.T) string {
	t.Helper()
	var value string
	require.NoError(t, f.db.QueryRow(`SELECT md5(COALESCE(jsonb_agg(to_jsonb(s) ORDER BY id)::text,'[]')) FROM user_subscriptions s WHERE group_id=$1`, f.nativeGroup).Scan(&value))
	return value
}

func (f *subscriptionQuotaDomainFixture) assertFinancialIsolation(t *testing.T) {
	t.Helper()
	require.Equal(t, f.financialBefore, subscriptionQuotaLegacyFingerprints(t, f.db, f.financialColumns), "quota rights must not write ordinary money, API Keys, GPT terms/cycles/ledger/payments/boost/reset/receipts, or usage billing")
	require.Equal(t, f.nativeBefore, f.nativeFingerprint(t), "native windows/precision/timestamps must not be rewritten")
}

func quotaDomainInt(value int64) *int64     { return &value }
func quotaDomainMoney(value string) *string { return &value }

func (f *subscriptionQuotaDomainFixture) input(operation, quota string, revision int64) service.SubscriptionQuotaOpenInput {
	return service.SubscriptionQuotaOpenInput{GroupID: f.group, Operation: operation, QuotaUSD: quotaDomainMoney(quota), ExpectedRevision: quotaDomainInt(revision)}
}

func (f *subscriptionQuotaDomainFixture) open(t *testing.T, user int64, in service.SubscriptionQuotaOpenInput) *domain.SubscriptionQuotaMutationResult {
	t.Helper()
	result, err := f.api.OpenTerm(context.Background(), user, f.admin, in, uuid.NewString())
	require.NoError(t, err)
	require.False(t, result.Replayed)
	require.Equal(t, 168*time.Hour, result.Term.ExpiresAt.Sub(result.Term.StartsAt))
	return result
}

func quotaDomainJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	require.NoError(t, err)
	return string(data)
}

func TestSubscriptionQuotaDomain_PreviewAndIndependentUsersSharePerUserCard(t *testing.T) {
	f := newSubscriptionQuotaDomainFixture(t)
	columns := subscriptionQuotaLegacyColumns(t, f.db)
	before := subscriptionQuotaLegacyFingerprints(t, f.db, columns)
	preview, err := f.api.Preview(context.Background(), f.userA, f.admin, f.input("open", "100", 0))
	require.NoError(t, err)
	require.Equal(t, "100.00000000", preview.EffectiveQuotaUSD)
	require.Zero(t, preview.CurrentRevision)
	require.NotNil(t, preview.NextTerms)
	require.NotNil(t, preview.Warnings)
	require.Equal(t, 168*time.Hour, preview.ProposedExpiresAt.Sub(preview.ProposedStartsAt))
	require.Equal(t, before, subscriptionQuotaLegacyFingerprints(t, f.db, columns), "preview must not create a parent, term, operation, outbox, or wallet effect")
	a := f.open(t, f.userA, f.input("open", "100", 0))
	b := f.open(t, f.userB, f.input("open", "300", 0))
	require.NotEqual(t, a.Term.ID, b.Term.ID)
	require.NotEqual(t, a.Term.SubscriptionID, b.Term.SubscriptionID)
	require.Equal(t, "100.00000000", a.Term.QuotaUSD)
	require.Equal(t, "300.00000000", b.Term.QuotaUSD)
	var cards, keys int
	require.NoError(t, f.db.QueryRow(`SELECT count(*) FROM subscription_quota_terms WHERE user_id=$1 AND group_id=$2`, f.userA, f.group).Scan(&cards))
	require.NoError(t, f.db.QueryRow(`SELECT count(*) FROM api_keys WHERE user_id=$1 AND group_id=$2`, f.userA, f.group).Scan(&keys))
	require.Equal(t, 1, cards)
	require.Equal(t, 2, keys, "multiple existing identities must not mint separate allowances")
	for _, user := range []int64{f.userA, f.userB} {
		current, err := f.repo.HasCurrentTerm(context.Background(), user, f.group)
		require.NoError(t, err)
		require.True(t, current)
	}
	f.assertFinancialIsolation(t)
}

func TestSubscriptionQuotaDomain_InvalidMoneyAndMissingDefaultsWriteNothing(t *testing.T) {
	f := newSubscriptionQuotaDomainFixture(t)
	columns := subscriptionQuotaLegacyColumns(t, f.db)
	before := subscriptionQuotaLegacyFingerprints(t, f.db, columns)
	for _, value := range []string{"", "0", "0.00000000", "-1", "-0", "NaN", "Infinity", "-Infinity", "1e2", "+1", "001", ".1", "1.", " 1", "1 ", "0.000000001", "1000000000000", "1.123456789"} {
		t.Run(value, func(t *testing.T) {
			_, err := f.api.OpenTerm(context.Background(), f.userA, f.admin, f.input("open", value, 0), uuid.NewString())
			require.ErrorIs(t, err, service.ErrSubscriptionQuotaInvalid)
		})
	}
	in := f.input("open", "1", 0)
	in.QuotaUSD = nil
	_, err := f.api.OpenTerm(context.Background(), f.userA, f.admin, in, uuid.NewString())
	require.ErrorIs(t, err, service.ErrSubscriptionQuotaRequired)
	for _, revision := range []*int64{nil, quotaDomainInt(-1)} {
		invalid := f.input("open", "100", 0)
		invalid.ExpectedRevision = revision
		_, err = f.api.OpenTerm(context.Background(), f.userA, f.admin, invalid, uuid.NewString())
		require.ErrorIs(t, err, service.ErrSubscriptionQuotaInput)
	}
	for _, key := range []string{"", "short", strings.Repeat("x", 129), " invalid-key-with-leading-space", "invalid-key-with\nnewline"} {
		_, err = f.api.OpenTerm(context.Background(), f.userA, f.admin, f.input("open", "100", 0), key)
		require.ErrorIs(t, err, service.ErrSubscriptionQuotaInput)
	}
	require.Equal(t, before, subscriptionQuotaLegacyFingerprints(t, f.db, columns), "invalid money and omitted sources must leave every table unchanged")
	_, err = f.db.Exec(`UPDATE subscription_plans SET term_quota_usd=NULL WHERE id=$1`, f.plan)
	require.NoError(t, err)
	columns = subscriptionQuotaLegacyColumns(t, f.db)
	// The administrator's explicit test-fixture plan edit is outside the call;
	// snapshot after it so every service-induced row change is still forbidden.
	before = subscriptionQuotaLegacyFingerprints(t, f.db, columns)
	in.PlanID = &f.plan
	_, err = f.api.OpenTerm(context.Background(), f.userA, f.admin, in, uuid.NewString())
	require.ErrorIs(t, err, service.ErrSubscriptionQuotaRequired)
	require.Equal(t, before, subscriptionQuotaLegacyFingerprints(t, f.db, columns), "all invalid operations, including default resolution, must leave zero durable writes")
	f.assertFinancialIsolation(t)
}

func TestSubscriptionQuotaDomain_ConcurrentReplayAndRevisionSerializeSchedule(t *testing.T) {
	f := newSubscriptionQuotaDomainFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	type outcome struct {
		result *domain.SubscriptionQuotaMutationResult
		err    error
	}
	start := make(chan struct{})
	results := make(chan outcome, 8)
	var workers sync.WaitGroup
	key := uuid.NewString()
	for i := 0; i < 8; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			result, err := f.api.OpenTerm(ctx, f.userA, f.admin, f.input("open", "100", 0), key)
			results <- outcome{result, err}
		}()
	}
	close(start)
	workers.Wait()
	close(results)
	var first *domain.SubscriptionQuotaMutationResult
	fresh := 0
	for result := range results {
		require.NoError(t, result.err)
		require.NotNil(t, result.result)
		if !result.result.Replayed {
			fresh++
		}
		copy := *result.result
		copy.Replayed = false
		if first == nil {
			first = &copy
		} else {
			require.JSONEq(t, quotaDomainJSON(t, first), quotaDomainJSON(t, copy), "concurrent duplicate response and original DB times must be identical")
		}
	}
	require.Equal(t, 1, fresh)
	require.EqualValues(t, 1, first.Summary.Revision)
	var operations, cards int
	require.NoError(t, f.db.QueryRow(`SELECT count(*) FROM subscription_quota_operations`).Scan(&operations))
	require.NoError(t, f.db.QueryRow(`SELECT count(*) FROM subscription_quota_terms`).Scan(&cards))
	require.Equal(t, 1, operations)
	require.Equal(t, 1, cards)

	start = make(chan struct{})
	results = make(chan outcome, 2)
	for i := 0; i < 2; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			result, err := f.api.OpenTerm(ctx, f.userA, f.admin, f.input("renew", "200", 1), uuid.NewString())
			results <- outcome{result, err}
		}()
	}
	close(start)
	workers.Wait()
	close(results)
	var second *domain.SubscriptionQuotaMutationResult
	conflicts := 0
	for result := range results {
		if result.err != nil {
			require.ErrorIs(t, result.err, service.ErrSubscriptionQuotaRevision)
			conflicts++
		} else {
			second = result.result
		}
	}
	require.Equal(t, 1, conflicts)
	require.NotNil(t, second)
	require.True(t, first.Term.ExpiresAt.Equal(second.Term.StartsAt))
	require.EqualValues(t, 2, second.Summary.Revision)
	third := f.open(t, f.userA, f.input("renew", "300", 2))
	require.True(t, second.Term.ExpiresAt.Equal(third.Term.StartsAt))
	require.EqualValues(t, 3, third.Term.TermNo)
	require.EqualValues(t, 3, third.Summary.Revision)
	var overlaps int
	require.NoError(t, f.db.QueryRow(`SELECT count(*) FROM subscription_quota_terms a JOIN subscription_quota_terms b ON a.subscription_id=b.subscription_id AND a.id<b.id AND tstzrange(a.starts_at,a.expires_at,'[)') && tstzrange(b.starts_at,b.expires_at,'[)')`).Scan(&overlaps))
	require.Zero(t, overlaps)
	columns := subscriptionQuotaLegacyColumns(t, f.db)
	before := subscriptionQuotaLegacyFingerprints(t, f.db, columns)
	_, err := f.api.OpenTerm(ctx, f.userA, f.admin, f.input("renew", "400", 1), uuid.NewString())
	require.ErrorIs(t, err, service.ErrSubscriptionQuotaRevision)
	require.Equal(t, before, subscriptionQuotaLegacyFingerprints(t, f.db, columns), "a stale independent intent must not write an operation or advance revision")
	f.assertFinancialIsolation(t)
}

func (f *subscriptionQuotaDomainFixture) cancel(t *testing.T, termID, revision int64, key string) *domain.SubscriptionQuotaMutationResult {
	t.Helper()
	result, err := f.api.CancelTerm(context.Background(), termID, f.admin, service.SubscriptionQuotaCancelInput{ExpectedRevision: quotaDomainInt(revision), Reason: "synthetic cancellation"}, key)
	require.NoError(t, err)
	require.Equal(t, "cancelled", result.Term.Status)
	require.Equal(t, "0.00000000", result.Term.RemainingUSD)
	return result
}

func (f *subscriptionQuotaDomainFixture) parent(t *testing.T, id int64) (time.Time, time.Time, string, int64) {
	t.Helper()
	var start, expiry time.Time
	var status string
	var revision int64
	require.NoError(t, f.db.QueryRow(`SELECT starts_at,expires_at,status,quota_revision FROM user_subscriptions WHERE id=$1`, id).Scan(&start, &expiry, &status, &revision))
	return start, expiry, status, revision
}

func TestSubscriptionQuotaDomain_CancellationPreservesGapShrinksTailAndReopensNow(t *testing.T) {
	f := newSubscriptionQuotaDomainFixture(t)
	a := f.open(t, f.userA, f.input("open", "100", 0))
	b := f.open(t, f.userA, f.input("renew", "200", 1))
	c := f.open(t, f.userA, f.input("renew", "300", 2))
	key := uuid.NewString()
	cancelledMiddle := f.cancel(t, b.Term.ID, 3, key)
	require.Equal(t, b.Term.QuotaUSD, cancelledMiddle.Term.QuotaUSD)
	require.True(t, b.Term.StartsAt.Equal(cancelledMiddle.Term.StartsAt))
	require.True(t, b.Term.ExpiresAt.Equal(cancelledMiddle.Term.ExpiresAt))
	require.Len(t, cancelledMiddle.Summary.NextTerms, 1)
	require.Equal(t, c.Term.ID, cancelledMiddle.Summary.NextTerms[0].ID)
	require.True(t, c.Term.StartsAt.Equal(cancelledMiddle.Summary.NextTerms[0].StartsAt), "cancelling the middle must not shift later cards into the gap")
	gap := f.cancel(t, a.Term.ID, 4, uuid.NewString())
	require.Nil(t, gap.Summary.CurrentTerm)
	require.NotNil(t, gap.Summary.LastTerm)
	require.Equal(t, a.Term.ID, gap.Summary.LastTerm.ID, "the most recently ended card follows cancellation time, not largest term number")
	require.Len(t, gap.Summary.NextTerms, 1)
	_, expiry, _, revision := f.parent(t, a.Term.SubscriptionID)
	require.True(t, c.Term.ExpiresAt.Equal(expiry))
	require.EqualValues(t, 5, revision)
	has, err := f.repo.HasCurrentTerm(context.Background(), f.userA, f.group)
	require.NoError(t, err)
	require.False(t, has, "a far future parent expiry cannot grant current rights across a cancelled gap")
	ids, err := f.repo.ListCurrentTermGroupIDs(context.Background(), f.userA)
	require.NoError(t, err)
	require.Empty(t, ids)
	_, err = f.api.OpenTerm(context.Background(), f.userA, f.admin, f.input("open", "400", 5), uuid.NewString())
	require.ErrorIs(t, err, service.ErrSubscriptionQuotaSchedule, "future uncancelled cards still prohibit a new open")
	d := f.open(t, f.userA, f.input("renew", "400", 5))
	require.True(t, c.Term.ExpiresAt.Equal(d.Term.StartsAt), "renewal appends at the queue end instead of filling cancelled gaps")
	f.cancel(t, d.Term.ID, 6, uuid.NewString())
	_, expiry, _, revision = f.parent(t, a.Term.SubscriptionID)
	require.True(t, c.Term.ExpiresAt.Equal(expiry), "cancelling the future tail must shrink the compatibility parent")
	require.EqualValues(t, 7, revision)
	allCancelled := f.cancel(t, c.Term.ID, 7, uuid.NewString())
	require.NotNil(t, allCancelled.Summary.LastTerm)
	require.Equal(t, c.Term.ID, allCancelled.Summary.LastTerm.ID, "the earlier-numbered card cancelled last is the most recently ended")
	_, expiry, status, revision := f.parent(t, a.Term.SubscriptionID)
	require.Equal(t, "cancelled", status)
	require.True(t, d.Term.ExpiresAt.Equal(expiry), "when all cards are cancelled preserve the historical endpoint")
	require.EqualValues(t, 8, revision)
	e := f.open(t, f.userA, f.input("open", "500", 8))
	require.EqualValues(t, 5, e.Term.TermNo)
	require.WithinDuration(t, e.Summary.ServerTime, e.Term.StartsAt, time.Microsecond)
	require.True(t, e.Term.StartsAt.Before(a.Term.ExpiresAt), "opening after all cancellations uses fresh DB now, not a cancelled future tail")
	_, expiry, status, revision = f.parent(t, a.Term.SubscriptionID)
	require.Equal(t, "active", status)
	require.True(t, e.Term.ExpiresAt.Equal(expiry))
	require.EqualValues(t, 9, revision)
	replayed, err := f.api.CancelTerm(context.Background(), b.Term.ID, f.admin, service.SubscriptionQuotaCancelInput{ExpectedRevision: quotaDomainInt(3), Reason: "synthetic cancellation"}, key)
	require.NoError(t, err)
	want := *cancelledMiddle
	want.Replayed = true
	require.JSONEq(t, quotaDomainJSON(t, want), quotaDomainJSON(t, replayed), "cancel replay returns its original result after later schedule changes")
	_, err = f.api.CancelTerm(context.Background(), b.Term.ID, f.admin, service.SubscriptionQuotaCancelInput{ExpectedRevision: quotaDomainInt(3), Reason: "changed reason"}, key)
	require.ErrorIs(t, err, service.ErrSubscriptionQuotaIdempotency)
	f.assertFinancialIsolation(t)
}

func TestSubscriptionQuotaDomain_ExhaustionDoesNotActivateRenewalEarly(t *testing.T) {
	f := newSubscriptionQuotaDomainFixture(t)
	a := f.open(t, f.userA, f.input("open", "0.00000001", 0))
	_, err := f.db.Exec(`UPDATE subscription_quota_terms SET used_usd=quota_usd WHERE id=$1`, a.Term.ID)
	require.NoError(t, err, "synthetic fixture models previously settled usage without running an upstream")
	b := f.open(t, f.userA, f.input("renew", "999999999999.99999999", 1))
	require.Equal(t, "999999999999.99999999", b.Term.QuotaUSD)
	require.True(t, a.Term.ExpiresAt.Equal(b.Term.StartsAt))
	require.Equal(t, "scheduled", b.Term.Status)
	require.Equal(t, "0.00000000", b.Term.UsedUSD)
	require.Equal(t, "0.00000000", b.Term.RemainingUSD)
	require.NotNil(t, b.Summary.CurrentTerm)
	require.Equal(t, a.Term.ID, b.Summary.CurrentTerm.ID)
	require.Equal(t, "exhausted", b.Summary.CurrentTerm.Status)
	require.Equal(t, "0.00000000", b.Summary.CurrentTerm.RemainingUSD)
	has, err := f.repo.HasCurrentTerm(context.Background(), f.userA, f.group)
	require.NoError(t, err)
	require.True(t, has, "current card identity remains visible even when generation allowance is exhausted")
	ids, err := f.repo.ListCurrentTermGroupIDs(context.Background(), f.userA)
	require.NoError(t, err)
	require.Equal(t, []int64{f.group}, ids)
	f.assertFinancialIsolation(t)
}

func TestSubscriptionQuotaDomain_ExactHalfOpenBoundaryUsesProductionPredicate(t *testing.T) {
	f := newSubscriptionQuotaDomainFixture(t)
	a := f.open(t, f.userA, f.input("open", "100.12345678", 0))
	terms, err := subscriptionQuotaReadTerms(context.Background(), f.db, a.Term.SubscriptionID, false)
	require.NoError(t, err)
	require.Len(t, terms, 1)
	stored := terms[0]
	require.Equal(t, 168*time.Hour, stored.ExpiresAt.Sub(stored.StartsAt))
	for _, point := range []struct {
		name      string
		at        time.Time
		status    string
		entitled  bool
		remaining string
	}{
		{"before_start", stored.StartsAt.Add(-time.Microsecond), "scheduled", false, "0.00000000"},
		{"at_start", stored.StartsAt, "active", true, "100.12345678"},
		{"before_expiry", stored.ExpiresAt.Add(-time.Microsecond), "active", true, "100.12345678"},
		{"at_expiry", stored.ExpiresAt, "expired", false, "0.00000000"},
		{"after_expiry", stored.ExpiresAt.Add(time.Microsecond), "expired", false, "0.00000000"},
	} {
		t.Run(point.name, func(t *testing.T) {
			projection := domain.ProjectSubscriptionQuotaTerm(stored, point.at)
			require.Equal(t, point.status, projection.Status)
			require.Equal(t, point.remaining, projection.RemainingUSD)
			var exists bool
			// Reuse the actual production authorization FROM and predicate. Only
			// its materialized database clock input is deterministic in this test.
			err := f.db.QueryRow(`WITH quota_clock AS MATERIALIZED (SELECT $3::timestamptz AS now) SELECT EXISTS(SELECT 1`+subscriptionQuotaCurrentTermIdentity+subscriptionQuotaCurrentTermPredicate+` AND t.user_id=$1 AND t.group_id=$2)`, f.userA, f.group, point.at).Scan(&exists)
			require.NoError(t, err)
			require.Equal(t, point.entitled, exists)
		})
	}
	f.cancel(t, a.Term.ID, 1, uuid.NewString())
	terms, err = subscriptionQuotaReadTerms(context.Background(), f.db, a.Term.SubscriptionID, false)
	require.NoError(t, err)
	projected := domain.ProjectSubscriptionQuotaTerm(terms[0], stored.StartsAt)
	require.Equal(t, "cancelled", projected.Status)
	require.Equal(t, "0.00000000", projected.RemainingUSD)
	var exists bool
	require.NoError(t, f.db.QueryRow(`WITH quota_clock AS MATERIALIZED (SELECT $3::timestamptz AS now) SELECT EXISTS(SELECT 1`+subscriptionQuotaCurrentTermIdentity+subscriptionQuotaCurrentTermPredicate+` AND t.user_id=$1 AND t.group_id=$2)`, f.userA, f.group, stored.StartsAt).Scan(&exists))
	require.False(t, exists)
	f.assertFinancialIsolation(t)
}

func TestSubscriptionQuotaDomain_PlanChangeDeletionAndReplayUseOriginalSnapshot(t *testing.T) {
	f := newSubscriptionQuotaDomainFixture(t)
	key := uuid.NewString()
	in := service.SubscriptionQuotaOpenInput{GroupID: f.group, Operation: "open", PlanID: &f.plan, ExpectedRevision: quotaDomainInt(0), Notes: "original synthetic intent"}
	first, err := f.api.OpenTerm(context.Background(), f.userA, f.admin, in, key)
	require.NoError(t, err, "manual administrators may select a valid plan that is not for sale")
	require.Equal(t, "125.12345678", first.Term.QuotaUSD)
	_, err = f.db.Exec(`UPDATE subscription_plans SET term_quota_usd=999,name='Changed synthetic plan',validity_days=14 WHERE id=$1`, f.plan)
	require.NoError(t, err)
	want := *first
	want.Replayed = true
	for _, deletePlan := range []bool{false, true} {
		if deletePlan {
			_, err = f.db.Exec(`DELETE FROM subscription_plans WHERE id=$1`, f.plan)
			require.NoError(t, err)
		}
		columns := subscriptionQuotaLegacyColumns(t, f.db)
		before := subscriptionQuotaLegacyFingerprints(t, f.db, columns)
		replayed, err := f.api.OpenTerm(context.Background(), f.userA, f.admin, in, key)
		require.NoError(t, err, "original replay precedes today's plan validity and stale expected_revision")
		require.True(t, replayed.Replayed)
		require.JSONEq(t, quotaDomainJSON(t, want), quotaDomainJSON(t, replayed))
		require.Equal(t, before, subscriptionQuotaLegacyFingerprints(t, f.db, columns), "replay must not append an operation/outbox row or advance parent/term revisions")
	}
	changed := in
	changed.QuotaUSD = quotaDomainMoney("125.12345678")
	_, err = f.api.OpenTerm(context.Background(), f.userA, f.admin, changed, key)
	require.ErrorIs(t, err, service.ErrSubscriptionQuotaIdempotency, "an explicit override is a different intent even if its amount matches the original default")
	changed = in
	changed.PlanID = quotaDomainInt(f.plan + 900000)
	_, err = f.api.OpenTerm(context.Background(), f.userA, f.admin, changed, key)
	require.ErrorIs(t, err, service.ErrSubscriptionQuotaIdempotency)
	other := f.input("open", "300", 0)
	b, err := f.api.OpenTerm(context.Background(), f.userB, f.admin, other, key)
	require.NoError(t, err, "a different user has an independent operation scope")
	require.NotEqual(t, first.Term.ID, b.Term.ID)
	var cards, operations int
	require.NoError(t, f.db.QueryRow(`SELECT count(*) FROM subscription_quota_terms WHERE user_id=$1 AND group_id=$2`, f.userA, f.group).Scan(&cards))
	require.NoError(t, f.db.QueryRow(`SELECT count(*) FROM subscription_quota_operations`).Scan(&operations))
	require.Equal(t, 1, cards)
	require.Equal(t, 2, operations)
	f.assertFinancialIsolation(t)
}

func TestSubscriptionQuotaDomain_IdempotencyScopesNeverReturnOtherActorsResult(t *testing.T) {
	f := newSubscriptionQuotaDomainFixture(t)
	ctx := context.Background()
	key := uuid.NewString()
	in := f.input("open", "1", 0)
	a, err := f.api.OpenTerm(ctx, f.userA, f.admin, in, key)
	require.NoError(t, err)
	in.QuotaUSD = quotaDomainMoney("1.00000000")
	replay, err := f.api.OpenTerm(ctx, f.userA, f.admin, in, key)
	require.NoError(t, err, "equivalent exact decimal spelling normalizes to the same intent")
	require.True(t, replay.Replayed)
	require.Equal(t, a.Term.ID, replay.Term.ID)
	for _, amount := range []string{"2", "1.00000001"} {
		changed := in
		changed.QuotaUSD = quotaDomainMoney(amount)
		_, err = f.api.OpenTerm(ctx, f.userA, f.admin, changed, key)
		require.ErrorIs(t, err, service.ErrSubscriptionQuotaIdempotency)
	}
	foreign := in
	foreign.ExpectedRevision = quotaDomainInt(1)
	otherActor, err := f.api.OpenTerm(ctx, f.userA, f.otherAdmin, foreign, key)
	require.ErrorIs(t, err, service.ErrSubscriptionQuotaSchedule)
	require.Nil(t, otherActor, "another actor must never receive the stored original response")
	otherGroup := in
	otherGroup.GroupID = f.otherGroup
	b, err := f.api.OpenTerm(ctx, f.userA, f.admin, otherGroup, key)
	require.NoError(t, err)
	require.False(t, b.Replayed)
	require.Equal(t, f.otherGroup, b.Term.GroupID)
	require.NotEqual(t, a.Term.ID, b.Term.ID)
	renew := f.input("renew", "2", 1)
	c, err := f.api.OpenTerm(ctx, f.userA, f.admin, renew, key)
	require.NoError(t, err, "open and renew have independent action scopes")
	require.False(t, c.Replayed)
	require.True(t, a.Term.ExpiresAt.Equal(c.Term.StartsAt))
	var operations int
	require.NoError(t, f.db.QueryRow(`SELECT count(*) FROM subscription_quota_operations`).Scan(&operations))
	require.Equal(t, 3, operations)
	f.assertFinancialIsolation(t)
}

func TestSubscriptionQuotaDomain_OwnershipStablePaginationAndEmptyArrays(t *testing.T) {
	f := newSubscriptionQuotaDomainFixture(t)
	ctx := context.Background()
	empty, err := f.api.ListSummaries(ctx, f.userB)
	require.NoError(t, err)
	require.NotNil(t, empty)
	require.Empty(t, empty)
	emptyPage, err := f.api.ListTerms(ctx, f.userB, f.group, service.SubscriptionQuotaPageInput{})
	require.NoError(t, err)
	require.NotNil(t, emptyPage.Items)
	require.Empty(t, emptyPage.Items)
	require.False(t, emptyPage.HasMore)
	require.Nil(t, emptyPage.NextCursor)
	a := f.open(t, f.userA, f.input("open", "100", 0))
	b := f.open(t, f.userA, f.input("renew", "200", 1))
	c := f.open(t, f.userA, f.input("renew", "300", 2))
	foreign := f.open(t, f.userB, f.input("open", "400", 0))
	otherIn := f.input("open", "500", 0)
	otherIn.GroupID = f.otherGroup
	other := f.open(t, f.userA, otherIn)
	page, err := f.api.ListTerms(ctx, f.userA, f.group, service.SubscriptionQuotaPageInput{Limit: 1})
	require.NoError(t, err)
	require.True(t, page.HasMore)
	require.Len(t, page.Items, 1)
	require.Equal(t, c.Term.ID, page.Items[0].ID)
	require.NotNil(t, page.NextCursor)
	f.open(t, f.userA, f.input("renew", "600", 3))
	remaining, err := f.api.ListTerms(ctx, f.userA, f.group, service.SubscriptionQuotaPageInput{Cursor: *page.NextCursor, Limit: 100})
	require.NoError(t, err)
	require.Len(t, remaining.Items, 2, "inserting a newer card must not shift or duplicate the next page")
	require.Equal(t, []int64{b.Term.ID, a.Term.ID}, []int64{remaining.Items[0].ID, remaining.Items[1].ID})
	require.False(t, remaining.HasMore)
	foreignPage, err := f.api.ListTerms(ctx, f.userB, f.group, service.SubscriptionQuotaPageInput{Cursor: *page.NextCursor, Limit: 100})
	require.NoError(t, err)
	require.Empty(t, foreignPage.Items, "a valid foreign cursor cannot bypass user filtering")
	otherPage, err := f.api.ListTerms(ctx, f.userA, f.otherGroup, service.SubscriptionQuotaPageInput{})
	require.NoError(t, err)
	require.Len(t, otherPage.Items, 1)
	require.Equal(t, other.Term.ID, otherPage.Items[0].ID)
	usage, err := f.api.ListUsage(ctx, f.userA, a.Term.ID, service.SubscriptionQuotaPageInput{})
	require.NoError(t, err)
	require.NotNil(t, usage.Items)
	require.Empty(t, usage.Items)
	for _, target := range []int64{a.Term.ID, a.Term.ID + 9000000} {
		leaked, err := f.api.ListUsage(ctx, f.userB, target, service.SubscriptionQuotaPageInput{})
		require.ErrorIs(t, err, service.ErrSubscriptionQuotaNotFound, "foreign and missing term IDs have the same response")
		require.Nil(t, leaked)
	}
	own, err := f.api.ListSummaries(ctx, f.userB)
	require.NoError(t, err)
	require.Len(t, own, 1)
	require.Equal(t, foreign.Term.ID, own[0].CurrentTerm.ID)
	for _, bad := range []service.SubscriptionQuotaPageInput{{Cursor: "not-a-cursor"}, {Limit: -1}, {Limit: 101}} {
		_, err = f.api.ListTerms(ctx, f.userA, f.group, bad)
		require.ErrorIs(t, err, service.ErrSubscriptionQuotaInput)
	}
	f.assertFinancialIsolation(t)
}

func TestSubscriptionQuotaDomain_InvalidSourcesAndClosedIssuanceWriteNothing(t *testing.T) {
	f := newSubscriptionQuotaDomainFixture(t)
	ctx := context.Background()
	var wrongPlan, longPlan int64
	require.NoError(t, f.db.QueryRow(`INSERT INTO subscription_plans(group_id,name,price,term_quota_usd,validity_days,validity_unit) VALUES($1,'Wrong group synthetic plan',1,100,7,'day') RETURNING id`, f.otherGroup).Scan(&wrongPlan))
	require.NoError(t, f.db.QueryRow(`INSERT INTO subscription_plans(group_id,name,price,term_quota_usd,validity_days,validity_unit) VALUES($1,'Wrong length synthetic plan',1,100,14,'day') RETURNING id`, f.group).Scan(&longPlan))
	columns := subscriptionQuotaLegacyColumns(t, f.db)
	before := subscriptionQuotaLegacyFingerprints(t, f.db, columns)
	for _, plan := range []int64{wrongPlan, longPlan} {
		in := f.input("open", "100", 0)
		in.PlanID = quotaDomainInt(plan)
		_, err := f.api.OpenTerm(ctx, f.userA, f.admin, in, uuid.NewString())
		require.ErrorIs(t, err, service.ErrSubscriptionQuotaInput)
	}
	for _, test := range []struct {
		name string
		api  *service.SubscriptionQuotaService
		in   service.SubscriptionQuotaOpenInput
	}{
		{"issue_disabled", service.NewSubscriptionQuotaService(f.repo, &config.Config{RunMode: config.RunModeStandard}), f.input("open", "100", 0)},
		{"not_allowlisted", service.NewSubscriptionQuotaService(f.repo, &config.Config{RunMode: config.RunModeStandard, SubscriptionQuota: config.SubscriptionQuotaConfig{IssueEnabled: true}}), f.input("open", "100", 0)},
		{"simple", service.NewSubscriptionQuotaService(f.repo, &config.Config{RunMode: config.RunModeSimple, SubscriptionQuota: f.cfg.SubscriptionQuota}), f.input("open", "100", 0)},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := test.api.OpenTerm(ctx, f.userA, f.admin, test.in, uuid.NewString())
			require.ErrorIs(t, err, service.ErrSubscriptionQuotaBillingUnavailable)
		})
	}
	in := f.input("open", "100", 0)
	in.GroupID = f.nativeGroup
	_, err := f.api.OpenTerm(ctx, f.userA, f.admin, in, uuid.NewString())
	require.Error(t, err)
	_, err = f.api.OpenTerm(ctx, f.userA+900000, f.admin, f.input("open", "100", 0), uuid.NewString())
	require.ErrorIs(t, err, service.ErrSubscriptionQuotaNotFound)
	_, err = f.api.OpenTerm(ctx, f.userA, f.userB, f.input("open", "100", 0), uuid.NewString())
	require.ErrorIs(t, err, service.ErrSubscriptionQuotaNotFound)
	require.Equal(t, before, subscriptionQuotaLegacyFingerprints(t, f.db, columns), "invalid sources, actor/target IDs and disabled issuance leave every table unchanged")

	// Explicit exact allowance wins over a currently valid selected default.
	in = f.input("open", "200.87654321", 0)
	in.PlanID = &f.plan
	opened := f.open(t, f.userA, in)
	require.Equal(t, "200.87654321", opened.Term.QuotaUSD)
	var snapshot domain.SubscriptionQuotaSourceSnapshot
	var data []byte
	require.NoError(t, f.db.QueryRow(`SELECT source_snapshot FROM subscription_quota_terms WHERE id=$1`, opened.Term.ID).Scan(&data))
	require.NoError(t, json.Unmarshal(data, &snapshot))
	require.Equal(t, "admin_override", snapshot.QuotaSource)
	require.Equal(t, "200.87654321", snapshot.QuotaUSD)
	require.NotNil(t, snapshot.PlanDefaultQuotaUSD)
	require.Equal(t, "125.12345678", *snapshot.PlanDefaultQuotaUSD)
	require.Equal(t, 168, snapshot.DurationHours)
	closed := service.NewSubscriptionQuotaService(f.repo, &config.Config{RunMode: config.RunModeStandard})
	cancelled, err := closed.CancelTerm(ctx, opened.Term.ID, f.admin, service.SubscriptionQuotaCancelInput{ExpectedRevision: quotaDomainInt(1), Reason: "stop synthetic rights while issuing is closed"}, uuid.NewString())
	require.NoError(t, err, "disabling issuance must not block administrative cancellation")
	require.Equal(t, "cancelled", cancelled.Term.Status)
	f.assertFinancialIsolation(t)
}

func TestSubscriptionQuotaDomain_LegacyGroupDeletionRejectsWithoutWaitingForParent(t *testing.T) {
	f := newSubscriptionQuotaDomainFixture(t)
	a := f.open(t, f.userA, f.input("open", "100", 0))
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, f.db)))
	t.Cleanup(func() { _ = client.Close() })
	groups := newGroupRepositoryWithSQL(client, f.db)
	type outcome struct {
		result *domain.SubscriptionQuotaMutationResult
		err    error
	}
	runBlockedMutation := func(name string, mutate func(context.Context) (*domain.SubscriptionQuotaMutationResult, error)) *domain.SubscriptionQuotaMutationResult {
		blocker, err := f.db.BeginTx(context.Background(), nil)
		require.NoError(t, err)
		t.Cleanup(func() { _ = blocker.Rollback() })
		var locked int64
		require.NoError(t, blocker.QueryRow(`SELECT id FROM user_subscriptions WHERE id=$1 FOR UPDATE`, a.Term.SubscriptionID).Scan(&locked))
		require.Equal(t, a.Term.SubscriptionID, locked)
		columns := subscriptionQuotaLegacyColumns(t, f.db)
		before := subscriptionQuotaLegacyFingerprints(t, f.db, columns)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		results := make(chan outcome, 1)
		go func() { result, err := mutate(ctx); results <- outcome{result, err} }()
		require.Eventually(t, func() bool {
			var waiting bool
			err := f.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND state='active' AND wait_event_type='Lock' AND query LIKE 'SELECT id,quota_revision FROM user_subscriptions%FOR UPDATE')`).Scan(&waiting)
			return err == nil && waiting
		}, 3*time.Second, 10*time.Millisecond, "the real quota mutation must be waiting on this parent before legacy deletion begins")
		for _, group := range []struct {
			name string
			id   int64
		}{{"with_parent", f.group}, {"empty_term_group", f.otherGroup}} {
			for _, operation := range []struct {
				name string
				call func(context.Context, int64) error
			}{
				{"delete_cascade", func(ctx context.Context, id int64) error { _, err := groups.DeleteCascade(ctx, id); return err }},
				{"delete_if_empty", func(ctx context.Context, id int64) error { _, err := groups.DeleteCascadeIfEmpty(ctx, id); return err }},
				{"direct_delete", groups.Delete},
			} {
				t.Run(name+"/"+group.name+"/"+operation.name, func(t *testing.T) {
					callCtx, callCancel := context.WithTimeout(ctx, 2*time.Second)
					defer callCancel()
					started := time.Now()
					err := operation.call(callCtx, group.id)
					require.ErrorIs(t, err, service.ErrLegacySubscriptionOperationUnsupported, "term group deletion must reject before touching the locked parent")
					t.Logf("legacy deletion rejected in %s while %s and parent row lock remained pending", time.Since(started), name)
				})
			}
		}
		// The existing GPT preparation path must not acquire a Claude parent
		// lock. Roll it back so this lock-order probe changes no GPT rights.
		var gptGroup int64
		require.NoError(t, f.db.QueryRow(`SELECT group_id FROM carpool_billing_bindings WHERE user_id=$1`, f.userA).Scan(&gptGroup))
		gpt, err := f.db.BeginTx(ctx, nil)
		require.NoError(t, err)
		t.Cleanup(func() { _ = gpt.Rollback() })
		_, err = gpt.ExecContext(ctx, `SET LOCAL lock_timeout='500ms'`)
		require.NoError(t, err)
		require.NoError(t, prepareCarpoolUserGroupTx(ctx, gpt, domain.CreateCarpoolTermParams{UserID: f.userA, GroupID: gptGroup}), "actual GPT preparation must finish while the Claude parent lock and quota mutation are pending")
		require.NoError(t, gpt.Rollback())
		require.Equal(t, before, subscriptionQuotaLegacyFingerprints(t, f.db, columns), "rejected deletion and rolled-back GPT probe must not change group/parent deleted_at, terms, outboxes, or financial data")
		var groupDeleted, parentDeleted bool
		require.NoError(t, f.db.QueryRow(`SELECT g.deleted_at IS NOT NULL,p.deleted_at IS NOT NULL FROM groups g JOIN user_subscriptions p ON p.group_id=g.id WHERE p.id=$1`, a.Term.SubscriptionID).Scan(&groupDeleted, &parentDeleted))
		require.False(t, groupDeleted)
		require.False(t, parentDeleted)
		require.NoError(t, blocker.Rollback())
		select {
		case completed := <-results:
			require.NoError(t, completed.err)
			require.NotNil(t, completed.result)
			return completed.result
		case <-time.After(3 * time.Second):
			t.Fatal("quota mutation did not finish after releasing the parent lock")
			return nil
		}
	}
	b := runBlockedMutation("renew", func(ctx context.Context) (*domain.SubscriptionQuotaMutationResult, error) {
		return f.api.OpenTerm(ctx, f.userA, f.admin, f.input("renew", "200", 1), uuid.NewString())
	})
	require.True(t, a.Term.ExpiresAt.Equal(b.Term.StartsAt), "rights renewal remains available after rejected legacy deletion and lock release")
	cancelled := runBlockedMutation("cancel", func(ctx context.Context) (*domain.SubscriptionQuotaMutationResult, error) {
		return f.api.CancelTerm(ctx, a.Term.ID, f.admin, service.SubscriptionQuotaCancelInput{ExpectedRevision: quotaDomainInt(2), Reason: "synthetic concurrent group deletion probe"}, uuid.NewString())
	})
	require.Equal(t, "cancelled", cancelled.Term.Status)
	require.EqualValues(t, 3, cancelled.Summary.Revision)
	f.assertFinancialIsolation(t)
}
