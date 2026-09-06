package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	coderws "github.com/coder/websocket"
)

const (
	defaultListenAddress = ":8090"
	defaultModel         = "gpt-4o-mini"
	maxRequestBodyBytes  = 64 << 10
	maxControlBodyBytes  = 16 << 10
	maxOutputBytes       = 4 << 10
	maxDelayMilliseconds = 10_000
	maxFailureCount      = 100
	maxTokenCount        = 10_000_000
	websocketWriteLimit  = 5 * time.Second
)

var (
	caseNamePattern               = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)
	syntheticAuthorizationPattern = regexp.MustCompile(`^Bearer synthetic-[0-9a-f]{16}-([12])$`)
)

type scenario struct {
	Name          string `json:"name"`
	InputTokens   int    `json:"input_tokens"`
	OutputTokens  int    `json:"output_tokens"`
	Output        string `json:"output"`
	DelayMS       int    `json:"delay_ms"`
	Status        int    `json:"status"`
	OmitUsage     bool   `json:"omit_usage"`
	WSFailureMode string `json:"ws_failure_mode,omitempty"`
}

type scenarioState struct {
	config            scenario
	failuresRemaining int
	usesRemaining     int
}

type controlRequest struct {
	Mode          string  `json:"mode"`
	Name          string  `json:"name"`
	InputTokens   *int    `json:"input_tokens"`
	OutputTokens  *int    `json:"output_tokens"`
	Output        *string `json:"output"`
	DelayMS       *int    `json:"delay_ms"`
	Status        *int    `json:"status"`
	Failures      *int    `json:"failures"`
	OmitUsage     *bool   `json:"omit_usage"`
	WSFailureMode *string `json:"ws_failure_mode"`
}

type inferenceRequest struct {
	Type   string `json:"type"`
	Model  string `json:"model"`
	Stream bool   `json:"stream"`
}

type statsSnapshot struct {
	Requests                      uint64                       `json:"requests"`
	WSConnections                 uint64                       `json:"ws_connections"`
	ByEndpoint                    map[string]uint64            `json:"by_endpoint"`
	ByTransport                   map[string]uint64            `json:"by_transport"`
	ByCase                        map[string]uint64            `json:"by_case"`
	ByStatus                      map[string]uint64            `json:"by_status"`
	ByCaseSyntheticAccountOutcome map[string]map[string]uint64 `json:"by_case_synthetic_account_outcome"`
	Rejected                      uint64                       `json:"rejected"`
	NextSequence                  uint64                       `json:"next_sequence"`
}

type requestStats struct {
	mu                            sync.Mutex
	requests                      uint64
	wsConnections                 uint64
	byEndpoint                    map[string]uint64
	byTransport                   map[string]uint64
	byCase                        map[string]uint64
	byStatus                      map[string]uint64
	byCaseSyntheticAccountOutcome map[string]map[string]uint64
	rejected                      uint64
}

type mockServer struct {
	sequence atomic.Uint64

	scenarioMu sync.Mutex
	named      map[string]*scenarioState
	next       *scenarioState
	defaults   scenario

	stats requestStats
}

