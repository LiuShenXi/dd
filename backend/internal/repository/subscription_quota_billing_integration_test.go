//go:build integration

package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io/fs"
	"math"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

// All records come from a fresh database on the original synthetic TestMain
// PostgreSQL container. No configured application DSN or upstream is consulted.
type subscriptionQuotaBillingFixture struct {
	rights                     *subscriptionQuotaDomainFixture
	db                         *sql.DB
	port                       service.SubscriptionQuotaBillingRepository
	account, keyA, keyA2, keyB int64
}

func newSubscriptionQuotaBillingFixture(t *testing.T) *subscriptionQuotaBillingFixture {
	t.Helper()
	rights := newSubscriptionQuotaDomainFixture(t)
	port, ok := rights.repo.(service.SubscriptionQuotaBillingRepository)
	require.True(t, ok, "the real quota repository must expose the I3 billing port")
	f := &subscriptionQuotaBillingFixture{rights: rights, db: rights.db, port: port}
	require.NoError(t, f.db.QueryRow(`SELECT min(id),max(id) FROM api_keys WHERE user_id=$1 AND group_id=$2`, rights.userA, rights.group).Scan(&f.keyA, &f.keyA2))
	require.NotEqual(t, f.keyA, f.keyA2)
	require.NoError(t, f.db.QueryRow(`INSERT INTO api_keys(user_id,group_id,key,name,quota) VALUES($1,$2,'synthetic-i3-user-b-no-upstream','Synthetic I3 user B',1000000) RETURNING id`, rights.userB, rights.group).Scan(&f.keyB))
	_, err := f.db.Exec(`UPDATE api_keys SET quota=1000000,rate_limit_5h=1000000,rate_limit_1d=1000000,rate_limit_7d=1000000 WHERE id IN ($1,$2,$3)`, f.keyA, f.keyA2, f.keyB)
	require.NoError(t, err)
	require.NoError(t, f.db.QueryRow(`INSERT INTO accounts(name,platform,type,credentials,extra,status) VALUES('Synthetic I3 account no upstream','anthropic','apikey','{}','{"quota_limit":1000000,"quota_used":0,"quota_daily_limit":1000000,"quota_daily_used":0,"quota_weekly_limit":1000000,"quota_weekly_used":0}','active') RETURNING id`).Scan(&f.account))
	_, err = f.db.Exec(`INSERT INTO account_groups(account_id,group_id) VALUES($1,$2),($1,$3)`, f.account, rights.group, rights.otherGroup)
	require.NoError(t, err)
	return f
}

func (f *subscriptionQuotaBillingFixture) command(snapshot *domain.SubscriptionQuotaBillingSnapshot, cost, accountCost string) *service.UsageBillingCommand {
	copy := snapshot.Clone()
	amount := decimal.RequireFromString(cost)
	cmd := &service.UsageBillingCommand{RequestID: copy.RequestID, UserID: copy.UserID, APIKeyID: copy.APIKeyID, AccountID: copy.AccountID, AccountType: copy.AccountType, BillingType: service.BillingTypeSubscription, Model: "synthetic-claude-no-upstream", InputTokens: 10, OutputTokens: 5, RequestPayloadHash: strings.Repeat("a", 64), SubscriptionTermSnapshot: &copy, SubscriptionTermCost: amount, SubscriptionTermCounters: &domain.SubscriptionQuotaCounterCosts{}}
	if copy.KeyQuotaTracked {
		cmd.SubscriptionTermCounters.APIKeyQuotaCost = amount
	}
	if copy.KeyRateLimitTracked {
		cmd.SubscriptionTermCounters.APIKeyRateLimitCost = amount
	}
	if copy.AccountQuotaTracked {
		cmd.SubscriptionTermCounters.AccountQuotaCost = decimal.RequireFromString(accountCost)
	}
	return cmd
}

func (f *subscriptionQuotaBillingFixture) applier() service.UsageBillingRepository {
	return ProvideUsageBillingRepository(nil, f.db, NewCarpoolRepository(f.db), f.rights.repo)
}

func (f *subscriptionQuotaBillingFixture) known(t *testing.T, snapshot *domain.SubscriptionQuotaBillingSnapshot, cost, accountCost string) (domain.SubscriptionQuotaKnownUsage, *service.UsageBillingCommand) {
	return f.knownWithLog(t, snapshot, cost, accountCost, nil)
}

func (f *subscriptionQuotaBillingFixture) knownWithLog(t *testing.T, snapshot *domain.SubscriptionQuotaBillingSnapshot, cost, accountCost string, editLog func(*service.UsageLog)) (domain.SubscriptionQuotaKnownUsage, *service.UsageBillingCommand) {
	t.Helper()
	command := f.command(snapshot, cost, accountCost)
	payload, err := service.MarshalSubscriptionQuotaUsageBillingReceipt(command)
	require.NoError(t, err)
	command, err = service.DecodeSubscriptionQuotaUsageBillingReceipt(payload)
	require.NoError(t, err)
	actual := command.SubscriptionTermCost.InexactFloat64()
	account := decimal.RequireFromString(accountCost).InexactFloat64()
	exactAccount := decimal.RequireFromString(accountCost)
	one := 1.0
	log := &service.UsageLog{RequestID: snapshot.RequestID, UserID: snapshot.UserID, APIKeyID: snapshot.APIKeyID, AccountID: snapshot.AccountID, GroupID: quotaDomainInt(snapshot.GroupID), SubscriptionID: quotaDomainInt(snapshot.SubscriptionID), SubscriptionTermID: quotaDomainInt(snapshot.TermID), SubscriptionQuotaRequestID: quotaDomainInt(snapshot.RequestRowID), Model: command.Model, RequestedModel: command.Model, InputTokens: command.InputTokens, OutputTokens: command.OutputTokens, InputCost: actual, TotalCost: actual, ActualCost: actual, RateMultiplier: 1, AccountRateMultiplier: &one, AccountStatsCost: &account, BillingType: service.BillingTypeSubscription, CreatedAt: snapshot.PricingAt}
	log.SubscriptionQuotaAccountStatsCost = &exactAccount
	if editLog != nil {
		editLog(log)
	}
	usage, err := service.MarshalSubscriptionQuotaUsageLogPayloadForCommand(log, command)
	require.NoError(t, err)
	known := domain.SubscriptionQuotaKnownUsage{Snapshot: snapshot.Clone(), ActualCostUSD: cost, ReceiptVersion: service.SubscriptionQuotaUsageBillingReceiptVersion, RequestFingerprint: command.RequestFingerprint, BillingPayload: payload, UsageLogPayload: usage}
	require.NoError(t, f.db.QueryRow(`SELECT revision FROM subscription_quota_requests WHERE id=$1`, snapshot.RequestRowID).Scan(&known.RequestRevision))
	return known, command
}

func (f *subscriptionQuotaBillingFixture) persist(t *testing.T, snapshot *domain.SubscriptionQuotaBillingSnapshot, cost, accountCost string) (domain.SubscriptionQuotaKnownUsage, *service.UsageBillingCommand) {
	t.Helper()
	known, command := f.known(t, snapshot, cost, accountCost)
	require.NoError(t, f.port.PersistKnownUsage(context.Background(), known))
	return known, command
}

func (f *subscriptionQuotaBillingFixture) requireCharge(t *testing.T, snapshot *domain.SubscriptionQuotaBillingSnapshot, cost, accountCost string, ledgerRows int) {
	t.Helper()
	amounts := f.amounts(t, snapshot)
	require.Equal(t, "settled", amounts.Status)
	for _, amount := range []string{amounts.Used, amounts.DebitSum, amounts.RequestCost, amounts.KeyQuota, amounts.Key5H, amounts.Key1D, amounts.Key7D} {
		require.Equal(t, cost, amount)
	}
	for _, amount := range []string{amounts.AccountQuota, amounts.AccountDaily, amounts.AccountWeekly} {
		require.Equal(t, accountCost, amount)
	}
	require.Equal(t, ledgerRows, amounts.LedgerRows)
	require.Equal(t, 1, amounts.DedupRows)
	data, err := json.Marshal(amounts)
	require.NoError(t, err)
	t.Logf("I3 real PG stored deltas request_row_id=%d term_id=%d key_id=%d account_id=%d: %s", snapshot.RequestRowID, snapshot.TermID, snapshot.APIKeyID, snapshot.AccountID, data)
}

func (f *subscriptionQuotaBillingFixture) admission(user, key int64) domain.SubscriptionQuotaAdmissionParams {
	return domain.SubscriptionQuotaAdmissionParams{
		UserID: user, APIKeyID: key, GroupID: f.rights.group, AccountID: f.account, ExpectedAccountType: service.AccountTypeAPIKey,
		RequestID: service.NewSubscriptionQuotaAdmissionRequestID(),
		FreezePricing: func(at time.Time) (json.RawMessage, error) {
			// This is a repository durability fixture, not an alternate model
			// price resolver. Real gateway pricing is tested by its owner.
			return json.Marshal(struct {
				Version          int       `json:"version"`
				Kind             string    `json:"kind"`
				PricingAt        time.Time `json:"pricing_at"`
				FrozenMultiplier string    `json:"frozen_multiplier"`
			}{1, "synthetic-repository-frozen-pricing", at, "1.25000000"})
		},
	}
}

func (f *subscriptionQuotaBillingFixture) admit(t *testing.T, user, key int64) *domain.SubscriptionQuotaBillingSnapshot {
	t.Helper()
	snapshot, err := f.port.AdmitRequest(context.Background(), f.admission(user, key))
	require.NoError(t, err)
	require.NotNil(t, snapshot)
	require.Positive(t, snapshot.RequestRowID)
	require.Equal(t, user, snapshot.UserID)
	require.Equal(t, key, snapshot.APIKeyID)
	require.Equal(t, f.rights.group, snapshot.GroupID)
	require.Equal(t, f.account, snapshot.AccountID)
	require.True(t, strings.HasPrefix(snapshot.RequestID, "subquota:"))
	require.True(t, snapshot.AdmittedAt.Equal(snapshot.PricingAt))
	require.NotEmpty(t, snapshot.PricingSnapshotHash)
	return snapshot
}

// Capture immediately before a Claude billing action. Tests that intentionally
// open/cancel a card or perform a real GPT charge use separate checkpoints.
func subscriptionQuotaBillingMoneyGuard(t *testing.T, db *sql.DB) func() {
	t.Helper()
	columns := make(map[string][]string)
	for table, names := range subscriptionQuotaLegacyColumns(t, db) {
		if table == "users" || table == "user_subscriptions" || table == "redeem_codes" || strings.HasPrefix(table, "carpool_") || strings.HasPrefix(table, "payment_") {
			columns[table] = names
		}
	}
	before := subscriptionQuotaLegacyFingerprints(t, db, columns)
	return func() {
		t.Helper()
		require.Equal(t, before, subscriptionQuotaLegacyFingerprints(t, db, columns), "Claude billing must leave ordinary balances/frozen balances, native windows, GPT terms/cycles/ledger/receipts/reset/boost/payment data unchanged")
	}
}

// Permit effects only on the admitted identity. Keeping full foreign rows in
// the fingerprint protects user B, the next card and other in-flight requests;
// excluding an entire quota or statistics table would not provide that proof.
func (f *subscriptionQuotaBillingFixture) ownershipGuard(t *testing.T, snapshots ...*domain.SubscriptionQuotaBillingSnapshot) func() {
	t.Helper()
	require.NotEmpty(t, snapshots)
	var terms, requests, keys []int64
	for _, s := range snapshots {
		terms = append(terms, s.TermID)
		requests = append(requests, s.RequestRowID)
		keys = append(keys, s.APIKeyID)
	}
	type check struct {
		query string
		args  []any
	}
	checks := map[string]check{
		"foreign_terms":            {`SELECT * FROM subscription_quota_terms WHERE NOT(id=ANY($1))`, []any{pq.Array(terms)}},
		"foreign_requests":         {`SELECT * FROM subscription_quota_requests WHERE NOT(id=ANY($1))`, []any{pq.Array(requests)}},
		"foreign_ledger":           {`SELECT * FROM subscription_quota_ledger WHERE NOT(term_id=ANY($1) AND request_row_id=ANY($2))`, []any{pq.Array(terms), pq.Array(requests)}},
		"foreign_usage_logs":       {`SELECT l.* FROM usage_logs l WHERE NOT(l.subscription_quota_request_id=ANY($1) AND l.account_id=$2 AND EXISTS(SELECT 1 FROM subscription_quota_requests r WHERE r.id=l.subscription_quota_request_id AND r.term_id=l.subscription_term_id AND r.api_key_id=l.api_key_id AND r.user_id=l.user_id AND r.group_id=l.group_id AND r.request_id=l.request_id))`, []any{pq.Array(requests), f.account}},
		"foreign_keys":             {`SELECT * FROM api_keys WHERE NOT(id=ANY($1))`, []any{pq.Array(keys)}},
		"foreign_accounts":         {`SELECT * FROM accounts WHERE id<>$1`, []any{f.account}},
		"foreign_auth_outbox":      {`SELECT * FROM auth_cache_invalidation_outbox WHERE cache_key NOT IN (SELECT encode(sha256(convert_to(key,'UTF8')),'hex') FROM api_keys WHERE id=ANY($1))`, []any{pq.Array(keys)}},
		"foreign_scheduler_outbox": {`SELECT * FROM scheduler_outbox WHERE account_id IS DISTINCT FROM $1 OR group_id IS NOT NULL OR event_type NOT IN ($2,$3)`, []any{f.account, service.SchedulerOutboxEventAccountLastUsed, service.SchedulerOutboxEventAccountChanged}},
		"term_identity":            {`SELECT to_jsonb(t)-'used_usd'-'revision'-'lifecycle_state'-'closed_at'-'updated_at' AS identity FROM subscription_quota_terms t`, nil},
		"key_identity":             {`SELECT to_jsonb(k)-'quota_used'-'usage_5h'-'usage_1d'-'usage_7d'-'window_5h_start'-'window_1d_start'-'window_7d_start'-'status'-'updated_at' AS identity FROM api_keys k`, nil},
		"account_identity":         {`SELECT (to_jsonb(a)-'extra'-'updated_at'-'last_used_at') || jsonb_build_object('extra',a.extra-'quota_used'-'quota_daily_used'-'quota_weekly_used'-'quota_daily_start'-'quota_weekly_start'-'quota_daily_reset_at'-'quota_weekly_reset_at') AS identity FROM accounts a WHERE id=$1`, []any{f.account}},
	}
	capture := func() map[string]string {
		values := make(map[string]string, len(checks))
		for name, check := range checks {
			var hash string
			require.NoError(t, f.db.QueryRow(`SELECT md5(COALESCE(jsonb_agg(to_jsonb(x) ORDER BY to_jsonb(x)::text)::text,'[]')) FROM (`+check.query+`) x`, check.args...).Scan(&hash), name)
			values[name] = hash
		}
		return values
	}
	before := capture()
	var lastUsedBefore sql.NullTime
	require.NoError(t, f.db.QueryRow(`SELECT last_used_at FROM accounts WHERE id=$1`, f.account).Scan(&lastUsedBefore))
	type keyState struct{ status, quota, used string }
	keyBefore := make(map[int64]keyState)
	for _, key := range keys {
		var state keyState
		require.NoError(t, f.db.QueryRow(`SELECT status,quota::text,quota_used::text FROM api_keys WHERE id=$1`, key).Scan(&state.status, &state.quota, &state.used))
		keyBefore[key] = state
	}
	money := subscriptionQuotaBillingMoneyGuard(t, f.db)
	return func() {
		t.Helper()
		require.Equal(t, before, capture(), "only the exact admitted term/request/ledger/log and its Key/account counters may change; foreign cards and identities remain byte-exact")
		for key, initial := range keyBefore {
			var status, used string
			require.NoError(t, f.db.QueryRow(`SELECT status,quota_used::text FROM api_keys WHERE id=$1`, key).Scan(&status, &used))
			expected := initial.status
			quota := decimal.RequireFromString(initial.quota)
			if expected == service.StatusAPIKeyActive && quota.IsPositive() && decimal.RequireFromString(initial.used).LessThan(quota) && !decimal.RequireFromString(used).LessThan(quota) {
				expected = service.StatusAPIKeyQuotaExhausted
			}
			require.Equal(t, expected, status, "only the admitted Key may make its documented active-to-quota-exhausted transition; disabled/deleted entities must not be revived")
		}
		expectedLastUsed := lastUsedBefore
		for _, snapshot := range snapshots {
			var settled bool
			require.NoError(t, f.db.QueryRow(`SELECT status='settled' FROM subscription_quota_requests WHERE id=$1`, snapshot.RequestRowID).Scan(&settled))
			if settled && (!expectedLastUsed.Valid || snapshot.AdmittedAt.After(expectedLastUsed.Time)) {
				expectedLastUsed = sql.NullTime{Time: snapshot.AdmittedAt, Valid: true}
			}
		}
		var lastUsedAfter sql.NullTime
		require.NoError(t, f.db.QueryRow(`SELECT last_used_at FROM accounts WHERE id=$1`, f.account).Scan(&lastUsedAfter))
		require.Equal(t, expectedLastUsed.Valid, lastUsedAfter.Valid)
		if expectedLastUsed.Valid {
			require.True(t, expectedLastUsed.Time.Equal(lastUsedAfter.Time), "only the original account's monotonic admitted-at publication may advance its last-used time")
		}
		money()
		t.Logf("I3 row ownership verified: term_ids=%v request_row_ids=%v key_ids=%v account_id=%d; all foreign-row, identity and other-wallet fingerprints unchanged", terms, requests, keys, f.account)
	}
}

type subscriptionQuotaBillingStoredAmounts struct {
	Used, DebitSum, RequestCost               string
	KeyQuota, Key5H, Key1D, Key7D             string
	AccountQuota, AccountDaily, AccountWeekly string
	LedgerRows, DedupRows, LogRows            int
	Status                                    string
	ParentRevision, TermRevision              int64
}

func (f *subscriptionQuotaBillingFixture) amounts(t *testing.T, snapshot *domain.SubscriptionQuotaBillingSnapshot) subscriptionQuotaBillingStoredAmounts {
	t.Helper()
	var a subscriptionQuotaBillingStoredAmounts
	require.NoError(t, f.db.QueryRow(`SELECT t.used_usd::text,t.revision,p.quota_revision FROM subscription_quota_terms t JOIN user_subscriptions p ON p.id=t.subscription_id WHERE t.id=$1`, snapshot.TermID).Scan(&a.Used, &a.TermRevision, &a.ParentRevision))
	require.NoError(t, f.db.QueryRow(`SELECT COALESCE(sum(delta_used_usd),0)::numeric(20,8)::text,count(*) FROM subscription_quota_ledger WHERE term_id=$1 AND entry_kind='usage_debit'`, snapshot.TermID).Scan(&a.DebitSum, &a.LedgerRows))
	require.NoError(t, f.db.QueryRow(`SELECT COALESCE(actual_cost_usd::text,'NULL'),status FROM subscription_quota_requests WHERE id=$1`, snapshot.RequestRowID).Scan(&a.RequestCost, &a.Status))
	require.NoError(t, f.db.QueryRow(`SELECT quota_used::numeric(20,8)::text,usage_5h::numeric(20,8)::text,usage_1d::numeric(20,8)::text,usage_7d::numeric(20,8)::text FROM api_keys WHERE id=$1`, snapshot.APIKeyID).Scan(&a.KeyQuota, &a.Key5H, &a.Key1D, &a.Key7D))
	require.NoError(t, f.db.QueryRow(`SELECT COALESCE((extra->>'quota_used')::numeric,0)::numeric(20,8)::text,COALESCE((extra->>'quota_daily_used')::numeric,0)::numeric(20,8)::text,COALESCE((extra->>'quota_weekly_used')::numeric,0)::numeric(20,8)::text FROM accounts WHERE id=$1`, f.account).Scan(&a.AccountQuota, &a.AccountDaily, &a.AccountWeekly))
	require.NoError(t, f.db.QueryRow(`SELECT count(*) FROM usage_billing_dedup WHERE request_id=$1 AND api_key_id=$2`, snapshot.RequestID, snapshot.APIKeyID).Scan(&a.DedupRows))
	require.NoError(t, f.db.QueryRow(`SELECT count(*) FROM usage_logs WHERE request_id=$1 AND api_key_id=$2`, snapshot.RequestID, snapshot.APIKeyID).Scan(&a.LogRows))
	return a
}

