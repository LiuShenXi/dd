//go:build unit

package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"hash/crc32"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

type termVertexTokenCache struct{ GeminiTokenCache }

func (*termVertexTokenCache) GetAccessToken(context.Context, string) (string, error) {
	return "synthetic-cached-vertex-token", nil
}

func termBedrockFrame(data string) []byte {
	payload, _ := json.Marshal(map[string]string{"bytes": base64.StdEncoding.EncodeToString([]byte(data))})
	var headers bytes.Buffer
	for _, header := range []struct{ key, value string }{{":event-type", "chunk"}, {":message-type", "event"}} {
		headers.WriteByte(byte(len(header.key)))
		headers.WriteString(header.key)
		headers.WriteByte(7)
		_ = binary.Write(&headers, binary.BigEndian, uint16(len(header.value)))
		headers.WriteString(header.value)
	}
	var frame bytes.Buffer
	_ = binary.Write(&frame, binary.BigEndian, uint32(16+headers.Len()+len(payload)))
	_ = binary.Write(&frame, binary.BigEndian, uint32(headers.Len()))
	_ = binary.Write(&frame, binary.BigEndian, crc32.ChecksumIEEE(frame.Bytes()))
	frame.Write(headers.Bytes())
	frame.Write(payload)
	_ = binary.Write(&frame, binary.BigEndian, crc32.ChecksumIEEE(frame.Bytes()))
	return frame.Bytes()
}

type termForwardBillingStub struct {
	mu                                        sync.Mutex
	admits, noCharges, reconciles, heartbeats int
	admitErr                                  error
	expectedAccountType                       string
	currentAccountType                        string
	snapshot                                  *domain.SubscriptionQuotaBillingSnapshot
}

func (b *termForwardBillingStub) Admit(ctx context.Context, in domain.SubscriptionQuotaAdmissionParams) (*domain.SubscriptionQuotaBillingSnapshot, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.admits++
	b.expectedAccountType = in.ExpectedAccountType
	if b.currentAccountType != "" && b.currentAccountType != in.ExpectedAccountType {
		return nil, ErrSubscriptionQuotaBillingUnavailable
	}
	if b.admitErr != nil {
		return nil, b.admitErr
	}
	at := time.Date(2026, 10, 19, 13, 0, 0, 0, time.UTC)
	raw, err := in.FreezePricing(at)
	if err != nil {
		return nil, err
	}
	b.snapshot = &domain.SubscriptionQuotaBillingSnapshot{SchemaVersion: 1, RequestRowID: 1, RequestID: in.RequestID, UserID: in.UserID, APIKeyID: in.APIKeyID, GroupID: in.GroupID, AccountID: in.AccountID, SubscriptionID: 31, TermID: 41, AdmittedAt: at, PricingAt: at, PricingSnapshot: raw}
	copy := b.snapshot.Clone()
	return &copy, nil
}
func (b *termForwardBillingStub) PersistKnownUsage(context.Context, *domain.SubscriptionQuotaBillingSnapshot, decimal.Decimal, json.RawMessage, json.RawMessage) error {
	return nil
}
func (b *termForwardBillingStub) MarkNoCharge(context.Context, *domain.SubscriptionQuotaBillingSnapshot, domain.SubscriptionQuotaNoChargeEvidence) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.noCharges++
	return nil
}
func (b *termForwardBillingStub) MarkReconcileRequired(context.Context, *domain.SubscriptionQuotaBillingSnapshot, string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.reconciles++
	return nil
}
func (b *termForwardBillingStub) Heartbeat(context.Context, *domain.SubscriptionQuotaBillingSnapshot) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.heartbeats++
	return nil
}

type termHTTPUpstream struct {
	billing  *termForwardBillingStub
	calls    int
	response func() *http.Response
	err      error
}

func (u *termHTTPUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.calls++
	if u.billing.snapshot == nil {
		return nil, errors.New("send happened before durable admission")
	}
	if u.err != nil {
		return nil, u.err
	}
	return u.response(), nil
}
func (u *termHTTPUpstream) DoWithTLS(req *http.Request, proxy string, id int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxy, id, concurrency)
}