func main() {
	listenAddress := flag.String("listen", defaultListenAddress, "listen address")
	flag.Parse()

	server := &http.Server{
		Addr:              *listenAddress,
		Handler:           newMockServer(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    32 << 10,
	}
	log.Printf("synthetic mock upstream listening on %s", *listenAddress)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func newMockServer() *mockServer {
	return &mockServer{
		named: make(map[string]*scenarioState),
		defaults: scenario{
			Name:         "default",
			InputTokens:  1000,
			OutputTokens: 100,
			Output:       "synthetic response",
			Status:       http.StatusServiceUnavailable,
		},
		stats: requestStats{
			byEndpoint:                    make(map[string]uint64),
			byTransport:                   make(map[string]uint64),
			byCase:                        make(map[string]uint64),
			byStatus:                      make(map[string]uint64),
			byCaseSyntheticAccountOutcome: make(map[string]map[string]uint64),
		},
	}
}

func (s *mockServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	sequence := s.sequence.Add(1)
	requestID := fmt.Sprintf("req_mock_%08d", sequence)
	w.Header().Set("X-Request-ID", requestID)
	w.Header().Set("Cache-Control", "no-store")

	switch r.URL.Path {
	case "/v1/models":
		s.handleModels(w, r, requestID)
	case "/v1/chat/completions", "/chat/completions":
		s.handleHTTPInference(w, r, "chat", requestID, sequence)
	case "/v1/responses", "/responses":
		if isWebSocketUpgrade(r) {
			s.handleWebSocketResponses(w, r)
			return
		}
		s.handleHTTPInference(w, r, "responses", requestID, sequence)
	case "/__control":
		s.handleControl(w, r, requestID)
	case "/__stats":
		s.handleStats(w, r, requestID)
	default:
		s.stats.record("unknown", "http", "routing", http.StatusNotFound, true)
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error":      map[string]any{"type": "not_found", "message": "synthetic route not found"},
			"request_id": requestID,
		})
	}
}

func (s *mockServer) handleModels(w http.ResponseWriter, r *http.Request, requestID string) {
	if r.Method != http.MethodGet {
		s.methodNotAllowed(w, r, requestID)
		return
	}
	s.stats.record(r.URL.Path, "http_json", "models", http.StatusOK, false)
	writeJSON(w, http.StatusOK, map[string]any{
		"object": "list",
		"data": []map[string]any{{
			"id": defaultModel, "object": "model", "created": int64(1_700_000_000), "owned_by": "synthetic",
		}},
		"request_id": requestID,
	})
}

func (s *mockServer) handleHTTPInference(w http.ResponseWriter, r *http.Request, kind, requestID string, sequence uint64) {
	if r.Method != http.MethodPost {
		s.methodNotAllowed(w, r, requestID)
		return
	}

	var req inferenceRequest
	if err := decodeBoundedJSON(r.Body, maxRequestBodyBytes, &req); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, errBodyTooLarge) {
			status = http.StatusRequestEntityTooLarge
		}
		s.stats.record(r.URL.Path, "http", "invalid", status, true)
		writeAPIError(w, status, requestID, "invalid_request_error", err.Error())
		return
	}
	if strings.TrimSpace(req.Model) == "" {
		req.Model = defaultModel
	}

	selectedCase := strings.TrimSpace(r.Header.Get("X-Mock-Case"))
	configured, fail := s.resolveScenario(selectedCase, req.Model)
	transport := "http_json"
	if req.Stream {
		transport = "http_sse"
	}
	if !waitForDelay(r.Context(), configured.DelayMS) {
		s.stats.record(r.URL.Path, transport, configured.Name, 499, true)
		return
	}
	if fail {
		s.stats.record(r.URL.Path, transport, configured.Name, configured.Status, false)
		writeAPIError(w, configured.Status, requestID, "synthetic_failure", "configured synthetic failure")
		return
	}

	responseID := responseID(kind, sequence)
	s.stats.record(r.URL.Path, transport, configured.Name, http.StatusOK, false)
	if kind == "chat" {
		if req.Stream {
			s.writeChatSSE(w, requestID, responseID, req.Model, configured, sequence)
			return
		}
		writeJSON(w, http.StatusOK, chatCompletion(responseID, req.Model, configured, sequence))
		return
	}
	if req.Stream {
		s.writeResponsesSSE(w, requestID, responseID, req.Model, configured, sequence)
		return
	}
	writeJSON(w, http.StatusOK, responsesPayload(responseID, req.Model, configured, sequence, "completed"))
}