func TestSubscriptionQuotaBilling_ServerIdentitiesAndSharedCardScope(t *testing.T) {
	f := newSubscriptionQuotaBillingFixture(t)
	a := f.rights.open(t, f.rights.userA, f.rights.input("open", "100", 0))
	b := f.rights.open(t, f.rights.userB, f.rights.input("open", "300", 0))
	unchanged := subscriptionQuotaBillingMoneyGuard(t, f.db)
	first := f.admit(t, f.rights.userA, f.keyA)
	second := f.admit(t, f.rights.userA, f.keyA2)
	other := f.admit(t, f.rights.userB, f.keyB)
	require.Equal(t, a.Term.ID, first.TermID)
	require.Equal(t, first.TermID, second.TermID, "both Keys bind the same user's card rather than receiving separate allowances")
	require.Equal(t, b.Term.ID, other.TermID)
	require.NotEqual(t, first.TermID, other.TermID)
	require.NotEqual(t, first.RequestID, second.RequestID, "the actual production server identity generator creates independent consumption IDs")
	require.NotEqual(t, first.RequestRowID, second.RequestRowID)
	for _, snapshot := range []*domain.SubscriptionQuotaBillingSnapshot{first, second, other} {
		amounts := f.amounts(t, snapshot)
		require.Equal(t, "admitted", amounts.Status)
		require.Equal(t, "NULL", amounts.RequestCost, "unknown usage is SQL NULL, not a zero fee")
		require.Equal(t, "0.00000000", amounts.Used)
		require.Zero(t, amounts.LedgerRows)
		require.Zero(t, amounts.DedupRows)
		require.Zero(t, amounts.LogRows)
	}
	ctx := service.ContextWithSubscriptionQuotaBillingSnapshot(context.Background(), first)
	want := append(json.RawMessage(nil), first.PricingSnapshot...)
	first.PricingSnapshot[0] = '['
	from, ok := service.SubscriptionQuotaBillingSnapshotFromContext(ctx)
	require.True(t, ok)
	require.JSONEq(t, string(want), string(from.PricingSnapshot))
	from.PricingSnapshot[0] = '['
	again, ok := service.SubscriptionQuotaBillingSnapshotFromContext(ctx)
	require.True(t, ok)
	require.JSONEq(t, string(want), string(again.PricingSnapshot), "context admission snapshots must copy mutable raw JSON on both write and read")
	unchanged()
}

func TestSubscriptionQuotaBilling_UnknownUsagePausesCurrentCardWithoutZeroCharge(t *testing.T) {
	f := newSubscriptionQuotaBillingFixture(t)
	f.rights.open(t, f.rights.userA, f.rights.input("open", "100", 0))
	snapshot := f.admit(t, f.rights.userA, f.keyA)
	unchanged := subscriptionQuotaBillingMoneyGuard(t, f.db)
	require.NoError(t, f.port.MarkReconcileRequired(context.Background(), snapshot, "synthetic_usage_missing_after_client_disconnect"))
	amounts := f.amounts(t, snapshot)
	require.Equal(t, "reconcile_required", amounts.Status)
	require.Equal(t, "NULL", amounts.RequestCost)
	require.Equal(t, "0.00000000", amounts.Used)
	require.Zero(t, amounts.LedgerRows)
	require.Zero(t, amounts.LogRows)
	_, err := f.port.AdmitRequest(context.Background(), f.admission(f.rights.userA, f.keyA2))
	require.Error(t, err, "an unresolved current-card request must pause new admission for both Keys")
	var requests int
	require.NoError(t, f.db.QueryRow(`SELECT count(*) FROM subscription_quota_requests WHERE user_id=$1 AND group_id=$2`, f.rights.userA, f.rights.group).Scan(&requests))
	require.Equal(t, 1, requests, "denial must not append a billable request")
	unchanged()
}

// Insert synthetic immutable cards around a real database-clock boundary. Never
// rewrite issued timestamps to move a real card through time.
func (f *subscriptionQuotaBillingFixture) boundaryCards(t *testing.T, expiresIn time.Duration) (parent, oldTerm, nextTerm int64, boundary time.Time) {
	t.Helper()
	var now time.Time
	require.NoError(t, f.db.QueryRow(`SELECT clock_timestamp()`).Scan(&now))
	boundary = now.Add(expiresIn).UTC().Truncate(time.Microsecond)
	starts := boundary.Add(-domain.SubscriptionQuotaDuration)
	require.NoError(t, f.db.QueryRow(`INSERT INTO user_subscriptions(user_id,group_id,starts_at,expires_at,status,quota_revision,assigned_by) VALUES($1,$2,$3,$4,'active',2,$5) RETURNING id`, f.rights.userA, f.rights.group, starts, boundary.Add(domain.SubscriptionQuotaDuration), f.rights.admin).Scan(&parent))
	data, err := json.Marshal(domain.SubscriptionQuotaSourceSnapshot{SchemaVersion: 1, QuotaUSD: "100.00000000", DurationHours: 168, QuotaSource: "admin"})
	require.NoError(t, err)
	for _, card := range []struct {
		id     *int64
		no     int
		starts time.Time
		state  string
	}{{&oldTerm, 1, starts, "active"}, {&nextTerm, 2, boundary, "scheduled"}} {
		require.NoError(t, f.db.QueryRow(`INSERT INTO subscription_quota_terms(subscription_id,user_id,group_id,term_no,quota_usd,starts_at,expires_at,lifecycle_state,source_kind,source_snapshot,created_by) VALUES($1,$2,$3,$4,100,$5,$6,$7,'admin',$8,$9) RETURNING id`, parent, f.rights.userA, f.rights.group, card.no, card.starts, card.starts.Add(domain.SubscriptionQuotaDuration), card.state, data, f.rights.admin).Scan(card.id))
	}
	return
}

func subscriptionQuotaBillingWaitForClock(t *testing.T, db *sql.DB, boundary time.Time) {
	t.Helper()
	require.Eventually(t, func() bool {
		var reached bool
		err := db.QueryRow(`SELECT clock_timestamp() >= $1`, boundary).Scan(&reached)
		return err == nil && reached
	}, 5*time.Second, 10*time.Millisecond, "the synthetic card boundary must be crossed by the actual PG clock")
}

func TestSubscriptionQuotaBilling_OldUnknownDoesNotPauseNextCard(t *testing.T) {
	f := newSubscriptionQuotaBillingFixture(t)
	_, oldTerm, nextTerm, boundary := f.boundaryCards(t, 2*time.Second)
	unchanged := subscriptionQuotaBillingMoneyGuard(t, f.db)
	old := f.admit(t, f.rights.userA, f.keyA)
	require.Equal(t, oldTerm, old.TermID)
	require.NoError(t, f.port.MarkReconcileRequired(context.Background(), old, "synthetic_old_card_missing_usage"))
	_, err := f.port.AdmitRequest(context.Background(), f.admission(f.rights.userA, f.keyA2))
	require.Error(t, err)
	subscriptionQuotaBillingWaitForClock(t, f.db, boundary)
	next := f.admit(t, f.rights.userA, f.keyA2)
	require.Equal(t, nextTerm, next.TermID)
	require.False(t, next.AdmittedAt.Before(boundary))
	oldAmounts := f.amounts(t, old)
	nextAmounts := f.amounts(t, next)
	require.Equal(t, "reconcile_required", oldAmounts.Status)
	require.Equal(t, "NULL", oldAmounts.RequestCost)
	require.Equal(t, "0.00000000", oldAmounts.Used)
	require.Equal(t, "admitted", nextAmounts.Status)
	require.Equal(t, "0.00000000", nextAmounts.Used)
	unchanged()
}

func TestSubscriptionQuotaBilling_AdmissionWaitCrossesExpiryAndSelectsNextCard(t *testing.T) {
	f := newSubscriptionQuotaBillingFixture(t)
	_, oldTerm, nextTerm, boundary := f.boundaryCards(t, 2*time.Second)
	unchanged := subscriptionQuotaBillingMoneyGuard(t, f.db)
	blocker, err := f.db.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = blocker.Rollback() })
	var id int64
	require.NoError(t, blocker.QueryRow(`SELECT id FROM subscription_quota_terms WHERE id=$1 FOR UPDATE`, oldTerm).Scan(&id))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	type outcome struct {
		snapshot *domain.SubscriptionQuotaBillingSnapshot
		err      error
	}
	results := make(chan outcome, 1)
	go func() {
		snapshot, err := f.port.AdmitRequest(ctx, f.admission(f.rights.userA, f.keyA))
		results <- outcome{snapshot, err}
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		err := f.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND state='active' AND wait_event_type='Lock' AND query LIKE '%subscription_quota_terms%FOR UPDATE%')`).Scan(&waiting)
		return err == nil && waiting
	}, time.Second, 10*time.Millisecond, "actual admission must wait on the originally selected term")
	subscriptionQuotaBillingWaitForClock(t, f.db, boundary)
	require.NoError(t, blocker.Rollback())
	select {
	case finished := <-results:
		require.NoError(t, finished.err)
		require.NotNil(t, finished.snapshot)
		require.Equal(t, nextTerm, finished.snapshot.TermID, "fresh admission time must not be attached to the expired selected card")
		require.False(t, finished.snapshot.AdmittedAt.Before(boundary))
		require.True(t, finished.snapshot.AdmittedAt.Equal(finished.snapshot.PricingAt))
		var oldRequests, nextRequests int
		require.NoError(t, f.db.QueryRow(`SELECT count(*) FILTER(WHERE term_id=$1),count(*) FILTER(WHERE term_id=$2) FROM subscription_quota_requests`, oldTerm, nextTerm).Scan(&oldRequests, &nextRequests))
		require.Zero(t, oldRequests)
		require.Equal(t, 1, nextRequests)
	case <-time.After(3 * time.Second):
		t.Fatal("admission did not finish after the original term lock was released")
	}
	unchanged()
}

func TestSubscriptionQuotaBilling_PartialMixedAndInvalidSourcesWriteNothing(t *testing.T) {
	f := newSubscriptionQuotaBillingFixture(t)
	f.rights.open(t, f.rights.userA, f.rights.input("open", "100", 0))
	snapshot := f.admit(t, f.rights.userA, f.keyA)
	columns := subscriptionQuotaLegacyColumns(t, f.db)
	before := subscriptionQuotaLegacyFingerprints(t, f.db, columns)
	for _, test := range []struct {
		name   string
		change func(*service.UsageBillingCommand)
	}{
		{"balance_positive", func(c *service.UsageBillingCommand) { c.BalanceCost = 1 }},
		{"balance_negative", func(c *service.UsageBillingCommand) { c.BalanceCost = -1 }},
		{"balance_nan", func(c *service.UsageBillingCommand) { c.BalanceCost = math.NaN() }},
		{"balance_inf", func(c *service.UsageBillingCommand) { c.BalanceCost = math.Inf(1) }},
		{"native_cost", func(c *service.UsageBillingCommand) { c.SubscriptionCost = 1 }},
		{"native_negative", func(c *service.UsageBillingCommand) { c.SubscriptionCost = -1 }},
		{"native_identity_zero_cost", func(c *service.UsageBillingCommand) {
			c.SubscriptionID = quotaDomainInt(snapshot.SubscriptionID)
			c.SubscriptionTermCost = decimal.Zero
			c.APIKeyQuotaCost = 0
			c.APIKeyRateLimitCost = 0
			c.SubscriptionTermCounters = &domain.SubscriptionQuotaCounterCosts{}
		}},
		{"carpool_cost_without_identity", func(c *service.UsageBillingCommand) { c.CarpoolCost = decimal.NewFromInt(1) }},
		{"carpool_negative", func(c *service.UsageBillingCommand) { c.CarpoolCost = decimal.NewFromInt(-1) }},
		{"carpool_identity_zero_cost", func(c *service.UsageBillingCommand) {
			c.CarpoolSnapshot = &domain.CarpoolBillingSnapshot{}
			c.SubscriptionTermCost = decimal.Zero
			c.APIKeyQuotaCost = 0
			c.APIKeyRateLimitCost = 0
			c.SubscriptionTermCounters = &domain.SubscriptionQuotaCounterCosts{}
		}},
		{"missing_term_snapshot", func(c *service.UsageBillingCommand) { c.SubscriptionTermSnapshot = nil }},
		{"missing_term_id", func(c *service.UsageBillingCommand) { c.SubscriptionTermSnapshot.TermID = 0 }},
		{"wrong_user", func(c *service.UsageBillingCommand) { c.SubscriptionTermSnapshot.UserID = f.rights.userB }},
		{"wrong_key", func(c *service.UsageBillingCommand) { c.SubscriptionTermSnapshot.APIKeyID = f.keyA2 }},
		{"wrong_account", func(c *service.UsageBillingCommand) { c.AccountID++ }},
		{"negative_term_cost", func(c *service.UsageBillingCommand) { c.SubscriptionTermCost = decimal.NewFromInt(-1) }},
		{"excess_term_precision", func(c *service.UsageBillingCommand) {
			c.SubscriptionTermCost = decimal.RequireFromString("0.000078125")
		}},
		{"term_overflow", func(c *service.UsageBillingCommand) {
			c.SubscriptionTermCost = decimal.RequireFromString("1000000000000")
		}},
		{"key_nan", func(c *service.UsageBillingCommand) { c.APIKeyQuotaCost = math.NaN() }},
		{"account_inf", func(c *service.UsageBillingCommand) { c.AccountQuotaCost = math.Inf(1) }},
		{"account_negative", func(c *service.UsageBillingCommand) { c.AccountQuotaCost = -1 }},
		{"account_excess_precision", func(c *service.UsageBillingCommand) { c.AccountQuotaCost = 0.000000001 }},
		{"missing_exact_counters", func(c *service.UsageBillingCommand) { c.SubscriptionTermCounters = nil }},
		{"exact_counter_negative", func(c *service.UsageBillingCommand) {
			c.SubscriptionTermCounters.AccountQuotaCost = decimal.NewFromInt(-1)
		}},
		{"exact_counter_excess_precision", func(c *service.UsageBillingCommand) {
			c.SubscriptionTermCounters.AccountQuotaCost = decimal.RequireFromString("0.000000001")
		}},
		{"exact_counter_overflow", func(c *service.UsageBillingCommand) {
			c.SubscriptionTermCounters.AccountQuotaCost = decimal.RequireFromString("1000000000000")
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			cmd := f.command(snapshot, "1.00000000", "0.10000000")
			test.change(cmd)
			cmd.RequestFingerprint = ""
			require.Error(t, cmd.Validate())
			_, err := f.applier().Apply(context.Background(), cmd)
			require.Error(t, err)
		})
	}
	require.Equal(t, before, subscriptionQuotaLegacyFingerprints(t, f.db, columns), "invalid raw sources must be rejected before normalization can erase them and before any dedup or business row is written")
}

func TestSubscriptionQuotaBilling_PricingAndReceiptSurviveRealJSONBRoundTrip(t *testing.T) {
	f := newSubscriptionQuotaBillingFixture(t)
	f.rights.open(t, f.rights.userA, f.rights.input("open", "100", 0))
	_, err := f.db.Exec(`CREATE TABLE synthetic_i3_jsonb_receipts(id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,payload jsonb NOT NULL)`)
	require.NoError(t, err)
	var equivalentPricingHash string
	for _, test := range []struct{ name, rate, one string }{
		{"exponent_and_decimal", "1e-7", "1.0"},
		{"expanded_and_integer", "0.0000001", "1"},
		{"expanded_with_zeroes", "0.0000001000", "1.000"},
		{"uppercase_exponent", "1E-7", "1e0"},
	} {
		t.Run(test.name, func(t *testing.T) {
			pricing := strings.ReplaceAll(strings.ReplaceAll(`{"version":1,"input_token_rate":RATE,"multiplier":ONE,"nested":[RATE,{"rate":RATE,"one":ONE}]}`, "RATE", test.rate), "ONE", test.one)
			p := f.admission(f.rights.userA, f.keyA)
			p.FreezePricing = func(time.Time) (json.RawMessage, error) { return json.RawMessage(pricing), nil }
			snapshot, err := f.port.AdmitRequest(context.Background(), p)
			require.NoError(t, err)
			var storedEnvelope json.RawMessage
			require.NoError(t, f.db.QueryRow(`SELECT pricing_snapshot FROM subscription_quota_requests WHERE id=$1`, snapshot.RequestRowID).Scan(&storedEnvelope))
			restored := snapshot.Clone()
			require.NoError(t, subscriptionQuotaDecodePricingEnvelope(storedEnvelope, &restored))
			require.Equal(t, snapshot.PricingSnapshotHash, restored.PricingSnapshotHash, "the actual admitted pricing envelope hash survives PostgreSQL JSONB numeric normalization")
			if equivalentPricingHash == "" {
				equivalentPricingHash = snapshot.PricingSnapshotHash
			} else {
				require.Equal(t, equivalentPricingHash, snapshot.PricingSnapshotHash, "numeric spelling is not a distinct frozen price")
			}
			payload, err := service.MarshalSubscriptionQuotaUsageBillingReceipt(f.command(snapshot, "0.00007813", "0.00000003"))
			require.NoError(t, err)
			original, err := service.DecodeSubscriptionQuotaUsageBillingReceipt(payload)
			require.NoError(t, err)
			var storedReceipt json.RawMessage
			require.NoError(t, f.db.QueryRow(`INSERT INTO synthetic_i3_jsonb_receipts(payload) VALUES($1) RETURNING payload`, payload).Scan(&storedReceipt))
			roundTrip, err := service.DecodeSubscriptionQuotaUsageBillingReceipt(storedReceipt)
			require.NoError(t, err, "decode the actual JSONB INSERT RETURNING result, not an in-memory marshal of the same bytes")
			require.Equal(t, original.RequestFingerprint, roundTrip.RequestFingerprint)
			require.Equal(t, snapshot.PricingSnapshotHash, roundTrip.SubscriptionTermSnapshot.PricingSnapshotHash)
			require.Equal(t, snapshot.RequestRowID, roundTrip.SubscriptionTermSnapshot.RequestRowID)
			require.Equal(t, snapshot.TermID, roundTrip.SubscriptionTermSnapshot.TermID)
			require.Equal(t, snapshot.APIKeyID, roundTrip.APIKeyID)
			require.True(t, snapshot.AdmittedAt.Equal(roundTrip.SubscriptionTermSnapshot.AdmittedAt))
			require.Equal(t, "0.00007813", roundTrip.SubscriptionTermCost.StringFixed(8))
			t.Logf("actual PG JSONB preserved pricing_hash=%s request_fingerprint=%s rate=%s one=%s", snapshot.PricingSnapshotHash, roundTrip.RequestFingerprint, test.rate, test.one)
			guard := f.ownershipGuard(t, snapshot)
			known, _ := f.persist(t, snapshot, "0.00007813", "0.00000003")
			var storedKnown []byte
			require.NoError(t, f.db.QueryRow(`SELECT billing_payload FROM subscription_quota_requests WHERE id=$1`, snapshot.RequestRowID).Scan(&storedKnown))
			storedCommand, err := service.DecodeSubscriptionQuotaUsageBillingReceipt(storedKnown)
			require.NoError(t, err)
			require.Equal(t, known.RequestFingerprint, storedCommand.RequestFingerprint)
			require.NoError(t, f.recovery(time.Second, 10).RunOnce(context.Background()), "actual worker must claim, decode and settle the JSONB-normalized known receipt")
			require.Equal(t, "settled", f.amounts(t, snapshot).Status)
			require.Equal(t, 1, f.amounts(t, snapshot).LogRows)
			guard()
		})
	}
}

func TestSubscriptionQuotaBilling_KnownFirstConcurrentApplyAndExactPrecision(t *testing.T) {
	f := newSubscriptionQuotaBillingFixture(t)
	f.rights.open(t, f.rights.userA, f.rights.input("open", "100", 0))
	next := f.rights.open(t, f.rights.userA, f.rights.input("renew", "200", 1))
	f.rights.open(t, f.rights.userB, f.rights.input("open", "300", 0))
	snapshot := f.admit(t, f.rights.userA, f.keyA)
	guard := f.ownershipGuard(t, snapshot)
	// The existing pricing rule quantizes the half boundary once. Receipts
	// themselves accept only the resulting exact eight-place amount.
	cost := service.QuantizeUsageBillingAmount(0.000078125)
	require.Equal(t, "0.00007813", decimal.NewFromFloat(cost).StringFixed(8))
	known, _ := f.persist(t, snapshot, "0.00007813", "0.00000003")
	before := f.amounts(t, snapshot)
	require.Equal(t, "usage_known", before.Status)
	require.Equal(t, "0.00007813", before.RequestCost)
	require.Equal(t, "0.00000000", before.Used)
	require.Zero(t, before.LedgerRows)
	require.Zero(t, before.DedupRows)
	require.Zero(t, before.LogRows)
	require.EqualValues(t, 2, before.ParentRevision)
	type result struct {
		applied bool
		err     error
	}
	outcomes := make(chan result, 8)
	start := make(chan struct{})
	var workers sync.WaitGroup
	for i := 0; i < 8; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			cmd, err := service.DecodeSubscriptionQuotaUsageBillingReceipt(known.BillingPayload)
			if err != nil {
				outcomes <- result{err: err}
				return
			}
			applied, err := f.applier().Apply(context.Background(), cmd)
			if err != nil {
				outcomes <- result{err: err}
				return
			}
			outcomes <- result{applied: applied.Applied}
		}()
	}
	close(start)
	workers.Wait()
	close(outcomes)
	fresh := 0
	for outcome := range outcomes {
		require.NoError(t, outcome.err)
		if outcome.applied {
			fresh++
		}
	}
	require.Equal(t, 1, fresh)
	f.requireCharge(t, snapshot, "0.00007813", "0.00000003", 1)
	after := f.amounts(t, snapshot)
	require.EqualValues(t, 2, after.ParentRevision)
	require.EqualValues(t, 1, after.TermRevision)
	var nextUsed string
	require.NoError(t, f.db.QueryRow(`SELECT used_usd::text FROM subscription_quota_terms WHERE id=$1`, next.Term.ID).Scan(&nextUsed))
	require.Equal(t, "0.00000000", nextUsed)
	guard()
}

func TestSubscriptionQuotaBilling_ApplyFailureRollsBackButKnownReceiptSurvives(t *testing.T) {
	f := newSubscriptionQuotaBillingFixture(t)
	f.rights.open(t, f.rights.userA, f.rights.input("open", "100", 0))
	snapshot := f.admit(t, f.rights.userA, f.keyA)
	guard := f.ownershipGuard(t, snapshot)
	_, command := f.persist(t, snapshot, "1.23456789", "0.12345678")
	_, err := f.db.Exec(`CREATE FUNCTION synthetic_i3_fail_key_counter() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.quota_used IS DISTINCT FROM OLD.quota_used THEN RAISE EXCEPTION 'synthetic I3 key counter failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER synthetic_i3_fail_key_counter BEFORE UPDATE ON api_keys FOR EACH ROW EXECUTE FUNCTION synthetic_i3_fail_key_counter()`)
	require.NoError(t, err)
	_, err = f.applier().Apply(context.Background(), command)
	require.ErrorContains(t, err, "synthetic I3 key counter failure")
	a := f.amounts(t, snapshot)
	require.Equal(t, "usage_known", a.Status)
	require.Equal(t, "1.23456789", a.RequestCost)
	require.Equal(t, "0.00000000", a.Used)
	require.Zero(t, a.LedgerRows)
	require.Zero(t, a.DedupRows)
	require.Equal(t, "0.00000000", a.KeyQuota)
	require.Equal(t, "0.00000000", a.AccountQuota)
	_, err = f.db.Exec(`DROP TRIGGER synthetic_i3_fail_key_counter ON api_keys; DROP FUNCTION synthetic_i3_fail_key_counter()`)
	require.NoError(t, err)
	result, err := f.applier().Apply(context.Background(), command)
	require.NoError(t, err)
	require.True(t, result.Applied)
	f.requireCharge(t, snapshot, "1.23456789", "0.12345678", 1)
	guard()
}