func termUpstreamResponse(body string, stream bool) *http.Response {
	content := "application/json"
	if stream {
		content = "text/event-stream"
	}
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{content}, "X-Request-Id": []string{"client-cannot-override-server-id"}}, Body: io.NopCloser(strings.NewReader(body))}
}
func termSSE(input, output string) string {
	return "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude-sonnet-4\",\"content\":[],\"usage\":{\"input_tokens\":" + input + ",\"cache_creation_input_tokens\":30,\"cache_creation\":{\"ephemeral_5m_input_tokens\":10,\"ephemeral_1h_input_tokens\":20}}}}\n\nevent: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\nevent: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"answer\"}}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":" + output + "}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
}

func termForwardFixture(t *testing.T, path string) (*GatewayService, *gin.Context, *termForwardBillingStub, *termHTTPUpstream, *SubscriptionQuotaForwardState) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", path, nil)
	billing := &termForwardBillingStub{}
	ctx, state, err := ContextWithSubscriptionQuotaForward(c.Request.Context(), termPricingKey(), billing, ChannelUsageFields{OriginalModel: "claude-sonnet-4"})
	require.NoError(t, err)
	c.Request = c.Request.WithContext(ctx)
	upstream := &termHTTPUpstream{billing: billing}
	cfg := &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}
	svc := &GatewayService{cfg: cfg, httpUpstream: upstream, billingService: NewBillingService(cfg, nil), responseHeaderFilter: compileResponseHeaderFilter(cfg), rateLimitService: &RateLimitService{}, deferredService: &DeferredService{}}
	t.Cleanup(func() { require.NoError(t, state.FinishForward()); state.StopHeartbeat() })
	return svc, c, billing, upstream, state
}

func TestSubscriptionQuotaForwardNativeAdmissionAndUsage(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, accountType := range []string{AccountTypeAPIKey, AccountTypeOAuth, AccountTypeSetupToken} {
			t.Run(accountType+map[bool]string{true: "_stream", false: "_json"}[stream], func(t *testing.T) {
				svc, c, billing, upstream, state := termForwardFixture(t, "/v1/messages")
				account := newAnthropicAPIKeyAccountForTest()
				if accountType != AccountTypeAPIKey {
					account = newAnthropicOAuthAccountForPartialUsageTest()
					account.Type = accountType
					account.Credentials["token"] = "synthetic-setup-token"
				}
				upstream.response = func() *http.Response {
					if stream {
						return termUpstreamResponse(termSSE("0", "0"), true)
					}
					return termUpstreamResponse(`{"id":"msg","type":"message","role":"assistant","model":"claude-sonnet-4","content":[{"type":"text","text":"answer"}],"stop_reason":"end_turn","usage":{"input_tokens":0,"output_tokens":0}}`, false)
				}
				body := []byte(`{"model":"claude-sonnet-4","stream":` + map[bool]string{true: "true", false: "false"}[stream] + `,"max_tokens":10,"messages":[{"role":"user","content":"hello"}]}`)
				parsed, err := ParseGatewayRequest(NewRequestBodyRef(body), PlatformAnthropic)
				require.NoError(t, err)
				result, err := svc.Forward(c.Request.Context(), c, account, parsed)
				require.NoError(t, err)
				require.NotNil(t, result)
				require.True(t, result.UsageObserved)
				require.True(t, result.UsageFinal)
				require.Equal(t, 0, result.Usage.OutputTokens)
				require.Equal(t, 1, upstream.calls)
				require.Equal(t, 1, billing.admits)
				require.Equal(t, account.Type, billing.expectedAccountType)
				require.True(t, strings.HasPrefix(state.Snapshot().RequestID, "subquota:"))
				require.NotEqual(t, result.RequestID, state.Snapshot().RequestID)
				if stream {
					require.Equal(t, 20, result.Usage.CacheCreation1hTokens)
				}
				state.MarkUsageQueued()
				_, err = svc.Forward(c.Request.Context(), c, account, parsed)
				require.Error(t, err)
				require.Equal(t, 1, upstream.calls, "no second real send even on same account")
			})
		}
	}
}

