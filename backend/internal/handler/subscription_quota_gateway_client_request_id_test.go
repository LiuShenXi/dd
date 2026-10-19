//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

type termRepeatedClientIDRecorder struct {
	service.SubscriptionQuotaGatewayBilling
	service.UsageBillingRepository
	service.UsageLogRepository
	mu                        sync.Mutex
	snapshots                 []domain.SubscriptionQuotaBillingSnapshot
	known, applied, published map[string]int
	ack                       chan int64
}

func (r *termRepeatedClientIDRecorder) Admit(_ context.Context, in domain.SubscriptionQuotaAdmissionParams) (*domain.SubscriptionQuotaBillingSnapshot, error) {
	if in.ExpectedAccountType != service.AccountTypeAPIKey {
		return nil, service.ErrSubscriptionQuotaBillingUnavailable
	}
	at := time.Now().UTC()
	pricing, err := in.FreezePricing(at)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	snapshot := domain.SubscriptionQuotaBillingSnapshot{SchemaVersion: 1, RequestRowID: int64(len(r.snapshots) + 1), RequestID: in.RequestID, UserID: in.UserID, APIKeyID: in.APIKeyID, GroupID: in.GroupID, AccountID: in.AccountID, AccountType: service.AccountTypeAPIKey, SubscriptionID: 31, TermID: 41, PricingAt: at, AdmittedAt: at, PricingSnapshot: pricing}
	_, snapshot.PricingSnapshotHash, err = domain.MarshalSubscriptionQuotaPricingEnvelope(snapshot)
	if err != nil {
		return nil, err
	}
	r.snapshots = append(r.snapshots, snapshot.Clone())
	return &snapshot, nil
}
func (*termRepeatedClientIDRecorder) Heartbeat(context.Context, *domain.SubscriptionQuotaBillingSnapshot) error {
	return nil
}
func (r *termRepeatedClientIDRecorder) PersistKnownUsage(_ context.Context, snapshot *domain.SubscriptionQuotaBillingSnapshot, cost decimal.Decimal, billing, usage json.RawMessage) error {
	command, err := service.DecodeSubscriptionQuotaUsageBillingReceipt(billing)
	if err != nil {
		return err
	}
	if !domain.SubscriptionQuotaSnapshotsEqual(*snapshot, *command.SubscriptionTermSnapshot) || !cost.Equal(command.SubscriptionTermCost) {
		return service.ErrSubscriptionQuotaReceiptConflict
	}
	if err := service.ValidateSubscriptionQuotaUsageLogPayload(usage, command); err != nil {
		return err
	}
	r.mu.Lock()
	r.known[snapshot.RequestID]++
	r.mu.Unlock()
	return nil
}
func (r *termRepeatedClientIDRecorder) Apply(_ context.Context, command *service.UsageBillingCommand) (*service.UsageBillingApplyResult, error) {
	if err := command.Validate(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.known[command.RequestID] != 1 || !command.SubscriptionTermCost.IsPositive() {
		return nil, fmt.Errorf("Apply must follow one positive durable known fact")
	}
	r.applied[command.RequestID]++
	return &service.UsageBillingApplyResult{Applied: true}, nil
}
func (r *termRepeatedClientIDRecorder) Create(_ context.Context, log *service.UsageLog) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.applied[log.RequestID] != 1 {
		return false, fmt.Errorf("publication must follow one Apply")
	}
	r.published[log.RequestID]++
	return true, nil
}
func (r *termRepeatedClientIDRecorder) MarkUsageLogRecorded(_ context.Context, id int64, _ string) error {
	r.ack <- id
	return nil
}
func (*termRepeatedClientIDRecorder) MarkNoCharge(context.Context, *domain.SubscriptionQuotaBillingSnapshot, domain.SubscriptionQuotaNoChargeEvidence) error {
	return fmt.Errorf("unexpected no-charge transition")
}
func (*termRepeatedClientIDRecorder) MarkReconcileRequired(context.Context, *domain.SubscriptionQuotaBillingSnapshot, string) error {
	return fmt.Errorf("unexpected unknown-usage transition")
}

type termRepeatedClientIDUpstream struct{ calls atomic.Int32 }

func (u *termRepeatedClientIDUpstream) Do(*http.Request, string, int64, int) (*http.Response, error) {
	u.calls.Add(1)
	// Repeating even the provider's own request ID must not collapse two real
	// calls into one server consumption identity.
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}, "X-Request-Id": []string{"same-provider-request-id"}}, Body: io.NopCloser(strings.NewReader(`{"type":"message","model":"claude-sonnet-4","content":[],"usage":{"input_tokens":2,"output_tokens":1}}`))}, nil
}
func (u *termRepeatedClientIDUpstream) DoWithTLS(req *http.Request, proxy string, id int64, n int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxy, id, n)
}

