//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// A coherent synthetic history, not a short-card feature: both cards are168h.
// This helper only appends new identities and never edits issued A/B cards.
func (e *subscriptionQuotaI4Environment) prepareBoundary(t *testing.T, seconds int) {
	t.Helper()
	require.GreaterOrEqual(t, seconds, 20)
	require.LessOrEqual(t, seconds, 300)
	e.BoundaryEmail = "i4-expiry-" + uuid.NewString() + "@example.invalid"
	e.BoundaryKey = "sk-cwq-i4-expiry-" + uuid.NewString()
	parse := func(value string) int64 {
		id, err := strconv.ParseInt(value, 10, 64)
		require.NoError(t, err)
		return id
	}
	e.BoundaryUser = parse(e.sql(t, fmt.Sprintf(`INSERT INTO users(email,password_hash,balance,concurrency,claude_subscription_concurrency) VALUES('%s','%s',0,5,2) RETURNING id`, e.BoundaryEmail, e.PasswordHash)))
	e.BoundaryExpires = e.sql(t, fmt.Sprintf(`SELECT to_char((clock_timestamp()+make_interval(secs=>%d)) AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"')`, seconds))
	e.BoundaryParent = parse(e.sql(t, fmt.Sprintf(`INSERT INTO user_subscriptions(user_id,group_id,starts_at,expires_at,status,quota_revision,assigned_by) VALUES(%d,%d,'%s'::timestamptz-INTERVAL '168 hours','%s'::timestamptz+INTERVAL '168 hours','active',2,%d) RETURNING id`, e.BoundaryUser, e.Group, e.BoundaryExpires, e.BoundaryExpires, e.Admin)))
	for i, quota := range []string{"13.00000000", "17.00000000"} {
		snapshot, err := json.Marshal(domain.SubscriptionQuotaSourceSnapshot{SchemaVersion: 1, QuotaUSD: quota, DurationHours: 168, QuotaSource: "admin"})
		require.NoError(t, err)
		starts, ends, state := "'"+e.BoundaryExpires+"'::timestamptz-INTERVAL '168 hours'", "'"+e.BoundaryExpires+"'::timestamptz", "active"
		if i == 1 {
			starts, ends, state = "'"+e.BoundaryExpires+"'::timestamptz", "'"+e.BoundaryExpires+"'::timestamptz+INTERVAL '168 hours'", "scheduled"
		}
		id := parse(e.sql(t, fmt.Sprintf(`INSERT INTO subscription_quota_terms(subscription_id,user_id,group_id,term_no,quota_usd,starts_at,expires_at,lifecycle_state,source_kind,source_snapshot,created_by) VALUES(%d,%d,%d,%d,%s,%s,%s,'%s','admin','%s'::jsonb,%d) RETURNING id`, e.BoundaryParent, e.BoundaryUser, e.Group, i+1, quota, starts, ends, state, snapshot, e.Admin)))
		if i == 0 {
			e.BoundaryTerm = id
		} else {
			e.BoundaryNext = id
		}
	}
	e.BoundaryKeyID = parse(e.sql(t, fmt.Sprintf(`INSERT INTO api_keys(user_id,group_id,key,name,quota,rate_limit_5h,rate_limit_1d,rate_limit_7d) VALUES(%d,%d,'%s','I4 independent historical expiry identity',1000000,1000000,1000000,1000000) RETURNING id`, e.BoundaryUser, e.Group, e.BoundaryKey)))
	require.Equal(t, "2|2", e.sql(t, fmt.Sprintf(`SELECT count(*)||'|'||count(*) FILTER(WHERE expires_at=starts_at+INTERVAL '168 hours') FROM subscription_quota_terms WHERE subscription_id=%d`, e.BoundaryParent)))
	encoded, err := json.MarshalIndent(e, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(subscriptionQuotaI4PrivatePath(t), encoded, 0600))
	t.Logf("synthetic boundary prepared user=%d email=%s original_term=%d next_term=%d expires_UTC=%s; each card retains complete168h, A100/B300 unchanged; browser countdown not yet accepted", e.BoundaryUser, e.BoundaryEmail, e.BoundaryTerm, e.BoundaryNext, e.BoundaryExpires)
}

func TestSubscriptionQuotaI4V2PrepareExpiryFixture(t *testing.T) {
	e := subscriptionQuotaI4Load(t)
	seconds, err := strconv.Atoi(os.Getenv("CWQ_I4_BOUNDARY_SECONDS"))
	require.NoError(t, err)
	e.prepareBoundary(t, seconds)
}