func (s *mockServer) handleWebSocketResponses(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.methodNotAllowed(w, r, w.Header().Get("X-Request-ID"))
		return
	}
	conn, err := coderws.Accept(w, r, &coderws.AcceptOptions{CompressionMode: coderws.CompressionContextTakeover})
	if err != nil {
		return
	}
	defer func() { _ = conn.CloseNow() }()
	conn.SetReadLimit(maxRequestBodyBytes)
	s.stats.addWSConnection()

	for {
		messageType, payload, err := conn.Read(r.Context())
		if err != nil {
			return
		}
		if messageType != coderws.MessageText && messageType != coderws.MessageBinary {
			continue
		}

		sequence := s.sequence.Add(1)
		requestID := fmt.Sprintf("req_mock_%08d", sequence)
		var req inferenceRequest
		if len(payload) > maxRequestBodyBytes || json.Unmarshal(payload, &req) != nil || req.Type != "response.create" {
			s.stats.record(r.URL.Path, "websocket", "invalid", http.StatusBadRequest, true)
			s.writeWSJSON(r.Context(), conn, map[string]any{
				"type": "error", "request_id": requestID,
				"error": map[string]any{"type": "invalid_request_error", "message": "expected a bounded response.create JSON message"},
			})
			continue
		}
		if strings.TrimSpace(req.Model) == "" {
			req.Model = defaultModel
		}
		configured, fail := s.resolveScenario(strings.TrimSpace(r.Header.Get("X-Mock-Case")), req.Model)
		if !waitForDelay(r.Context(), configured.DelayMS) {
			return
		}
		responseID := responseID("responses", sequence)
		if fail {
			s.stats.record(r.URL.Path, "websocket", configured.Name, configured.Status, false)
			s.stats.recordSyntheticAccountOutcome(configured.Name, r.Header.Get("Authorization"), configured.Status)
			if configured.WSFailureMode == "rate_limit_error" {
				s.writeWSJSON(r.Context(), conn, map[string]any{
					"type": "error", "request_id": requestID,
					"error": map[string]any{
						"code": "rate_limit_exceeded", "type": "rate_limit_error", "message": "configured synthetic failure",
					},
				})
				continue
			}
			failed := responsesPayload(responseID, req.Model, configured, sequence, "failed")
			failed["output"] = []any{}
			failed["error"] = map[string]any{"code": "synthetic_failure", "message": "configured synthetic failure"}
			s.writeWSJSON(r.Context(), conn, map[string]any{
				"type": "response.failed", "request_id": requestID, "response": failed,
			})
			continue
		}

		s.stats.record(r.URL.Path, "websocket", configured.Name, http.StatusOK, false)
		s.stats.recordSyntheticAccountOutcome(configured.Name, r.Header.Get("Authorization"), http.StatusOK)
		for _, event := range responseEvents(requestID, responseID, req.Model, configured, sequence) {
			if err := s.writeWSJSON(r.Context(), conn, event); err != nil {
				return
			}
		}
	}
}

func (s *mockServer) writeWSJSON(parent context.Context, conn *coderws.Conn, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(parent, websocketWriteLimit)
	defer cancel()
	return conn.Write(ctx, coderws.MessageText, payload)
}

func (s *mockServer) handleControl(w http.ResponseWriter, r *http.Request, requestID string) {
	if !isPrivatePeer(r.RemoteAddr) {
		s.stats.record(r.URL.Path, "control", "control", http.StatusForbidden, true)
		writeAPIError(w, http.StatusForbidden, requestID, "forbidden", "control API is restricted to private network peers")
		return
	}
	if r.Method != http.MethodPost {
		s.methodNotAllowed(w, r, requestID)
		return
	}

	var input controlRequest
	if err := decodeStrictBoundedJSON(r.Body, maxControlBodyBytes, &input); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, errBodyTooLarge) {
			status = http.StatusRequestEntityTooLarge
		}
		s.stats.record(r.URL.Path, "control", "control", status, true)
		writeAPIError(w, status, requestID, "invalid_control", err.Error())
		return
	}
	input.Mode = strings.ToLower(strings.TrimSpace(input.Mode))
	state, err := buildScenarioState(input)
	if err != nil {
		s.stats.record(r.URL.Path, "control", "control", http.StatusBadRequest, true)
		writeAPIError(w, http.StatusBadRequest, requestID, "invalid_control", err.Error())
		return
	}

	s.scenarioMu.Lock()
	switch input.Mode {
	case "next":
		state.usesRemaining = state.failuresRemaining + 1
		s.next = state
	case "named":
		s.named[state.config.Name] = state
	}
	configuredSnapshot := state.config
	failuresRemaining := state.failuresRemaining
	s.scenarioMu.Unlock()

	s.stats.record(r.URL.Path, "control", configuredSnapshot.Name, http.StatusOK, false)
	writeJSON(w, http.StatusOK, map[string]any{
		"request_id":         requestID,
		"mode":               input.Mode,
		"case":               configuredSnapshot,
		"failures_remaining": failuresRemaining,
	})
}

