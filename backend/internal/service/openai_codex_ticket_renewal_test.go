package service

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func renewalOldTicket(expires time.Time) *openAICodexTicket {
	return &openAICodexTicket{Model: "gpt-6-astra", State: fakeCodexTicketState(292), Length: 292,
		CapturedAt: time.Now().Add(-2 * time.Hour), ExpiresAt: expires}
}

func TestCodexTicketSameBlobRenewsNativeTTLBeforeAndAfterExpiry(t *testing.T) {
	for _, expired := range []bool{false, true} {
		name := "near_expiry"
		expires := time.Now().Add(time.Minute)
		if expired {
			name, expires = "expired", time.Now().Add(-time.Minute)
		}
		t.Run(name, func(t *testing.T) {
			svc, repo, upstream := sentinelFixture(t)
			old := renewalOldTicket(expires)
			svc.storeOpenAICodexTicket(context.Background(), repo.account, old)
			started := time.Now()
			ticket, err := svc.collectOpenAICodexTicket(context.Background(), repo.account, old.Model)
			require.NoError(t, err)
			require.Len(t, upstream.requests, 1)
			require.NotSame(t, old, ticket)
			require.Equal(t, old.State, ticket.State)
			require.False(t, ticket.CapturedAt.Before(started))
			require.True(t, ticket.ExpiresAt.After(old.ExpiresAt))
			require.Equal(t, time.Hour, ticket.ExpiresAt.Sub(ticket.CapturedAt))
			require.True(t, ticket.valid(time.Now(), 292))
			require.False(t, svc.openAICodexTicketBlocksAccount(repo.account, old.Model))
			require.True(t, codexTicketGenerationMatches(parseOpenAICodexTicketExtra(repo.account, old.Model), ticket))
		})
	}
}

func TestCodexTicketSameBlobPersistenceFailureKeepsOldLease(t *testing.T) {
	svc, repo, _ := sentinelFixture(t)
	old := renewalOldTicket(time.Now().Add(time.Minute))
	svc.storeOpenAICodexTicket(context.Background(), repo.account, old)
	repo.persistErr = errors.New("synthetic persistence failure")
	ticket, err := svc.collectOpenAICodexTicket(context.Background(), repo.account, old.Model)
	require.Error(t, err)
	require.Nil(t, ticket)
	require.Same(t, old, svc.lookupOpenAICodexTicket(repo.account, old.Model))
	require.True(t, codexTicketGenerationMatches(parseOpenAICodexTicketExtra(repo.account, old.Model), old))
}

func TestCodexSentinelRenewedReceiptAndOldEventBecomesStale(t *testing.T) {
	svc, repo, upstream := sentinelFixture(t)
	old := renewalOldTicket(time.Now().Add(time.Minute))
	svc.storeOpenAICodexTicket(context.Background(), repo.account, old)
	headers := http.Header{}
	headers.Set(openAICodexTurnStateHeader, old.State)
	observation := svc.sentinelObservation(repo.account, old.Model, headers)
	// Model a completed request that preceded the new probe; do not depend on
	// sub-millisecond wall-clock resolution in a zero-latency in-memory transport.
	observation.sentAt = time.Now().Add(-time.Second)
	observation.completed("gpt-5.6-luna")
	body := sentinelRefreshBody(codexTicketVersion(old), "model_mismatch", "test-instance:1")
	code, result, raw := sentinelRequest(t, svc, "POST", body, sentinelTestToken)
	require.Equal(t, 200, code)
	require.Equal(t, "renewed", result["status"])
	require.Equal(t, float64(2), result["account_id"])
	require.Equal(t, old.Model, result["model"])
	require.Equal(t, true, result["persisted"])
	require.Equal(t, true, result["ready"])
	require.Equal(t, codexTicketVersion(old), result["ticket_version"])
	require.Equal(t, codexTicketVersion(old), result["previous_ticket_version"])
	previousCapturedValue, ok := result["previous_captured_at_unix_ms"].(float64)
	require.True(t, ok, "previous_captured_at_unix_ms must be a JSON number")
	require.Equal(t, old.CapturedAt.UnixMilli(), int64(previousCapturedValue))
	capturedValue, ok := result["captured_at_unix_ms"].(float64)
	require.True(t, ok, "captured_at_unix_ms must be a JSON number")
	captured := int64(capturedValue)
	require.Greater(t, captured, old.CapturedAt.UnixMilli())
	require.LessOrEqual(t, captured, time.Now().UnixMilli())
	require.NotContains(t, raw, old.State)
	cached, err := svc.schedulerSnapshot.cache.GetAccount(context.Background(), 2)
	require.NoError(t, err)
	require.Equal(t, captured, parseOpenAICodexTicketExtra(cached, old.Model).CapturedAt.UnixMilli())
	code, result, _ = sentinelRequest(t, svc, "POST", body, sentinelTestToken)
	require.Equal(t, 200, code)
	require.Equal(t, "stale", result["status"])
	require.Len(t, upstream.requests, 1)
}

func TestCodexSentinelExpiredSameBlobReportsRenewedWithPreviousVersion(t *testing.T) {
	svc, repo, _ := sentinelFixture(t)
	old := renewalOldTicket(time.Now().Add(-time.Minute))
	svc.storeOpenAICodexTicket(context.Background(), repo.account, old)
	require.Empty(t, svc.sentinelVersion(repo.account, old.Model))
	code, result, _ := sentinelRequest(t, svc, "POST", sentinelRefreshBody("", "missing", "periodic"), sentinelTestToken)
	require.Equal(t, 200, code)
	require.Equal(t, "renewed", result["status"])
	require.Equal(t, codexTicketVersion(old), result["ticket_version"])
	require.Equal(t, codexTicketVersion(old), result["previous_ticket_version"])
	captured, ok := result["captured_at_unix_ms"].(float64)
	require.True(t, ok, "captured_at_unix_ms must be a JSON number")
	previousCaptured, ok := result["previous_captured_at_unix_ms"].(float64)
	require.True(t, ok, "previous_captured_at_unix_ms must be a JSON number")
	require.Greater(t, captured, previousCaptured)
	require.True(t, svc.lookupOpenAICodexTicket(repo.account, old.Model).valid(time.Now(), 292))
}

type renewalStaleCache struct {
	SchedulerCache
	account *Account
}

func (c *renewalStaleCache) SetAccount(context.Context, *Account) error { return nil }
func (c *renewalStaleCache) GetAccount(context.Context, int64) (*Account, error) {
	return c.account, nil
}

func TestCodexSentinelSameBlobRequiresNewGenerationInDatabaseAndCache(t *testing.T) {
	for _, layer := range []string{"database", "cache"} {
		t.Run(layer, func(t *testing.T) {
			svc, repo, _ := sentinelFixture(t)
			old := renewalOldTicket(time.Now().Add(time.Minute))
			svc.storeOpenAICodexTicket(context.Background(), repo.account, old)
			reason := "persistence_unverified"
			if layer == "database" {
				repo.ignoreSave = true
			} else {
				stale, err := repo.GetByID(context.Background(), 2)
				require.NoError(t, err)
				svc.schedulerSnapshot.cache = &renewalStaleCache{account: stale}
				reason = "cache_unverified"
			}
			code, result, _ := sentinelRequest(t, svc, "POST", sentinelRefreshBody(codexTicketVersion(old), "expiry", "periodic"), sentinelTestToken)
			require.Equal(t, 503, code)
			require.Equal(t, "failed", result["status"])
			require.Equal(t, reason, result["reason"])
			require.NotContains(t, result, "persisted")
			require.NotContains(t, result, "captured_at_unix_ms")
		})
	}
}