func TestSubscriptionQuotaI4V2CrossCardLeaseBoundary(t *testing.T) {
	e := subscriptionQuotaI4Load(t)
	e.prepareBoundary(t, 25)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	beforeRequest := e.sql(t, `SELECT COALESCE(max(id),0) FROM subscription_quota_requests`)
	foreign := e.sql(t, fmt.Sprintf(`SELECT md5(jsonb_agg(to_jsonb(t) ORDER BY id)::text) FROM subscription_quota_terms t WHERE id NOT IN (%d,%d)`, e.BoundaryTerm, e.BoundaryNext))
	money := e.fingerprint(t, "ordinary_and_GPT")
	e.providerGate(t, true)
	defer e.providerGate(t, false)
	type result struct {
		raw []byte
		err error
	}
	done := make(chan result, 2)
	for i := 0; i < 2; i++ {
		response, err := e.heldRuntimeRequest(ctx, e.URL, e.BoundaryKey, false)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, response.StatusCode)
		go func() {
			raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
			_ = response.Body.Close()
			done <- result{raw, err}
		}()
	}
	key := fmt.Sprintf("concurrency:scope:{claude_subscription:user:%d}:active", e.BoundaryUser)
	original := e.redisStrings(t, "ZRANGE", key, "0", "-1")
	require.Len(t, original, 2)
	expires, err := time.Parse(time.RFC3339Nano, e.BoundaryExpires)
	require.NoError(t, err)
	timer := time.NewTimer(time.Until(expires) + time.Second)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
		t.Fatal("boundary wait exceeded root context")
	}
	require.ElementsMatch(t, original, e.redisStrings(t, "ZRANGE", key, "0", "-1"), "new current card cannot reset existing user concurrency members; renewal may reorder their scores")
	probeCtx, stop := context.WithTimeout(ctx, time.Second)
	response, _ := e.heldRuntimeRequest(probeCtx, e.URL2, e.BoundaryKey, false)
	if response != nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
		_ = response.Body.Close()
	}
	stop()
	require.Equal(t, "2|0", e.sql(t, fmt.Sprintf(`SELECT count(*)||'|'||count(*) FILTER(WHERE term_id=%d) FROM subscription_quota_requests WHERE id>%s`, e.BoundaryNext, beforeRequest)))
	e.providerGate(t, false)
	for i := 0; i < 2; i++ {
		select {
		case out := <-done:
			require.NoError(t, out.err)
			require.Contains(t, string(out.raw), "message_stop")
		case <-time.After(8 * time.Second):
			t.Fatal("historical original-card stream did not finish")
		}
	}
	require.Eventually(t, func() bool {
		return e.sql(t, fmt.Sprintf(`SELECT count(*) FROM subscription_quota_requests WHERE id>%s AND term_id=%d AND status='settled' AND usage_log_recorded_at IS NOT NULL`, beforeRequest, e.BoundaryTerm)) == "2"
	}, 12*time.Second, 100*time.Millisecond)
	require.Equal(t, "0.00120000|0.00000000", e.sql(t, fmt.Sprintf(`SELECT (SELECT used_usd::text FROM subscription_quota_terms WHERE id=%d)||'|'||(SELECT used_usd::text FROM subscription_quota_terms WHERE id=%d)`, e.BoundaryTerm, e.BoundaryNext)))
	status, _, _ := e.post(t, "/v1/messages", e.BoundaryKey, false, false)
	require.Equal(t, http.StatusOK, status)
	require.Eventually(t, func() bool {
		return e.sql(t, fmt.Sprintf(`SELECT used_usd::text FROM subscription_quota_terms WHERE id=%d`, e.BoundaryNext)) == "0.00060000"
	}, 12*time.Second, 100*time.Millisecond)
	require.Equal(t, foreign, e.sql(t, fmt.Sprintf(`SELECT md5(jsonb_agg(to_jsonb(t) ORDER BY id)::text) FROM subscription_quota_terms t WHERE id NOT IN (%d,%d)`, e.BoundaryTerm, e.BoundaryNext)))
	require.Equal(t, money, e.fingerprint(t, "ordinary_and_GPT"))
	t.Log("actual HTTP/Redis/PG cross-card boundary: both original user lease UUIDs survive expiry; next-card request waits; late known charges only the two original admissions, next card untouched until new post-release request; both cards168h, A/B/GPT/native/ordinary unchanged")
}

