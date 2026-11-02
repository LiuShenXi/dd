//go:build unit

package handler

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type termDisconnectUpstream struct{ reader io.ReadCloser }
type termDisconnectBillingRepo struct{ service.UsageBillingRepository }

func (termDisconnectBillingRepo) Apply(_ context.Context, cmd *service.UsageBillingCommand) (*service.UsageBillingApplyResult, error) {
	if err := cmd.Validate(); err != nil {
		return nil, err
	}
	return &service.UsageBillingApplyResult{Applied: true}, nil
}

type termDisconnectLogs struct{ service.UsageLogRepository }

func (termDisconnectLogs) Create(context.Context, *service.UsageLog) (bool, error) { return true, nil }

func (u termDisconnectUpstream) Do(*http.Request, string, int64, int) (*http.Response, error) {
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: u.reader}, nil
}
func (u termDisconnectUpstream) DoWithTLS(req *http.Request, proxy string, id int64, n int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxy, id, n)
}

// This uses a real TCP client cancellation, rather than a fake ResponseWriter.
// The original request context is cancelled before the final provider bill.
func TestSubscriptionQuotaGatewayTCPDisconnectMandatoryKnownPersistence(t *testing.T) {
	for _, complete := range []bool{true, false} {
		t.Run(map[bool]string{true: "complete bill", false: "missing final output"}[complete], func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			cfg := &config.Config{Gateway: config.GatewayConfig{MaxLineSize: 4 << 20}}
			billing := &termHandlerBilling{known: make(chan termHandlerKnownFact, 1), unknown: make(chan struct{}, 1)}
			cache := &service.BillingCacheService{}
			cache.SetSubscriptionQuotaGatewayBilling(billing)
			pool := service.NewUsageRecordWorkerPoolWithOptions(service.UsageRecordWorkerPoolOptions{WorkerCount: 1, QueueSize: 1, TaskTimeout: 5 * time.Second, OverflowPolicy: "drop"})
			defer pool.Stop()
			reader, writer := io.Pipe()
			defer reader.Close()
			defer writer.Close()
			svc := service.NewGatewayService(nil, nil, termDisconnectLogs{}, termDisconnectBillingRepo{}, nil, nil, nil, nil, cfg, nil, nil, service.NewBillingService(cfg, nil), nil, cache, nil, termDisconnectUpstream{reader: reader}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
			h := &GatewayHandler{gatewayService: svc, billingCacheService: cache, usageRecordWorkerPool: pool}
			clientGone, forwardDone, usageDone := make(chan struct{}), make(chan *service.ForwardResult, 1), make(chan struct{})
			router := gin.New()
			router.POST("/v1/messages", func(c *gin.Context) {
				incoming := c.Request.Context()
				go func() { <-incoming.Done(); close(clientGone) }()
				key := termHandlerKey()
				key.User = &service.User{ID: key.UserID, Status: service.StatusActive}
				state, err := h.prepareSubscriptionQuotaForward(c, key, key, service.PlatformAnthropic, service.ChannelUsageFields{OriginalModel: "claude-sonnet-4"})
				if err != nil {
					c.Status(503)
					return
				}
				defer finishSubscriptionQuotaForward(state)
				parsed, err := service.ParseGatewayRequest(service.NewRequestBodyRef([]byte(`{"model":"claude-sonnet-4","stream":true,"messages":[{"role":"user","content":"hello"}]}`)), service.PlatformAnthropic)
				if err != nil {
					c.Status(400)
					return
				}
				account := &service.Account{ID: 51, Platform: service.PlatformAnthropic, Type: service.AccountTypeAPIKey, Credentials: map[string]any{"api_key": "synthetic"}, Extra: map[string]any{"anthropic_passthrough": true}}
				result, _ := svc.Forward(c.Request.Context(), c, account, parsed)
				forwardDone <- result
				if result != nil {
					h.submitProtectedGroupUsageRecordTask(c.Request.Context(), nil, result, account.ID, time.Now(), func(ctx context.Context) {
						defer close(usageDone)
						_ = svc.RecordUsage(ctx, &service.RecordUsageInput{SubscriptionQuotaSnapshot: service.SubscriptionQuotaForwardBillingSnapshot(ctx), Result: result, APIKey: key, User: key.User, Account: account})
					})
				} else {
					close(usageDone)
				}
			})
			server := httptest.NewServer(router)
			defer server.Close()
			go func() {
				_, _ = io.WriteString(writer, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"type\":\"message\",\"model\":\"claude-sonnet-4\",\"usage\":{\"input_tokens\":13,\"cache_creation_input_tokens\":7}}}\n\n")
				<-clientGone
				for i := 0; i < 8; i++ {
					_, _ = fmt.Fprintf(writer, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":%q}}\n\n", strings.Repeat("x", 64<<10))
				}
				if complete {
					_, _ = io.WriteString(writer, "event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":5}}\n\n")
				}
				_, _ = io.WriteString(writer, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
				_ = writer.Close()
			}()
			clientCtx, cancel := context.WithCancel(context.Background())
			defer cancel()
			req, err := http.NewRequestWithContext(clientCtx, "POST", server.URL+"/v1/messages", nil)
			require.NoError(t, err)
			client := &http.Client{Timeout: 5 * time.Second}
			response, err := client.Do(req)
			require.NoError(t, err)
			cancel()
			_ = response.Body.Close()
			var result *service.ForwardResult
			select {
			case result = <-forwardDone:
			case <-time.After(5 * time.Second):
				_ = reader.Close()
				t.Fatal("upstream did not drain after TCP cancellation")
			}
			require.NotNil(t, result)
			require.True(t, result.ClientDisconnect)
			require.Equal(t, complete, result.UsageFinal)
			select {
			case <-usageDone:
			case <-time.After(5 * time.Second):
				t.Fatal("mandatory usage task did not finish")
			}
			if complete {
				select {
				case fact := <-billing.known:
					require.NoError(t, fact.ctxErr)
					command, err := service.DecodeSubscriptionQuotaUsageBillingReceipt(fact.billing)
					require.NoError(t, err)
					require.Equal(t, 13, command.InputTokens)
					require.Equal(t, 5, command.OutputTokens)
					require.EqualValues(t, 41, fact.snapshot.TermID)
				case <-time.After(time.Second):
					t.Fatal("complete disconnected bill never reached mandatory known persistence")
				}
			} else {
				select {
				case <-billing.unknown:
				case <-time.After(time.Second):
					t.Fatal("missing final output did not remain unknown")
				}
				select {
				case <-billing.known:
					t.Fatal("partial input/cache was mistaken for a zero final bill")
				default:
				}
			}
		})
	}
}
