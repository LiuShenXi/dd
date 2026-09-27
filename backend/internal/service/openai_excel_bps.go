package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"github.com/Wei-Shaw/sub2api/internal/util/logredact"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

var excelBPSReplay basispoints.ReplayCache
var excelBPSOpaque = &basispoints.OpaqueStore{Root: os.Getenv("BPS_OPAQUE_DIR")}

// Restore at the last HTTP request boundary, including native compact/fallback
// routes. A later scheduler choice must never send a local reference upstream.
func restoreBPSOpaqueRequest(c *gin.Context, req *http.Request, body []byte) (*http.Request, error) {
	scope := fmt.Sprintf("key:%d", getAPIKeyIDFromContext(c))
	reader, length, err := excelBPSOpaque.OpenRequest(scope, body)
	if err != nil {
		return nil, err
	}
	if req.Body != nil {
		_ = req.Body.Close()
	}
	req.Body, req.ContentLength = reader, length
	req.GetBody = func() (io.ReadCloser, error) { r, _, err := excelBPSOpaque.OpenRequest(scope, body); return r, err }
	return req, nil
}

func excelBPSAccountID(account *Account, accessToken string) string {
	if accountID := strings.TrimSpace(account.GetChatGPTAccountID()); accountID != "" {
		return accountID
	}
	claims, err := openai.DecodeIDToken(accessToken)
	if err != nil || claims.OpenAIAuth == nil {
		return ""
	}
	return strings.TrimSpace(claims.OpenAIAuth.ChatGPTAccountID)
}

func newExcelBPSRequest(ctx context.Context, body []byte, token, accountID string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, basispoints.ResponsesURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header = http.Header{
		"Authorization": {"Bearer " + token}, "Chatgpt-Account-Id": {accountID}, "X-Openai-Account-Id": {accountID},
		"X-Basispoints-Auth-Mode": {"chatgpt"}, "Content-Type": {"application/json"}, "Accept": {"text/event-stream"},
		"Origin": {"https://bps.openai.com"}, "User-Agent": {"Mozilla/5.0"},
		"X-Openai-Internal-Basispoints-Client-Product":       {"basispoints-excel-plugin"},
		"X-Openai-Internal-Basispoints-Client-Agent-Profile": {"excel"},
	}
	return req, nil
}

