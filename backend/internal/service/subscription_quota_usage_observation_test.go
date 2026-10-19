//go:build unit

package service

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newTermMeteringContext(t *testing.T) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
	c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), subscriptionQuotaForwardContextKey{}, &SubscriptionQuotaForwardState{}))
	beginSubscriptionQuotaMetering(c)
	return c
}

func TestSubscriptionQuotaMeteringPresenceAndFinality(t *testing.T) {
	for _, tc := range []struct {
		name            string
		events          []string
		observed, final bool
	}{
		{"explicit zero", []string{`{"type":"message","model":"raw-model","usage":{"input_tokens":0,"output_tokens":0}}`}, true, true},
		{"empty", []string{`{"type":"message","usage":{}}`}, false, false},
		{"missing", []string{`{"type":"message"}`}, false, false},
		{"null", []string{`{"type":"message","usage":null}`}, false, false},
		{"partial", []string{`{"type":"message_start","message":{"model":"raw-model","usage":{"input_tokens":3,"output_tokens":0}}}`, `{"type":"message_stop"}`}, true, false},
		{"complete stream", []string{`{"type":"message_start","message":{"model":"raw-model","usage":{"input_tokens":3,"output_tokens":0}}}`, `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":0}}`}, true, true},
		{"no terminal", []string{`{"type":"message_start","message":{"usage":{"input_tokens":3}}}`, `{"type":"message_delta","usage":{"output_tokens":4}}`}, true, false},
		{"negative", []string{`{"type":"message","usage":{"input_tokens":1,"output_tokens":-1}}`}, false, false},
		{"fraction", []string{`{"type":"message","usage":{"input_tokens":1,"output_tokens":1.5}}`}, false, false},
		{"string", []string{`{"type":"message","usage":{"input_tokens":1,"output_tokens":"2"}}`}, false, false},
		{"nullable cache", []string{`{"type":"message","usage":{"input_tokens":1,"output_tokens":2,"cache_read_input_tokens":null}}`}, false, false},
		{"nested cache", []string{`{"type":"message","usage":{"input_tokens":1,"output_tokens":2,"cache_creation":{"ephemeral_5m_input_tokens":3,"ephemeral_1h_input_tokens":4}}}`}, true, true},
		{"bad nested cache", []string{`{"type":"message","usage":{"input_tokens":1,"output_tokens":2,"cache_creation":{"ephemeral_1h_input_tokens":-4}}}`}, false, false},
		{"bedrock metrics", []string{`{"amazon-bedrock-invocationMetrics":{"inputTokenCount":0,"outputTokenCount":0}}`}, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newTermMeteringContext(t)
			for _, data := range tc.events {
				observeSubscriptionQuotaMetering(c, []byte(data), "test_raw")
			}
			result := &ForwardResult{}
			attachSubscriptionQuotaMetering(c, result)
			require.Equal(t, tc.observed, result.UsageObserved)
			require.Equal(t, tc.final, result.UsageFinal)
			require.Equal(t, tc.final, completeSubscriptionQuotaMetering(c))
		})
	}
}

func TestSubscriptionQuotaMeteringRawModelConflict(t *testing.T) {
	c := newTermMeteringContext(t)
	observeSubscriptionQuotaMetering(c, []byte(`{"type":"message_start","message":{"model":"actual-a","usage":{"input_tokens":1}}}`), "raw")
	observeSubscriptionQuotaMetering(c, []byte(`{"type":"message_delta","model":"actual-b","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`), "raw")
	result := &ForwardResult{}
	attachSubscriptionQuotaMetering(c, result)
	require.True(t, result.UsageFinal)
	require.True(t, result.UpstreamResponseModelConflict)
}

func TestSubscriptionQuotaMeteringAuthoritativeBucketsAndServerEvidence(t *testing.T) {
	c := newTermMeteringContext(t)
	observeSubscriptionQuotaMetering(c, []byte(`{"type":"message_start","message":{"usage":{"input_tokens":11,"cache_creation_input_tokens":7,"cache_creation":{"ephemeral_5m_input_tokens":3,"ephemeral_1h_input_tokens":4}}}}`), "raw")
	observeSubscriptionQuotaMetering(c, []byte(`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":0,"input_tokens":0}}`), "raw")
	result := &ForwardResult{Usage: ClaudeUsage{InputTokens: 999, OutputTokens: 999}}
	attachSubscriptionQuotaMetering(c, result)
	require.True(t, result.UsageFinal)
	require.Zero(t, result.Usage.InputTokens)
	require.Zero(t, result.Usage.OutputTokens)
	require.Equal(t, 3, result.Usage.CacheCreation5mTokens)
	require.Equal(t, 4, result.Usage.CacheCreation1hTokens)
	observeSubscriptionQuotaMetering(c, []byte(`{"type":"content_block_start","content_block":{"type":"server_tool_use","name":"web_search"}}`), "raw")
	attachSubscriptionQuotaMetering(c, result)
	require.False(t, result.UsageObserved)
	require.False(t, result.UsageFinal)
}