func TestSubscriptionQuotaForwardConvertedEndpoints(t *testing.T) {
	for _, endpoint := range []string{"chat", "responses"} {
		for _, stream := range []bool{false, true} {
			t.Run(endpoint+map[bool]string{true: "_stream", false: "_buffered"}[stream], func(t *testing.T) {
				svc, c, billing, upstream, state := termForwardFixture(t, "/v1/"+endpoint)
				upstream.response = func() *http.Response { return termUpstreamResponse(termSSE("17", "2"), true) }
				body := []byte(`{"model":"claude-sonnet-4","stream":` + map[bool]string{true: "true", false: "false"}[stream] + `,"messages":[{"role":"user","content":"hello"}]}`)
				var result *ForwardResult
				var err error
				if endpoint == "chat" {
					result, err = svc.ForwardAsChatCompletions(c.Request.Context(), c, newAnthropicAPIKeyAccountForTest(), body, nil)
				} else {
					body = []byte(`{"model":"claude-sonnet-4","stream":` + map[bool]string{true: "true", false: "false"}[stream] + `,"input":"hello"}`)
					result, err = svc.ForwardAsResponses(c.Request.Context(), c, newAnthropicAPIKeyAccountForTest(), body, nil)
				}
				require.NoError(t, err)
				require.NotNil(t, result)
				require.True(t, result.UsageFinal)
				require.Equal(t, 17, result.Usage.InputTokens)
				require.Equal(t, 2, result.Usage.OutputTokens)
				require.Equal(t, 10, result.Usage.CacheCreation5mTokens)
				require.Equal(t, 20, result.Usage.CacheCreation1hTokens)
				require.Equal(t, 1, upstream.calls)
				require.Equal(t, AccountTypeAPIKey, billing.expectedAccountType)
				state.MarkUsageQueued()
			})
		}
	}
}

func TestSubscriptionQuotaForwardFailClosedBeforeSend(t *testing.T) {
	for _, endpoint := range []string{"messages", "chat", "responses"} {
		for _, tool := range []string{"web_search_20250305", "web_fetch_20250910", "code_execution", "unknown_server_builtin"} {
			t.Run(endpoint+"_"+tool, func(t *testing.T) {
				svc, c, billing, upstream, _ := termForwardFixture(t, "/v1/"+endpoint)
				body := []byte(`{"model":"claude-sonnet-4","input":"hello","messages":[{"role":"user","content":"hello"}],"tools":[{"type":"` + tool + `","name":"search"}]}`)
				var err error
				switch endpoint {
				case "messages":
					parsed, e := ParseGatewayRequest(NewRequestBodyRef(body), PlatformAnthropic)
					require.NoError(t, e)
					_, err = svc.Forward(c.Request.Context(), c, newAnthropicAPIKeyAccountForTest(), parsed)
				case "chat":
					_, err = svc.ForwardAsChatCompletions(c.Request.Context(), c, newAnthropicAPIKeyAccountForTest(), body, nil)
				default:
					_, err = svc.ForwardAsResponses(c.Request.Context(), c, newAnthropicAPIKeyAccountForTest(), body, nil)
				}
				require.ErrorIs(t, err, ErrSubscriptionQuotaEndpointUnsupported)
				require.Zero(t, billing.admits)
				require.Zero(t, upstream.calls)
			})
		}
	}
	for _, kind := range []string{"function", "custom", "bash_20250124", "computer_20250124", "text_editor_20250124", "memory_20250818", ""} {
		c := newTermMeteringContext(t)
		require.NoError(t, rejectUnsupportedSubscriptionQuotaTools(c.Request.Context(), []byte(`{"tools":[{"type":"`+kind+`","name":"web_search"}]}`)), "client function named web_search remains allowed")
	}
}

