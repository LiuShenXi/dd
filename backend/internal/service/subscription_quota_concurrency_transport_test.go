//go:build unit

package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
)

type termCancellationHTTPUpstream struct {
	target  *url.URL
	client  *http.Client
	billing *termForwardBillingStub
	calls   atomic.Int32
}

func (u *termCancellationHTTPUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.billing.mu.Lock()
	admitted := u.billing.snapshot != nil
	u.billing.mu.Unlock()
	if !admitted {
		return nil, fmt.Errorf("actual HTTP send before term admission")
	}
	u.calls.Add(1)
	copy := req.Clone(req.Context())
	address := *req.URL
	address.Scheme, address.Host = u.target.Scheme, u.target.Host
	copy.URL, copy.Host = &address, ""
	return u.client.Do(copy)
}
func (u *termCancellationHTTPUpstream) DoWithTLS(req *http.Request, proxy string, id int64, limit int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxy, id, limit)
}

func TestSubscriptionQuotaConcurrencyV2ActualHTTPUpstreamCancellationKeepsPartialUsageUnknown(t *testing.T) {
	for _, adapter := range []string{"messages", "passthrough", "chat", "responses"} {
		t.Run(adapter, func(t *testing.T) {
			started, providerCancelled := make(chan struct{}), make(chan struct{})
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = fmt.Fprint(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"partial\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude-sonnet-4\",\"content\":[],\"usage\":{\"input_tokens\":100,\"cache_creation_input_tokens\":0}}}\n\n")
				w.(http.Flusher).Flush()
				close(started)
				select {
				case <-r.Context().Done():
					close(providerCancelled)
				case <-time.After(5 * time.Second):
				}
			}))
			defer provider.Close()
			target, err := url.Parse(provider.URL)
			require.NoError(t, err)
			svc, c, billing, _, state := termForwardFixture(t, "/v1/messages")
			ctx, cancel := context.WithCancel(c.Request.Context())
			defer cancel()
			c.Request = c.Request.WithContext(ctx)
			upstream := &termCancellationHTTPUpstream{target: target, client: &http.Client{Timeout: 5 * time.Second}, billing: billing}
			svc.httpUpstream = upstream
			account := newAnthropicAPIKeyAccountForTest()
			if adapter == "passthrough" {
				account.Extra = map[string]any{"anthropic_passthrough": true}
			}
			done := make(chan *ForwardResult, 1)
			go func() {
				var result *ForwardResult
				switch adapter {
				case "chat":
					result, _ = svc.ForwardAsChatCompletions(ctx, c, account, []byte(`{"model":"claude-sonnet-4","stream":true,"messages":[{"role":"user","content":"hello"}]}`), nil)
				case "responses":
					result, _ = svc.ForwardAsResponses(ctx, c, account, []byte(`{"model":"claude-sonnet-4","stream":true,"input":"hello"}`), nil)
				default:
					parsed, parseErr := ParseGatewayRequest(NewRequestBodyRef([]byte(`{"model":"claude-sonnet-4","stream":true,"max_tokens":10,"messages":[{"role":"user","content":"hello"}]}`)), PlatformAnthropic)
					if parseErr != nil {
						done <- nil
						return
					}
					result, _ = svc.Forward(ctx, c, account, parsed)
				}
				done <- result
			}()
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				t.Fatal("actual provider did not receive admitted request")
			}
			cancel()
			select {
			case <-providerCancelled:
			case <-time.After(2 * time.Second):
				t.Fatal("term cancellation did not reach the actual HTTP provider")
			}
			select {
			case result := <-done:
				if result != nil {
					require.False(t, result.UsageFinal)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("local forward did not end after upstream cancellation")
			}
			require.NoError(t, state.FinishForward())
			billing.mu.Lock()
			require.Equal(t, 1, billing.reconciles)
			require.Zero(t, billing.noCharges)
			require.Equal(t, 1, billing.admits)
			billing.mu.Unlock()
			require.EqualValues(t, 1, upstream.calls.Load(), "unknown consumption must not trigger another actual send")
		})
	}
}
