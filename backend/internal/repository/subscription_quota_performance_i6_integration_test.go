//go:build integration

package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// This is a database scaling experiment, not a microbenchmark or an R0-image
// comparison. All measured requests use real admission, durable known receipts
// and Apply. Bulk cold history preserves SQL constraints, widths and exact
// ledger totals, but copies an opaque settled receipt for row-size realism;
// historical receipt replay/codec correctness is deliberately not measured.
type quotaI6PerfDistribution struct {
	Samples int     `json:"samples"`
	P50MS   float64 `json:"p50_ms"`
	P95MS   float64 `json:"p95_ms"`
	P99MS   float64 `json:"p99_ms"`
	MaxMS   float64 `json:"max_ms"`
}

type quotaI6PerfRun struct {
	ElapsedMS float64                 `json:"elapsed_ms"`
	GPT       quotaI6PerfDistribution `json:"gpt_apply"`
	Claude    quotaI6PerfDistribution `json:"claude_apply"`
	Scope     quotaI6PerfDistribution `json:"same_scope_wait"`
	Other     quotaI6PerfDistribution `json:"independent_scope_wait"`
	Errors    []string                `json:"errors"`
	LockWait  map[string]int          `json:"sampled_lock_waits"`
	Deadlocks int64                   `json:"deadlock_delta"`
	RPS       map[string]float64      `json:"completion_rps"`
	LaneMS    map[string]float64      `json:"lane_elapsed_ms"`
}

type quotaI6PerfScale struct {
	TargetHistory int                        `json:"target_bulk_history_rows"`
	Requests      int64                      `json:"actual_request_rows"`
	Ledger        int64                      `json:"actual_ledger_rows"`
	DatabaseBytes int64                      `json:"database_bytes"`
	Admit         quotaI6PerfDistribution    `json:"claude_admission"`
	Backlog       quotaI6PerfDistribution    `json:"backlog"`
	GPTOnly       quotaI6PerfRun             `json:"gpt_only_control"`
	Mixed         quotaI6PerfRun             `json:"gpt_and_claude"`
	Plans         map[string]json.RawMessage `json:"explain_analyze_buffers"`
}