func (s *mockServer) handleStats(w http.ResponseWriter, r *http.Request, requestID string) {
	if !isPrivatePeer(r.RemoteAddr) {
		writeAPIError(w, http.StatusForbidden, requestID, "forbidden", "stats API is restricted to private network peers")
		return
	}
	switch r.Method {
	case http.MethodGet:
		snapshot := s.stats.snapshot(s.sequence.Load() + 1)
		if r.URL.Query().Get("reset") == "1" || strings.EqualFold(r.URL.Query().Get("reset"), "true") {
			s.stats.reset()
		}
		writeJSON(w, http.StatusOK, map[string]any{"request_id": requestID, "stats": snapshot})
	case http.MethodDelete:
		s.stats.reset()
		writeJSON(w, http.StatusOK, map[string]any{"request_id": requestID, "reset": true})
	default:
		s.methodNotAllowed(w, r, requestID)
	}
}

func buildScenarioState(input controlRequest) (*scenarioState, error) {
	input.Mode = strings.ToLower(strings.TrimSpace(input.Mode))
	if input.Mode != "next" && input.Mode != "named" {
		return nil, errors.New(`mode must be "next" or "named"`)
	}
	name := strings.TrimSpace(input.Name)
	if input.Mode == "next" && name == "" {
		name = "next"
	}
	if !caseNamePattern.MatchString(name) {
		return nil, errors.New("name must contain only 1-64 letters, digits, dot, underscore, or hyphen")
	}

	configured := scenario{
		Name:         name,
		InputTokens:  1000,
		OutputTokens: 100,
		Output:       "synthetic response for " + name,
		Status:       http.StatusServiceUnavailable,
	}
	if input.InputTokens != nil {
		configured.InputTokens = *input.InputTokens
	}
	if input.OutputTokens != nil {
		configured.OutputTokens = *input.OutputTokens
	}
	if input.Output != nil {
		configured.Output = *input.Output
	}
	if input.DelayMS != nil {
		configured.DelayMS = *input.DelayMS
	}
	if input.Status != nil {
		configured.Status = *input.Status
	}
	if input.OmitUsage != nil {
		configured.OmitUsage = *input.OmitUsage
	}
	if input.WSFailureMode != nil {
		configured.WSFailureMode = strings.TrimSpace(*input.WSFailureMode)
	}
	failures := 0
	if input.Failures != nil {
		failures = *input.Failures
	}
	if configured.InputTokens < 0 || configured.InputTokens > maxTokenCount || configured.OutputTokens < 0 || configured.OutputTokens > maxTokenCount {
		return nil, fmt.Errorf("token counts must be between 0 and %d", maxTokenCount)
	}
	if len(configured.Output) > maxOutputBytes {
		return nil, fmt.Errorf("output exceeds %d bytes", maxOutputBytes)
	}
	if configured.DelayMS < 0 || configured.DelayMS > maxDelayMilliseconds {
		return nil, fmt.Errorf("delay_ms must be between 0 and %d", maxDelayMilliseconds)
	}
	if failures < 0 || failures > maxFailureCount {
		return nil, fmt.Errorf("failures must be between 0 and %d", maxFailureCount)
	}
	if configured.Status < 400 || configured.Status > 599 {
		return nil, errors.New("status must be an HTTP failure status between 400 and 599")
	}
	if configured.WSFailureMode != "" && configured.WSFailureMode != "rate_limit_error" {
		return nil, errors.New(`ws_failure_mode must be empty or "rate_limit_error"`)
	}
	if configured.WSFailureMode == "rate_limit_error" && configured.Status != http.StatusTooManyRequests {
		return nil, errors.New("rate_limit_error requires status 429")
	}
	return &scenarioState{config: configured, failuresRemaining: failures}, nil
}