func TestSubscriptionQuotaBilling_ExplicitZeroHasCompleteSettledFact(t *testing.T) {
	f := newSubscriptionQuotaBillingFixture(t)
	f.rights.open(t, f.rights.userA, f.rights.input("open", "100", 0))
	snapshot := f.admit(t, f.rights.userA, f.keyA)
	guard := f.ownershipGuard(t, snapshot)
	_, command := f.persist(t, snapshot, "0.00000000", "0.00000000")
	result, err := f.applier().Apply(context.Background(), command)
	require.NoError(t, err)
	require.True(t, result.Applied)
	f.requireCharge(t, snapshot, "0.00000000", "0.00000000", 1)
	guard()
}

func TestSubscriptionQuotaBilling_ArchivedDedupReplaysAndRejectsChangedFact(t *testing.T) {
	f := newSubscriptionQuotaBillingFixture(t)
	f.rights.open(t, f.rights.userA, f.rights.input("open", "100", 0))
	snapshot := f.admit(t, f.rights.userA, f.keyA)
	guard := f.ownershipGuard(t, snapshot)
	_, command := f.persist(t, snapshot, "1.00000000", "0.10000000")
	first, err := f.applier().Apply(context.Background(), command)
	require.NoError(t, err)
	require.True(t, first.Applied)
	_, err = f.db.Exec(`INSERT INTO usage_billing_dedup_archive(request_id,api_key_id,request_fingerprint,created_at) SELECT request_id,api_key_id,request_fingerprint,created_at FROM usage_billing_dedup WHERE request_id=$1 AND api_key_id=$2`, snapshot.RequestID, snapshot.APIKeyID)
	require.NoError(t, err)
	_, err = f.db.Exec(`DELETE FROM usage_billing_dedup WHERE request_id=$1 AND api_key_id=$2`, snapshot.RequestID, snapshot.APIKeyID)
	require.NoError(t, err)
	replay, err := f.applier().Apply(context.Background(), command)
	require.NoError(t, err)
	require.False(t, replay.Applied)
	f.requireCharge(t, snapshot, "1.00000000", "0.10000000", 1)
	_, err = f.db.Exec(`DELETE FROM usage_billing_dedup WHERE request_id=$1 AND api_key_id=$2`, snapshot.RequestID, snapshot.APIKeyID)
	require.NoError(t, err)
	columns := subscriptionQuotaLegacyColumns(t, f.db)
	before := subscriptionQuotaLegacyFingerprints(t, f.db, columns)
	changed := f.command(snapshot, "2.00000000", "0.20000000")
	_, err = f.applier().Apply(context.Background(), changed)
	require.ErrorIs(t, err, service.ErrUsageBillingRequestConflict)
	require.Equal(t, before, subscriptionQuotaLegacyFingerprints(t, f.db, columns), "archived-fingerprint conflict must roll back a newly attempted hot claim and all business writes")
	guard()
}

func TestSubscriptionQuotaBilling_KnownAfterCancellationStillDebitsOriginalCard(t *testing.T) {
	f := newSubscriptionQuotaBillingFixture(t)
	opened := f.rights.open(t, f.rights.userA, f.rights.input("open", "100", 0))
	next := f.rights.open(t, f.rights.userA, f.rights.input("renew", "200", 1))
	snapshot := f.admit(t, f.rights.userA, f.keyA)
	_, command := f.persist(t, snapshot, "0.50000000", "0.10000000")
	f.rights.cancel(t, opened.Term.ID, 2, service.NewSubscriptionQuotaAdmissionRequestID())
	guard := f.ownershipGuard(t, snapshot)
	result, err := f.applier().Apply(context.Background(), command)
	require.NoError(t, err)
	require.True(t, result.Applied)
	f.requireCharge(t, snapshot, "0.50000000", "0.10000000", 1)
	var used string
	require.NoError(t, f.db.QueryRow(`SELECT used_usd::text FROM subscription_quota_terms WHERE id=$1`, next.Term.ID).Scan(&used))
	require.Equal(t, "0.00000000", used)
	guard()
}

func TestSubscriptionQuotaBilling_KnownAfterExpiryStillDebitsOriginalCard(t *testing.T) {
	f := newSubscriptionQuotaBillingFixture(t)
	_, old, next, boundary := f.boundaryCards(t, 2*time.Second)
	snapshot := f.admit(t, f.rights.userA, f.keyA)
	require.Equal(t, old, snapshot.TermID)
	_, command := f.persist(t, snapshot, "0.50000000", "0.10000000")
	guard := f.ownershipGuard(t, snapshot)
	subscriptionQuotaBillingWaitForClock(t, f.db, boundary)
	result, err := f.applier().Apply(context.Background(), command)
	require.NoError(t, err)
	require.True(t, result.Applied)
	f.requireCharge(t, snapshot, "0.50000000", "0.10000000", 1)
	guard()
	newSnapshot := f.admit(t, f.rights.userA, f.keyA2)
	require.Equal(t, next, newSnapshot.TermID)
	require.Equal(t, "0.00000000", f.amounts(t, newSnapshot).Used)
}

func TestSubscriptionQuotaBilling_KnownSurvivesLaterDisabledOrDeletedEntities(t *testing.T) {
	for _, mode := range []string{"key_disabled", "key_softdeleted", "account_softdeleted"} {
		t.Run(mode, func(t *testing.T) {
			f := newSubscriptionQuotaBillingFixture(t)
			f.rights.open(t, f.rights.userA, f.rights.input("open", "100", 0))
			snapshot := f.admit(t, f.rights.userA, f.keyA)
			_, command := f.persist(t, snapshot, "0.50000000", "0.10000000")
			var err error
			switch mode {
			case "key_disabled":
				_, err = f.db.Exec(`UPDATE api_keys SET status='disabled' WHERE id=$1`, snapshot.APIKeyID)
			case "key_softdeleted":
				_, err = f.db.Exec(`UPDATE api_keys SET deleted_at=clock_timestamp() WHERE id=$1`, snapshot.APIKeyID)
			case "account_softdeleted":
				_, err = f.db.Exec(`UPDATE accounts SET deleted_at=clock_timestamp() WHERE id=$1`, snapshot.AccountID)
			}
			require.NoError(t, err)
			guard := f.ownershipGuard(t, snapshot)
			first, err := f.applier().Apply(context.Background(), command)
			require.NoError(t, err)
			require.True(t, first.Applied)
			replay, err := f.applier().Apply(context.Background(), command)
			require.NoError(t, err)
			require.False(t, replay.Applied)
			f.requireCharge(t, snapshot, "0.50000000", "0.10000000", 1)
			var remains bool
			switch mode {
			case "key_disabled":
				require.NoError(t, f.db.QueryRow(`SELECT status='disabled' FROM api_keys WHERE id=$1`, snapshot.APIKeyID).Scan(&remains))
			case "key_softdeleted":
				require.NoError(t, f.db.QueryRow(`SELECT deleted_at IS NOT NULL FROM api_keys WHERE id=$1`, snapshot.APIKeyID).Scan(&remains))
			case "account_softdeleted":
				require.NoError(t, f.db.QueryRow(`SELECT deleted_at IS NOT NULL FROM accounts WHERE id=$1`, snapshot.AccountID).Scan(&remains))
			}
			require.True(t, remains, "settlement must never resurrect an entity")
			if mode != "key_disabled" {
				tx, err := f.db.BeginTx(context.Background(), nil)
				require.NoError(t, err)
				if mode == "key_softdeleted" {
					_, err = incrementUsageBillingAPIKeyQuota(context.Background(), tx, snapshot.APIKeyID, 1)
					require.ErrorIs(t, err, service.ErrAPIKeyNotFound)
				} else {
					_, err = incrementUsageBillingAccountQuota(context.Background(), tx, snapshot.AccountID, 1)
					require.Error(t, err)
				}
				require.NoError(t, tx.Rollback())
			}
			guard()
		})
	}
}

func TestSubscriptionQuotaBilling_KnownReceiptConflictPreservesOriginalFact(t *testing.T) {
	f := newSubscriptionQuotaBillingFixture(t)
	f.rights.open(t, f.rights.userA, f.rights.input("open", "100", 0))
	snapshot := f.admit(t, f.rights.userA, f.keyA)
	guard := f.ownershipGuard(t, snapshot)
	first, _ := f.persist(t, snapshot, "1.00000000", "0.10000000")
	columns := subscriptionQuotaLegacyColumns(t, f.db)
	before := subscriptionQuotaLegacyFingerprints(t, f.db, columns)
	require.NoError(t, f.port.PersistKnownUsage(context.Background(), first))
	require.Equal(t, before, subscriptionQuotaLegacyFingerprints(t, f.db, columns), "same known receipt replay has no durable effect")
	changed, _ := f.known(t, snapshot, "2.00000000", "0.20000000")
	require.ErrorIs(t, f.port.PersistKnownUsage(context.Background(), changed), service.ErrSubscriptionQuotaReceiptConflict)
	var cost, status, fingerprint string
	var payload, usage json.RawMessage
	require.NoError(t, f.db.QueryRow(`SELECT actual_cost_usd::text,status,request_fingerprint,billing_payload,usage_log_payload FROM subscription_quota_requests WHERE id=$1`, snapshot.RequestRowID).Scan(&cost, &status, &fingerprint, &payload, &usage))
	require.Equal(t, "1.00000000", cost)
	require.Equal(t, "reconcile_required", status)
	require.Equal(t, first.RequestFingerprint, fingerprint)
	require.JSONEq(t, string(first.BillingPayload), string(payload))
	require.JSONEq(t, string(first.UsageLogPayload), string(usage))
	require.ErrorIs(t, f.port.MarkNoCharge(context.Background(), snapshot, domain.SubscriptionQuotaNoChargeEvidence{Kind: "upstream_rejected", Reference: "http:429"}), service.ErrSubscriptionQuotaReceiptConflict, "known facts cannot be replaced with no-charge evidence")
	_, err := f.port.AdmitRequest(context.Background(), f.admission(f.rights.userA, f.keyA2))
	require.ErrorIs(t, err, service.ErrSubscriptionQuotaReconciliation)
	a := f.amounts(t, snapshot)
	require.Equal(t, "0.00000000", a.Used)
	require.Zero(t, a.LedgerRows)
	require.Zero(t, a.LogRows)
	guard()
}

func TestSubscriptionQuotaBilling_TwoKeysPendingCostDeniesMoreAndPreservesOvershoot(t *testing.T) {
	f := newSubscriptionQuotaBillingFixture(t)
	f.rights.open(t, f.rights.userA, f.rights.input("open", "1", 0))
	next := f.rights.open(t, f.rights.userA, f.rights.input("renew", "200", 1))
	f.rights.open(t, f.rights.userB, f.rights.input("open", "300", 0))
	a := f.admit(t, f.rights.userA, f.keyA)
	b := f.admit(t, f.rights.userA, f.keyA2)
	require.Equal(t, a.TermID, b.TermID)
	guard := f.ownershipGuard(t, a, b)
	_, commandA := f.persist(t, a, "0.80000000", "0.08000000")
	_, commandB := f.persist(t, b, "0.70000000", "0.07000000")
	summary, err := f.rights.api.GetSummary(context.Background(), f.rights.userA, f.rights.group)
	require.NoError(t, err)
	require.NotNil(t, summary.CurrentTerm)
	require.Equal(t, "0.00000000", summary.CurrentTerm.UsedUSD)
	require.Equal(t, "1.50000000", summary.CurrentTerm.PendingUSD)
	require.Equal(t, "0.00000000", summary.CurrentTerm.RemainingUSD)
	require.Equal(t, "exhausted", summary.CurrentTerm.Status)
	_, err = f.port.AdmitRequest(context.Background(), f.admission(f.rights.userA, f.keyA))
	require.ErrorIs(t, err, service.ErrSubscriptionQuotaExhausted)
	for _, command := range []*service.UsageBillingCommand{commandA, commandB} {
		result, err := f.applier().Apply(context.Background(), command)
		require.NoError(t, err)
		require.True(t, result.Applied)
	}
	amountA := f.amounts(t, a)
	amountB := f.amounts(t, b)
	require.Equal(t, "1.50000000", amountA.Used)
	require.Equal(t, "1.50000000", amountA.DebitSum)
	require.Equal(t, 2, amountA.LedgerRows)
	require.Equal(t, "0.80000000", amountA.RequestCost)
	require.Equal(t, "0.70000000", amountB.RequestCost)
	for _, amount := range []string{amountA.KeyQuota, amountA.Key5H, amountA.Key1D, amountA.Key7D} {
		require.Equal(t, "0.80000000", amount)
	}
	for _, amount := range []string{amountB.KeyQuota, amountB.Key5H, amountB.Key1D, amountB.Key7D} {
		require.Equal(t, "0.70000000", amount)
	}
	for _, amount := range []string{amountA.AccountQuota, amountA.AccountDaily, amountA.AccountWeekly} {
		require.Equal(t, "0.15000000", amount)
	}
	require.EqualValues(t, 2, amountA.ParentRevision)
	require.EqualValues(t, 2, amountA.TermRevision)
	var used string
	require.NoError(t, f.db.QueryRow(`SELECT used_usd::text FROM subscription_quota_terms WHERE id=$1`, next.Term.ID).Scan(&used))
	require.Equal(t, "0.00000000", used)
	summary, err = f.rights.api.GetSummary(context.Background(), f.rights.userA, f.rights.group)
	require.NoError(t, err)
	require.Equal(t, "0.00000000", summary.CurrentTerm.PendingUSD)
	require.Equal(t, "1.50000000", summary.CurrentTerm.UsedUSD)
	require.Equal(t, "0.50000000", summary.CurrentTerm.RawOverageUSD)
	data, err := json.Marshal([]subscriptionQuotaBillingStoredAmounts{amountA, amountB})
	require.NoError(t, err)
	t.Logf("I3 actual two-Key overshoot deltas (B card and next card unchanged): %s", data)
	guard()
}

func TestSubscriptionQuotaBilling_StaleAdmissionStaysNullAndSkipsLockedRequest(t *testing.T) {
	f := newSubscriptionQuotaBillingFixture(t)
	f.rights.open(t, f.rights.userA, f.rights.input("open", "100", 0))
	old := f.admit(t, f.rights.userA, f.keyA)
	fresh := f.admit(t, f.rights.userA, f.keyA2)
	guard := f.ownershipGuard(t, old, fresh)
	_, err := f.db.Exec(`UPDATE subscription_quota_requests SET updated_at=clock_timestamp()-interval '2 hours',lease_until=clock_timestamp()-interval '1 minute' WHERE id=$1`, old.RequestRowID)
	require.NoError(t, err)
	blocker, err := f.db.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = blocker.Rollback() })
	var id int64
	require.NoError(t, blocker.QueryRow(`SELECT id FROM subscription_quota_requests WHERE id=$1 FOR UPDATE`, old.RequestRowID).Scan(&id))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, f.port.MarkStaleAdmissions(ctx, time.Hour, 100), "stale scan must skip a separately locked request row")
	require.Equal(t, "admitted", f.amounts(t, old).Status)
	require.NoError(t, blocker.Rollback())
	require.NoError(t, f.port.MarkStaleAdmissions(context.Background(), time.Hour, 100))
	a := f.amounts(t, old)
	require.Equal(t, "reconcile_required", a.Status)
	require.Equal(t, "NULL", a.RequestCost)
	require.Zero(t, a.LedgerRows)
	require.Zero(t, a.LogRows)
	require.Equal(t, "admitted", f.amounts(t, fresh).Status)
	_, command := f.persist(t, old, "0.50000000", "0.10000000")
	result, err := f.applier().Apply(context.Background(), command)
	require.NoError(t, err)
	require.True(t, result.Applied)
	f.requireCharge(t, old, "0.50000000", "0.10000000", 1)
	guard()
}