// BPS deliberately bypasses Codex ticket/cookie injection and OAuth plugins:
// only the selected account's bearer and ChatGPT account ID belong on this host.
func (s *OpenAIGatewayService) forwardExcelBPS(ctx context.Context, c *gin.Context, account *Account, body []byte, start time.Time) (*OpenAIForwardResult, error) {
	fail := func(status int, code, message string) (*OpenAIForwardResult, error) {
		// A compact keepalive may already have committed SSE headers. Otherwise
		// finish a single JSON response so the handler cannot append another error.
		committed := StopOpenAICompactSSEKeepaliveCommitted(c)
		MarkResponseCommitted(c)
		if committed {
			writeOpenAICompactSSEFailureMessage(c, status, code, message)
		} else {
			c.JSON(status, gin.H{"error": gin.H{"type": "invalid_request_error", "code": code, "message": message}})
		}
		return nil, fmt.Errorf("excel BPS: %s", code)
	}
	originalModel := gjson.GetBytes(body, "model").String()
	model := account.GetMappedModel(originalModel)
	stream := gjson.GetBytes(body, "stream").Bool()
	// Keep a streaming upstream request alive after downstream cancellation so
	// the terminal usage event can still be drained and billed.
	upstreamCtx, releaseUpstreamCtx := detachStreamUpstreamContext(ctx, stream)
	defer releaseUpstreamCtx()
	var err error
	body, err = sjson.SetBytes(body, "model", model)
	if err != nil {
		return fail(400, "basispoints_request_invalid", "Invalid model request")
	}
	identity, _ := resolveOpenAIWSExecutionScope(c, body, getAPIKeyIDFromContext(c))
	if identity != "" {
		body, err = sjson.SetBytes(body, "prompt_cache_key", identity)
		if err != nil {
			return nil, err
		}
	}
	if isOpenAIResponsesCompactPath(c) {
		var request map[string]any
		if err = json.Unmarshal(body, &request); err != nil {
			return fail(400, "basispoints_request_invalid", "Invalid compact request")
		}
		var input []any
		switch v := request["input"].(type) {
		case []any:
			input = v
		case string:
			input = []any{map[string]any{"role": "user", "content": v}}
		default:
			return fail(400, "basispoints_request_invalid", "Compact requires input")
		}
		request["input"] = append(input, map[string]any{"type": "compaction_trigger"})
		request["tool_choice"] = "none"
		body, err = json.Marshal(request)
		if err != nil {
			return nil, err
		}
	}
	scope := fmt.Sprintf("account:%d/key:%d/thread:%s", account.ID, getAPIKeyIDFromContext(c), identity)
	opaqueScope := fmt.Sprintf("key:%d", getAPIKeyIDFromContext(c))
	upstreamBody, bridge, err := basispoints.Prepare(body, scope, &excelBPSReplay)
	if err != nil {
		return fail(400, "basispoints_request_invalid", err.Error())
	}
	token, _, err := s.GetAccessToken(ctx, account)
	if err != nil {
		return fail(502, "basispoints_auth_unavailable", "Account OAuth credential is unavailable")
	}
	accountID := excelBPSAccountID(account, token)
	if accountID == "" {
		return fail(400, "basispoints_account_id_missing", "Excel BPS requires chatgpt_account_id")
	}
	// Include tenant/thread, upstream account and credential in the cache scope.
	// Only their digest is retained by AttachmentCache. Upload before inference,
	// but after Prepare so validation and turn fingerprints use original input.
	if bridge.HasInlineImages || bridge.HasToolImages {
		attachmentScope, _ := json.Marshal([]string{scope, accountID, token})
		upstreamBody, err = excelBPSAttachments.UploadImages(ctx, upstreamBody, string(attachmentScope), func(uploadCtx context.Context, mediaType string, data []byte) (string, error) {
			return s.uploadExcelBPSImage(uploadCtx, account, token, accountID, mediaType, data)
		})
		if err != nil {
			var attachmentErr *excelBPSAttachmentError
			if errors.As(err, &attachmentErr) {
				return fail(attachmentErr.status, attachmentErr.code, attachmentErr.message)
			}
			return fail(400, "basispoints_image_invalid", "Could not prepare the image attachment")
		}
	}
	requestCtx := WithHTTPUpstreamRedirectsDisabled(WithHTTPUpstreamProfile(upstreamCtx, HTTPUpstreamProfileLongStream))
	req, err := newExcelBPSRequest(requestCtx, upstreamBody, token, accountID)
	if err != nil {
		return nil, err
	}
	opaqueBody, opaqueLength, err := excelBPSOpaque.OpenRequest(opaqueScope, upstreamBody)
	if err != nil {
		return fail(400, "basispoints_compaction_unavailable", err.Error())
	}
	defer opaqueBody.Close()
	req.Body, req.ContentLength = opaqueBody, opaqueLength
	req.GetBody = func() (io.ReadCloser, error) {
		body, _, err := excelBPSOpaque.OpenRequest(opaqueScope, upstreamBody)
		return body, err
	}
	proxyURL := ""
	if account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	SetActualOpenAIUpstreamEndpoint(c, "/basispoints/api/responses")
	SetOpsUpstreamModel(c, model)
	sent := time.Now()
	resp, err := s.httpUpstream.Do(req, proxyURL, account.ID, account.Concurrency)
	SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(sent).Milliseconds())
	if err != nil {
		return fail(502, "basispoints_transport_error", "Excel BPS connection failed; request was not replayed")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 512<<10))
		// Preserve the original rejection for Ops without exposing it to clients.
		// BPS errors can echo request fields, so redact before storing diagnostics.
		upstreamMessage := fmt.Sprintf("Excel BPS returned HTTP %d", resp.StatusCode)
		upstreamDetail := ""
		if s.cfg != nil && s.cfg.Gateway.LogUpstreamErrorBody {
			safeBody := excelBPSSanitizeErrorBody(string(raw), token, account)
			maxBytes := s.cfg.Gateway.LogUpstreamErrorBodyMaxBytes
			if maxBytes <= 0 {
				maxBytes = 2048
			}
			upstreamDetail, _ = sanitizeErrorBodyForStorage(safeBody, maxBytes)
			if message := strings.TrimSpace(extractUpstreamErrorMessage([]byte(safeBody))); message != "" {
				upstreamMessage = truncateString(message, 2048)
			}
		}
		setOpsUpstreamError(c, resp.StatusCode, upstreamMessage, upstreamDetail)
		appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
			Platform: account.Platform, AccountID: account.ID, AccountName: account.Name,
			ProxyID: opsUpstreamProxyID(account), ProxyName: opsUpstreamProxyName(account),
			UpstreamStatusCode: resp.StatusCode, UpstreamRequestID: resp.Header.Get("x-request-id"),
			UpstreamURL: basispoints.ResponsesURL, Kind: "http_error",
			Message: upstreamMessage, Detail: upstreamDetail, UpstreamResponseBody: upstreamDetail,
		})
		code := gjson.GetBytes(raw, "error.code").String()
		if code == "basispoints_model_access_changed" {
			return fail(resp.StatusCode, code, "This model is not available on the account's Excel BPS endpoint")
		}
		return fail(resp.StatusCode, "basispoints_upstream_error", "Excel BPS rejected this request; account scheduling was not changed")
	}

	result := &OpenAIForwardResult{Model: originalModel, UpstreamModel: model, UpstreamEndpoint: "/basispoints/api/responses", Stream: stream, ReasoningEffort: &bridge.Effort, RequestedReasoningEffort: &bridge.RequestedEffort, RequestID: resp.Header.Get("x-request-id")}
	if stream {
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("X-Accel-Buffering", "no")
	}
	// Pass validated events directly. A second SSE scanner used to retain and
	// copy the whole large event again and impose a second hard line limit.
	events := make(chan basispoints.Event)
	ack := make(chan struct{})
	stop := make(chan struct{})
	done := make(chan struct{})
	readResult := make(chan error, 1)
	go func() {
		defer close(done)
		readResult <- bridge.Transform(resp.Body, func(event basispoints.Event) error {
			if err := excelBPSOpaque.CompactEvent(opaqueScope, event); err != nil {
				return err
			}
			select {
			case events <- event:
			case <-stop:
				return io.ErrClosedPipe
			}
			// The event's private spool must live until the consumer has written it.
			select {
			case <-ack:
				return nil
			case <-stop:
				return io.ErrClosedPipe
			}
		})
	}()
	defer func() { close(stop); _ = resp.Body.Close(); <-done }()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	clientDisconnected := false
	var clientDisconnectErr error
	terminal := ""