func TestSubscriptionQuotaForwardFailedAdmissionAndUnknown(t *testing.T) {
	t.Run("admission failure", func(t *testing.T) {
		svc, c, billing, upstream, _ := termForwardFixture(t, "/v1/messages")
		billing.admitErr = ErrSubscriptionQuotaExhausted
		body := []byte(`{"model":"claude-sonnet-4","messages":[{"role":"user","content":"hi"}]}`)
		parsed, err := ParseGatewayRequest(NewRequestBodyRef(body), PlatformAnthropic)
		require.NoError(t, err)
		_, err = svc.Forward(c.Request.Context(), c, newAnthropicAPIKeyAccountForTest(), parsed)
		require.Error(t, err)
		require.Zero(t, upstream.calls)
	})
	t.Run("transport unknown", func(t *testing.T) {
		svc, c, billing, upstream, state := termForwardFixture(t, "/v1/messages")
		upstream.err = errors.New("synthetic transport reset")
		body := []byte(`{"model":"claude-sonnet-4","messages":[{"role":"user","content":"hi"}]}`)
		parsed, err := ParseGatewayRequest(NewRequestBodyRef(body), PlatformAnthropic)
		require.NoError(t, err)
		_, err = svc.Forward(c.Request.Context(), c, newAnthropicAPIKeyAccountForTest(), parsed)
		require.Error(t, err)
		require.NoError(t, state.FinishForward())
		require.Equal(t, 1, upstream.calls)
		require.Equal(t, 1, billing.reconciles)
		require.Zero(t, billing.noCharges)
	})
}

func TestSubscriptionQuotaForwardAccountTypeMismatchBeforeSend(t *testing.T) {
	for _, endpoint := range []string{"messages", "chat", "responses"} {
		t.Run(endpoint, func(t *testing.T) {
			svc, c, billing, upstream, state := termForwardFixture(t, "/v1/"+endpoint)
			// The selected sender still has API-key semantics, while the final
			// admission port observes that the locked database row became OAuth.
			billing.currentAccountType = AccountTypeOAuth
			account := newAnthropicAPIKeyAccountForTest()
			body := []byte(`{"model":"claude-sonnet-4","input":"hello","messages":[{"role":"user","content":"hello"}]}`)
			var err error
			switch endpoint {
			case "messages":
				parsed, parseErr := ParseGatewayRequest(NewRequestBodyRef(body), PlatformAnthropic)
				require.NoError(t, parseErr)
				_, err = svc.Forward(c.Request.Context(), c, account, parsed)
			case "chat":
				_, err = svc.ForwardAsChatCompletions(c.Request.Context(), c, account, body, nil)
			default:
				_, err = svc.ForwardAsResponses(c.Request.Context(), c, account, body, nil)
			}
			require.ErrorIs(t, err, ErrSubscriptionQuotaBillingUnavailable)
			require.Equal(t, AccountTypeAPIKey, billing.expectedAccountType)
			require.Equal(t, 1, billing.admits)
			require.Nil(t, state.Snapshot())
			require.Zero(t, upstream.calls)
		})
	}
}