func quotaI6PerfDistributionOf(values []time.Duration) quotaI6PerfDistribution {
	if len(values) == 0 {
		return quotaI6PerfDistribution{}
	}
	ordered := append([]time.Duration(nil), values...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	percentile := func(p float64) float64 {
		return float64(ordered[int(math.Ceil(float64(len(ordered))*p))-1]) / float64(time.Millisecond)
	}
	return quotaI6PerfDistribution{Samples: len(values), P50MS: percentile(.50), P95MS: percentile(.95), P99MS: percentile(.99), MaxMS: float64(ordered[len(ordered)-1]) / float64(time.Millisecond)}
}

func quotaI6PerfConnection(t *testing.T, fixture *sql.DB, label string) *sql.DB {
	t.Helper()
	var database string
	require.NoError(t, fixture.QueryRow(`SELECT current_database()`).Scan(&database))
	dsn, err := url.Parse(integrationDSN)
	require.NoError(t, err)
	dsn.Path = "/" + database
	query := dsn.Query()
	query.Set("application_name", "cwq-i6-"+label)
	dsn.RawQuery = query.Encode()
	db, err := sql.Open("postgres", dsn.String())
	require.NoError(t, err)
	db.SetMaxOpenConns(6)
	db.SetMaxIdleConns(6)
	require.NoError(t, db.Ping())
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	return db
}

func quotaI6PerfSeedHistory(t *testing.T, f *subscriptionQuotaBillingFixture, templateID, termID int64, from, to int) {
	t.Helper()
	if from == to {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	// No production DSN is consulted. These rows exist only in this fresh
	// TestMain-owned database and are never sent to recovery or a real provider.
	_, err := f.db.ExecContext(ctx, `WITH history AS (
 INSERT INTO subscription_quota_requests(request_id,api_key_id,subscription_id,term_id,user_id,group_id,admitted_at,pricing_at,pricing_snapshot,status,actual_cost_usd,receipt_version,request_fingerprint,billing_payload,usage_log_payload,settled_at,usage_log_recorded_at)
 SELECT 'i6-cold-history:'||n,api_key_id,subscription_id,term_id,user_id,group_id,admitted_at,pricing_at,pricing_snapshot,'settled',0.00000001,receipt_version,encode(sha256(convert_to(n::text,'UTF8')),'hex'),billing_payload,usage_log_payload,settled_at,settled_at
 FROM subscription_quota_requests CROSS JOIN generate_series($2::int+1,$3::int) n WHERE id=$1
 RETURNING id,term_id
 ) INSERT INTO subscription_quota_ledger(term_id,request_row_id,event_key,entry_kind,delta_used_usd)
 SELECT term_id,id,'i6-cold-history:'||id,'usage_debit',0.00000001 FROM history`, templateID, from, to)
	require.NoError(t, err)
	_, err = f.db.ExecContext(ctx, `UPDATE subscription_quota_terms SET used_usd=(SELECT sum(delta_used_usd) FROM subscription_quota_ledger WHERE term_id=$1) WHERE id=$1`, termID)
	require.NoError(t, err)
	_, err = f.db.ExecContext(ctx, `ANALYZE subscription_quota_requests; ANALYZE subscription_quota_ledger; ANALYZE subscription_quota_terms`)
	require.NoError(t, err)
}

func quotaI6PerfPrepare(t *testing.T, f *subscriptionQuotaBillingFixture, g *subscriptionQuotaGPTBillingFixture, count int, claude bool) ([]*service.UsageBillingCommand, []time.Duration) {
	t.Helper()
	commands := make([]*service.UsageBillingCommand, 0, count)
	admissions := make([]time.Duration, 0, count)
	for i := 0; i < count; i++ {
		if !claude {
			commands = append(commands, f.knownGPT(t, g, "0.00000001"))
			continue
		}
		start := time.Now()
		snapshot := f.admit(t, f.rights.userA, f.keyA)
		admissions = append(admissions, time.Since(start))
		_, cmd := f.persist(t, snapshot, "0.00000001", "0.00000000")
		commands = append(commands, cmd)
	}
	return commands, admissions
}

func quotaI6PerfRunApply(t *testing.T, f *subscriptionQuotaBillingFixture, gptDB, claudeDB, observer *sql.DB, gpt, claude []*service.UsageBillingCommand) quotaI6PerfRun {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var beforeDeadlocks int64
	require.NoError(t, observer.QueryRowContext(ctx, `SELECT deadlocks FROM pg_stat_database WHERE datname=current_database()`).Scan(&beforeDeadlocks))
	start := make(chan struct{})
	stop := make(chan struct{})
	var workers, observers sync.WaitGroup
	var mu sync.Mutex
	samples := map[string][]time.Duration{"gpt": {}, "claude": {}, "same": {}, "other": {}}
	out := quotaI6PerfRun{Errors: []string{}, LockWait: map[string]int{}, RPS: map[string]float64{}, LaneMS: map[string]float64{}}
	lastEnd := map[string]time.Time{}
	record := func(label string, elapsed time.Duration, err error) {
		mu.Lock()
		defer mu.Unlock()
		samples[label] = append(samples[label], elapsed)
		lastEnd[label] = time.Now()
		if err != nil {
			out.Errors = append(out.Errors, label+": "+err.Error())
		}
	}
	for _, lane := range []struct {
		label string
		db    *sql.DB
		cmds  []*service.UsageBillingCommand
	}{{"gpt", gptDB, gpt}, {"claude", claudeDB, claude}} {
		if len(lane.cmds) == 0 {
			continue
		}
		applier := ProvideUsageBillingRepository(nil, lane.db, NewCarpoolRepository(lane.db), NewSubscriptionQuotaRepository(lane.db))
		var next atomic.Int64
		for worker := 0; worker < 2; worker++ {
			workers.Add(1)
			go func() {
				defer workers.Done()
				<-start
				for {
					i := int(next.Add(1)) - 1
					if i >= len(lane.cmds) {
						return
					}
					began := time.Now()
					result, err := applier.Apply(ctx, lane.cmds[i])
					if err == nil && (result == nil || !result.Applied) {
						err = fmt.Errorf("new request did not apply")
					}
					record(lane.label, time.Since(began), err)
				}
			}()
		}
	}
	// Scope probes measure actual waiting for the same PostgreSQL advisory lock,
	// plus an independent user scope on the same database as a control.
	for _, probe := range []struct {
		label string
		user  int64
	}{{"same", f.rights.userA}, {"other", f.rights.userB}} {
		observers.Add(1)
		go func() {
			defer observers.Done()
			<-start
			for {
				select {
				case <-stop:
					return
				default:
				}
				tx, err := observer.BeginTx(ctx, nil)
				if err != nil {
					record(probe.label, 0, err)
					return
				}
				began := time.Now()
				err = subscriptionQuotaScopeLock(ctx, tx, probe.user, f.rights.group)
				record(probe.label, time.Since(began), err)
				_ = tx.Rollback()
				select {
				case <-stop:
					return
				case <-time.After(2 * time.Millisecond):
				}
			}
		}()
	}
	observers.Add(1)
	go func() {
		defer observers.Done()
		<-start
		for {
			select {
			case <-stop:
				return
			case <-time.After(5 * time.Millisecond):
			}
			rows, err := observer.QueryContext(ctx, `SELECT application_name,count(*) FROM pg_stat_activity WHERE datname=current_database() AND application_name LIKE 'cwq-i6-%' AND wait_event_type='Lock' GROUP BY application_name`)
			if err != nil {
				record("monitor", 0, err)
				return
			}
			for rows.Next() {
				var label string
				var count int
				if err := rows.Scan(&label, &count); err != nil {
					record("monitor", 0, err)
					continue
				}
				mu.Lock()
				out.LockWait[label] += count
				mu.Unlock()
			}
			_ = rows.Close()
		}
	}()
	began := time.Now()
	close(start)
	workers.Wait()
	elapsed := time.Since(began)
	close(stop)
	observers.Wait()
	out.ElapsedMS = float64(elapsed) / float64(time.Millisecond)
	out.GPT, out.Claude = quotaI6PerfDistributionOf(samples["gpt"]), quotaI6PerfDistributionOf(samples["claude"])
	out.Scope, out.Other = quotaI6PerfDistributionOf(samples["same"]), quotaI6PerfDistributionOf(samples["other"])
	for _, label := range []string{"gpt", "claude"} {
		if len(samples[label]) > 0 {
			laneElapsed := lastEnd[label].Sub(began)
			out.RPS[label] = float64(len(samples[label])) / laneElapsed.Seconds()
			out.LaneMS[label] = float64(laneElapsed) / float64(time.Millisecond)
		}
	}
	var afterDeadlocks int64
	require.NoError(t, observer.QueryRowContext(ctx, `SELECT deadlocks FROM pg_stat_database WHERE datname=current_database()`).Scan(&afterDeadlocks))
	out.Deadlocks = afterDeadlocks - beforeDeadlocks
	require.Empty(t, out.Errors)
	require.Zero(t, out.Deadlocks)
	return out
}

func TestSubscriptionQuotaPerformanceI6_HistoryScalingAndGPTControl(t *testing.T) {
	f := newSubscriptionQuotaBillingFixture(t)
	card := f.rights.open(t, f.rights.userA, f.rights.input("open", "1000000", 0))
	g := f.currentGPT(t, "1000000")
	template := f.admit(t, f.rights.userA, f.keyA)
	_, cmd := f.persist(t, template, "0.00000001", "0.00000000")
	_, err := f.applier().Apply(context.Background(), cmd)
	require.NoError(t, err)
	_, err = f.db.Exec(`UPDATE subscription_quota_requests SET usage_log_recorded_at=clock_timestamp() WHERE id=$1`, template.RequestRowID)
	require.NoError(t, err)
	gptDB := quotaI6PerfConnection(t, f.db, "gpt")
	claudeDB := quotaI6PerfConnection(t, f.db, "claude")
	observer := quotaI6PerfConnection(t, f.db, "observer")
	backlog := &subscriptionQuotaRepository{db: f.db}
	const samples = 96
	previous := 0
	for _, history := range []int{0, 10000, 100000, 300000} {
		quotaI6PerfSeedHistory(t, f, template.RequestRowID, card.Term.ID, previous, history)
		previous = history
		scale := quotaI6PerfScale{TargetHistory: history, Plans: map[string]json.RawMessage{}}
		require.NoError(t, f.db.QueryRow(`SELECT count(*),pg_database_size(current_database()) FROM subscription_quota_requests`).Scan(&scale.Requests, &scale.DatabaseBytes))
		require.NoError(t, f.db.QueryRow(`SELECT count(*) FROM subscription_quota_ledger`).Scan(&scale.Ledger))
		for name, statement := range map[string]string{
			"ledger_sum_in_apply":         `SELECT used_usd=COALESCE((SELECT SUM(delta_used_usd) FROM subscription_quota_ledger WHERE term_id=$1),0) FROM subscription_quota_terms WHERE id=$1`,
			"reconciliation_in_admission": `SELECT count(*) FROM subscription_quota_requests WHERE term_id=$1 AND (status='reconcile_required' OR last_error_category='ledger_mismatch')`,
		} {
			var plan []byte
			require.NoError(t, f.db.QueryRow(`EXPLAIN (ANALYZE,BUFFERS,FORMAT JSON) `+statement, card.Term.ID).Scan(&plan))
			scale.Plans[name] = plan
		}
		var backlogPlan []byte
		require.NoError(t, f.db.QueryRow(`EXPLAIN (ANALYZE,BUFFERS,FORMAT JSON) `+subscriptionQuotaBacklogSQL).Scan(&backlogPlan))
		scale.Plans["backlog"] = backlogPlan
		var durations []time.Duration
		for i := 0; i < 20; i++ {
			start := time.Now()
			_, err := backlog.BillingBacklog(context.Background())
			require.NoError(t, err)
			durations = append(durations, time.Since(start))
		}
		scale.Backlog = quotaI6PerfDistributionOf(durations)
		gptOnly, _ := quotaI6PerfPrepare(t, f, g, samples, false)
		scale.GPTOnly = quotaI6PerfRunApply(t, f, gptDB, claudeDB, observer, gptOnly, nil)
		claude, admissions := quotaI6PerfPrepare(t, f, g, samples, true)
		scale.Admit = quotaI6PerfDistributionOf(admissions)
		gptMixed, _ := quotaI6PerfPrepare(t, f, g, samples, false)
		scale.Mixed = quotaI6PerfRunApply(t, f, gptDB, claudeDB, observer, gptMixed, claude)
		var consistent bool
		require.NoError(t, f.db.QueryRow(`SELECT used_usd=(SELECT sum(delta_used_usd) FROM subscription_quota_ledger WHERE term_id=$1) FROM subscription_quota_terms WHERE id=$1`, card.Term.ID).Scan(&consistent))
		require.True(t, consistent)
		data, err := json.Marshal(scale)
		require.NoError(t, err)
		t.Logf("I6_PERF_RESULT %s", data)
	}
}