readEvents:
	for {
		select {
		case event := <-events:
			kind := event.Kind
			s.parseSSEUsageBytes(event.UsageJSON(), &result.Usage)
			if result.FirstTokenMs == nil && (kind == "response.output_text.delta" || kind == "response.output_item.added") {
				ms := int(time.Since(start).Milliseconds())
				result.FirstTokenMs = &ms
			}
			switch kind {
			case "response.completed", "response.failed", "response.incomplete", "error":
				terminal = kind
				result.ResponseID = event.ResponseID()
				result.UpstreamResponseModel = event.ResponseModel()
			}
			if !clientDisconnected {
				var writeErr error
				if stream {
					writeErr = event.WriteSSE(c.Writer)
					if writeErr == nil {
						c.Writer.Flush()
					}
				} else if kind == "response.completed" {
					c.Header("Content-Type", "application/json")
					c.Status(200)
					writeErr = event.WriteResponse(c.Writer)
				}
				if writeErr != nil {
					clientDisconnected = true
					result.ClientDisconnect = true
					clientDisconnectErr = writeErr
				}
			}
			ack <- struct{}{}
		case err = <-readResult:
			break readEvents
		case <-upstreamCtx.Done():
			err = upstreamCtx.Err()
			break readEvents
		case <-heartbeat.C:
			if stream && !clientDisconnected {
				if _, writeErr := c.Writer.WriteString(": keepalive\n\n"); writeErr != nil {
					clientDisconnected = true
					result.ClientDisconnect = true
					clientDisconnectErr = writeErr
				} else {
					c.Writer.Flush()
				}
			}
		}
	}
	result.Duration = time.Since(start)
	result.UpstreamTerminalEvent = terminal
	if err != nil || terminal == "" {
		if upstreamCtx.Err() != nil {
			result.ClientDisconnect = true
			return result, upstreamCtx.Err()
		}
		MarkResponseCommitted(c)
		if stream && !clientDisconnected {
			_, _ = c.Writer.WriteString("event: response.failed\ndata: {\"type\":\"response.failed\",\"response\":{\"status\":\"failed\",\"error\":{\"code\":\"basispoints_stream_incomplete\",\"message\":\"Upstream stream ended before completion\"}}}\n\n")
			c.Writer.Flush()
		} else if !clientDisconnected {
			c.JSON(502, gin.H{"error": gin.H{"code": "basispoints_stream_incomplete", "message": "Excel BPS stream ended before completion"}})
		}
		return result, fmt.Errorf("excel BPS stream incomplete")
	}
	if terminal != "response.completed" {
		MarkResponseCommitted(c)
	}
	if !stream && !clientDisconnected {
		if terminal != "response.completed" {
			c.JSON(502, gin.H{"error": gin.H{"code": "basispoints_protocol_error", "message": "Excel BPS did not complete the response"}})
		}
	}
	if terminal != "response.completed" {
		return result, fmt.Errorf("excel BPS terminal: %s", terminal)
	}
	s.bindHTTPResponseAccount(ctx, c, account, result.ResponseID)
	if clientDisconnected {
		if clientDisconnectErr == nil {
			clientDisconnectErr = errors.New("client disconnected after Excel BPS response started")
		}
		return result, clientDisconnectErr
	}
	return result, nil
}