func TestSubscriptionQuotaForwardBedrockAndVertex(t *testing.T) {
	for _, kind := range []string{"bedrock_apikey", "bedrock_sigv4", "vertex"} {
		for _, stream := range []bool{false, true} {
			t.Run(kind+map[bool]string{true: "_stream", false: "_json"}[stream], func(t *testing.T) {
				svc, c, billing, upstream, state := termForwardFixture(t, "/v1/messages")
				account := &Account{ID: 401, Platform: PlatformAnthropic, Type: AccountTypeBedrock, Concurrency: 1, Credentials: map[string]any{"auth_mode": "apikey", "api_key": "synthetic", "aws_region": "us-east-1"}}
				if kind == "bedrock_sigv4" {
					account.Credentials = map[string]any{"aws_access_key_id": "synthetic-id", "aws_secret_access_key": "synthetic-secret", "aws_region": "us-east-1"}
				}
				account.Credentials["model_mapping"] = map[string]any{"claude-sonnet-4": "claude-sonnet-4-5"}
				if kind == "vertex" {
					account = newVertexServiceAccount(401)
					account.Credentials["service_account_json"] = map[string]any{"client_email": "synthetic@example.invalid", "private_key": "never-used-cache-hit", "project_id": "synthetic"}
					svc.claudeTokenProvider = &ClaudeTokenProvider{tokenCache: &termVertexTokenCache{}}
				}
				upstream.response = func() *http.Response {
					if !stream {
						return termUpstreamResponse(`{"id":"msg","type":"message","role":"assistant","model":"claude-sonnet-4","content":[],"stop_reason":"end_turn","usage":{"input_tokens":5,"output_tokens":2}}`, false)
					}
					if kind == "vertex" {
						return termUpstreamResponse(termSSE("5", "2"), true)
					}
					var frames bytes.Buffer
					for _, event := range []string{`{"type":"message_start","message":{"id":"msg","type":"message","role":"assistant","model":"claude-sonnet-4","content":[],"usage":{"input_tokens":5}}}`, `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"amazon-bedrock-invocationMetrics":{"inputTokenCount":5,"outputTokenCount":2}}`, `{"type":"message_stop"}`} {
						frames.Write(termBedrockFrame(event))
					}
					return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/vnd.amazon.eventstream"}}, Body: io.NopCloser(bytes.NewReader(frames.Bytes()))}
				}
				body := []byte(`{"model":"claude-sonnet-4","stream":` + map[bool]string{true: "true", false: "false"}[stream] + `,"max_tokens":10,"messages":[{"role":"user","content":"hello"}]}`)
				parsed, err := ParseGatewayRequest(NewRequestBodyRef(body), PlatformAnthropic)
				require.NoError(t, err)
				result, err := svc.Forward(c.Request.Context(), c, account, parsed)
				require.NoError(t, err)
				require.NotNil(t, result)
				require.True(t, result.UsageFinal)
				require.Equal(t, 5, result.Usage.InputTokens)
				require.Equal(t, 2, result.Usage.OutputTokens)
				require.Equal(t, 1, billing.admits)
				require.Equal(t, account.Type, billing.expectedAccountType)
				require.Equal(t, 1, upstream.calls)
				state.MarkUsageQueued()
			})
		}
	}
	for _, endpoint := range []string{"chat", "responses"} {
		t.Run("unsupported_bedrock_"+endpoint, func(t *testing.T) {
			svc, c, billing, upstream, _ := termForwardFixture(t, "/v1/"+endpoint)
			account := &Account{Platform: PlatformAnthropic, Type: AccountTypeBedrock}
			var err error
			if endpoint == "chat" {
				_, err = svc.ForwardAsChatCompletions(c.Request.Context(), c, account, []byte(`{}`), nil)
			} else {
				_, err = svc.ForwardAsResponses(c.Request.Context(), c, account, []byte(`{}`), nil)
			}
			require.ErrorIs(t, err, ErrSubscriptionQuotaEndpointUnsupported)
			require.Zero(t, billing.admits)
			require.Zero(t, upstream.calls)
		})
	}
}