func TestSubscriptionQuotaI4V2RedisFaultCancelsActualUpstream(t *testing.T) {
	e := subscriptionQuotaI4Load(t)
	e.prepareBoundary(t, 180)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()
	beforeRequest := e.sql(t, `SELECT COALESCE(max(id),0) FROM subscription_quota_requests`)
	money := e.fingerprint(t, "ordinary_and_GPT")
	foreign := e.sql(t, fmt.Sprintf(`SELECT md5(jsonb_agg(to_jsonb(t) ORDER BY id)::text) FROM subscription_quota_terms t WHERE id NOT IN (%d,%d)`, e.BoundaryTerm, e.BoundaryNext))
	legacyKey := fmt.Sprintf("concurrency:user:%d", e.A)
	legacyBefore := e.redisStrings(t, "ZRANGE", legacyKey, "0", "-1", "WITHSCORES")
	legacyMember := uuid.NewString()
	clock := e.redisStrings(t, "TIME")
	require.Len(t, clock, 2)
	e.docker(t, nil, "exec", e.Redis, "redis-cli", "ZADD", legacyKey, clock[0], legacyMember)
	defer e.docker(t, nil, "exec", e.Redis, "redis-cli", "ZREM", legacyKey, legacyMember)
	legacyHeld := e.redisStrings(t, "ZRANGE", legacyKey, "0", "-1", "WITHSCORES")
	e.providerGate(t, true)
	defer e.providerGate(t, false)
	response, err := e.heldRuntimeRequest(ctx, e.URL, e.BoundaryKey, false)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.StatusCode)
	type result struct {
		raw []byte
		err error
	}
	done := make(chan result, 1)
	go func() {
		raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		_ = response.Body.Close()
		done <- result{raw, err}
	}()
	beforeCalls, active := e.providerState(t)
	require.Equal(t, 1, active)
	started := time.Now()
	// This pauses only the fixture's isolated Redis, preserving its data and
	// leaving the application, provider and PG running. Actual lease heartbeats
	// fail at their real two-second bound until the confirmed lease safety edge.
	e.docker(t, nil, "exec", e.Redis, "redis-cli", "CLIENT", "PAUSE", "65000", "ALL")
	defer e.docker(t, nil, "exec", e.Redis, "redis-cli", "CLIENT", "UNPAUSE")
	require.Eventually(t, func() bool { _, active := e.providerState(t); return active == 0 }, 58*time.Second, time.Second, "actual upstream context must cancel before the last confirmed sixty-second lease expires")
	elapsed := time.Since(started)
	require.Greater(t, elapsed, 45*time.Second)
	require.Less(t, elapsed, 59*time.Second)
	e.docker(t, nil, "exec", e.Redis, "redis-cli", "CLIENT", "UNPAUSE")
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("actual client did not finish after upstream cancellation")
	}
	require.Eventually(t, func() bool {
		return e.sql(t, `SELECT count(*) FROM subscription_quota_requests WHERE id>`+beforeRequest+` AND status='reconcile_required'`) == "1"
	}, 12*time.Second, 100*time.Millisecond)
	require.Equal(t, "0|0|0.00000000", e.sql(t, `SELECT (SELECT count(*) FROM subscription_quota_ledger WHERE request_row_id>`+beforeRequest+`)||'|'||(SELECT count(*) FROM usage_billing_dedup d JOIN subscription_quota_requests r ON r.request_id=d.request_id AND r.api_key_id=d.api_key_id WHERE r.id>`+beforeRequest+`)||'|'||(SELECT used_usd::text FROM subscription_quota_terms WHERE id=`+strconv.FormatInt(e.BoundaryTerm, 10)+`)`))
	key := fmt.Sprintf("concurrency:scope:{claude_subscription:user:%d}:active", e.BoundaryUser)
	require.Eventually(t, func() bool { return len(e.redisStrings(t, "ZRANGE", key, "0", "-1")) == 0 }, 10*time.Second, 100*time.Millisecond)
	require.Empty(t, e.redisStrings(t, "ZRANGE", fmt.Sprintf("concurrency:user:%d", e.BoundaryUser), "0", "-1"), "Claude fault cannot fall back into legacy user slots")
	require.Equal(t, legacyHeld, e.redisStrings(t, "ZRANGE", legacyKey, "0", "-1", "WITHSCORES"))
	e.providerGate(t, false)
	status, _, _ := e.post(t, "/v1/messages", e.BoundaryKey, false, false)
	require.Equal(t, http.StatusServiceUnavailable, status, "unknown original request pauses its current card")
	calls, _ := e.providerState(t)
	require.Equal(t, beforeCalls, calls)
	require.Equal(t, foreign, e.sql(t, fmt.Sprintf(`SELECT md5(jsonb_agg(to_jsonb(t) ORDER BY id)::text) FROM subscription_quota_terms t WHERE id NOT IN (%d,%d)`, e.BoundaryTerm, e.BoundaryNext)))
	require.Equal(t, money, e.fingerprint(t, "ordinary_and_GPT"))
	e.docker(t, nil, "exec", e.Redis, "redis-cli", "ZREM", legacyKey, legacyMember)
	require.Equal(t, legacyBefore, e.redisStrings(t, "ZRANGE", legacyKey, "0", "-1", "WITHSCORES"))
	t.Logf("actual Redis pause/runtime cancellation elapsed=%s provider_cancelled=true partial_usage_reconcile_preserved=true zero_debit_dedup=true no_GPT_fallback_or_slot_clear=true; fixture unknown is retained, not SQL-cleared", elapsed)
}