type subscriptionQuotaGPTBillingFixture struct {
	repo                    *CarpoolRepository
	group, key, term, cycle int64
}

func (f *subscriptionQuotaBillingFixture) currentGPT(t *testing.T, base string) *subscriptionQuotaGPTBillingFixture {
	return f.gptWindow(t, base, -time.Hour, 30*24*time.Hour)
}

func (f *subscriptionQuotaBillingFixture) gptWindow(t *testing.T, base string, startOffset, duration time.Duration) *subscriptionQuotaGPTBillingFixture {
	t.Helper()
	g := &subscriptionQuotaGPTBillingFixture{repo: NewCarpoolRepository(f.db)}
	var plan int64
	var snapshot json.RawMessage
	require.NoError(t, f.db.QueryRow(`SELECT c.group_id,c.plan_id,c.plan_snapshot,k.id FROM carpool_terms c JOIN api_keys k ON k.user_id=c.user_id AND k.group_id=c.group_id WHERE c.user_id=$1 ORDER BY c.id DESC,k.id LIMIT 1`, f.rights.userA).Scan(&g.group, &plan, &snapshot, &g.key))
	_, err := f.db.Exec(`UPDATE api_keys SET quota=1000000,rate_limit_5h=1000000,rate_limit_1d=1000000,rate_limit_7d=1000000 WHERE id=$1`, g.key)
	require.NoError(t, err)
	var now time.Time
	require.NoError(t, f.db.QueryRow(`SELECT clock_timestamp()`).Scan(&now))
	start := now.Add(startOffset)
	require.NoError(t, f.db.QueryRow(`INSERT INTO carpool_terms(user_id,scope_id,group_id,plan_id,plan_snapshot,starts_at,expires_at,status,source_mode,history_complete,statistics_since,created_by) VALUES($1,$2,$3,$4,$5,$6,$7,'active','new',true,$6,$8) RETURNING id`, f.rights.userA, domain.CarpoolGlobalScopeID, g.group, plan, snapshot, start, start.Add(duration), f.rights.admin).Scan(&g.term))
	require.NoError(t, f.db.QueryRow(`INSERT INTO carpool_cycles(term_id,cycle_no,starts_at,ends_at,base_quota_usd,base_balance_usd,boost_balance_usd,manual_balance_usd,state,activated_at) VALUES($1,1,$2,$3,550,$4,0,0,'active',$2) RETURNING id`, g.term, start, start.Add(7*24*time.Hour), base).Scan(&g.cycle))
	return g
}

func (f *subscriptionQuotaBillingFixture) knownGPT(t *testing.T, g *subscriptionQuotaGPTBillingFixture, cost string) *service.UsageBillingCommand {
	t.Helper()
	snapshot, err := g.repo.Admit(context.Background(), f.rights.userA, g.key, g.group, "carpool:i3:"+uuid.NewString(), time.Time{})
	require.NoError(t, err)
	amount := decimal.RequireFromString(cost)
	command := &service.UsageBillingCommand{RequestID: snapshot.RequestID, APIKeyID: snapshot.APIKeyID, UserID: snapshot.UserID, CarpoolSnapshot: snapshot, CarpoolCost: amount, APIKeyQuotaCost: amount.InexactFloat64(), APIKeyRateLimitCost: amount.InexactFloat64()}
	payload, err := service.MarshalCarpoolUsageBillingReceipt(command)
	require.NoError(t, err)
	require.NoError(t, g.repo.PersistKnownUsage(context.Background(), *snapshot, amount, payload))
	return command
}

func TestSubscriptionQuotaBilling_RealGPTInterleaveAndConcurrentSettlement(t *testing.T) {
	f := newSubscriptionQuotaBillingFixture(t)
	f.rights.open(t, f.rights.userA, f.rights.input("open", "100", 0))
	next := f.rights.open(t, f.rights.userA, f.rights.input("renew", "200", 1))
	other := f.rights.open(t, f.rights.userB, f.rights.input("open", "300", 0))
	g := f.currentGPT(t, "100")
	claude := f.admit(t, f.rights.userA, f.keyA)
	guard := f.ownershipGuard(t, claude)
	_, command := f.persist(t, claude, "1.00000000", "0.10000000")
	result, err := f.applier().Apply(context.Background(), command)
	require.NoError(t, err)
	require.True(t, result.Applied)
	guard()
	claudeColumns := make(map[string][]string)
	for table, columns := range subscriptionQuotaLegacyColumns(t, f.db) {
		if strings.HasPrefix(table, "subscription_quota_") {
			claudeColumns[table] = columns
		}
	}
	beforeGPT := subscriptionQuotaLegacyFingerprints(t, f.db, claudeColumns)
	gptCommand := f.knownGPT(t, g, "2.00000000")
	result, err = f.applier().Apply(context.Background(), gptCommand)
	require.NoError(t, err)
	require.True(t, result.Applied)
	require.Equal(t, beforeGPT, subscriptionQuotaLegacyFingerprints(t, f.db, claudeColumns), "actual GPT admission, known receipt and settlement must leave every Claude domain row unchanged")
	second := f.admit(t, f.rights.userA, f.keyA2)
	_, claudeCommand := f.persist(t, second, "1.25000000", "0.12500000")
	secondGPT := f.knownGPT(t, g, "2.50000000")
	protected := make(map[string][]string)
	for table, columns := range subscriptionQuotaLegacyColumns(t, f.db) {
		if table == "users" || table == "user_subscriptions" || table == "usage_logs" || table == "groups" || table == "redeem_codes" || strings.HasPrefix(table, "payment_") || (strings.HasPrefix(table, "carpool_") && table != "carpool_cycles" && table != "carpool_ledger" && table != "carpool_billing_requests") {
			protected[table] = columns
		}
	}
	before := subscriptionQuotaLegacyFingerprints(t, f.db, protected)
	foreignQueries := map[string]struct {
		query string
		args  []any
	}{
		"claude_foreign_terms":    {`SELECT * FROM subscription_quota_terms WHERE id<>$1`, []any{second.TermID}},
		"claude_foreign_requests": {`SELECT * FROM subscription_quota_requests WHERE id<>$1`, []any{second.RequestRowID}},
		"claude_foreign_ledger":   {`SELECT * FROM subscription_quota_ledger WHERE request_row_id<>$1`, []any{second.RequestRowID}},
		"gpt_foreign_cycles":      {`SELECT * FROM carpool_cycles WHERE id<>$1`, []any{g.cycle}},
		"gpt_foreign_ledger":      {`SELECT * FROM carpool_ledger WHERE NOT(term_id=$1 AND request_id=$2 AND api_key_id=$3)`, []any{g.term, secondGPT.RequestID, g.key}},
		"gpt_foreign_requests":    {`SELECT * FROM carpool_billing_requests WHERE id<>$1`, []any{secondGPT.CarpoolSnapshot.BillingRequestID}},
		"other_keys":              {`SELECT * FROM api_keys WHERE id<>$1 AND id<>$2`, []any{second.APIKeyID, g.key}},
		"other_accounts":          {`SELECT * FROM accounts WHERE id<>$1`, []any{f.account}},
		"key_identity":            {`SELECT to_jsonb(k)-'quota_used'-'usage_5h'-'usage_1d'-'usage_7d'-'window_5h_start'-'window_1d_start'-'window_7d_start'-'updated_at' AS identity FROM api_keys k`, nil},
		"account_identity":        {`SELECT (to_jsonb(a)-'extra'-'updated_at'-'last_used_at') || jsonb_build_object('extra',a.extra-'quota_used'-'quota_daily_used'-'quota_weekly_used'-'quota_daily_start'-'quota_weekly_start'-'quota_daily_reset_at'-'quota_weekly_reset_at') AS identity FROM accounts a WHERE id=$1`, []any{f.account}},
	}
	captureForeign := func() map[string]string {
		values := make(map[string]string, len(foreignQueries))
		for name, q := range foreignQueries {
			var hash string
			require.NoError(t, f.db.QueryRow(`SELECT md5(COALESCE(jsonb_agg(to_jsonb(x) ORDER BY to_jsonb(x)::text)::text,'[]')) FROM (`+q.query+`)x`, q.args...).Scan(&hash))
			values[name] = hash
		}
		return values
	}
	foreignBefore := captureForeign()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	outcomes := make(chan error, 2)
	start := make(chan struct{})
	var workers sync.WaitGroup
	for _, cmd := range []*service.UsageBillingCommand{claudeCommand, secondGPT} {
		workers.Add(1)
		go func(cmd *service.UsageBillingCommand) {
			defer workers.Done()
			<-start
			result, err := f.applier().Apply(ctx, cmd)
			if err == nil && !result.Applied {
				outcomes <- service.ErrUsageBillingRequestConflict
				return
			}
			outcomes <- err
		}(cmd)
	}
	close(start)
	workers.Wait()
	close(outcomes)
	for err := range outcomes {
		require.NoError(t, err, "actual GPT and Claude settlement transactions must complete without a lock cycle")
	}
	var used, base, boost, manual string
	var ledgerRows int
	require.NoError(t, f.db.QueryRow(`SELECT used_usd::text FROM subscription_quota_terms WHERE id=$1`, claude.TermID).Scan(&used))
	require.Equal(t, "2.25000000", used)
	require.NoError(t, f.db.QueryRow(`SELECT base_balance_usd::text,boost_balance_usd::text,manual_balance_usd::text FROM carpool_cycles WHERE id=$1`, g.cycle).Scan(&base, &boost, &manual))
	require.Equal(t, "95.50000000", base)
	require.Equal(t, "0.00000000", boost)
	require.Equal(t, "0.00000000", manual)
	require.NoError(t, f.db.QueryRow(`SELECT count(*) FROM subscription_quota_ledger WHERE term_id=$1`, claude.TermID).Scan(&ledgerRows))
	require.Equal(t, 2, ledgerRows)
	for _, id := range []int64{next.Term.ID, other.Term.ID} {
		require.NoError(t, f.db.QueryRow(`SELECT used_usd::text FROM subscription_quota_terms WHERE id=$1`, id).Scan(&used))
		require.Equal(t, "0.00000000", used)
	}
	require.Equal(t, before, subscriptionQuotaLegacyFingerprints(t, f.db, protected), "interleaving changes only the respective ledgers/counters, never balances, native windows, reset/boost/payment or plans")
	require.Equal(t, foreignBefore, captureForeign(), "simultaneous billing must not touch B/next/other requests, old GPT cycles/ledger/receipts or unrelated Keys/accounts")
	var lastUsed time.Time
	require.NoError(t, f.db.QueryRow(`SELECT last_used_at FROM accounts WHERE id=$1`, f.account).Scan(&lastUsed))
	require.True(t, second.AdmittedAt.Equal(lastUsed), "concurrent GPT settlement cannot replace the original Claude account's admitted-at publication")
	var accountCost string
	require.NoError(t, f.db.QueryRow(`SELECT (extra->>'quota_used')::numeric(20,8)::text FROM accounts WHERE id=$1`, f.account).Scan(&accountCost))
	require.Equal(t, "0.22500000", accountCost)
	t.Logf("I3 real concurrent deltas: Claude used=2.25000000(two debits), GPT base=95.50000000(two charges totaling4.50000000), account=0.22500000; B,next,native,balances/reset/boost/payment unchanged")
}

func TestSubscriptionQuotaBilling_TwoClaimersLeaseExpiryAndNoRequestLockWhileWaitingScope(t *testing.T) {
	f := newSubscriptionQuotaBillingFixture(t)
	f.rights.open(t, f.rights.userA, f.rights.input("open", "100", 0))
	var snapshots []*domain.SubscriptionQuotaBillingSnapshot
	var receipts []domain.SubscriptionQuotaKnownUsage
	for i := 0; i < 4; i++ {
		snapshot := f.admit(t, f.rights.userA, f.keyA)
		known, _ := f.persist(t, snapshot, "1.00000000", "0.10000000")
		snapshots = append(snapshots, snapshot)
		receipts = append(receipts, known)
	}
	guard := f.ownershipGuard(t, snapshots...)
	type claimed struct {
		items []domain.SubscriptionQuotaKnownUsage
		err   error
	}
	results := make(chan claimed, 2)
	start := make(chan struct{})
	var workers sync.WaitGroup
	for _, owner := range []string{"synthetic-worker-a", "synthetic-worker-b"} {
		workers.Add(1)
		go func(owner string) {
			defer workers.Done()
			<-start
			batch, err := f.port.ClaimRecoverable(context.Background(), domain.SubscriptionQuotaRecoveryClaim{Owner: owner, Limit: 2, Lease: time.Minute})
			results <- claimed{batch, err}
		}(owner)
	}
	close(start)
	workers.Wait()
	close(results)
	seen := make(map[int64]domain.SubscriptionQuotaKnownUsage)
	for result := range results {
		require.NoError(t, result.err)
		require.Len(t, result.items, 2)
		for _, item := range result.items {
			_, duplicate := seen[item.Snapshot.RequestRowID]
			require.False(t, duplicate, "two workers may not claim the same unexpired request")
			require.EqualValues(t, 1, item.RequestRevision, "lease ownership must not increment billing revision")
			seen[item.Snapshot.RequestRowID] = item
		}
	}
	require.Len(t, seen, 4)
	none, err := f.port.ClaimRecoverable(context.Background(), domain.SubscriptionQuotaRecoveryClaim{Owner: "synthetic-worker-c", Limit: 10, Lease: time.Minute})
	require.NoError(t, err)
	require.Empty(t, none)
	first := snapshots[0]
	oldOwner := seen[first.RequestRowID].LeaseOwner
	_, err = f.db.Exec(`UPDATE subscription_quota_requests SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1`, first.RequestRowID)
	require.NoError(t, err)
	reclaimed, err := f.port.ClaimRecoverable(context.Background(), domain.SubscriptionQuotaRecoveryClaim{Owner: "synthetic-worker-c", Limit: 1, Lease: time.Minute})
	require.NoError(t, err)
	require.Len(t, reclaimed, 1)
	require.Equal(t, first.RequestRowID, reclaimed[0].Snapshot.RequestRowID)
	require.EqualValues(t, 1, reclaimed[0].RequestRevision)
	require.NoError(t, f.port.FinishRecoveryAttempt(context.Background(), first.RequestRowID, oldOwner, "transient", time.Second))
	var owner string
	require.NoError(t, f.db.QueryRow(`SELECT lease_owner FROM subscription_quota_requests WHERE id=$1`, first.RequestRowID).Scan(&owner))
	require.Equal(t, "synthetic-worker-c", owner, "a stale worker cannot clear its successor's lease")
	blocker, err := f.db.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = blocker.Rollback() })
	require.NoError(t, subscriptionQuotaScopeLock(context.Background(), blocker, first.UserID, first.GroupID))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		command, err := service.DecodeSubscriptionQuotaUsageBillingReceipt(reclaimed[0].BillingPayload)
		if err == nil {
			_, err = f.applier().Apply(ctx, command)
		}
		finished <- err
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		err := f.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND state='active' AND wait_event_type='Lock' AND query LIKE '%pg_advisory_xact_lock%')`).Scan(&waiting)
		return err == nil && waiting
	}, time.Second, 10*time.Millisecond)
	probe, err := f.db.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	var id int64
	require.NoError(t, probe.QueryRow(`SELECT id FROM subscription_quota_requests WHERE id=$1 FOR UPDATE NOWAIT`, first.RequestRowID).Scan(&id), "claim committed and settlement waits for scope without owning the request row lock")
	require.NoError(t, probe.Rollback())
	require.NoError(t, blocker.Rollback())
	select {
	case err := <-finished:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("reclaimed receipt did not settle after scope release")
	}
	for _, known := range receipts {
		command, err := service.DecodeSubscriptionQuotaUsageBillingReceipt(known.BillingPayload)
		require.NoError(t, err)
		_, err = f.applier().Apply(context.Background(), command)
		require.NoError(t, err)
	}
	a := f.amounts(t, first)
	require.Equal(t, "4.00000000", a.Used)
	require.Equal(t, "4.00000000", a.DebitSum)
	require.Equal(t, 4, a.LedgerRows)
	require.Equal(t, "4.00000000", a.KeyQuota)
	require.Equal(t, "0.40000000", a.AccountQuota)
	guard()
}

func TestSubscriptionQuotaBilling_HeartbeatLeaseProtectsLiveAdmissionWithoutRevision(t *testing.T) {
	f := newSubscriptionQuotaBillingFixture(t)
	f.rights.open(t, f.rights.userA, f.rights.input("open", "100", 0))
	snapshot := f.admit(t, f.rights.userA, f.keyA)
	guard := f.ownershipGuard(t, snapshot)
	_, err := f.db.Exec(`UPDATE subscription_quota_requests SET updated_at=clock_timestamp()-interval '2 hours' WHERE id=$1`, snapshot.RequestRowID)
	require.NoError(t, err)
	require.NoError(t, f.port.MarkStaleAdmissions(context.Background(), time.Minute, 100))
	require.Equal(t, "admitted", f.amounts(t, snapshot).Status, "an unexpired live forward lease prevents stale classification")
	_, err = f.db.Exec(`UPDATE subscription_quota_requests SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1`, snapshot.RequestRowID)
	require.NoError(t, err)
	require.NoError(t, f.port.Heartbeat(context.Background(), snapshot))
	require.NoError(t, f.port.MarkStaleAdmissions(context.Background(), time.Minute, 100))
	var revision int64
	var live bool
	require.NoError(t, f.db.QueryRow(`SELECT revision,lease_until>clock_timestamp() FROM subscription_quota_requests WHERE id=$1`, snapshot.RequestRowID).Scan(&revision, &live))
	require.Zero(t, revision)
	require.True(t, live)
	require.Equal(t, "admitted", f.amounts(t, snapshot).Status)
	f.persist(t, snapshot, "0.50000000", "0.10000000")
	require.NoError(t, f.port.Heartbeat(context.Background(), snapshot))
	var noLease bool
	require.NoError(t, f.db.QueryRow(`SELECT revision,lease_until IS NULL FROM subscription_quota_requests WHERE id=$1`, snapshot.RequestRowID).Scan(&revision, &noLease))
	require.EqualValues(t, 1, revision)
	require.True(t, noLease, "a late heartbeat cannot revive a completed forward lease")
	guard()
}

func (f *subscriptionQuotaBillingFixture) recovery(timeout time.Duration, batch int) *service.SubscriptionQuotaRecoveryService {
	cfg := &config.Config{RunMode: config.RunModeStandard, SubscriptionQuota: config.SubscriptionQuotaConfig{IssueEnabled: false, AdmissionEnabled: false, RecoveryEnabled: true, RecoveryBatchSize: batch, RecoveryItemTimeout: timeout, RequestStaleGrace: time.Hour}}
	return service.NewSubscriptionQuotaRecoveryService(f.port, f.applier(), NewUsageLogRepository(nil, f.db), cfg)
}

func TestSubscriptionQuotaBilling_RecoveryFirstTimeoutDoesNotCancelNextIndependentContext(t *testing.T) {
	f := newSubscriptionQuotaBillingFixture(t)
	f.rights.open(t, f.rights.userA, f.rights.input("open", "100", 0))
	f.rights.open(t, f.rights.userB, f.rights.input("open", "300", 0))
	a := f.admit(t, f.rights.userA, f.keyA)
	b := f.admit(t, f.rights.userB, f.keyB)
	f.persist(t, a, "1.00000000", "0.10000000")
	f.persist(t, b, "2.00000000", "0.20000000")
	guard := f.ownershipGuard(t, a, b)
	blocker, err := f.db.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = blocker.Rollback() })
	require.NoError(t, subscriptionQuotaScopeLock(context.Background(), blocker, a.UserID, a.GroupID))
	worker := f.recovery(200*time.Millisecond, 10)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = worker.RunOnce(ctx)
	require.Error(t, err, "the intentionally blocked first receipt reports its own timeout")
	require.NoError(t, ctx.Err(), "the worker root remains active")
	amountA := f.amounts(t, a)
	amountB := f.amounts(t, b)
	require.Equal(t, "usage_known", amountA.Status)
	require.Equal(t, "0.00000000", amountA.Used)
	require.Zero(t, amountA.LedgerRows)
	require.Equal(t, "settled", amountB.Status)
	require.Equal(t, "2.00000000", amountB.Used)
	require.Equal(t, 1, amountB.LedgerRows)
	require.Equal(t, 1, amountB.LogRows)
	require.NoError(t, blocker.Rollback())
	_, err = f.db.Exec(`UPDATE subscription_quota_requests SET next_retry_at=clock_timestamp(),lease_owner=NULL,lease_until=NULL WHERE id=$1`, a.RequestRowID)
	require.NoError(t, err)
	require.NoError(t, worker.RunOnce(ctx))
	require.Equal(t, "settled", f.amounts(t, a).Status)
	require.Equal(t, "2.00000000", f.amounts(t, b).Used)
	var total string
	require.NoError(t, f.db.QueryRow(`SELECT (extra->>'quota_used')::numeric(20,8)::text FROM accounts WHERE id=$1`, f.account).Scan(&total))
	require.Equal(t, "0.30000000", total)
	guard()
}

func TestSubscriptionQuotaBilling_RecoveryRepairsFailedLogAndNeverRecreatesCleanedHistory(t *testing.T) {
	f := newSubscriptionQuotaBillingFixture(t)
	f.rights.open(t, f.rights.userA, f.rights.input("open", "100", 0))
	snapshot := f.admit(t, f.rights.userA, f.keyA)
	f.persist(t, snapshot, "0.50000000", "0.10000000")
	guard := f.ownershipGuard(t, snapshot)
	_, err := f.db.Exec(`CREATE FUNCTION synthetic_i3_fail_usage_log() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic I3 usage-log insertion failure'; END $$; CREATE TRIGGER synthetic_i3_fail_usage_log BEFORE INSERT ON usage_logs FOR EACH ROW EXECUTE FUNCTION synthetic_i3_fail_usage_log()`)
	require.NoError(t, err)
	worker := f.recovery(time.Second, 10)
	require.ErrorContains(t, worker.RunOnce(context.Background()), "synthetic I3 usage-log insertion failure")
	f.requireCharge(t, snapshot, "0.50000000", "0.10000000", 1)
	require.Zero(t, f.amounts(t, snapshot).LogRows)
	var recorded bool
	require.NoError(t, f.db.QueryRow(`SELECT usage_log_recorded_at IS NOT NULL FROM subscription_quota_requests WHERE id=$1`, snapshot.RequestRowID).Scan(&recorded))
	require.False(t, recorded)
	_, err = f.db.Exec(`DROP TRIGGER synthetic_i3_fail_usage_log ON usage_logs; DROP FUNCTION synthetic_i3_fail_usage_log()`)
	require.NoError(t, err)
	// Simulate the other crash window: insertion commits, marking fails.
	_, err = f.db.Exec(`CREATE FUNCTION synthetic_i3_fail_log_marker() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.usage_log_recorded_at IS NOT NULL AND OLD.usage_log_recorded_at IS NULL THEN RAISE EXCEPTION 'synthetic I3 usage-log marker failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER synthetic_i3_fail_log_marker BEFORE UPDATE ON subscription_quota_requests FOR EACH ROW EXECUTE FUNCTION synthetic_i3_fail_log_marker()`)
	require.NoError(t, err)
	require.ErrorContains(t, worker.RunOnce(context.Background()), "synthetic I3 usage-log marker failure")
	require.Equal(t, 1, f.amounts(t, snapshot).LogRows)
	staleKnown, err := f.port.ListPendingUsageLogs(context.Background(), 10)
	require.NoError(t, err)
	require.Len(t, staleKnown, 1, "worker A already holds its pending publication snapshot before worker B acknowledges it")
	cleanup := NewUsageCleanupRepository(nil, f.db)
	filters := service.UsageCleanupFilters{StartTime: snapshot.AdmittedAt.Add(-time.Hour), EndTime: snapshot.AdmittedAt.Add(time.Hour), UserID: quotaDomainInt(snapshot.UserID)}
	deleted, err := cleanup.DeleteUsageLogsBatch(context.Background(), filters, 100)
	require.NoError(t, err)
	require.Zero(t, deleted, "unconfirmed log insertion cannot race cleanup")
	aggregation := NewDashboardAggregationRepository(f.db)
	require.NoError(t, aggregation.CleanupUsageLogs(context.Background(), snapshot.AdmittedAt.Add(time.Hour)))
	require.Equal(t, 1, f.amounts(t, snapshot).LogRows, "scheduled dashboard cleanup must also preserve an unconfirmed durable log")
	deleted, err = cleanupUsageLogsBatchWithRollupInvalidation(context.Background(), f.db, snapshot.AdmittedAt.Add(time.Hour))
	require.NoError(t, err)
	require.Zero(t, deleted, "the transactional scheduled-cleanup batch must enforce the same acknowledgement guard")
	_, err = f.db.Exec(`DROP TRIGGER synthetic_i3_fail_log_marker ON subscription_quota_requests; DROP FUNCTION synthetic_i3_fail_log_marker()`)
	require.NoError(t, err)
	require.NoError(t, worker.RunOnce(context.Background()))
	require.Equal(t, 1, f.amounts(t, snapshot).LogRows)
	require.NoError(t, f.db.QueryRow(`SELECT usage_log_recorded_at IS NOT NULL FROM subscription_quota_requests WHERE id=$1`, snapshot.RequestRowID).Scan(&recorded))
	require.True(t, recorded)
	deleted, err = cleanup.DeleteUsageLogsBatch(context.Background(), filters, 100)
	require.NoError(t, err)
	require.EqualValues(t, 1, deleted)
	require.NoError(t, worker.RunOnce(context.Background()))
	require.Zero(t, f.amounts(t, snapshot).LogRows, "acknowledged history deleted by cleanup is never recreated")
	staleLog, err := service.DecodeSubscriptionQuotaUsageLogPayload(staleKnown[0].UsageLogPayload)
	require.NoError(t, err)
	_, err = NewUsageLogRepository(nil, f.db).Create(context.Background(), staleLog)
	require.NoError(t, err, "a worker holding a pre-ack snapshot must observe the durable acknowledgement and skip publication after cleanup")
	require.NoError(t, f.port.MarkUsageLogRecorded(context.Background(), snapshot.RequestRowID, staleKnown[0].RequestFingerprint), "an already acknowledged matching fact remains idempotent after its statistical log is cleaned")
	require.Zero(t, f.amounts(t, snapshot).LogRows, "stale worker A cannot resurrect the log acknowledged by B and removed by normal cleanup")
	require.NoError(t, aggregation.CleanupUsageLogs(context.Background(), snapshot.AdmittedAt.Add(time.Hour)))
	require.NoError(t, worker.RunOnce(context.Background()))
	require.Zero(t, f.amounts(t, snapshot).LogRows, "running scheduled cleanup after manual cleanup must not reset the durable publication acknowledgement")
	f.requireCharge(t, snapshot, "0.50000000", "0.10000000", 1)
	guard()
}