func (s *mockServer) resolveScenario(explicitCase, model string) (scenario, bool) {
	s.scenarioMu.Lock()
	defer s.scenarioMu.Unlock()

	caseName := strings.TrimSpace(explicitCase)
	if caseName == "" {
		caseName = strings.TrimSpace(model)
	}
	if configured := s.named[caseName]; configured != nil {
		fail := configured.failuresRemaining > 0
		if fail {
			configured.failuresRemaining--
		}
		return configured.config, fail
	}
	if s.next != nil {
		configured := s.next
		fail := configured.failuresRemaining > 0
		if fail {
			configured.failuresRemaining--
		}
		configured.usesRemaining--
		if configured.usesRemaining <= 0 {
			s.next = nil
		}
		return configured.config, fail
	}
	return s.defaults, false
}

func (s *mockServer) methodNotAllowed(w http.ResponseWriter, r *http.Request, requestID string) {
	s.stats.record(r.URL.Path, "http", "routing", http.StatusMethodNotAllowed, true)
	writeAPIError(w, http.StatusMethodNotAllowed, requestID, "method_not_allowed", "synthetic route does not support this method")
}

func (s *mockServer) writeChatSSE(w http.ResponseWriter, requestID, responseID, model string, configured scenario, sequence uint64) {
	prepareSSE(w, requestID)
	created := int64(1_700_000_000 + sequence)
	writeSSEData(w, map[string]any{
		"id": responseID, "object": "chat.completion.chunk", "created": created, "model": model,
		"choices": []map[string]any{{"index": 0, "delta": map[string]any{"role": "assistant"}, "finish_reason": nil}},
	})
	writeSSEData(w, map[string]any{
		"id": responseID, "object": "chat.completion.chunk", "created": created, "model": model,
		"choices": []map[string]any{{"index": 0, "delta": map[string]any{"content": configured.Output}, "finish_reason": nil}},
	})
	writeSSEData(w, map[string]any{
		"id": responseID, "object": "chat.completion.chunk", "created": created, "model": model,
		"choices": []map[string]any{{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}},
		"usage":   chatUsage(configured),
	})
	_, _ = io.WriteString(w, "data: [DONE]\n\n")
	flush(w)
}

func (s *mockServer) writeResponsesSSE(w http.ResponseWriter, requestID, responseID, model string, configured scenario, sequence uint64) {
	prepareSSE(w, requestID)
	for _, event := range responseEvents(requestID, responseID, model, configured, sequence) {
		eventType, _ := event["type"].(string)
		writeSSEEvent(w, eventType, event)
	}
	flush(w)
}

func chatCompletion(responseID, model string, configured scenario, sequence uint64) map[string]any {
	return map[string]any{
		"id": responseID, "object": "chat.completion", "created": int64(1_700_000_000 + sequence), "model": model,
		"choices": []map[string]any{{
			"index": 0, "message": map[string]any{"role": "assistant", "content": configured.Output}, "finish_reason": "stop",
		}},
		"usage": chatUsage(configured),
	}
}

func chatUsage(configured scenario) map[string]any {
	return map[string]any{
		"prompt_tokens": configured.InputTokens, "completion_tokens": configured.OutputTokens,
		"total_tokens":          configured.InputTokens + configured.OutputTokens,
		"prompt_tokens_details": map[string]any{"cached_tokens": 0},
	}
}

