//go:build integration

package repository

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

// Run serially against the explicit final-image fixture, after long SSE and
// before collecting scope metrics. The provider gate belongs to this test.
func TestSubscriptionQuotaI6RuntimeClaudeQueuePromotion(t *testing.T) {
	e := subscriptionQuotaI4Load(t)
	require.Equal(t, "I6", e.Stage)
	require.Equal(t, "t", e.sql(t, fmt.Sprintf(`SELECT claude_subscription_concurrency IS NULL FROM users WHERE id=%d`, e.B)))
	require.Equal(t, "300.00000000", e.sql(t, fmt.Sprintf(`SELECT quota_usd::text FROM subscription_quota_terms WHERE id=%d`, e.BTerm)))
	beforeRequest := e.sql(t, `SELECT coalesce(max(id),0) FROM subscription_quota_requests`)
	beforeUsed := decimal.RequireFromString(e.sql(t, fmt.Sprintf(`SELECT used_usd::text FROM subscription_quota_terms WHERE id=%d`, e.BTerm)))
	// Recovery retries may touch an unrelated closing term's updated_at. Keep
	// every identity, financial, interval, lifecycle and revision field guarded.
	foreignSQL := fmt.Sprintf(`SELECT md5(jsonb_agg(to_jsonb(t)-'updated_at' ORDER BY id)::text) FROM subscription_quota_terms t WHERE id<>%d`, e.BTerm)
	foreignCards, otherMoney := e.sql(t, foreignSQL), e.fingerprint(t, "ordinary_and_GPT")
	activeKey := fmt.Sprintf("concurrency:scope:{claude_subscription:user:%d}:active", e.B)
	waitKey := fmt.Sprintf("concurrency:scope:{claude_subscription:user:%d}:waiting", e.B)
	accountKey := fmt.Sprintf("concurrency:account:%d", e.Account)
	for _, key := range []string{activeKey, waitKey} {
		require.Empty(t, e.redisStrings(t, "ZRANGE", key, "0", "-1"), "fixture must be idle")
	}
	// Earlier fault cases may leave someone else's legacy account lease. Do
	// not delete it or require a globally empty pool; track only our additions.
	baselineAccount := map[string]int64{}
	baseline := e.redisStrings(t, "ZRANGE", accountKey, "0", "-1", "WITHSCORES")
	for i := 0; i < len(baseline); i += 2 {
		score, err := strconv.ParseInt(baseline[i+1], 10, 64)
		require.NoError(t, err)
		baselineAccount[baseline[i]] = score
	}
	beforeCalls, beforeActive := e.providerState(t)
	require.Zero(t, beforeActive)
	e.providerGate(t, true)
	defer e.providerGate(t, false)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	type result struct {
		index, status int
		raw           []byte
		err           error
	}
	done := make(chan result, 4)
	start := func(index int) {
		go func() {
			out := result{index: index}
			response, err := e.heldClaudeRequest(ctx, e.BKey)
			out.err = err
			if response != nil {
				out.status = response.StatusCode
				out.raw, out.err = io.ReadAll(io.LimitReader(response.Body, 1<<20))
				_ = response.Body.Close()
			}
			done <- out
		}()
	}
	finish := func() int {
		select {
		case out := <-done:
			require.NoError(t, out.err)
			require.Equal(t, http.StatusOK, out.status)
			require.Contains(t, string(out.raw), "message_stop")
			return out.index
		case <-time.After(10 * time.Second):
			t.Fatal("released Claude stream did not complete")
			return -1
		}
	}
	for i := 0; i < 3; i++ {
		start(i)
	}
	require.Eventually(t, func() bool { calls, active := e.providerState(t); return calls == beforeCalls+3 && active == 3 }, 8*time.Second, 100*time.Millisecond)
	initial := e.redisStrings(t, "ZRANGE", activeKey, "0", "-1")
	require.Len(t, initial, 3)
	start(3)
	require.Eventually(t, func() bool { return len(e.redisStrings(t, "ZRANGE", waitKey, "0", "-1")) == 1 }, 8*time.Second, 100*time.Millisecond)
	calls, active := e.providerState(t)
	require.Equal(t, beforeCalls+3, calls, "queued request must not reach the upstream")
	require.Equal(t, 3, active)
	require.Len(t, e.redisStrings(t, "ZRANGE", activeKey, "0", "-1"), 3)
	require.Equal(t, beforeUsed.StringFixed(8), e.sql(t, fmt.Sprintf(`SELECT used_usd::text FROM subscription_quota_terms WHERE id=%d`, e.BTerm)))
	// Releasing one actual upstream leaves the global hold in place, so the
	// fourth request can finish admission but cannot complete before inspection.
	e.docker(t, nil, "exec", e.Provider, "wget", "-q", "-O", "-", "--header", "Content-Type: application/json", "--post-data", fmt.Sprintf(`{"release_sequence":%d}`, beforeCalls+1), "http://127.0.0.1:8081/__control")
	require.NotEqual(t, 3, finish())
	require.Eventually(t, func() bool {
		calls, active := e.providerState(t)
		return calls == beforeCalls+4 && active == 3 && len(e.redisStrings(t, "ZRANGE", waitKey, "0", "-1")) == 0 && len(e.redisStrings(t, "ZRANGE", activeKey, "0", "-1")) == 3
	}, 10*time.Second, 100*time.Millisecond)
	remainingOriginal := 0
	for _, member := range e.redisStrings(t, "ZRANGE", activeKey, "0", "-1") {
		for _, original := range initial {
			if member == original {
				remainingOriginal++
			}
		}
	}
	require.Equal(t, 2, remainingOriginal, "promotion replaces only the released owner with a new active lease")
	e.providerGate(t, false)
	for i := 0; i < 3; i++ {
		finish()
	}
	require.Eventually(t, func() bool {
		return e.sql(t, fmt.Sprintf(`SELECT count(*) FROM subscription_quota_requests WHERE id>%s AND user_id=%d AND term_id=%d AND status='settled' AND usage_log_recorded_at IS NOT NULL AND actual_cost_usd=0.00060000`, beforeRequest, e.B, e.BTerm)) == "4"
	}, 15*time.Second, 100*time.Millisecond)
	require.Equal(t, "4", e.sql(t, `SELECT count(*) FROM subscription_quota_requests WHERE id>`+beforeRequest))
	require.Equal(t, "4", e.sql(t, `SELECT count(*) FROM usage_logs WHERE subscription_quota_request_id>`+beforeRequest))
	require.Equal(t, "4|0.00240000", e.sql(t, `SELECT count(*)||'|'||sum(delta_used_usd)::text FROM subscription_quota_ledger WHERE request_row_id>`+beforeRequest))
	require.Equal(t, beforeUsed.Add(decimal.RequireFromString("0.00240000")).StringFixed(8), e.sql(t, fmt.Sprintf(`SELECT used_usd::text FROM subscription_quota_terms WHERE id=%d`, e.BTerm)))
	require.Equal(t, foreignCards, e.sql(t, foreignSQL))
	require.Equal(t, otherMoney, e.fingerprint(t, "ordinary_and_GPT"))
	var finalAccount []string
	require.Eventually(t, func() bool {
		calls, active := e.providerState(t)
		finalAccount = e.redisStrings(t, "ZRANGE", accountKey, "0", "-1")
		for _, member := range finalAccount {
			if _, existed := baselineAccount[member]; !existed {
				return false
			}
		}
		return calls == beforeCalls+4 && active == 0 && len(e.redisStrings(t, "ZRANGE", activeKey, "0", "-1")) == 0 && len(e.redisStrings(t, "ZRANGE", waitKey, "0", "-1")) == 0
	}, 5*time.Second, 100*time.Millisecond)
	redisTime := e.redisStrings(t, "TIME")
	now, err := strconv.ParseInt(redisTime[0], 10, 64)
	require.NoError(t, err)
	// This fixture has no gateway TTL override or config file: Config.Load's
	// 30-minute default reaches Wire, not the repository's 15-minute fallback.
	for member, score := range baselineAccount {
		if score > now-30*60 {
			require.Contains(t, finalAccount, member, "our request cleanup must preserve another unexpired account lease")
		}
	}
	t.Logf("account leases baseline=%d final=%d: all new members released; unrelated unexpired owners preserved; actual legacy Acquire may prune only scores <= Redis TIME-1800", len(baselineAccount), len(finalAccount))
	t.Log("T67 actual final-image Claude promotion: B default3 fills three slots; fourth waits with no upstream call; one release promotes fourth while retaining two owners and active3; all four message_stop/settled/logack, exact B debit0.00240000, other cards/GPT/native/ordinary unchanged, own user/account slots and waiting tickets cleared")
}