func TestSubscriptionQuotaBilling_TwoActualRecoveryWorkersHaveExactlyOnceEffects(t *testing.T) {
	f := newSubscriptionQuotaBillingFixture(t)
	f.rights.open(t, f.rights.userA, f.rights.input("open", "100", 0))
	var snapshots []*domain.SubscriptionQuotaBillingSnapshot
	for i := 0; i < 4; i++ {
		snapshot := f.admit(t, f.rights.userA, f.keyA)
		f.persist(t, snapshot, "0.25000000", "0.02500000")
		snapshots = append(snapshots, snapshot)
	}
	guard := f.ownershipGuard(t, snapshots...)
	a := f.recovery(5*time.Second, 2)
	b := f.recovery(5*time.Second, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, worker := range []*service.SubscriptionQuotaRecoveryService{a, b} {
		go func(worker *service.SubscriptionQuotaRecoveryService) { <-start; results <- worker.RunOnce(ctx) }(worker)
	}
	close(start)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	var workerErrors []error
	seenWaits := make(map[string]bool)
	for len(workerErrors) < 2 {
		select {
		case err := <-results:
			workerErrors = append(workerErrors, err)
			var pgError *pq.Error
			if errors.As(err, &pgError) {
				t.Logf("actual worker PostgreSQL diagnostic code=%s message=%s detail=%s where=%s", pgError.Code, pgError.Message, pgError.Detail, pgError.Where)
			}
		case <-ticker.C:
			rows, err := f.db.QueryContext(ctx, `SELECT pid,wait_event,pg_blocking_pids(pid)::text,left(regexp_replace(query,'[[:space:]]+',' ','g'),450) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND pid<>pg_backend_pid()`)
			if err != nil {
				continue
			}
			for rows.Next() {
				var pid int
				var wait, blockers, query string
				if rows.Scan(&pid, &wait, &blockers, &query) == nil {
					state := strconv.Itoa(pid) + " " + wait + " " + blockers + " " + query
					if !seenWaits[state] {
						seenWaits[state] = true
						t.Logf("actual two-worker lock graph: %s", state)
					}
				}
			}
			_ = rows.Close()
		case <-ctx.Done():
			require.FailNow(t, "actual workers exceeded their root bound", ctx.Err())
		}
	}
	for _, err := range workerErrors {
		require.NoError(t, err)
	}
	require.NoError(t, a.RunOnce(ctx))
	require.NoError(t, b.RunOnce(ctx))
	for _, snapshot := range snapshots {
		amounts := f.amounts(t, snapshot)
		require.Equal(t, "settled", amounts.Status)
		require.Equal(t, "0.25000000", amounts.RequestCost)
		require.Equal(t, 1, amounts.LogRows)
		var recorded bool
		require.NoError(t, f.db.QueryRow(`SELECT usage_log_recorded_at IS NOT NULL FROM subscription_quota_requests WHERE id=$1`, snapshot.RequestRowID).Scan(&recorded))
		require.True(t, recorded)
	}
	amounts := f.amounts(t, snapshots[0])
	require.Equal(t, "1.00000000", amounts.Used)
	require.Equal(t, "1.00000000", amounts.DebitSum)
	require.Equal(t, 4, amounts.LedgerRows)
	require.Equal(t, "1.00000000", amounts.KeyQuota)
	require.Equal(t, "0.10000000", amounts.AccountQuota)
	guard()
}

func TestSubscriptionQuotaBilling_LateConflictNeverReopensTerminalSettledOrNoCharge(t *testing.T) {
	for _, terminal := range []string{"settled", "no_charge"} {
		t.Run(terminal, func(t *testing.T) {
			f := newSubscriptionQuotaBillingFixture(t)
			f.rights.open(t, f.rights.userA, f.rights.input("open", "100", 0))
			snapshot := f.admit(t, f.rights.userA, f.keyA)
			if terminal == "settled" {
				_, cmd := f.persist(t, snapshot, "1.00000000", "0.10000000")
				_, err := f.applier().Apply(context.Background(), cmd)
				require.NoError(t, err)
			} else {
				require.NoError(t, f.port.MarkNoCharge(context.Background(), snapshot, domain.SubscriptionQuotaNoChargeEvidence{Kind: "upstream_rejected", Reference: "http:429"}))
			}
			guard := f.ownershipGuard(t, snapshot)
			var originalCost string
			var originalFingerprint sql.NullString
			var originalPayload, originalEvidence []byte
			require.NoError(t, f.db.QueryRow(`SELECT actual_cost_usd::text,request_fingerprint,billing_payload,COALESCE(resolution_evidence,'{}'::jsonb)-'latest_receipt_conflict' FROM subscription_quota_requests WHERE id=$1`, snapshot.RequestRowID).Scan(&originalCost, &originalFingerprint, &originalPayload, &originalEvidence))
			changed, _ := f.known(t, snapshot, "2.00000000", "0.20000000")
			require.ErrorIs(t, f.port.PersistKnownUsage(context.Background(), changed), service.ErrSubscriptionQuotaReceiptConflict)
			var status, cost, category string
			var fingerprint sql.NullString
			var payload, evidence []byte
			require.NoError(t, f.db.QueryRow(`SELECT status,actual_cost_usd::text,request_fingerprint,billing_payload,COALESCE(resolution_evidence,'{}'::jsonb)-'latest_receipt_conflict',last_error_category FROM subscription_quota_requests WHERE id=$1`, snapshot.RequestRowID).Scan(&status, &cost, &fingerprint, &payload, &evidence, &category))
			require.Equal(t, terminal, status)
			require.Equal(t, originalCost, cost)
			require.Equal(t, originalFingerprint, fingerprint)
			require.Equal(t, originalPayload, payload)
			require.Equal(t, originalEvidence, evidence)
			var conflictFingerprint string
			require.NoError(t, f.db.QueryRow(`SELECT resolution_evidence->'latest_receipt_conflict'->>'fingerprint' FROM subscription_quota_requests WHERE id=$1`, snapshot.RequestRowID).Scan(&conflictFingerprint))
			require.Equal(t, changed.RequestFingerprint, conflictFingerprint, "late contradiction is appended as diagnostic evidence without replacing the terminal financial fact")
			require.Contains(t, category, "receipt_conflict")
			if terminal == "settled" {
				f.requireCharge(t, snapshot, "1.00000000", "0.10000000", 1)
			} else {
				a := f.amounts(t, snapshot)
				require.Equal(t, "0.00000000", a.RequestCost)
				require.Equal(t, "0.00000000", a.Used)
				require.Zero(t, a.LedgerRows)
				require.Zero(t, a.DedupRows)
				require.Zero(t, a.LogRows)
			}
			guard()
		})
	}
}

func (f *subscriptionQuotaBillingFixture) resolver(t *testing.T) *service.SubscriptionQuotaResolutionService {
	t.Helper()
	repo, ok := f.rights.repo.(service.SubscriptionQuotaResolutionRepository)
	require.True(t, ok)
	return service.NewSubscriptionQuotaResolutionService(repo, nil)
}

func TestSubscriptionQuotaBilling_AuditedResolveReplaysAndRejectsInvalidOrKnownFacts(t *testing.T) {
	f := newSubscriptionQuotaBillingFixture(t)
	f.rights.open(t, f.rights.userA, f.rights.input("open", "100", 0))
	snapshot := f.admit(t, f.rights.userA, f.keyA)
	require.NoError(t, f.port.MarkReconcileRequired(context.Background(), snapshot, "usage_missing"))
	resolver := f.resolver(t)
	guard := f.ownershipGuard(t, snapshot)
	in := service.SubscriptionQuotaResolveInput{ExpectedRequestRevision: quotaDomainInt(1), Resolution: "no_charge", EvidenceReference: "synthetic-audit/upstream-billing-record-absent", Reason: "operator verified synthetic upstream did not charge"}
	columns := subscriptionQuotaLegacyColumns(t, f.db)
	before := subscriptionQuotaLegacyFingerprints(t, f.db, columns)
	for _, bad := range []service.SubscriptionQuotaResolveInput{{ExpectedRequestRevision: quotaDomainInt(1), Resolution: "no_charge", Reason: in.Reason}, {ExpectedRequestRevision: quotaDomainInt(1), Resolution: "no_charge", EvidenceReference: in.EvidenceReference}, {ExpectedRequestRevision: nil, Resolution: "no_charge", EvidenceReference: in.EvidenceReference, Reason: in.Reason}} {
		_, err := resolver.ResolveRequest(context.Background(), f.rights.admin, snapshot.RequestRowID, bad, uuid.NewString())
		require.ErrorIs(t, err, service.ErrSubscriptionQuotaInput)
	}
	_, err := resolver.ResolveRequest(context.Background(), f.rights.userB, snapshot.RequestRowID, in, uuid.NewString())
	require.ErrorIs(t, err, service.ErrSubscriptionQuotaNotFound)
	stale := in
	stale.ExpectedRequestRevision = quotaDomainInt(0)
	_, err = resolver.ResolveRequest(context.Background(), f.rights.admin, snapshot.RequestRowID, stale, uuid.NewString())
	require.ErrorIs(t, err, service.ErrSubscriptionQuotaRequestRevision)
	require.Equal(t, before, subscriptionQuotaLegacyFingerprints(t, f.db, columns), "invalid/stale/unauthorized resolution cannot write audit, fact or operation")
	key := uuid.NewString()
	first, err := resolver.ResolveRequest(context.Background(), f.rights.admin, snapshot.RequestRowID, in, key)
	require.NoError(t, err)
	require.Equal(t, "no_charge", first.Request.Status)
	require.NotNil(t, first.Request.ActualCostUSD)
	require.Equal(t, "0.00000000", *first.Request.ActualCostUSD)
	var evidence []byte
	require.NoError(t, f.db.QueryRow(`SELECT resolution_evidence->'manual_resolution' FROM subscription_quota_requests WHERE id=$1`, snapshot.RequestRowID).Scan(&evidence))
	var audit struct {
		ActorID    int64                                 `json:"actor_id"`
		Intent     service.SubscriptionQuotaResolveInput `json:"intent"`
		ResolvedAt time.Time                             `json:"resolved_at"`
	}
	require.NoError(t, json.Unmarshal(evidence, &audit))
	require.Equal(t, f.rights.admin, audit.ActorID)
	require.Equal(t, in.EvidenceReference, audit.Intent.EvidenceReference)
	require.Equal(t, in.Reason, audit.Intent.Reason)
	require.False(t, audit.ResolvedAt.IsZero())
	before = subscriptionQuotaLegacyFingerprints(t, f.db, columns)
	replayed, err := resolver.ResolveRequest(context.Background(), f.rights.admin, snapshot.RequestRowID, in, key)
	require.NoError(t, err)
	want := *first
	want.Replayed = true
	require.JSONEq(t, quotaDomainJSON(t, want), quotaDomainJSON(t, replayed))
	require.Equal(t, before, subscriptionQuotaLegacyFingerprints(t, f.db, columns), "original successful resolution replay precedes current revision and writes nothing")
	changed := in
	changed.Reason = "changed operator intent"
	_, err = resolver.ResolveRequest(context.Background(), f.rights.admin, snapshot.RequestRowID, changed, key)
	require.ErrorIs(t, err, service.ErrSubscriptionQuotaIdempotency)
	foreign, err := resolver.ResolveRequest(context.Background(), f.rights.otherAdmin, snapshot.RequestRowID, in, key)
	require.ErrorIs(t, err, service.ErrSubscriptionQuotaRequestRevision)
	require.Nil(t, foreign)
	var operations int
	require.NoError(t, f.db.QueryRow(`SELECT count(*) FROM subscription_quota_operations WHERE operation_kind='resolve_request' AND idempotency_key=$1 AND actor_id=$2`, key, f.rights.admin).Scan(&operations))
	require.Equal(t, 1, operations)
	guard()

	knownSnapshot := f.admit(t, f.rights.userA, f.keyA2)
	original, _ := f.persist(t, knownSnapshot, "1.00000000", "0.10000000")
	conflict, _ := f.known(t, knownSnapshot, "2.00000000", "0.20000000")
	require.ErrorIs(t, f.port.PersistKnownUsage(context.Background(), conflict), service.ErrSubscriptionQuotaReceiptConflict)
	var revision int64
	require.NoError(t, f.db.QueryRow(`SELECT revision FROM subscription_quota_requests WHERE id=$1`, knownSnapshot.RequestRowID).Scan(&revision))
	in.ExpectedRequestRevision = quotaDomainInt(revision)
	before = subscriptionQuotaLegacyFingerprints(t, f.db, columns)
	_, err = resolver.ResolveRequest(context.Background(), f.rights.admin, knownSnapshot.RequestRowID, in, uuid.NewString())
	require.ErrorIs(t, err, service.ErrSubscriptionQuotaNotResolvable)
	require.Equal(t, before, subscriptionQuotaLegacyFingerprints(t, f.db, columns))
	var payload []byte
	require.NoError(t, f.db.QueryRow(`SELECT billing_payload FROM subscription_quota_requests WHERE id=$1`, knownSnapshot.RequestRowID).Scan(&payload))
	require.JSONEq(t, string(original.BillingPayload), string(payload))
}

func TestSubscriptionQuotaBilling_ManualNoChargeAndLateTrustedUsageRacePreserveFirstFact(t *testing.T) {
	f := newSubscriptionQuotaBillingFixture(t)
	f.rights.open(t, f.rights.userA, f.rights.input("open", "100", 0))
	snapshot := f.admit(t, f.rights.userA, f.keyA)
	require.NoError(t, f.port.MarkReconcileRequired(context.Background(), snapshot, "usage_missing"))
	known, command := f.known(t, snapshot, "1.00000000", "0.10000000")
	guard := f.ownershipGuard(t, snapshot)
	in := service.SubscriptionQuotaResolveInput{ExpectedRequestRevision: quotaDomainInt(1), Resolution: "no_charge", EvidenceReference: "synthetic-audit/racing-upstream-evidence", Reason: "audited synthetic race"}
	resolver := f.resolver(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := make(chan struct{})
	knownResult := make(chan error, 1)
	type resolved struct {
		result *domain.SubscriptionQuotaResolveResult
		err    error
	}
	manualResult := make(chan resolved, 1)
	go func() { <-start; knownResult <- f.port.PersistKnownUsage(ctx, known) }()
	go func() {
		<-start
		result, err := resolver.ResolveRequest(ctx, f.rights.admin, snapshot.RequestRowID, in, uuid.NewString())
		manualResult <- resolved{result, err}
	}()
	close(start)
	knownErr := <-knownResult
	manual := <-manualResult
	if knownErr == nil {
		require.Error(t, manual.err)
		require.Nil(t, manual.result)
		a := f.amounts(t, snapshot)
		require.Equal(t, "usage_known", a.Status)
		require.Equal(t, "1.00000000", a.RequestCost)
		result, err := f.applier().Apply(ctx, command)
		require.NoError(t, err)
		require.True(t, result.Applied)
		f.requireCharge(t, snapshot, "1.00000000", "0.10000000", 1)
	} else {
		require.ErrorIs(t, knownErr, service.ErrSubscriptionQuotaReceiptConflict)
		require.NoError(t, manual.err)
		require.NotNil(t, manual.result)
		a := f.amounts(t, snapshot)
		require.Equal(t, "no_charge", a.Status)
		require.Equal(t, "0.00000000", a.RequestCost)
		require.Equal(t, "0.00000000", a.Used)
		require.Zero(t, a.LedgerRows)
		require.Zero(t, a.LogRows)
	}
	guard()
}

func TestSubscriptionQuotaBilling_ClosingConvergesWithoutRewritingCardOrParentRevision(t *testing.T) {
	for _, resolution := range []string{"known_settled", "determinate_no_charge"} {
		t.Run(resolution, func(t *testing.T) {
			f := newSubscriptionQuotaBillingFixture(t)
			opened := f.rights.open(t, f.rights.userA, f.rights.input("open", "100", 0))
			snapshot := f.admit(t, f.rights.userA, f.keyA)
			cancelled := f.rights.cancel(t, opened.Term.ID, 1, uuid.NewString())
			require.Equal(t, "closing", cancelled.Term.LifecycleState)
			guard := f.ownershipGuard(t, snapshot)
			if resolution == "known_settled" {
				_, command := f.persist(t, snapshot, "1.00000000", "0.10000000")
				_, err := f.applier().Apply(context.Background(), command)
				require.NoError(t, err)
			} else {
				require.NoError(t, f.port.MarkNoCharge(context.Background(), snapshot, domain.SubscriptionQuotaNoChargeEvidence{Kind: "upstream_rejected", Reference: "http:400"}))
			}
			var state string
			var start, end time.Time
			var closed bool
			var parentRevision int64
			require.NoError(t, f.db.QueryRow(`SELECT t.lifecycle_state,t.starts_at,t.expires_at,t.closed_at IS NOT NULL,p.quota_revision FROM subscription_quota_terms t JOIN user_subscriptions p ON p.id=t.subscription_id WHERE t.id=$1`, snapshot.TermID).Scan(&state, &start, &end, &closed, &parentRevision))
			require.Equal(t, "closed", state)
			require.True(t, closed)
			require.True(t, opened.Term.StartsAt.Equal(start))
			require.True(t, opened.Term.ExpiresAt.Equal(end))
			require.Equal(t, 168*time.Hour, end.Sub(start))
			require.EqualValues(t, 2, parentRevision)
			guard()
		})
	}
	t.Run("natural_end_then_audited_resolution", func(t *testing.T) {
		f := newSubscriptionQuotaBillingFixture(t)
		_, old, next, boundary := f.boundaryCards(t, 2*time.Second)
		snapshot := f.admit(t, f.rights.userA, f.keyA)
		require.NoError(t, f.port.MarkReconcileRequired(context.Background(), snapshot, "usage_missing"))
		subscriptionQuotaBillingWaitForClock(t, f.db, boundary)
		maintainer, ok := f.rights.repo.(interface {
			MaintainTermLifecycles(context.Context, int) error
		})
		require.True(t, ok)
		require.NoError(t, maintainer.MaintainTermLifecycles(context.Background(), 100))
		var state string
		require.NoError(t, f.db.QueryRow(`SELECT lifecycle_state FROM subscription_quota_terms WHERE id=$1`, old).Scan(&state))
		require.Equal(t, "closing", state)
		guard := f.ownershipGuard(t, snapshot)
		_, err := f.resolver(t).ResolveRequest(context.Background(), f.rights.admin, snapshot.RequestRowID, service.SubscriptionQuotaResolveInput{ExpectedRequestRevision: quotaDomainInt(1), Resolution: "no_charge", EvidenceReference: "synthetic-audit/natural-expiry-no-charge", Reason: "verified after natural expiry"}, uuid.NewString())
		require.NoError(t, err)
		var duration float64
		var parentRevision int64
		require.NoError(t, f.db.QueryRow(`SELECT t.lifecycle_state,extract(epoch from t.expires_at-t.starts_at),p.quota_revision FROM subscription_quota_terms t JOIN user_subscriptions p ON p.id=t.subscription_id WHERE t.id=$1`, old).Scan(&state, &duration, &parentRevision))
		require.Equal(t, "closed", state)
		require.Equal(t, float64(168*60*60), duration)
		require.EqualValues(t, 2, parentRevision)
		guard()
		admitted := f.admit(t, f.rights.userA, f.keyA2)
		require.Equal(t, next, admitted.TermID)
	})
}

func TestSubscriptionQuotaBilling_NumericExtremesRemainExactThroughJSONBRecoveryAndStatistics(t *testing.T) {
	// Independent databases keep the maximum value from being added twice or
	// obscured by another request's counters. These are storage, not float, assertions.
	for _, amount := range []string{"999999999999.99999999", "0.00000001"} {
		t.Run(amount, func(t *testing.T) {
			f := newSubscriptionQuotaBillingFixture(t)
			const maximum = "999999999999.99999999"
			_, err := f.db.Exec(`UPDATE api_keys SET quota=$2::numeric,rate_limit_5h=$2::numeric,rate_limit_1d=$2::numeric,rate_limit_7d=$2::numeric WHERE id=$1`, f.keyA, maximum)
			require.NoError(t, err)
			_, err = f.db.Exec(`UPDATE accounts SET extra=extra || jsonb_build_object('quota_limit',$2::numeric,'quota_daily_limit',$2::numeric,'quota_weekly_limit',$2::numeric) WHERE id=$1`, f.account, maximum)
			require.NoError(t, err)
			f.rights.open(t, f.rights.userA, f.rights.input("open", maximum, 0))
			snapshot := f.admit(t, f.rights.userA, f.keyA)
			guard := f.ownershipGuard(t, snapshot)
			known, _ := f.knownWithLog(t, snapshot, amount, amount, func(log *service.UsageLog) {
				// Keep the legacy multiplier field in its normal supported range.
				// The storage boundary below supplies exact decimal components;
				// this case does not claim float pricing can represent the maximum.
				log.InputCost, log.TotalCost = 1, 1
				log.RateMultiplier = 1
			})
			var exactLog map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(known.UsageLogPayload, &exactLog))
			encodedAmount, err := json.Marshal(amount)
			require.NoError(t, err)
			exactLog["input_cost"], exactLog["total_cost"] = encodedAmount, encodedAmount
			known.UsageLogPayload, err = json.Marshal(exactLog)
			require.NoError(t, err)
			require.NoError(t, f.port.PersistKnownUsage(context.Background(), known))
			var receipt, logPayload []byte
			require.NoError(t, f.db.QueryRow(`SELECT billing_payload,usage_log_payload FROM subscription_quota_requests WHERE id=$1`, snapshot.RequestRowID).Scan(&receipt, &logPayload))
			decoded, err := service.DecodeSubscriptionQuotaUsageBillingReceipt(receipt)
			require.NoError(t, err)
			require.Equal(t, amount, decoded.SubscriptionTermCost.StringFixed(8))
			for _, counter := range []decimal.Decimal{decoded.SubscriptionTermCounters.APIKeyQuotaCost, decoded.SubscriptionTermCounters.APIKeyRateLimitCost, decoded.SubscriptionTermCounters.AccountQuotaCost} {
				require.Equal(t, amount, counter.StringFixed(8))
			}
			require.Equal(t, known.RequestFingerprint, decoded.RequestFingerprint)
			exact, err := service.SubscriptionQuotaUsageLogExactAmounts(logPayload)
			require.NoError(t, err)
			require.Equal(t, amount, exact["actual_cost"])
			require.Equal(t, amount, exact["account_stats_cost"])
			require.NoError(t, f.recovery(2*time.Second, 10).RunOnce(context.Background()))
			f.requireCharge(t, snapshot, amount, amount, 1)
			var actual, stats string
			require.NoError(t, f.db.QueryRow(`SELECT actual_cost::numeric(20,8)::text,account_stats_cost::numeric(20,8)::text FROM usage_logs WHERE subscription_quota_request_id=$1`, snapshot.RequestRowID).Scan(&actual, &stats))
			require.Equal(t, amount, actual)
			require.Equal(t, amount, stats)
			var recorded bool
			require.NoError(t, f.db.QueryRow(`SELECT usage_log_recorded_at IS NOT NULL FROM subscription_quota_requests WHERE id=$1`, snapshot.RequestRowID).Scan(&recorded))
			require.True(t, recorded)
			for _, column := range []string{"input_cost", "output_cost", "cache_creation_cost", "cache_read_cost", "total_cost", "actual_cost", "account_stats_cost"} {
				var precision, scale int
				require.NoError(t, f.db.QueryRow(`SELECT numeric_precision,numeric_scale FROM information_schema.columns WHERE table_schema='public' AND table_name='usage_logs' AND column_name=$1`, column).Scan(&precision, &scale))
				require.Equal(t, 22, precision)
				require.Equal(t, 10, scale, "existing GPT statistics retain their ten fractional digits")
			}
			aggregation := newDashboardAggregationRepositoryWithSQL(f.db)
			require.NoError(t, aggregation.AggregateRange(context.Background(), snapshot.PricingAt.Add(-time.Hour), snapshot.PricingAt.Add(time.Hour)))
			for _, table := range []string{"usage_dashboard_hourly", "usage_dashboard_daily"} {
				var aggregateActual, aggregateAccount string
				require.NoError(t, f.db.QueryRow(`SELECT actual_cost::numeric(20,8)::text,account_cost::numeric(20,8)::text FROM `+pq.QuoteIdentifier(table)).Scan(&aggregateActual, &aggregateAccount))
				require.Equal(t, amount, aggregateActual, table)
				require.Equal(t, amount, aggregateAccount, table)
			}
			// A synthetic next-day publication clock closes the log's day without
			// rewriting its durable creation time or any real application clock.
			require.NoError(t, aggregation.SyncGroupUsageRollups(context.Background(), service.GroupUsageTodayStart(snapshot.PricingAt.Add(24*time.Hour))))
			var groupActual string
			require.NoError(t, f.db.QueryRow(`SELECT actual_cost::numeric(20,8)::text FROM usage_group_daily_rollups WHERE group_id=$1 AND bucket_date=$2::date`, snapshot.GroupID, service.GroupUsageDate(snapshot.PricingAt)).Scan(&groupActual))
			require.Equal(t, amount, groupActual)
			guard()
			t.Logf("T41 exact stored receipt/ledger/Key/account/usage-log actual/account-stats/hourly/daily/group-rollup amount=%s; migration250 numeric(22,10)/aggregate numeric(30,10); original parent and foreign wallet/card rows unchanged", amount)
		})
	}
}

func TestSubscriptionQuotaBilling_MaintenanceScopeTimeoutDoesNotStarveIndependentExpiredCard(t *testing.T) {
	f := newSubscriptionQuotaBillingFixture(t)
	_, oldA, nextA, boundary := f.boundaryCards(t, -time.Second)
	endB := boundary.Add(-time.Second)
	startB := endB.Add(-domain.SubscriptionQuotaDuration)
	var parentB, termB int64
	require.NoError(t, f.db.QueryRow(`INSERT INTO user_subscriptions(user_id,group_id,starts_at,expires_at,status,quota_revision,assigned_by) VALUES($1,$2,$3,$4,'active',1,$5) RETURNING id`, f.rights.userB, f.rights.group, startB, endB, f.rights.admin).Scan(&parentB))
	data, err := json.Marshal(domain.SubscriptionQuotaSourceSnapshot{SchemaVersion: 1, QuotaUSD: "100.00000000", DurationHours: 168, QuotaSource: "admin"})
	require.NoError(t, err)
	require.NoError(t, f.db.QueryRow(`INSERT INTO subscription_quota_terms(subscription_id,user_id,group_id,term_no,quota_usd,starts_at,expires_at,lifecycle_state,source_kind,source_snapshot,created_by) VALUES($1,$2,$3,1,100,$4,$5,'active','admin',$6,$7) RETURNING id`, parentB, f.rights.userB, f.rights.group, startB, endB, data, f.rights.admin).Scan(&termB))
	_, err = f.db.Exec(`UPDATE subscription_quota_terms SET updated_at=clock_timestamp()-CASE WHEN user_id=$1 THEN INTERVAL '2 hours' ELSE INTERVAL '1 hour' END`, f.rights.userA)
	require.NoError(t, err)
	columns := subscriptionQuotaLegacyColumns(t, f.db)
	delete(columns, "subscription_quota_terms")
	before := subscriptionQuotaLegacyFingerprints(t, f.db, columns)
	var termIdentityBefore string
	identitySQL := `SELECT md5(jsonb_agg(to_jsonb(t)-'lifecycle_state'-'closed_at'-'revision'-'updated_at' ORDER BY id)::text) FROM subscription_quota_terms t`
	require.NoError(t, f.db.QueryRow(identitySQL).Scan(&termIdentityBefore))
	blocker, err := f.db.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = blocker.Rollback() })
	require.NoError(t, subscriptionQuotaScopeLock(context.Background(), blocker, f.rights.userA, f.rights.group))
	maintainer, ok := f.rights.repo.(interface {
		MaintainTermLifecycles(context.Context, int) error
	})
	require.True(t, ok)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	started := time.Now()
	err = maintainer.MaintainTermLifecycles(ctx, 100)
	require.Error(t, err, "the intentionally locked first scope reports its bounded failure")
	require.NoError(t, ctx.Err(), "a per-scope timeout must not cancel the maintenance root")
	require.GreaterOrEqual(t, time.Since(started), 4500*time.Millisecond)
	require.Less(t, time.Since(started), 10*time.Second)
	var stateA, stateB string
	require.NoError(t, f.db.QueryRow(`SELECT lifecycle_state FROM subscription_quota_terms WHERE id=$1`, oldA).Scan(&stateA))
	require.NoError(t, f.db.QueryRow(`SELECT lifecycle_state FROM subscription_quota_terms WHERE id=$1`, termB).Scan(&stateB))
	require.Equal(t, "active", stateA, "the blocked scope must remain unchanged")
	require.Equal(t, "closed", stateB, "the later independent expired scope must still converge in the same invocation")
	require.NoError(t, blocker.Rollback())
	require.NoError(t, maintainer.MaintainTermLifecycles(ctx, 100))
	require.NoError(t, f.db.QueryRow(`SELECT lifecycle_state FROM subscription_quota_terms WHERE id=$1`, oldA).Scan(&stateA))
	require.Equal(t, "closed", stateA)
	require.NoError(t, f.db.QueryRow(`SELECT lifecycle_state FROM subscription_quota_terms WHERE id=$1`, nextA).Scan(&stateA))
	require.Equal(t, "active", stateA)
	var termIdentityAfter string
	require.NoError(t, f.db.QueryRow(identitySQL).Scan(&termIdentityAfter))
	require.Equal(t, termIdentityBefore, termIdentityAfter, "maintenance cannot change issued 168h timestamps, sources, quotas, used amounts or card identity")
	require.Equal(t, before, subscriptionQuotaLegacyFingerprints(t, f.db, columns), "maintenance changes no parent schedule/revision, request, money, GPT, statistics, Key or account rows")
	t.Logf("maintenance first scope timed out after its 5s child bound; independent expired term=%d closed before first scope release; all original times/parent revisions/financial rows unchanged", termB)
}