// T29: real HTTP -> actual Forward -> mandatory actual RecordUsage. Financial
// ports are synthetic; independent PG tests prove each receipt debits once.
func TestSubscriptionQuotaGatewayRepeatedClientRequestIDCreatesIndependentKnownFacts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{Gateway: config.GatewayConfig{MaxLineSize: 4 << 20}}
	recorder := &termRepeatedClientIDRecorder{known: map[string]int{}, applied: map[string]int{}, published: map[string]int{}, ack: make(chan int64, 2)}
	cache := &service.BillingCacheService{}
	cache.SetSubscriptionQuotaGatewayBilling(recorder)
	pool := service.NewUsageRecordWorkerPoolWithOptions(service.UsageRecordWorkerPoolOptions{WorkerCount: 1, QueueSize: 1, TaskTimeout: 5 * time.Second, OverflowPolicy: "drop"})
	defer pool.Stop()
	upstream := &termRepeatedClientIDUpstream{}
	svc := service.NewGatewayService(nil, nil, recorder, recorder, nil, nil, nil, nil, cfg, nil, nil, service.NewBillingService(cfg, nil), nil, cache, nil, upstream, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	h := &GatewayHandler{gatewayService: svc, billingCacheService: cache, usageRecordWorkerPool: pool}
	headerSeen := make(chan string, 2)
	router := gin.New()
	router.POST("/v1/messages", func(c *gin.Context) {
		headerSeen <- c.GetHeader("X-Request-ID")
		key := termHandlerKey()
		key.User = &service.User{ID: key.UserID, Status: service.StatusActive}
		state, err := h.prepareSubscriptionQuotaForward(c, key, key, service.PlatformAnthropic, service.ChannelUsageFields{OriginalModel: "claude-sonnet-4"})
		if err != nil {
			c.AbortWithStatusJSON(503, gin.H{"error": err.Error()})
			return
		}
		defer finishSubscriptionQuotaForward(state)
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.AbortWithStatus(400)
			return
		}
		parsed, err := service.ParseGatewayRequest(service.NewRequestBodyRef(body), service.PlatformAnthropic)
		if err != nil {
			c.AbortWithStatus(400)
			return
		}
		account := &service.Account{ID: 51, Platform: service.PlatformAnthropic, Type: service.AccountTypeAPIKey, Credentials: map[string]any{"api_key": "synthetic"}, Extra: map[string]any{"anthropic_passthrough": true}}
		result, err := svc.Forward(c.Request.Context(), c, account, parsed)
		if err != nil || result == nil || !result.UsageObserved || !result.UsageFinal {
			return
		}
		h.submitProtectedGroupUsageRecordTask(c.Request.Context(), nil, result, account.ID, time.Now(), func(ctx context.Context) {
			_ = svc.RecordUsage(ctx, &service.RecordUsageInput{SubscriptionQuotaSnapshot: service.SubscriptionQuotaForwardBillingSnapshot(ctx), Result: result, APIKey: key, User: key.User, Account: account})
		})
	})
	server := httptest.NewServer(router)
	defer server.Close()
	client := &http.Client{Timeout: 5 * time.Second}
	const clientID = "same-client-request-id"
	for i := 0; i < 2; i++ {
		request, err := http.NewRequest("POST", server.URL+"/v1/messages", strings.NewReader(`{"model":"claude-sonnet-4","messages":[{"role":"user","content":"hello"}]}`))
		require.NoError(t, err)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-Request-ID", clientID)
		request.Header.Set("Request-ID", clientID)
		response, err := client.Do(request)
		require.NoError(t, err)
		body, err := io.ReadAll(response.Body)
		_ = response.Body.Close()
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, response.StatusCode, string(body))
		require.Equal(t, clientID, <-headerSeen)
		select {
		case rowID := <-recorder.ack:
			require.EqualValues(t, i+1, rowID)
		case <-time.After(5 * time.Second):
			t.Fatal("real HTTP call did not reach known -> Apply -> log acknowledgement")
		}
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	require.Len(t, recorder.snapshots, 2)
	require.NotEqual(t, recorder.snapshots[0].RequestID, recorder.snapshots[1].RequestID)
	for _, snapshot := range recorder.snapshots {
		require.True(t, strings.HasPrefix(snapshot.RequestID, "subquota:"))
		_, err := uuid.Parse(strings.TrimPrefix(snapshot.RequestID, "subquota:"))
		require.NoError(t, err)
		require.NotEqual(t, clientID, snapshot.RequestID)
		require.NotEqual(t, "same-provider-request-id", snapshot.RequestID)
		require.Equal(t, 1, recorder.known[snapshot.RequestID])
		require.Equal(t, 1, recorder.applied[snapshot.RequestID])
		require.Equal(t, 1, recorder.published[snapshot.RequestID])
	}
	require.Len(t, recorder.known, 2)
	require.Len(t, recorder.applied, 2)
	require.EqualValues(t, 2, upstream.calls.Load())
}