func responsesPayload(responseID, model string, configured scenario, sequence uint64, status string) map[string]any {
	messageID := fmt.Sprintf("msg_mock_%08d", sequence)
	payload := map[string]any{
		"id": responseID, "object": "response", "created_at": int64(1_700_000_000 + sequence), "status": status, "model": model,
		"output": []map[string]any{{
			"id": messageID, "type": "message", "status": status, "role": "assistant",
			"content": []map[string]any{{"type": "output_text", "text": configured.Output, "annotations": []any{}}},
		}},
		"usage": map[string]any{
			"input_tokens": configured.InputTokens, "output_tokens": configured.OutputTokens,
			"total_tokens":          configured.InputTokens + configured.OutputTokens,
			"input_tokens_details":  map[string]any{"cached_tokens": 0},
			"output_tokens_details": map[string]any{"reasoning_tokens": 0},
		},
	}
	if configured.OmitUsage {
		delete(payload, "usage")
	}
	return payload
}

func responseEvents(requestID, responseID, model string, configured scenario, sequence uint64) []map[string]any {
	messageID := fmt.Sprintf("msg_mock_%08d", sequence)
	created := responsesPayload(responseID, model, configured, sequence, "in_progress")
	created["output"] = []any{}
	delete(created, "usage")
	item := map[string]any{
		"id": messageID, "type": "message", "status": "in_progress", "role": "assistant", "content": []any{},
	}
	doneItem := map[string]any{
		"id": messageID, "type": "message", "status": "completed", "role": "assistant",
		"content": []map[string]any{{"type": "output_text", "text": configured.Output, "annotations": []any{}}},
	}
	part := map[string]any{"type": "output_text", "text": configured.Output, "annotations": []any{}}
	return []map[string]any{
		{"type": "response.created", "sequence_number": 0, "request_id": requestID, "response": created},
		{"type": "response.output_item.added", "sequence_number": 1, "request_id": requestID, "response_id": responseID, "output_index": 0, "item": item},
		{"type": "response.content_part.added", "sequence_number": 2, "request_id": requestID, "response_id": responseID, "item_id": messageID, "output_index": 0, "content_index": 0, "part": map[string]any{"type": "output_text", "text": "", "annotations": []any{}}},
		{"type": "response.output_text.delta", "sequence_number": 3, "request_id": requestID, "response_id": responseID, "item_id": messageID, "output_index": 0, "content_index": 0, "delta": configured.Output},
		{"type": "response.output_text.done", "sequence_number": 4, "request_id": requestID, "response_id": responseID, "item_id": messageID, "output_index": 0, "content_index": 0, "text": configured.Output},
		{"type": "response.content_part.done", "sequence_number": 5, "request_id": requestID, "response_id": responseID, "item_id": messageID, "output_index": 0, "content_index": 0, "part": part},
		{"type": "response.output_item.done", "sequence_number": 6, "request_id": requestID, "response_id": responseID, "output_index": 0, "item": doneItem},
		{"type": "response.completed", "sequence_number": 7, "request_id": requestID, "response": responsesPayload(responseID, model, configured, sequence, "completed")},
	}
}

func responseID(kind string, sequence uint64) string {
	if kind == "chat" {
		return fmt.Sprintf("chatcmpl_mock_%08d", sequence)
	}
	return fmt.Sprintf("resp_mock_%08d", sequence)
}

func prepareSSE(w http.ResponseWriter, requestID string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Request-ID", requestID)
	w.Header().Set("Cache-Control", "no-cache, no-store")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
}

func writeSSEData(w http.ResponseWriter, value any) {
	payload, err := json.Marshal(value)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
	flush(w)
}

func writeSSEEvent(w http.ResponseWriter, eventType string, value any) {
	payload, err := json.Marshal(value)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, payload)
	flush(w)
}

