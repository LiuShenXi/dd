//go:build sentineldiagnostic

package service

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// CodexTicketDiagnosticInput is read only by the separately built diagnostic
// executable. No server route or normal binary exposes this diagnostic.
type CodexTicketDiagnosticInput struct {
	AccountID                       int64  `json:"account_id"`
	Model                           string `json:"model"`
	AccessToken                     string `json:"access_token"`
	ChatGPTAccountID                string `json:"chatgpt_account_id"`
	ChatGPTAccountIsFedRAMP         bool   `json:"chatgpt_account_is_fedramp"`
	AccountConcurrency              int    `json:"account_concurrency"`
	HarvestProxyURL                 string `json:"harvest_proxy_url"`
	OldTicketSHA256                 string `json:"old_ticket_sha256"`
	ResponseHeaderTimeoutSeconds    int    `json:"response_header_timeout_seconds"`
	URLAllowlistEnabled             bool   `json:"url_allowlist_enabled"`
	URLAllowlistAllowPrivateHosts   bool   `json:"url_allowlist_allow_private_hosts"`
	DisableCodexIdentityEnforcement bool   `json:"disable_codex_identity_enforcement"`
}

// ValidCodexTicketDiagnosticInput restricts the diagnostic to the authorized
// account/models, a dedicated proxy, and one fixed upstream URL in the native probe.
func ValidCodexTicketDiagnosticInput(in CodexTicketDiagnosticInput) bool {
	if in.AccountID != 2 || (in.Model != "gpt-6-astra" && in.Model != "gpt-5.6-sol") || strings.TrimSpace(in.AccessToken) == "" || strings.TrimSpace(in.ChatGPTAccountID) == "" || in.AccountConcurrency <= 0 || in.ResponseHeaderTimeoutSeconds < 0 {
		return false
	}
	if len(in.AccessToken) > 16384 || len(in.ChatGPTAccountID) > 256 || strings.ContainsAny(in.AccessToken+in.ChatGPTAccountID, "\r\n") {
		return false
	}
	hash, err := hex.DecodeString(in.OldTicketSHA256)
	if err != nil || len(hash) != sha256.Size {
		return false
	}
	proxy, err := url.Parse(in.HarvestProxyURL)
	if err != nil || proxy.Hostname() == "" || proxy.Fragment != "" {
		return false
	}
	switch proxy.Scheme {
	case "http", "https", "socks5", "socks5h":
		return true
	default:
		return false
	}
}

// CodexTicketDiagnosticReport contains only an allowlisted set of safe metadata.
type CodexTicketDiagnosticReport struct {
	HTTPStatus      int    `json:"http_status"`
	TicketLength    int    `json:"ticket_length"`
	TicketPrefixOK  bool   `json:"ticket_prefix_ok"`
	SameAsOldTicket bool   `json:"same_as_old_ticket"`
	ErrorCategory   string `json:"error_category"`
	DurationMS      int64  `json:"duration_ms"`
}

// RunCodexTicketDiagnosticProbe sends exactly one native synthetic ping. It does
// not start the gateway/harvester, retrieve credentials, persist tickets or read
// business traffic. The returned report never contains a raw error or header.
func RunCodexTicketDiagnosticProbe(ctx context.Context, upstream HTTPUpstream, in CodexTicketDiagnosticInput) CodexTicketDiagnosticReport {
	report := CodexTicketDiagnosticReport{}
	if upstream == nil || !ValidCodexTicketDiagnosticInput(in) {
		report.ErrorCategory = "invalid_configuration"
		return report
	}
	if ctx.Err() != nil {
		report.ErrorCategory = "canceled"
		return report
	}
	SetCodexIdentityEnforcementEnabled(!in.DisableCodexIdentityEnforcement)
	svc := &OpenAIGatewayService{httpUpstream: upstream}
	account := &Account{ID: in.AccountID, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Concurrency: in.AccountConcurrency, Credentials: map[string]any{"chatgpt_account_id": in.ChatGPTAccountID, "chatgpt_account_is_fedramp": in.ChatGPTAccountIsFedRAMP}}
	start := time.Now()
	state, status, err := svc.fireOpenAICodexTicketProbe(ctx, account, in.AccessToken, in.Model, in.HarvestProxyURL, 25*time.Second)
	report.DurationMS = time.Since(start).Milliseconds()
	report.HTTPStatus = status
	report.TicketLength = len(state)
	report.TicketPrefixOK = strings.HasPrefix(state, openAICodexTicketStatePrefix)
	digest := sha256.Sum256([]byte(state))
	report.SameAsOldTicket = strings.EqualFold(hex.EncodeToString(digest[:]), in.OldTicketSHA256)
	if err != nil {
		report.ErrorCategory = codexDiagnosticErrorCategory(err)
		return report
	}
	switch {
	case status != http.StatusOK:
		report.ErrorCategory = "http_non_200"
	case state == "":
		report.ErrorCategory = "missing_ticket"
	case len(state) == 312:
		report.ErrorCategory = "turn_state_312"
	case len(state) != 292:
		report.ErrorCategory = "unexpected_ticket_length"
	case !report.TicketPrefixOK:
		report.ErrorCategory = "invalid_ticket_prefix"
	case report.SameAsOldTicket:
		report.ErrorCategory = "same_ticket"
	default:
		report.ErrorCategory = "new_ticket"
	}
	return report
}

func codexDiagnosticErrorCategory(err error) string {
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return "dns"
	}
	var unknown x509.UnknownAuthorityError
	if errors.As(err, &unknown) {
		return "tls_certificate"
	}
	var certificate x509.CertificateInvalidError
	if errors.As(err, &certificate) {
		return "tls_certificate"
	}
	var hostname x509.HostnameError
	if errors.As(err, &hostname) {
		return "tls_certificate"
	}
	var network net.Error
	if errors.As(err, &network) {
		if network.Timeout() {
			return "timeout"
		}
		return "network"
	}
	return "transport"
}
