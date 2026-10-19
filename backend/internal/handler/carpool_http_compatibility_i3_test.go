//go:build unit

package handler

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type carpoolCompatibilityHTTPUpstream struct{ service.HTTPUpstream }

func (*carpoolCompatibilityHTTPUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	return (&http.Client{Timeout: 5 * time.Second}).Do(req)
}
func (u *carpoolCompatibilityHTTPUpstream) DoWithTLS(req *http.Request, proxy string, id int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxy, id, concurrency)
}

// T22 HTTP/SSE transport compatibility is separate from PG's durable old-cycle
// recovery. The admission port selects successive canonical cycles; both real
// HTTP handlers must preserve those immutable identities through known/Apply/log.
func TestCarpoolHTTPCompatibilityI3ResponsesUseFreshCycleSnapshots(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "JSON", true: "SSE"}[stream], func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			var providerCalls atomic.Int32
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					w.WriteHeader(400)
					return
				}
				call := providerCalls.Add(1)
				response := fmt.Sprintf(`{"id":"resp_cycle_%d","object":"response","model":"gpt-5.2","status":"completed","output":[],"usage":{"input_tokens":2,"output_tokens":1,"total_tokens":3}}`, call)
				if gjson.GetBytes(body, "stream").Bool() {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = fmt.Fprintf(w, "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":%s}\n\n", response)
				} else {
					w.Header().Set("Content-Type", "application/json")
					_, _ = io.WriteString(w, response)
				}
			}))
			defer provider.Close()
			cfg := &config.Config{RunMode: config.RunModeStandard}
			cfg.Default.RateMultiplier = 1
			cfg.Security.URLAllowlist.Enabled = false
			cfg.Security.URLAllowlist.AllowInsecureHTTP = true
			groupID := int64(4201)
			account := service.Account{ID: 9901, Name: "synthetic-carpool-http", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Concurrency: 1, Credentials: map[string]any{"api_key": "synthetic", "base_url": provider.URL}, Extra: map[string]any{"openai_passthrough": true}}
			accountRepo := &openAIWSUsageHandlerAccountRepoStub{account: account}
			logs := &openAIWSUsageHandlerUsageLogRepoStub{created: make(chan *service.UsageLog, 2)}
			billing := &carpoolWSBillingStub{canonicalStart: time.Date(2026, 9, 6, 10, 0, 0, 123456000, time.UTC)}
			applier := &carpoolWSUsageBillingRepoStub{}
			cache := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
			defer cache.Stop()
			cache.SetCarpoolGatewayBilling(billing)
			gateway := service.NewOpenAIGatewayService(accountRepo, logs, applier, nil, nil, nil, nil, cfg, nil, nil, service.NewBillingService(cfg, nil), nil, cache, &carpoolCompatibilityHTTPUpstream{}, &service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil)
			concurrency := &concurrencyCacheMock{
				acquireUserSlotFn:    func(context.Context, int64, int, string) (bool, error) { return true, nil },
				acquireAccountSlotFn: func(context.Context, int64, int, string) (bool, error) { return true, nil },
			}
			h := &OpenAIGatewayHandler{gatewayService: gateway, billingCacheService: cache, apiKeyService: &service.APIKeyService{}, concurrencyHelper: NewConcurrencyHelper(service.NewConcurrencyService(concurrency), SSEPingFormatNone, time.Second), cfg: cfg, maxAccountSwitches: 1}
			key := &service.APIKey{ID: 1801, UserID: 1701, GroupID: &groupID, User: &service.User{ID: 1701, Status: service.StatusActive}, Group: &service.Group{ID: groupID, Platform: service.PlatformOpenAI, Status: service.StatusActive, SubscriptionType: service.SubscriptionTypeCarpool, RateMultiplier: 1}}
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set(string(middleware.ContextKeyAPIKey), key)
				c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: key.UserID, Concurrency: 1})
				c.Next()
			})
			router.POST("/v1/responses", h.Responses)
			server := httptest.NewServer(router)
			defer server.Close()
			client := &http.Client{Timeout: 5 * time.Second}
			var published []*service.UsageLog
			for i := 0; i < 2; i++ {
				request, err := http.NewRequest("POST", server.URL+"/v1/responses", strings.NewReader(fmt.Sprintf(`{"model":"gpt-5.2","input":"turn %d","stream":%t}`, i+1, stream)))
				require.NoError(t, err)
				request.Header.Set("Content-Type", "application/json")
				response, err := client.Do(request)
				require.NoError(t, err)
				body, err := io.ReadAll(response.Body)
				_ = response.Body.Close()
				require.NoError(t, err)
				require.Equal(t, http.StatusOK, response.StatusCode, string(body))
				if stream {
					require.Contains(t, response.Header.Get("Content-Type"), "text/event-stream")
					require.Contains(t, string(body), "response.completed")
				} else {
					require.Equal(t, "completed", gjson.GetBytes(body, "status").String())
				}
				select {
				case log := <-logs.created:
					published = append(published, log)
				case <-time.After(5 * time.Second):
					t.Fatal("actual Responses request did not publish its cycle usage")
				}
			}
			admitted, ids, snapshots, known, marked, _ := billing.state()
			require.Equal(t, 2, admitted)
			require.Len(t, snapshots, 2)
			require.NotEqual(t, ids[0], ids[1])
			require.NotEqual(t, snapshots[0].BillingRequestID, snapshots[1].BillingRequestID)
			require.NotEqual(t, snapshots[0].CycleID, snapshots[1].CycleID)
			require.Equal(t, snapshots, known)
			require.Empty(t, marked)
			commands := applier.appliedCommands()
			require.Len(t, commands, 2)
			require.Len(t, published, 2)
			for i, snapshot := range snapshots {
				require.Equal(t, snapshot, *commands[i].CarpoolSnapshot)
				require.Equal(t, snapshot.TermID, *published[i].CarpoolTermID)
				require.Equal(t, snapshot.CycleID, *published[i].CarpoolCycleID)
				require.Equal(t, snapshot.AdmittedAt, *published[i].CarpoolAdmittedAt)
				require.Equal(t, stream, published[i].Stream)
				require.Equal(t, 2, published[i].InputTokens)
				require.Equal(t, 1, published[i].OutputTokens)
			}
			require.EqualValues(t, 2, providerCalls.Load())
		})
	}
}