func TestSubscriptionQuotaForwardConvertedDisconnectAndMissingUsage(t *testing.T) {
	for _, endpoint := range []string{"chat", "responses"} {
		for _, missing := range []bool{false, true} {
			t.Run(endpoint+map[bool]string{true: "_missing", false: "_disconnect"}[missing], func(t *testing.T) {
				svc, c, billing, upstream, state := termForwardFixture(t, "/v1/"+endpoint)
				payload := termSSE("17", "2")
				if missing {
					payload = strings.Split(payload, "event: message_delta")[0]
				} else {
					c.Writer = &failWriteResponseWriter{ResponseWriter: c.Writer}
				}
				upstream.response = func() *http.Response { return termUpstreamResponse(payload, true) }
				stream := !missing
				var result *ForwardResult
				var err error
				if endpoint == "chat" {
					result, err = svc.ForwardAsChatCompletions(c.Request.Context(), c, newAnthropicAPIKeyAccountForTest(), []byte(`{"model":"claude-sonnet-4","stream":`+map[bool]string{true: "true", false: "false"}[stream]+`,"messages":[{"role":"user","content":"hi"}]}`), nil)
				} else {
					result, err = svc.ForwardAsResponses(c.Request.Context(), c, newAnthropicAPIKeyAccountForTest(), []byte(`{"model":"claude-sonnet-4","stream":`+map[bool]string{true: "true", false: "false"}[stream]+`,"input":"hi"}`), nil)
				}
				if missing {
					require.Error(t, err)
					require.Nil(t, result)
					require.NoError(t, state.FinishForward())
					require.Equal(t, 1, billing.reconciles)
					require.Zero(t, billing.noCharges)
				} else {
					require.NoError(t, err)
					require.True(t, result.ClientDisconnect)
					require.True(t, result.UsageFinal)
					require.Equal(t, 2, result.Usage.OutputTokens)
					state.MarkUsageQueued()
				}
				require.Equal(t, 1, upstream.calls)
			})
		}
	}
}

func TestSubscriptionQuotaForwardHeartbeatThroughUsage(t *testing.T) {
	billing := &termForwardBillingStub{}
	state := &SubscriptionQuotaForwardState{billing: billing, snapshot: &domain.SubscriptionQuotaBillingSnapshot{RequestID: "subquota:test"}}
	state.mu.Lock()
	state.startHeartbeatLocked(5 * time.Millisecond)
	state.mu.Unlock()
	defer state.StopHeartbeat()
	state.MarkUsageQueued()
	require.NoError(t, state.FinishForward())
	require.Eventually(t, func() bool { billing.mu.Lock(); defer billing.mu.Unlock(); return billing.heartbeats > 0 }, time.Second, time.Millisecond)
	state.StopHeartbeat()
	billing.mu.Lock()
	count := billing.heartbeats
	billing.mu.Unlock()
	time.Sleep(15 * time.Millisecond)
	billing.mu.Lock()
	require.Equal(t, count, billing.heartbeats)
	require.Zero(t, billing.reconciles)
	billing.mu.Unlock()
}

func TestSubscriptionQuotaForwardInternalRetrySendsOnce(t *testing.T) {
	for _, tc := range []struct {
		name     string
		status   int
		oauth    bool
		noCharge bool
	}{{"api overload", 529, false, false}, {"oauth forbidden", 403, true, true}} {
		t.Run(tc.name, func(t *testing.T) {
			svc, c, billing, upstream, state := termForwardFixture(t, "/v1/messages")
			upstream.response = func() *http.Response {
				return &http.Response{StatusCode: tc.status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"type":"error","error":{"type":"overloaded_error","message":"synthetic rejection"}}`))}
			}
			account := newAnthropicAPIKeyAccountForTest()
			if tc.oauth {
				account = newAnthropicOAuthAccountForPartialUsageTest()
			} else {
				account.Credentials["custom_error_codes_enabled"] = true
				account.Credentials["custom_error_codes"] = []any{float64(401)}
			}
			body := []byte(`{"model":"claude-sonnet-4","messages":[{"role":"user","content":"hello"}]}`)
			parsed, err := ParseGatewayRequest(NewRequestBodyRef(body), PlatformAnthropic)
			require.NoError(t, err)
			_, err = svc.Forward(c.Request.Context(), c, account, parsed)
			require.Error(t, err)
			require.Equal(t, 1, upstream.calls)
			require.Equal(t, 1, billing.admits)
			require.NoError(t, state.FinishForward())
			if tc.noCharge {
				require.Equal(t, 1, billing.noCharges)
				require.Zero(t, billing.reconciles)
			} else {
				require.Equal(t, 1, billing.reconciles)
				require.Zero(t, billing.noCharges)
			}
		})
	}
}
