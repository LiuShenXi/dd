//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

type termHandlerKnownFact struct {
	snapshot domain.SubscriptionQuotaBillingSnapshot
	billing  json.RawMessage
	ctxErr   error
}
type termHandlerBilling struct {
	snapshot *domain.SubscriptionQuotaBillingSnapshot
	known    chan termHandlerKnownFact
	unknown  chan struct{}
}

func (b *termHandlerBilling) Admit(_ context.Context, in domain.SubscriptionQuotaAdmissionParams) (*domain.SubscriptionQuotaBillingSnapshot, error) {
	at := time.Now().UTC()
	raw, err := in.FreezePricing(at)
	if err != nil {
		return nil, err
	}
	b.snapshot = &domain.SubscriptionQuotaBillingSnapshot{SchemaVersion: 1, RequestID: in.RequestID, RequestRowID: 1, UserID: in.UserID, APIKeyID: in.APIKeyID, GroupID: in.GroupID, AccountID: in.AccountID, SubscriptionID: 31, TermID: 41, PricingAt: at, AdmittedAt: at, PricingSnapshot: raw}
	b.snapshot.AccountType = service.AccountTypeAPIKey
	_, hash, err := domain.MarshalSubscriptionQuotaPricingEnvelope(*b.snapshot)
	if err != nil {
		return nil, err
	}
	b.snapshot.PricingSnapshotHash = hash
	copy := b.snapshot.Clone()
	return &copy, nil
}
func (*termHandlerBilling) Heartbeat(context.Context, *domain.SubscriptionQuotaBillingSnapshot) error {
	return nil
}
func (b *termHandlerBilling) PersistKnownUsage(ctx context.Context, snapshot *domain.SubscriptionQuotaBillingSnapshot, _ decimal.Decimal, billing json.RawMessage, _ json.RawMessage) error {
	if b.known != nil {
		select {
		case b.known <- termHandlerKnownFact{snapshot: snapshot.Clone(), billing: append(json.RawMessage(nil), billing...), ctxErr: ctx.Err()}:
		default:
		}
	}
	return nil
}
func (*termHandlerBilling) MarkNoCharge(context.Context, *domain.SubscriptionQuotaBillingSnapshot, domain.SubscriptionQuotaNoChargeEvidence) error {
	return nil
}
func (b *termHandlerBilling) MarkReconcileRequired(context.Context, *domain.SubscriptionQuotaBillingSnapshot, string) error {
	if b.unknown != nil {
		select {
		case b.unknown <- struct{}{}:
		default:
		}
	}
	return nil
}

type termHandlerUpstream struct {
	calls int
	body  string
}

func (u *termHandlerUpstream) Do(_ *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.calls++
	body := u.body
	if body == "" {
		body = `{"type":"message","model":"claude-sonnet-4","content":[],"usage":{"input_tokens":0,"output_tokens":0}}`
	}
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
}
func (u *termHandlerUpstream) DoWithTLS(req *http.Request, proxy string, id int64, n int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxy, id, n)
}

func termHandlerKey() *service.APIKey {
	gid := int64(6)
	return &service.APIKey{ID: 21, UserID: 11, GroupID: &gid, Group: &service.Group{ID: gid, Platform: service.PlatformAnthropic, SubscriptionType: service.SubscriptionTypeSubscription, SubscriptionQuotaMode: "term", RateMultiplier: 1}}
}