func flush(w http.ResponseWriter) {
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeAPIError(w http.ResponseWriter, status int, requestID, errorType, message string) {
	writeJSON(w, status, map[string]any{
		"error":      map[string]any{"type": errorType, "code": errorType, "message": message},
		"request_id": requestID,
	})
}

var errBodyTooLarge = errors.New("request body is too large")

func decodeBoundedJSON(body io.ReadCloser, limit int64, target any) error {
	defer body.Close()
	payload, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return errors.New("could not read request body")
	}
	if int64(len(payload)) > limit {
		return errBodyTooLarge
	}
	if len(strings.TrimSpace(string(payload))) == 0 {
		return errors.New("request body is required")
	}
	if err := json.Unmarshal(payload, target); err != nil {
		return errors.New("request body must be valid JSON")
	}
	return nil
}

func decodeStrictBoundedJSON(body io.ReadCloser, limit int64, target any) error {
	defer body.Close()
	reader := io.LimitReader(body, limit+1)
	payload, err := io.ReadAll(reader)
	if err != nil {
		return errors.New("could not read request body")
	}
	if int64(len(payload)) > limit {
		return errBodyTooLarge
	}
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errors.New("control body must be valid JSON with known fields")
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("control body must contain exactly one JSON object")
	}
	return nil
}

func waitForDelay(ctx context.Context, delayMS int) bool {
	if delayMS <= 0 {
		return true
	}
	timer := time.NewTimer(time.Duration(delayMS) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func isWebSocketUpgrade(r *http.Request) bool {
	return strings.EqualFold(strings.TrimSpace(r.Header.Get("Upgrade")), "websocket") &&
		strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade")
}

func isPrivatePeer(remoteAddress string) bool {
	host, _, err := net.SplitHostPort(remoteAddress)
	if err != nil {
		host = remoteAddress
	}
	host = strings.Trim(host, "[]")
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate())
}

func (s *requestStats) record(endpoint, transport, caseName string, status int, rejected bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests++
	s.byEndpoint[endpoint]++
	s.byTransport[transport]++
	s.byCase[caseName]++
	s.byStatus[strconv.Itoa(status)]++
	if rejected {
		s.rejected++
	}
}

func (s *requestStats) addWSConnection() {
	s.mu.Lock()
	s.wsConnections++
	s.mu.Unlock()
}

func (s *requestStats) recordSyntheticAccountOutcome(caseName, authorization string, status int) {
	matches := syntheticAuthorizationPattern.FindStringSubmatch(strings.TrimSpace(authorization))
	if len(matches) != 2 {
		return
	}
	outcome := "account_" + matches[1] + ":" + strconv.Itoa(status)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.byCaseSyntheticAccountOutcome[caseName] == nil {
		s.byCaseSyntheticAccountOutcome[caseName] = make(map[string]uint64)
	}
	s.byCaseSyntheticAccountOutcome[caseName][outcome]++
}

func (s *requestStats) snapshot(nextSequence uint64) statsSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return statsSnapshot{
		Requests: s.requests, WSConnections: s.wsConnections,
		ByEndpoint: cloneCounts(s.byEndpoint), ByTransport: cloneCounts(s.byTransport),
		ByCase: cloneCounts(s.byCase), ByStatus: cloneCounts(s.byStatus),
		ByCaseSyntheticAccountOutcome: cloneNestedCounts(s.byCaseSyntheticAccountOutcome),
		Rejected:                      s.rejected, NextSequence: nextSequence,
	}
}

func (s *requestStats) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = 0
	s.wsConnections = 0
	s.rejected = 0
	s.byEndpoint = make(map[string]uint64)
	s.byTransport = make(map[string]uint64)
	s.byCase = make(map[string]uint64)
	s.byStatus = make(map[string]uint64)
	s.byCaseSyntheticAccountOutcome = make(map[string]map[string]uint64)
}

func cloneCounts(source map[string]uint64) map[string]uint64 {
	result := make(map[string]uint64, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func cloneNestedCounts(source map[string]map[string]uint64) map[string]map[string]uint64 {
	result := make(map[string]map[string]uint64, len(source))
	for key, value := range source {
		result[key] = cloneCounts(value)
	}
	return result
}
