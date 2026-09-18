package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestOpenAICodexTicketFailOpenAllowsMissingAndExpiredRequests(t *testing.T) {
	for _, model := range []string{"gpt-6-astra", "gpt-5.6-sol"} {
		for _, state := range []string{"missing", "expired"} {
			t.Run(model+"/"+state, func(t *testing.T) {
				upstream := &httpUpstreamRecorder{}
				svc := ticketTestService(t, config.OpenAICodexTicketConfig{
					Enabled: true, FailClosed: false, TargetLength: 292,
					Models: []string{"gpt-6-astra", "gpt-5.6-sol"},
				}, upstream)
				account := ticketTestAccount(41)
				if state == "expired" {
					svc.storeOpenAICodexTicket(context.Background(), account, &openAICodexTicket{
						AccountID: account.ID, Model: model, State: fakeCodexTicketState(292), Length: 292,
						CapturedAt: time.Now().Add(-2 * time.Hour), ExpiresAt: time.Now().Add(-time.Minute),
					})
				}
				// Both scheduler admission and request-header injection must allow
				// the request without synchronously probing or injecting an expired ticket.
				require.False(t, svc.isOpenAIAccountRequestRuntimeBlocked(account, model, false))
				for _, incoming := range []string{"", "client-state"} {
					headers := http.Header{}
					if incoming != "" {
						headers.Set(openAICodexTurnStateHeader, incoming)
					}
					require.NoError(t, svc.applyOpenAICodexTicket(context.Background(), account, model, headers))
					require.Equal(t, incoming, headers.Get(openAICodexTurnStateHeader))
				}
				require.Empty(t, upstream.requests)
				// Ensure this fixture is actually ticket-gated: fail-closed reverses admission.
				svc.cfg.Gateway.OpenAICodexTicket.FailClosed = true
				require.True(t, svc.isOpenAIAccountRequestRuntimeBlocked(account, model, false))
				require.Error(t, svc.applyOpenAICodexTicket(context.Background(), account, model, http.Header{}))
				require.Empty(t, upstream.requests)
			})
		}
	}
}