func TestSubscriptionQuotaGatewayExactPathsAndIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	billing := &service.BillingCacheService{}
	billing.SetSubscriptionQuotaGatewayBilling(&termHandlerBilling{})
	h := &GatewayHandler{billingCacheService: billing}
	for _, path := range []string{"/v1/messages", "/v1/chat/completions", "/chat/completions", "/v1/responses", "/responses", "/backend-api/codex/responses"} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", path, nil)
		key := termHandlerKey()
		state, err := h.prepareSubscriptionQuotaForward(c, key, key, service.PlatformAnthropic, service.ChannelUsageFields{})
		require.NoError(t, err)
		require.NotNil(t, state)
		fromCtx, ok := service.SubscriptionQuotaForwardStateFromContext(c.Request.Context())
		require.True(t, ok)
		require.Same(t, state, fromCtx)
	}
	for _, path := range []string{"/v1/responses/compact", "/responses/compact", "/backend-api/codex/responses/compact", "/v1/responses/input_tokens", "/v1/messages/count_tokens", "/v1/embeddings", "/antigravity/v1/messages"} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", path, nil)
		key := termHandlerKey()
		_, err := h.prepareSubscriptionQuotaForward(c, key, key, service.PlatformAnthropic, service.ChannelUsageFields{})
		require.ErrorIs(t, err, service.ErrSubscriptionQuotaEndpointUnsupported)
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/v1/responses", nil)
	key := termHandlerKey()
	_, err := h.prepareSubscriptionQuotaForward(c, key, key, service.PlatformAnthropic, service.ChannelUsageFields{})
	require.Error(t, err)
	c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
	other := *key
	otherID := int64(7)
	other.GroupID = &otherID
	_, err = h.prepareSubscriptionQuotaForward(c, key, &other, service.PlatformAnthropic, service.ChannelUsageFields{})
	require.Error(t, err)
	_, err = h.prepareSubscriptionQuotaForward(c, key, key, service.PlatformGemini, service.ChannelUsageFields{})
	require.Error(t, err)
	h.billingCacheService = nil
	_, err = h.prepareSubscriptionQuotaForward(c, key, key, service.PlatformAnthropic, service.ChannelUsageFields{})
	require.ErrorIs(t, err, service.ErrSubscriptionQuotaBillingUnavailable)
}

func TestSubscriptionQuotaGatewayStableErrors(t *testing.T) {
	for _, failure := range []error{service.ErrSubscriptionQuotaExhausted, service.ErrSubscriptionQuotaNotActive, service.ErrSubscriptionQuotaReconciliation, service.ErrSubscriptionQuotaEndpointUnsupported} {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
		h := &GatewayHandler{}
		require.True(t, h.writeSubscriptionQuotaForwardError(c, failure, false))
		require.Contains(t, rec.Body.String(), "TERM_")
		require.NotEqual(t, 502, rec.Code)
	}
}

func TestSubscriptionQuotaGatewayMandatoryUsageAfterHTTPReturn(t *testing.T) {
	gin.SetMode(gin.TestMode)
	billing := &termHandlerBilling{}
	cache := &service.BillingCacheService{}
	cache.SetSubscriptionQuotaGatewayBilling(billing)
	pool := service.NewUsageRecordWorkerPoolWithOptions(service.UsageRecordWorkerPoolOptions{WorkerCount: 1, QueueSize: 1, TaskTimeout: 5 * time.Second, OverflowPolicy: "drop"})
	defer pool.Stop()
	cfg := &config.Config{Gateway: config.GatewayConfig{MaxLineSize: 4 * 1024 * 1024}}
	upstream := &termHandlerUpstream{}
	svc := service.NewGatewayService(nil, nil, nil, nil, nil, nil, nil, nil, cfg, nil, nil, service.NewBillingService(cfg, nil), nil, cache, nil, upstream, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	h := &GatewayHandler{billingCacheService: cache, usageRecordWorkerPool: pool}
	started, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	var state *service.SubscriptionQuotaForwardState
	router := gin.New()
	router.POST("/v1/messages", func(c *gin.Context) {
		key := termHandlerKey()
		var err error
		state, err = h.prepareSubscriptionQuotaForward(c, key, key, service.PlatformAnthropic, service.ChannelUsageFields{OriginalModel: "claude-sonnet-4"})
		require.NoError(t, err)
		defer finishSubscriptionQuotaForward(state)
		body := []byte(`{"model":"claude-sonnet-4","messages":[{"role":"user","content":"hi"}]}`)
		parsed, err := service.ParseGatewayRequest(service.NewRequestBodyRef(body), service.PlatformAnthropic)
		require.NoError(t, err)
		result, err := svc.Forward(c.Request.Context(), c, &service.Account{ID: 51, Platform: service.PlatformAnthropic, Type: service.AccountTypeAPIKey, Credentials: map[string]any{"api_key": "synthetic"}, Extra: map[string]any{"anthropic_passthrough": true}}, parsed)
		require.NoError(t, err)
		h.submitProtectedGroupUsageRecordTask(c.Request.Context(), nil, result, 51, time.Now(), func(ctx context.Context) {
			close(started)
			<-release
			snapshot, ok := service.SubscriptionQuotaBillingSnapshotFromContext(ctx)
			require.True(t, ok)
			require.NoError(t, ctx.Err())
			require.EqualValues(t, 41, snapshot.TermID)
			require.EqualValues(t, 51, snapshot.AccountID)
			require.True(t, strings.HasPrefix(snapshot.RequestID, "subquota:"))
			close(done)
		})
	})
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("POST", "/v1/messages", nil).WithContext(parent))
	require.Equal(t, 200, response.Code)
	cancel()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("mandatory usage did not start")
	}
	require.NotNil(t, state.Snapshot())
	mutable := state.Snapshot()
	mutable.TermID = 999
	mutable.PricingSnapshot[0] = 'x'
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("mandatory usage did not finish")
	}
	state.StopHeartbeat()
	require.Equal(t, 1, upstream.calls)
}

