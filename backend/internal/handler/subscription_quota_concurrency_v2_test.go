//go:build unit

package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type termConcurrencyProviderFunc func(context.Context, int64, time.Duration, func() error) (*service.UserScopeLease, error)

func (f termConcurrencyProviderFunc) AcquireClaudeSubscriptionLease(ctx context.Context, id int64, timeout time.Duration, ping func() error) (*service.UserScopeLease, error) {
	return f(ctx, id, timeout, ping)
}

func TestSubscriptionQuotaConcurrencyV2HelperKeepsCancelledForwardLeaseUntilFinalizer(t *testing.T) {
	for _, path := range []string{"/v1/messages", "/v1/chat/completions", "/chat/completions", "/v1/responses", "/responses", "/backend-api/codex/responses"} {
		t.Run(path, func(t *testing.T) {
			parent, cancel := context.WithCancel(context.Background())
			defer cancel()
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, path, nil).WithContext(parent)
			c.Request.Header.Set("X-Concurrency-Scope", "legacy_user")
			var calls, releases atomic.Int32
			key := termHandlerKey()
			helper := &ConcurrencyHelper{claudeConcurrency: termConcurrencyProviderFunc(func(ctx context.Context, userID int64, timeout time.Duration, _ func() error) (*service.UserScopeLease, error) {
				calls.Add(1)
				require.Equal(t, key.UserID, userID)
				require.Equal(t, maxConcurrencyWait, timeout)
				return service.NewUserScopeLease(ctx, func() { releases.Add(1) }), nil
			})}
			started := false
			release, err := helper.AcquireGatewayUserSlotWithWait(c, key, key.UserID, 999, false, &started)
			require.NoError(t, err)
			require.EqualValues(t, 1, calls.Load())
			cancel()
			require.ErrorIs(t, c.Request.Context().Err(), context.Canceled)
			require.Zero(t, releases.Load(), "Done alone must not free a still-running local forward")
			release()
			release()
			require.EqualValues(t, 1, releases.Load())
		})
	}
}

func TestSubscriptionQuotaConcurrencyV2HelperMissingDependencyAndIdentityFailClosed(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	key := termHandlerKey()
	helper := &ConcurrencyHelper{}
	started := false
	release, err := helper.AcquireGatewayUserSlotWithWait(c, key, key.UserID, 999, false, &started)
	require.ErrorIs(t, err, service.ErrTermConcurrencyUnavailable)
	require.Nil(t, release)
	helper.claudeConcurrency = termConcurrencyProviderFunc(func(context.Context, int64, time.Duration, func() error) (*service.UserScopeLease, error) {
		t.Fatal("wrong subject must not reach either pool")
		return nil, nil
	})
	_, err = helper.AcquireGatewayUserSlotWithWait(c, key, key.UserID+1, 999, false, &started)
	require.ErrorIs(t, err, service.ErrTermConcurrencyUnavailable)
}

func TestSubscriptionQuotaConcurrencyV2AccountCompletionReleaseSurvivesCancellationAndPanic(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	var releases atomic.Int32
	release := forwardCompletionRelease(func() { releases.Add(1) })
	cancel()
	require.ErrorIs(t, parent.Err(), context.Canceled)
	require.Zero(t, releases.Load())
	func() {
		defer func() { require.Equal(t, "synthetic forward panic", recover()) }()
		defer release()
		panic("synthetic forward panic")
	}()
	release()
	require.EqualValues(t, 1, releases.Load())
}

func TestSubscriptionQuotaConcurrencyV2TypedErrorsAndWaitingStreamingTerminal(t *testing.T) {
	for _, failure := range []struct {
		err    error
		status int
		code   string
	}{{service.ErrTermConcurrencyNotConfigured, 503, "TERM_CONCURRENCY_NOT_CONFIGURED"}, {service.ErrTermConcurrencyUnavailable, 503, "TERM_CONCURRENCY_UNAVAILABLE"}, {service.ErrTermConcurrencyQueueFull, 429, "TERM_CONCURRENCY_QUEUE_FULL"}, {service.ErrTermConcurrencyWaitTimeout, 429, "TERM_CONCURRENCY_WAIT_TIMEOUT"}} {
		t.Run(failure.code, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			(&GatewayHandler{}).handleConcurrencyError(c, failure.err, "user", false)
			require.Equal(t, failure.status, recorder.Code)
			require.Contains(t, recorder.Body.String(), failure.code)
		})
	}
	t.Run("responses_failed_after_wait_ping", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/backend-api/codex/responses", nil)
		key := termHandlerKey()
		helper := &ConcurrencyHelper{pingFormat: SSEPingFormatClaude, claudeConcurrency: termConcurrencyProviderFunc(func(_ context.Context, _ int64, _ time.Duration, ping func() error) (*service.UserScopeLease, error) {
			require.NoError(t, ping())
			return nil, service.ErrTermConcurrencyQueueFull
		})}
		started := false
		_, err := helper.AcquireGatewayUserSlotWithWait(c, key, key.UserID, 999, true, &started)
		require.ErrorIs(t, err, service.ErrTermConcurrencyQueueFull)
		require.True(t, started)
		(&GatewayHandler{}).handleConcurrencyError(c, err, "user", started)
		require.Equal(t, http.StatusOK, recorder.Code, "committed SSE must not rewrite HTTP status")
		require.Contains(t, recorder.Body.String(), "response.failed")
		require.Contains(t, recorder.Body.String(), "TERM_CONCURRENCY_QUEUE_FULL")
	})
}

func TestSubscriptionQuotaConcurrencyV2LeaseLossCauseOnlyOverridesTermErrors(t *testing.T) {
	for _, scenario := range []string{"term", "term_sse", "client_cancel", "legacy"} {
		t.Run(scenario, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			ctx, cancel := context.WithCancelCause(context.Background())
			cause := error(service.ErrTermConcurrencyUnavailable)
			if scenario == "client_cancel" {
				cause = context.Canceled
			}
			cancel(cause)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(ctx)
			key := termHandlerKey()
			if scenario == "legacy" {
				key.Group.SubscriptionQuotaMode = "windows"
			}
			started := scenario == "term_sse"
			if started {
				c.Writer.WriteHeader(http.StatusOK)
				_, _ = c.Writer.WriteString(":\n\n")
				c.Writer.Flush()
			}
			written := (&GatewayHandler{}).writeClaudeLeaseLoss(c, key, started)
			if scenario == "client_cancel" || scenario == "legacy" {
				require.False(t, written)
				require.Zero(t, recorder.Body.Len())
				return
			}
			require.True(t, written)
			require.Contains(t, recorder.Body.String(), "TERM_CONCURRENCY_UNAVAILABLE")
			if started {
				require.Equal(t, 200, recorder.Code)
				require.Contains(t, recorder.Body.String(), "response.failed")
			} else {
				require.Equal(t, 503, recorder.Code)
			}
		})
	}
}