var excelBPSBearerPattern = regexp.MustCompile(`(?i)\bBearer\s+[^\s"',;<>]+`)
var excelBPSURLCredentialsPattern = regexp.MustCompile(`(https?://)[^/\s@]+@`)

func excelBPSSanitizeErrorBody(raw, token string, account *Account) string {
	if !json.Valid([]byte(raw)) {
		return ""
	}
	secrets := append([]string{token}, excelBPSAccountSecrets(account)...)
	fields := make(map[string]string)
	for _, key := range []string{"message", "code", "type", "param"} {
		value := gjson.Get(raw, "error."+key)
		if value.Type != gjson.String {
			continue
		}
		clean := value.String()
		for _, secret := range secrets {
			if secret != "" {
				clean = strings.ReplaceAll(clean, secret, "[redacted]")
			}
		}
		clean = excelBPSBearerPattern.ReplaceAllString(clean, "Bearer [redacted]")
		clean = excelBPSURLCredentialsPattern.ReplaceAllString(clean, "${1}[redacted]@")
		clean = sanitizeUpstreamErrorMessage(clean)
		fields[key] = truncateString(logredact.RedactText(clean, "authorization", "api_key", "apikey", "token", "secret", "key", "cookie", "ticket", "recovery_ticket"), 2048)
	}
	encoded, _ := json.Marshal(map[string]any{"error": fields})
	return string(encoded)
}

func excelBPSAccountSecrets(account *Account) []string {
	var secrets []string
	for _, key := range []string{"access_token", "refresh_token", "id_token", "api_key", "session_key", "cookie"} {
		if value := account.GetCredential(key); value != "" {
			secrets = append(secrets, value)
		}
	}
	if account.Proxy != nil && account.Proxy.Password != "" {
		secrets = append(secrets, account.Proxy.Password)
	}
	return secrets
}