func TestSubscriptionQuotaGatewayAfterErrorPreservesFrozenForceCache(t *testing.T) {
	gin.SetMode(gin.TestMode)
	billing := &termHandlerBilling{known: make(chan termHandlerKnownFact, 1)}
	cache := &service.BillingCacheService{}
	cache.SetSubscriptionQuotaGatewayBilling(billing)
	pool := service.NewUsageRecordWorkerPoolWithOptions(service.UsageRecordWorkerPoolOptions{WorkerCount: 1, QueueSize: 1, TaskTimeout: 5 * time.Second, OverflowPolicy: "drop"})
	defer pool.Stop()
	cfg := &config.Config{Gateway: config.GatewayConfig{MaxLineSize: 4 << 20}}
	upstream := &termHandlerUpstream{body: `{"type":"message","model":"claude-sonnet-4","content":[],"usage":{"input_tokens":13,"output_tokens":5}}`}
	svc := service.NewGatewayService(nil, nil, termDisconnectLogs{}, termDisconnectBillingRepo{}, nil, nil, nil, nil, cfg, nil, nil, service.NewBillingService(cfg, nil), nil, cache, nil, upstream, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	h := &GatewayHandler{gatewayService: svc, billingCacheService: cache, usageRecordWorkerPool: pool}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/messages", nil).WithContext(service.WithForceCacheBilling(context.Background()))
	key := termHandlerKey()
	key.User = &service.User{ID: key.UserID, Status: service.StatusActive}
	channel := service.ChannelUsageFields{OriginalModel: "claude-sonnet-4"}
	state, err := h.prepareSubscriptionQuotaForward(c, key, key, service.PlatformAnthropic, channel)
	require.NoError(t, err)
	defer state.StopHeartbeat()
	payload := []byte(`{"model":"claude-sonnet-4","messages":[{"role":"user","content":"hello"}]}`)
	parsed, err := service.ParseGatewayRequest(service.NewRequestBodyRef(payload), service.PlatformAnthropic)
	require.NoError(t, err)
	account := &service.Account{ID: 51, Platform: service.PlatformAnthropic, Type: service.AccountTypeAPIKey, Credentials: map[string]any{"api_key": "synthetic"}, Extra: map[string]any{"anthropic_passthrough": true}}
	result, err := svc.Forward(c.Request.Context(), c, account, parsed)
	require.NoError(t, err)
	require.True(t, result.UsageObserved && result.UsageFinal)
	// Exercise the trusted-final-usage adapter-error branch, using the same
	// force-cache context which was frozen at the real before-send admission.
	h.submitSubscriptionQuotaUsageAfterError(c, state, key, account, nil, result, time.Now(), channel, payload)
	require.NoError(t, state.FinishForward())
	select {
	case fact := <-billing.known:
		cmd, err := service.DecodeSubscriptionQuotaUsageBillingReceipt(fact.billing)
		require.NoError(t, err)
		require.Zero(t, cmd.InputTokens)
		require.Equal(t, 13, cmd.CacheReadTokens)
		require.Equal(t, 5, cmd.OutputTokens)
	case <-time.After(2 * time.Second):
		t.Fatal("trusted final bill lost its frozen force-cache scalar")
	}
	require.Equal(t, 1, upstream.calls)
}
