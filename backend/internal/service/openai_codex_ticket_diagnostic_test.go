//go:build sentineldiagnostic

package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func diagnosticFixture() CodexTicketDiagnosticInput {
	hash := sha256.Sum256([]byte(fakeCodexTicketState(292)))
	return CodexTicketDiagnosticInput{AccountID: 2, Model: "gpt-6-astra", AccessToken: "synthetic-diagnostic-token", ChatGPTAccountID: "synthetic-account-id", AccountConcurrency: 1, HarvestProxyURL: "http://synthetic-user:synthetic-password@proxy.invalid:8080", OldTicketSHA256: hex.EncodeToString(hash[:]), ResponseHeaderTimeoutSeconds: 600}
}
func TestCodexTicketDiagnosticNativeProbeOnceAndSafeReport(t *testing.T) {
	defer SetCodexIdentityEnforcementEnabled(true)
	for _, tc := range []struct {
		name   string
		status int
		state  string
		want   string
		same   bool
	}{
		{"same", 200, fakeCodexTicketState(292), "same_ticket", true},
		{"new", 200, openAICodexTicketStatePrefix + strings.Repeat("C", 292-len(openAICodexTicketStatePrefix)), "new_ticket", false},
		{"312", 200, fakeCodexTicketState(312), "turn_state_312", false},
		{"non200", 403, "", "http_non_200", false},
		{"missing", 200, "", "missing_ticket", false},
		{"length", 200, "short", "unexpected_ticket_length", false},
		{"prefix", 200, strings.Repeat("X", 292), "invalid_ticket_prefix", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := diagnosticFixture()
			body := &passthroughCloseTrackingReadCloser{Reader: strings.NewReader("must-not-read-response-body")}
			h := http.Header{}
			h.Set(openAICodexTurnStateHeader, tc.state)
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: tc.status, Header: h, Body: body}}
			report := RunCodexTicketDiagnosticProbe(context.Background(), upstream, in)
			require.Equal(t, tc.want, report.ErrorCategory)
			require.Equal(t, tc.same, report.SameAsOldTicket)
			require.Equal(t, tc.status, report.HTTPStatus)
			require.Equal(t, len(tc.state), report.TicketLength)
			require.Len(t, upstream.requests, 1)
			require.True(t, body.closed)
			req := upstream.requests[0]
			require.Equal(t, chatgptCodexURL, req.URL.String())
			require.Equal(t, HTTPUpstreamProfileOpenAIHarvest, HTTPUpstreamProfileFromContext(req.Context()))
			require.True(t, req.Close)
			require.Equal(t, "Bearer "+in.AccessToken, req.Header.Get("Authorization"))
			require.Equal(t, in.ChatGPTAccountID, req.Header.Get("chatgpt-account-id"))
			require.Equal(t, in.HarvestProxyURL, upstream.lastProxyURL)
			require.Contains(t, string(upstream.lastBody), `"text":"ping"`)
			require.Contains(t, string(upstream.lastBody), `"store":false`)
			require.Empty(t, req.Header.Get(openAICodexTurnStateHeader))
			raw, err := json.Marshal(report)
			require.NoError(t, err)
			for _, secret := range []string{in.AccessToken, in.ChatGPTAccountID, in.HarvestProxyURL, in.OldTicketSHA256, "must-not-read-response-body"} {
				require.NotContains(t, string(raw), secret)
			}
			if len(tc.state) > 20 {
				require.NotContains(t, string(raw), tc.state)
			}
		})
	}
}
func TestCodexTicketDiagnosticNoNetworkWithoutValidInput(t *testing.T) {
	for _, mutate := range []func(*CodexTicketDiagnosticInput){
		func(in *CodexTicketDiagnosticInput) { in.AccountID = 3 },
		func(in *CodexTicketDiagnosticInput) { in.Model = "gpt-5.6-luna" },
		func(in *CodexTicketDiagnosticInput) { in.HarvestProxyURL = "" },
		func(in *CodexTicketDiagnosticInput) { in.OldTicketSHA256 = "bad" },
		func(in *CodexTicketDiagnosticInput) { in.AccessToken = "" },
	} {
		in := diagnosticFixture()
		mutate(&in)
		upstream := &httpUpstreamRecorder{err: io.EOF}
		report := RunCodexTicketDiagnosticProbe(context.Background(), upstream, in)
		require.Equal(t, "invalid_configuration", report.ErrorCategory)
		require.Empty(t, upstream.requests)
	}
}
func TestCodexTicketDiagnosticErrorCategoriesNeverExposeErrors(t *testing.T) {
	for _, tc := range []struct {
		err      error
		category string
	}{
		{context.DeadlineExceeded, "timeout"},
		{context.Canceled, "canceled"},
		{&net.DNSError{Err: "private-proxy-secret", Name: "private-host-secret"}, "dns"},
		{errors.New("private-token-secret"), "transport"},
	} {
		upstream := &httpUpstreamRecorder{err: tc.err}
		report := RunCodexTicketDiagnosticProbe(context.Background(), upstream, diagnosticFixture())
		require.Equal(t, tc.category, report.ErrorCategory)
		require.Len(t, upstream.requests, 1)
		raw, err := json.Marshal(report)
		require.NoError(t, err)
		require.NotContains(t, string(raw), "secret")
	}
}