func TestSubscriptionQuotaBilling_VerifiedResolutionUsesRealFrozenPricingAfterExpiryAndPriceChanges(t *testing.T) {
	f := newSubscriptionQuotaBillingFixture(t)
	_, old, next, boundary := f.boundaryCards(t, 3*time.Second)
	cfg := &config.Config{}
	billing := service.NewBillingService(cfg, nil)
	gateway := service.NewGatewayService(nil, nil, nil, nil, nil, nil, nil, nil, cfg, nil, nil, billing, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	group := &service.Group{ID: f.rights.group, Platform: service.PlatformAnthropic, SubscriptionType: "subscription", SubscriptionQuotaMode: domain.SubscriptionQuotaModeTerm, RateMultiplier: 0.8}
	key := &service.APIKey{ID: f.keyA, UserID: f.rights.userA, GroupID: quotaDomainInt(f.rights.group), Group: group}
	accountRate := 0.25
	account := &service.Account{ID: f.account, Platform: service.PlatformAnthropic, Type: service.AccountTypeAPIKey, RateMultiplier: &accountRate}
	factory, err := gateway.PrepareSubscriptionQuotaPricing(context.Background(), service.SubscriptionQuotaPricingInput{APIKey: key, Account: account, RequestedModel: "claude-sonnet-4", UpstreamModel: "claude-sonnet-4"})
	require.NoError(t, err)
	params := f.admission(f.rights.userA, f.keyA)
	params.FreezePricing = factory
	snapshot, err := f.port.AdmitRequest(context.Background(), params)
	require.NoError(t, err)
	require.Equal(t, old, snapshot.TermID)
	verified := service.SubscriptionQuotaVerifiedUsage{RequestedModel: "claude-sonnet-4", UpstreamModel: "claude-sonnet-4", InputTokens: 100, OutputTokens: 10, CacheCreation5mTokens: 3, CacheCreation1hTokens: 4, CacheReadTokens: 5}
	expected, err := (&service.GatewayService{}).CalculateSubscriptionQuotaVerifiedCost(snapshot.PricingSnapshot, verified, snapshot.PricingAt)
	require.NoError(t, err)
	cost := decimal.NewFromFloat(expected.Cost.ActualCost).Round(8).StringFixed(8)
	accountCost := decimal.NewFromFloat(expected.AccountQuotaCost).Round(8).StringFixed(8)
	require.True(t, decimal.RequireFromString(cost).IsPositive())
	require.NoError(t, f.port.MarkReconcileRequired(context.Background(), snapshot, "provider_usage_late"))
	group.RateMultiplier, accountRate = 99, 99
	_, err = f.db.Exec(`UPDATE groups SET rate_multiplier=99 WHERE id=$1`, f.rights.group)
	require.NoError(t, err)
	_, err = f.db.Exec(`UPDATE subscription_plans SET term_quota_usd=999 WHERE id=$1`, f.rights.plan)
	require.NoError(t, err)
	subscriptionQuotaBillingWaitForClock(t, f.db, boundary)
	guard := f.ownershipGuard(t, snapshot)
	resolutionRepo, ok := f.rights.repo.(service.SubscriptionQuotaResolutionRepository)
	require.True(t, ok)
	resolver := service.NewSubscriptionQuotaResolutionService(resolutionRepo, &service.GatewayService{})
	in := service.SubscriptionQuotaResolveInput{ExpectedRequestRevision: quotaDomainInt(1), Resolution: "verified_usage", EvidenceReference: "synthetic-provider-audit/late-real-token-counts", Reason: "verified provider usage with the original immutable pricing snapshot", VerifiedUsage: &verified}
	idempotencyKey := uuid.NewString()
	resolved, err := resolver.ResolveRequest(context.Background(), f.rights.admin, snapshot.RequestRowID, in, idempotencyKey)
	require.NoError(t, err)
	require.False(t, resolved.Replayed)
	require.Equal(t, "usage_known", resolved.Request.Status)
	require.NotNil(t, resolved.Request.ActualCostUSD)
	require.Equal(t, cost, *resolved.Request.ActualCostUSD)
	var receipt, logPayload []byte
	require.NoError(t, f.db.QueryRow(`SELECT billing_payload,usage_log_payload FROM subscription_quota_requests WHERE id=$1`, snapshot.RequestRowID).Scan(&receipt, &logPayload))
	command, err := service.DecodeSubscriptionQuotaUsageBillingReceipt(receipt)
	require.NoError(t, err)
	require.Equal(t, snapshot.PricingSnapshotHash, command.SubscriptionTermSnapshot.PricingSnapshotHash)
	require.True(t, snapshot.PricingAt.Equal(command.SubscriptionTermSnapshot.PricingAt))
	require.Equal(t, old, command.SubscriptionTermSnapshot.TermID)
	require.Equal(t, cost, command.SubscriptionTermCost.StringFixed(8))
	applied, err := f.applier().Apply(context.Background(), command)
	require.NoError(t, err)
	require.True(t, applied.Applied)
	log, err := service.DecodeSubscriptionQuotaUsageLogPayload(logPayload)
	require.NoError(t, err)
	_, err = NewUsageLogRepository(nil, f.db).Create(context.Background(), log)
	require.NoError(t, err)
	require.NoError(t, f.port.MarkUsageLogRecorded(context.Background(), snapshot.RequestRowID, command.RequestFingerprint))
	f.requireCharge(t, snapshot, cost, accountCost, 1)
	var nextUsed, oldState string
	require.NoError(t, f.db.QueryRow(`SELECT used_usd::text FROM subscription_quota_terms WHERE id=$1`, next).Scan(&nextUsed))
	require.Equal(t, "0.00000000", nextUsed)
	require.NoError(t, f.db.QueryRow(`SELECT lifecycle_state FROM subscription_quota_terms WHERE id=$1`, old).Scan(&oldState))
	require.Equal(t, "closed", oldState)
	columns := subscriptionQuotaLegacyColumns(t, f.db)
	beforeReplay := subscriptionQuotaLegacyFingerprints(t, f.db, columns)
	replay, err := resolver.ResolveRequest(context.Background(), f.rights.admin, snapshot.RequestRowID, in, idempotencyKey)
	require.NoError(t, err)
	require.True(t, replay.Replayed)
	require.Equal(t, "usage_known", replay.Request.Status, "replay returns the original audited operation result even after normal settlement")
	require.Equal(t, beforeReplay, subscriptionQuotaLegacyFingerprints(t, f.db, columns))
	guard()
	t.Logf("verified_usage used actual frozen Gateway pricing after expiry and mutable rate/plan changes: cost=%s account=%s original_term=%d next_term=%d; late resolution -> durable usage_known -> one normal Apply and log", cost, accountCost, old, next)
}

func TestSubscriptionQuotaBilling_UsageCostWidthUpgradePreservesHistoricalTenDecimalRowsAndTriggers(t *testing.T) {
	db := newUpgrade026Database(t)
	names, err := fs.Glob(migrations.FS, "*.sql")
	require.NoError(t, err)
	beforeWidth := make(fstest.MapFS)
	for _, name := range names {
		digits := strings.IndexFunc(name, func(r rune) bool { return r < '0' || r > '9' })
		require.Positive(t, digits)
		number, err := strconv.Atoi(name[:digits])
		require.NoError(t, err)
		if number >= 250 {
			continue
		}
		body, err := migrations.FS.ReadFile(name)
		require.NoError(t, err)
		beforeWidth[name] = &fstest.MapFile{Data: body}
	}
	require.NoError(t, applyMigrationsFS(context.Background(), db, beforeWidth))
	group, user, _ := seedUpgrade026FinancialFixture(t, db)
	var key, account, logID int64
	require.NoError(t, db.QueryRow(`SELECT id FROM api_keys WHERE user_id=$1 AND group_id=$2 ORDER BY id LIMIT 1`, user, group).Scan(&key))
	require.NoError(t, db.QueryRow(`INSERT INTO accounts(name,platform,type,credentials) VALUES('Synthetic historical width account','anthropic','apikey','{}') RETURNING id`).Scan(&account))
	require.NoError(t, db.QueryRow(`INSERT INTO usage_logs(user_id,api_key_id,account_id,group_id,model,input_cost,output_cost,cache_creation_cost,cache_read_cost,total_cost,actual_cost,account_stats_cost) VALUES($1,$2,$3,$4,'synthetic-existing-GPT-costs',0.0000000001,0.1234567891,0.2345678912,0.3456789123,0.4567891234,0.5678912345,0.6789123456) RETURNING id`, user, key, account, group).Scan(&logID))
	columns := subscriptionQuotaLegacyColumns(t, db)
	before := subscriptionQuotaLegacyFingerprints(t, db, columns)
	triggerQuery := `SELECT string_agg(pg_get_triggerdef(oid),'|' ORDER BY tgname) FROM pg_trigger WHERE tgrelid='usage_logs'::regclass AND NOT tgisinternal`
	var triggersBefore, triggersAfter string
	require.NoError(t, db.QueryRow(triggerQuery).Scan(&triggersBefore))
	require.NoError(t, ApplyMigrations(context.Background(), db))
	require.Equal(t, before, subscriptionQuotaLegacyFingerprints(t, db, columns), "widening statistics must preserve every old ten-decimal value and every business row")
	require.NoError(t, db.QueryRow(triggerQuery).Scan(&triggersAfter))
	require.Equal(t, triggersBefore, triggersAfter, "column-dependent invalidation triggers must be restored without weakening old GPT rollup behavior")
	var input string
	require.NoError(t, db.QueryRow(`SELECT input_cost::text FROM usage_logs WHERE id=$1`, logID).Scan(&input))
	require.Equal(t, "0.0000000001", input)
	require.NoError(t, ApplyMigrations(context.Background(), db))
	require.Equal(t, before, subscriptionQuotaLegacyFingerprints(t, db, columns), "re-running the upgrade is inert")
	repo := NewSubscriptionQuotaRepository(db)
	ready, ok := repo.(interface{ CheckBillingReadiness(context.Context) error })
	require.True(t, ok)
	require.NoError(t, ready.CheckBillingReadiness(context.Background()))
	cfg := &config.Config{RunMode: config.RunModeStandard, SubscriptionQuota: config.SubscriptionQuotaConfig{AdmissionEnabled: true, EnabledGroupIDs: []int64{group}}}
	_, err = service.ProvideSubscriptionQuotaService(repo, cfg)
	require.NoError(t, err, "startup capability check must exercise the actual ready schema and receipt/log codecs")
	_, err = db.Exec(`DELETE FROM schema_migrations WHERE filename='250_subscription_quota_usage_cost_width.sql'`)
	require.NoError(t, err)
	require.Error(t, ready.CheckBillingReadiness(context.Background()))
	_, err = service.ProvideSubscriptionQuotaService(repo, cfg)
	require.Error(t, err, "missing durable 250 evidence must fail admission-enabled startup even when similarly shaped columns exist")
	require.Equal(t, before, subscriptionQuotaLegacyFingerprints(t, db, columns), "readiness checks and their failures are strictly read-only")
	cfg.SubscriptionQuota.AdmissionEnabled = false
	_, err = service.ProvideSubscriptionQuotaService(repo, cfg)
	require.NoError(t, err, "the issue/admission-off recovery-compatible construction must not require activating new admission")
}

// A synthetic old immutable admission models a process that lost contact after
// the send. The fixture never changes an already issued card/request timestamp.
func (f *subscriptionQuotaBillingFixture) historicalAdmission(t *testing.T, at time.Time) *domain.SubscriptionQuotaBillingSnapshot {
	t.Helper()
	starts := at.Add(-time.Hour)
	expires := starts.Add(domain.SubscriptionQuotaDuration)
	var parent, term int64
	require.NoError(t, f.db.QueryRow(`INSERT INTO user_subscriptions(user_id,group_id,starts_at,expires_at,status,quota_revision,assigned_by) VALUES($1,$2,$3,$4,'active',1,$5) RETURNING id`, f.rights.userA, f.rights.group, starts, expires, f.rights.admin).Scan(&parent))
	source, err := json.Marshal(domain.SubscriptionQuotaSourceSnapshot{SchemaVersion: 1, QuotaUSD: "100.00000000", DurationHours: 168, QuotaSource: "admin"})
	require.NoError(t, err)
	require.NoError(t, f.db.QueryRow(`INSERT INTO subscription_quota_terms(subscription_id,user_id,group_id,term_no,quota_usd,starts_at,expires_at,lifecycle_state,source_kind,source_snapshot,created_by) VALUES($1,$2,$3,1,100,$4,$5,'closing','admin',$6,$7) RETURNING id`, parent, f.rights.userA, f.rights.group, starts, expires, source, f.rights.admin).Scan(&term))
	pricing, err := f.admission(f.rights.userA, f.keyA).FreezePricing(at)
	require.NoError(t, err)
	s := &domain.SubscriptionQuotaBillingSnapshot{SchemaVersion: 1, RequestID: service.NewSubscriptionQuotaAdmissionRequestID(), UserID: f.rights.userA, APIKeyID: f.keyA, GroupID: f.rights.group, SubscriptionID: parent, TermID: term, AccountID: f.account, AccountType: service.AccountTypeAPIKey, KeyQuotaTracked: true, KeyRateLimitTracked: true, AccountQuotaTracked: true, AdmittedAt: at, PricingAt: at, PricingSnapshot: pricing}
	envelope, hash, err := domain.MarshalSubscriptionQuotaPricingEnvelope(*s)
	require.NoError(t, err)
	s.PricingSnapshotHash = hash
	require.NoError(t, f.db.QueryRow(`INSERT INTO subscription_quota_requests(request_id,api_key_id,subscription_id,term_id,user_id,group_id,admitted_at,pricing_at,pricing_snapshot,status,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$7,$8,'admitted',$7,$7) RETURNING id`, s.RequestID, s.APIKeyID, parent, term, s.UserID, s.GroupID, at, envelope).Scan(&s.RequestRowID))
	return s
}

func TestSubscriptionQuotaBilling_LateHistoricalPublicationPreservesPurgedAggregateBaseAndScheduledRange(t *testing.T) {
	for _, sameUser := range []bool{false, true} {
		name := "new_active_user"
		if sameUser {
			name = "already_counted_user"
		}
		t.Run(name, func(t *testing.T) {
			f := newSubscriptionQuotaBillingFixture(t)
			var now time.Time
			require.NoError(t, f.db.QueryRow(`SELECT clock_timestamp()`).Scan(&now))
			at := now.Add(-30 * 24 * time.Hour).UTC().Truncate(time.Hour).Add(10 * time.Minute)
			snapshot := f.historicalAdmission(t, at)
			baseUser, baseKey := f.rights.userB, f.keyB
			if sameUser {
				baseUser, baseKey = f.rights.userA, f.keyA
			}
			_, err := f.db.Exec(`INSERT INTO usage_logs(user_id,api_key_id,account_id,group_id,request_id,model,input_tokens,output_tokens,total_cost,actual_cost,account_stats_cost,account_rate_multiplier,created_at) VALUES($1,$2,$3,$4,'synthetic-old-published-base','synthetic-no-upstream',20,10,2,3.25,2.75,1,$5)`, baseUser, baseKey, f.account, f.rights.group, at)
			require.NoError(t, err)
			aggregation := newDashboardAggregationRepositoryWithSQL(f.db)
			require.NoError(t, aggregation.AggregateRange(context.Background(), at.Add(-time.Hour), at.Add(time.Hour)))
			require.NoError(t, aggregation.UpdateAggregationWatermark(context.Background(), now))
			cleanup := NewUsageCleanupRepository(nil, f.db)
			deleted, err := cleanup.DeleteUsageLogsBatch(context.Background(), service.UsageCleanupFilters{StartTime: at.Add(-time.Hour), EndTime: at.Add(time.Hour), UserID: quotaDomainInt(baseUser)}, 100)
			require.NoError(t, err)
			require.EqualValues(t, 1, deleted)
			var rawCount int
			require.NoError(t, f.db.QueryRow(`SELECT count(*) FROM usage_logs`).Scan(&rawCount))
			require.Zero(t, rawCount, "the previously published base now exists only in retained aggregates")
			known, _ := f.persist(t, snapshot, "0.50000000", "0.10000000")
			guard := f.ownershipGuard(t, snapshot)
			cfg := &config.Config{RunMode: config.RunModeStandard, SubscriptionQuota: config.SubscriptionQuotaConfig{RecoveryEnabled: true, RecoveryBatchSize: 10, RecoveryItemTimeout: 5 * time.Second}, DashboardAgg: config.DashboardAggregationConfig{Enabled: true, LookbackSeconds: 300}}
			worker := service.NewSubscriptionQuotaRecoveryService(f.port, f.applier(), NewUsageLogRepository(nil, f.db), cfg)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			start := make(chan struct{})
			outcomes := make(chan error, 2)
			go func() { <-start; outcomes <- worker.RunOnce(ctx) }()
			go func() {
				<-start
				// This is the normal watermark/lookback scheduled range. An explicit
				// forced historical RecomputeRange has different pre-existing semantics.
				outcomes <- aggregation.AggregateRange(ctx, now.Add(-5*time.Minute), now.Add(time.Hour))
			}()
			close(start)
			for i := 0; i < 2; i++ {
				require.NoError(t, <-outcomes)
			}
			checkAggregates := func() {
				t.Helper()
				for _, bucket := range []struct{ table, where string }{
					{"usage_dashboard_hourly", "bucket_start=date_trunc('hour',$1::timestamptz)"},
					{"usage_dashboard_daily", "bucket_date=($1::timestamptz AT TIME ZONE 'UTC')::date"},
				} {
					var total, actual, account string
					var requests, input, output, active int64
					require.NoError(t, f.db.QueryRow(`SELECT total_cost::text,actual_cost::text,account_cost::text,total_requests,input_tokens,output_tokens,active_users FROM `+pq.QuoteIdentifier(bucket.table)+` WHERE `+bucket.where, at).Scan(&total, &actual, &account, &requests, &input, &output, &active))
					require.Equal(t, "2.5000000000", total, bucket.table)
					require.Equal(t, "3.7500000000", actual, bucket.table)
					require.Equal(t, "2.8500000000", account, bucket.table)
					require.EqualValues(t, 2, requests)
					require.EqualValues(t, 30, input)
					require.EqualValues(t, 15, output)
					expectedActive := int64(2)
					if sameUser {
						expectedActive = 1
					}
					require.Equal(t, expectedActive, active, "active-user contribution is deduplicated independently of raw history retention")
				}
			}
			checkAggregates()
			staleLog, err := service.DecodeSubscriptionQuotaUsageLogPayload(known.UsageLogPayload)
			require.NoError(t, err)
			_, err = NewUsageLogRepository(nil, f.db).Create(ctx, staleLog)
			require.NoError(t, err)
			require.NoError(t, worker.RunOnce(ctx))
			checkAggregates()
			f.requireCharge(t, snapshot, "0.50000000", "0.10000000", 1)
			guard()
			t.Logf("historical raw base purged; late term publication retained hourly/day base and added one exact contribution outside 5m scheduled lookback; same_user=%t; replay adds zero", sameUser)
		})
	}
}

func TestSubscriptionQuotaBilling_AllCleanupPathsProtectUnacknowledgedLogsAcrossCollidingPartitions(t *testing.T) {
	for _, path := range []string{"manual_batch", "scheduled_batch", "scheduled_partition_drop"} {
		t.Run(path, func(t *testing.T) {
			f := newSubscriptionQuotaBillingFixture(t)
			f.rights.open(t, f.rights.userA, f.rights.input("open", "100", 0))
			snapshot := f.admit(t, f.rights.userA, f.keyA)
			known, command := f.persist(t, snapshot, "0.50000000", "0.10000000")
			_, err := f.applier().Apply(context.Background(), command)
			require.NoError(t, err)
			month := time.Date(snapshot.AdmittedAt.Year(), snapshot.AdmittedAt.Month(), 1, 0, 0, 0, 0, time.UTC)
			previous, cutoff := month.AddDate(0, -1, 0), month.AddDate(0, 1, 0)
			// This fresh database intentionally models the supported pre-existing
			// monthly partition deployment shape. Each empty child has a physical
			// (0,1) first tuple, and no cross-partition id uniqueness is assumed.
			_, err = f.db.Exec(`ALTER TABLE usage_logs RENAME TO synthetic_i3_partition_template; CREATE TABLE usage_logs (LIKE synthetic_i3_partition_template INCLUDING DEFAULTS INCLUDING CONSTRAINTS) PARTITION BY RANGE(created_at)`)
			require.NoError(t, err)
			for _, from := range []time.Time{previous, month} {
				name := "usage_logs_" + from.Format("200601")
				_, err = f.db.Exec(`CREATE TABLE ` + pq.QuoteIdentifier(name) + ` PARTITION OF usage_logs FOR VALUES FROM (` + pq.QuoteLiteral(from.Format(time.RFC3339)) + `) TO (` + pq.QuoteLiteral(from.AddDate(0, 1, 0).Format(time.RFC3339)) + `)`)
				require.NoError(t, err)
			}
			_, err = f.db.Exec(`INSERT INTO usage_logs(id,user_id,api_key_id,account_id,group_id,request_id,model,actual_cost,created_at) VALUES(1001,$1,$2,$3,$4,'synthetic-removable-legacy-partition-row','synthetic-no-upstream',0.25,$5)`, f.rights.userB, f.keyB, f.account, f.rights.group, previous.Add(time.Hour))
			require.NoError(t, err)
			_, err = f.db.Exec(`INSERT INTO usage_logs(id,user_id,api_key_id,account_id,group_id,subscription_id,subscription_term_id,subscription_quota_request_id,request_id,model,input_tokens,output_tokens,actual_cost,created_at) VALUES(1001,$1,$2,$3,$4,$5,$6,$7,$8,$9,10,5,0.5,$10)`, snapshot.UserID, snapshot.APIKeyID, snapshot.AccountID, snapshot.GroupID, snapshot.SubscriptionID, snapshot.TermID, snapshot.RequestRowID, snapshot.RequestID, command.Model, snapshot.AdmittedAt)
			require.NoError(t, err)
			var legacyCTID, protectedCTID string
			require.NoError(t, f.db.QueryRow(`SELECT ctid::text FROM usage_logs WHERE request_id='synthetic-removable-legacy-partition-row'`).Scan(&legacyCTID))
			require.NoError(t, f.db.QueryRow(`SELECT ctid::text FROM usage_logs WHERE subscription_quota_request_id=$1`, snapshot.RequestRowID).Scan(&protectedCTID))
			require.Equal(t, legacyCTID, protectedCTID, "the fixture must prove real cross-partition ctid collision rather than assuming it")
			var protectedBefore, protectedAfter string
			require.NoError(t, f.db.QueryRow(`SELECT to_jsonb(l)::text FROM usage_logs l WHERE subscription_quota_request_id=$1`, snapshot.RequestRowID).Scan(&protectedBefore))
			columns := subscriptionQuotaLegacyColumns(t, f.db)
			for table := range columns {
				if strings.HasPrefix(table, "usage_logs") || table == "usage_group_rollup_state" || table == "usage_group_daily_rollups" {
					delete(columns, table)
				}
			}
			before := subscriptionQuotaLegacyFingerprints(t, f.db, columns)
			switch path {
			case "manual_batch":
				deleted, err := NewUsageCleanupRepository(nil, f.db).DeleteUsageLogsBatch(context.Background(), service.UsageCleanupFilters{StartTime: previous, EndTime: cutoff}, 100)
				require.NoError(t, err)
				require.EqualValues(t, 1, deleted)
			case "scheduled_batch":
				deleted, err := cleanupUsageLogsBatchWithRollupInvalidation(context.Background(), f.db, cutoff)
				require.NoError(t, err)
				require.EqualValues(t, 1, deleted)
			case "scheduled_partition_drop":
				require.NoError(t, NewDashboardAggregationRepository(f.db).CleanupUsageLogs(context.Background(), cutoff))
				var removed bool
				require.NoError(t, f.db.QueryRow(`SELECT to_regclass($1) IS NULL`, "usage_logs_"+previous.Format("200601")).Scan(&removed))
				require.True(t, removed, "ordinary unprotected historical partitions retain the existing DROP fast path")
			}
			require.NoError(t, f.db.QueryRow(`SELECT to_jsonb(l)::text FROM usage_logs l WHERE subscription_quota_request_id=$1`, snapshot.RequestRowID).Scan(&protectedAfter))
			require.Equal(t, protectedBefore, protectedAfter, "another child's selected ctid or id cannot delete an unacknowledged settled term log")
			var remaining int
			require.NoError(t, f.db.QueryRow(`SELECT count(*) FROM usage_logs`).Scan(&remaining))
			require.Equal(t, 1, remaining)
			require.Equal(t, before, subscriptionQuotaLegacyFingerprints(t, f.db, columns), "cleanup cannot change requests, receipts, ledger, card totals, parents, Keys/accounts, native windows or GPT money")
			require.NoError(t, f.port.MarkUsageLogRecorded(context.Background(), snapshot.RequestRowID, known.RequestFingerprint))
			require.NoError(t, NewDashboardAggregationRepository(f.db).CleanupUsageLogs(context.Background(), cutoff))
			var removed bool
			require.NoError(t, f.db.QueryRow(`SELECT to_regclass($1) IS NULL`, "usage_logs_"+month.Format("200601")).Scan(&removed))
			require.True(t, removed, "the protected partition becomes normally droppable after the durable acknowledgement")
			stale, err := service.DecodeSubscriptionQuotaUsageLogPayload(known.UsageLogPayload)
			require.NoError(t, err)
			_, err = NewUsageLogRepository(nil, f.db).Create(context.Background(), stale)
			require.NoError(t, err, "a stale reader must no-op after acknowledgement even when retention dropped the old partition")
			require.NoError(t, f.port.MarkUsageLogRecorded(context.Background(), snapshot.RequestRowID, known.RequestFingerprint))
			require.NoError(t, f.db.QueryRow(`SELECT count(*) FROM usage_logs`).Scan(&remaining))
			require.Zero(t, remaining)
			f.requireCharge(t, snapshot, "0.50000000", "0.10000000", 1)
			t.Logf("cleanup path=%s, real child ctid collision=%s and id=1001; unack protected, ack permits original DROP, stale publication creates zero rows", path, protectedCTID)
		})
	}
}

func TestSubscriptionQuotaBilling_OneWalletFailureNeverBlocksOrChargesTheOther(t *testing.T) {
	for _, scenario := range []string{"gpt_zero_claude_positive", "claude_exhausted_gpt_positive", "claude_expired_gpt_positive", "claude_cancelled_gpt_positive", "gpt_expired_claude_positive", "gpt_terminated_claude_positive"} {
		t.Run(scenario, func(t *testing.T) {
			f := newSubscriptionQuotaBillingFixture(t)
			base := "100"
			if scenario == "gpt_zero_claude_positive" {
				base = "0"
			}
			var g *subscriptionQuotaGPTBillingFixture
			if scenario == "gpt_expired_claude_positive" {
				g = f.gptWindow(t, base, -31*24*time.Hour, 30*24*time.Hour)
			} else {
				g = f.currentGPT(t, base)
			}
			var opened *domain.SubscriptionQuotaMutationResult
			if scenario == "claude_expired_gpt_positive" {
				f.boundaryCards(t, -169*time.Hour)
			} else {
				quota := "100"
				if scenario == "claude_exhausted_gpt_positive" {
					quota = "1"
				}
				opened = f.rights.open(t, f.rights.userA, f.rights.input("open", quota, 0))
			}
			if scenario == "gpt_terminated_claude_positive" {
				protected := make(map[string][]string)
				for table, names := range subscriptionQuotaLegacyColumns(t, f.db) {
					if strings.HasPrefix(table, "subscription_quota_") || table == "user_subscriptions" || table == "api_keys" || table == "users" {
						protected[table] = names
					}
				}
				beforeTermination := subscriptionQuotaLegacyFingerprints(t, f.db, protected)
				terminated, err := service.NewCarpoolService(g.repo).Terminate(context.Background(), g.term, f.rights.admin, service.CarpoolTerminateInput{Reason: "synthetic independent-wallet T16 termination"}, uuid.NewString())
				require.NoError(t, err)
				require.Equal(t, "terminated", terminated.Status)
				var cycleStatus string
				require.NoError(t, f.db.QueryRow(`SELECT state FROM carpool_cycles WHERE id=$1`, g.cycle).Scan(&cycleStatus))
				require.Equal(t, "closing", cycleStatus)
				require.Equal(t, beforeTermination, subscriptionQuotaLegacyFingerprints(t, f.db, protected), "terminating GPT must preserve the already-issued Claude parent/card, every Key and ordinary balances")
			}
			if scenario == "claude_cancelled_gpt_positive" {
				f.rights.cancel(t, opened.Term.ID, 1, uuid.NewString())
			}
			if scenario == "claude_exhausted_gpt_positive" {
				s := f.admit(t, f.rights.userA, f.keyA)
				_, cmd := f.persist(t, s, "1.00000000", "0.10000000")
				_, err := f.applier().Apply(context.Background(), cmd)
				require.NoError(t, err)
			}
			columns := subscriptionQuotaLegacyColumns(t, f.db)
			beforeFailure := subscriptionQuotaLegacyFingerprints(t, f.db, columns)
			if strings.HasPrefix(scenario, "gpt_") {
				_, err := g.repo.Admit(context.Background(), f.rights.userA, g.key, g.group, "carpool:i3:"+uuid.NewString(), time.Time{})
				if scenario == "gpt_zero_claude_positive" {
					require.ErrorIs(t, err, service.ErrCarpoolQuotaExhausted)
				} else {
					require.ErrorIs(t, err, service.ErrCarpoolUnavailable)
				}
				require.Equal(t, beforeFailure, subscriptionQuotaLegacyFingerprints(t, f.db, columns), "a rejected GPT request neither falls back to Claude nor mutates any wallet")
				s := f.admit(t, f.rights.userA, f.keyA)
				require.Equal(t, opened.Term.ID, s.TermID, "the original Claude card issued before GPT termination remains the consumption owner")
				guard := f.ownershipGuard(t, s)
				_, cmd := f.persist(t, s, "0.50000000", "0.05000000")
				_, err = f.applier().Apply(context.Background(), cmd)
				require.NoError(t, err)
				f.requireCharge(t, s, "0.50000000", "0.05000000", 1)
				guard()
			} else {
				_, err := f.port.AdmitRequest(context.Background(), f.admission(f.rights.userA, f.keyA2))
				switch scenario {
				case "claude_exhausted_gpt_positive":
					require.ErrorIs(t, err, service.ErrSubscriptionQuotaExhausted)
				case "claude_expired_gpt_positive":
					require.ErrorIs(t, err, service.ErrSubscriptionQuotaExpired)
				case "claude_cancelled_gpt_positive":
					require.ErrorIs(t, err, service.ErrSubscriptionQuotaCancelled)
				}
				require.Equal(t, beforeFailure, subscriptionQuotaLegacyFingerprints(t, f.db, columns), "a rejected Claude request neither falls back to GPT/balance/native windows nor writes a request")
				claudeColumns := make(map[string][]string)
				for table, names := range columns {
					if strings.HasPrefix(table, "subscription_quota_") || table == "user_subscriptions" || table == "users" {
						claudeColumns[table] = names
					}
				}
				beforeGPT := subscriptionQuotaLegacyFingerprints(t, f.db, claudeColumns)
				cmd := f.knownGPT(t, g, "0.50000000")
				_, err = f.applier().Apply(context.Background(), cmd)
				require.NoError(t, err)
				var remaining string
				require.NoError(t, f.db.QueryRow(`SELECT base_balance_usd::text FROM carpool_cycles WHERE id=$1`, g.cycle).Scan(&remaining))
				require.Equal(t, "99.50000000", remaining)
				require.Equal(t, beforeGPT, subscriptionQuotaLegacyFingerprints(t, f.db, claudeColumns), "the functioning GPT wallet settles independently of the Claude failure")
			}
			t.Logf("T14-T16 actual repository admission+known+Apply independence scenario=%s", scenario)
		})
	}
}

func TestSubscriptionQuotaBilling_GPTOpenAndRenewPreserveAlreadyIssuedClaudeCardAndKeys(t *testing.T) {
	f := newSubscriptionQuotaBillingFixture(t)
	_, err := f.db.Exec(`UPDATE users SET balance=0 WHERE id=$1`, f.rights.userB)
	require.NoError(t, err)
	card := f.rights.open(t, f.rights.userB, f.rights.input("open", "300", 0))
	var gptGroup, gptKey, plan int64
	require.NoError(t, f.db.QueryRow(`INSERT INTO groups(name,platform,subscription_type) VALUES('Synthetic independent GPT opening','openai','standard') RETURNING id`).Scan(&gptGroup))
	require.NoError(t, f.db.QueryRow(`INSERT INTO api_keys(user_id,group_id,key,name) VALUES($1,$2,'synthetic-i3-existing-claude-GPT-key','Synthetic separate GPT identity') RETURNING id`, f.rights.userB, gptGroup).Scan(&gptKey))
	require.NoError(t, f.db.QueryRow(`SELECT id FROM carpool_plans WHERE code='four_seat' AND enabled=TRUE ORDER BY version DESC,id DESC LIMIT 1`).Scan(&plan))
	protected := make(map[string][]string)
	for table, names := range subscriptionQuotaLegacyColumns(t, f.db) {
		if strings.HasPrefix(table, "subscription_quota_") || table == "user_subscriptions" || table == "api_keys" {
			protected[table] = names
		}
	}
	before := subscriptionQuotaLegacyFingerprints(t, f.db, protected)
	svc := service.NewCarpoolService(NewCarpoolRepository(f.db))
	opened, err := svc.Open(context.Background(), f.rights.userB, f.rights.admin, service.CarpoolOpenInput{GroupID: gptGroup, PlanID: plan, Mode: "new"}, uuid.NewString())
	require.NoError(t, err)
	require.Equal(t, before, subscriptionQuotaLegacyFingerprints(t, f.db, protected), "first GPT opening must preserve the existing issued Claude card, parent schedule and every Key")
	renewed, err := svc.Renew(context.Background(), opened.ID, f.rights.admin, service.CarpoolRenewInput{}, uuid.NewString())
	require.NoError(t, err)
	require.True(t, opened.ExpiresAt.Equal(renewed.StartsAt))
	require.Equal(t, before, subscriptionQuotaLegacyFingerprints(t, f.db, protected), "GPT renewal must preserve the existing issued Claude card, parent schedule and every Key")
	claude := f.admit(t, f.rights.userB, f.keyB)
	require.Equal(t, card.Term.ID, claude.TermID)
	_, err = NewCarpoolRepository(f.db).Admit(context.Background(), f.rights.userB, gptKey, gptGroup, "carpool:i3:"+uuid.NewString(), time.Time{})
	require.NoError(t, err)
	t.Logf("T18/T19 actual GPT first opening and renewal preserved Claude card=%d quota=300.00000000, original168h, parent and all Keys; both original identities still admit", card.Term.ID)
}

func TestSubscriptionQuotaBilling_LedgerMismatchHoldsOnlyOriginalCardWithoutRepairingMoney(t *testing.T) {
	for _, alreadySettled := range []bool{false, true} {
		name := "known_before_apply"
		if alreadySettled {
			name = "settled_archive_replay"
		}
		t.Run(name, func(t *testing.T) {
			f := newSubscriptionQuotaBillingFixture(t)
			opened := f.rights.open(t, f.rights.userA, f.rights.input("open", "100", 0))
			snapshot := f.admit(t, f.rights.userA, f.keyA)
			known, cmd := f.persist(t, snapshot, "1.00000000", "0.10000000")
			if alreadySettled {
				_, err := f.applier().Apply(context.Background(), cmd)
				require.NoError(t, err)
				_, err = f.db.Exec(`INSERT INTO usage_billing_dedup_archive(request_id,api_key_id,request_fingerprint,created_at) SELECT request_id,api_key_id,request_fingerprint,created_at FROM usage_billing_dedup WHERE request_id=$1 AND api_key_id=$2`, snapshot.RequestID, snapshot.APIKeyID)
				require.NoError(t, err)
				_, err = f.db.Exec(`DELETE FROM usage_billing_dedup WHERE request_id=$1 AND api_key_id=$2`, snapshot.RequestID, snapshot.APIKeyID)
				require.NoError(t, err)
			}
			// Fault injection changes only the synthetic materialized total. The
			// immutable ledger remains untouched; no test bypasses its trigger.
			_, err := f.db.Exec(`UPDATE subscription_quota_terms SET used_usd=2 WHERE id=$1`, snapshot.TermID)
			require.NoError(t, err)
			guard := f.ownershipGuard(t, snapshot)
			columns := subscriptionQuotaLegacyColumns(t, f.db)
			delete(columns, "subscription_quota_requests")
			before := subscriptionQuotaLegacyFingerprints(t, f.db, columns)
			metrics := service.GetSubscriptionQuotaMetricsSnapshot()
			_, err = f.applier().Apply(context.Background(), cmd)
			require.ErrorIs(t, err, service.ErrSubscriptionQuotaReceiptConflict)
			require.ErrorIs(t, err, service.ErrSubscriptionQuotaLedgerMismatch)
			require.Equal(t, metrics.LedgerMismatch+1, service.GetSubscriptionQuotaMetricsSnapshot().LedgerMismatch)
			require.Equal(t, before, subscriptionQuotaLegacyFingerprints(t, f.db, columns), "the hold must roll back its provisional hot dedup claim and cannot repair money, ledgers, counters or statistics")
			var status, category, cost, fingerprint string
			var payload []byte
			require.NoError(t, f.db.QueryRow(`SELECT status,last_error_category,actual_cost_usd::text,request_fingerprint,billing_payload FROM subscription_quota_requests WHERE id=$1`, snapshot.RequestRowID).Scan(&status, &category, &cost, &fingerprint, &payload))
			expectedStatus := "reconcile_required"
			if alreadySettled {
				expectedStatus = "settled"
			}
			require.Equal(t, expectedStatus, status)
			require.Equal(t, "ledger_mismatch", category)
			require.Equal(t, "1.00000000", cost)
			require.Equal(t, known.RequestFingerprint, fingerprint)
			require.JSONEq(t, string(known.BillingPayload), string(payload))
			require.Zero(t, f.amounts(t, snapshot).DedupRows)
			_, err = f.port.AdmitRequest(context.Background(), f.admission(f.rights.userA, f.keyA2))
			require.ErrorIs(t, err, service.ErrSubscriptionQuotaReconciliation, "a materialized-total mismatch pauses that current card even when the original request stays settled")
			changed, _ := f.known(t, snapshot, "2.00000000", "0.20000000")
			require.ErrorIs(t, f.port.PersistKnownUsage(context.Background(), changed), service.ErrSubscriptionQuotaReceiptConflict)
			require.NoError(t, f.db.QueryRow(`SELECT last_error_category FROM subscription_quota_requests WHERE id=$1`, snapshot.RequestRowID).Scan(&category))
			require.Equal(t, "ledger_mismatch", category, "a late receipt conflict cannot hide the durable card hold")
			guard()
			f.rights.cancel(t, opened.Term.ID, 1, uuid.NewString())
			next := f.rights.open(t, f.rights.userA, f.rights.input("open", "200", 2))
			admitted := f.admit(t, f.rights.userA, f.keyA2)
			require.Equal(t, next.Term.ID, admitted.TermID, "an old cancelled card's mismatch cannot pause a different new card")
			require.Equal(t, "0.00000000", f.amounts(t, admitted).Used)
			t.Logf("ledger mismatch=%s preserved original status=%s and payload; provisional hot dedup0, ledger/counters unchanged, only original card blocked, new card=%d admits", name, status, next.Term.ID)
		})
	}
}

func TestSubscriptionQuotaBilling_AccountTypeChangeWhileAdmissionWaitsRejectsStaleSelection(t *testing.T) {
	f := newSubscriptionQuotaBillingFixture(t)
	f.rights.open(t, f.rights.userA, f.rights.input("open", "100", 0))
	f.rights.open(t, f.rights.userB, f.rights.input("open", "300", 0))
	columns := subscriptionQuotaLegacyColumns(t, f.db)
	delete(columns, "accounts")
	before := subscriptionQuotaLegacyFingerprints(t, f.db, columns)
	var accountBefore, accountAfter string
	require.NoError(t, f.db.QueryRow(`SELECT (to_jsonb(a)-'type')::text FROM accounts a WHERE id=$1`, f.account).Scan(&accountBefore))
	blocker, err := f.db.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = blocker.Rollback() })
	_, err = blocker.Exec(`UPDATE accounts SET type='oauth' WHERE id=$1`, f.account)
	require.NoError(t, err)
	params := f.admission(f.rights.userA, f.keyA)
	factory := params.FreezePricing
	var factoryCalls atomic.Int64
	params.FreezePricing = func(at time.Time) (json.RawMessage, error) { factoryCalls.Add(1); return factory(at) }
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	type outcome struct {
		snapshot *domain.SubscriptionQuotaBillingSnapshot
		err      error
	}
	outcomes := make(chan outcome, 1)
	go func() { snapshot, err := f.port.AdmitRequest(ctx, params); outcomes <- outcome{snapshot, err} }()
	require.Eventually(t, func() bool {
		var waiting bool
		err := f.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%FROM accounts a%FOR SHARE%')`).Scan(&waiting)
		return err == nil && waiting
	}, 3*time.Second, 10*time.Millisecond, "actual admission must be waiting on the original selected account row")
	require.NoError(t, blocker.Commit())
	result := <-outcomes
	require.ErrorIs(t, result.err, service.ErrSubscriptionQuotaBillingUnavailable)
	require.Nil(t, result.snapshot)
	require.Zero(t, factoryCalls.Load(), "the stale account selection must reject before frozen pricing construction or any upstream intent")
	require.Equal(t, before, subscriptionQuotaLegacyFingerprints(t, f.db, columns), "changing the selected account type cannot produce an admission, ledger, dedup, usage log, counter, or other wallet write")
	require.NoError(t, f.db.QueryRow(`SELECT (to_jsonb(a)-'type')::text FROM accounts a WHERE id=$1`, f.account).Scan(&accountAfter))
	require.Equal(t, accountBefore, accountAfter, "only the explicit synthetic administrator type edit may have changed the account")
	fresh := f.admission(f.rights.userA, f.keyA)
	fresh.ExpectedAccountType = service.AccountTypeOAuth
	snapshot, err := f.port.AdmitRequest(ctx, fresh)
	require.NoError(t, err)
	require.Equal(t, service.AccountTypeOAuth, snapshot.AccountType)
	require.False(t, snapshot.AccountQuotaTracked, "a fresh matching OAuth selection cannot inherit the old apikey monetary tracking flag")
	t.Log("actual account row-lock wait observed; apikey→oauth stale selection rejected with zero durable effects, and freshly selected oauth froze its own untracked account-counter identity")
}
