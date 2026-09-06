package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

func TestDefaultHTTPJSONAndSSEUsage(t *testing.T) {
	server := httptest.NewServer(newMockServer())
	t.Cleanup(server.Close)

	chatResponse := postJSON(t, server.URL+"/v1/chat/completions", `{"model":"gpt-4o-mini"}`)
	require.Equal(t, http.StatusOK, chatResponse.StatusCode)
	chatRequestID := chatResponse.Header.Get("X-Request-ID")
	require.NotEmpty(t, chatRequestID)
	var chat map[string]any
	decodeResponse(t, chatResponse, &chat)
	require.Equal(t, float64(1000), chat["usage"].(map[string]any)["prompt_tokens"])
	require.Equal(t, float64(100), chat["usage"].(map[string]any)["completion_tokens"])

	responsesJSON := postJSON(t, server.URL+"/v1/responses", `{"model":"gpt-4o-mini"}`)
	require.Equal(t, http.StatusOK, responsesJSON.StatusCode)
	var responsePayload map[string]any
	decodeResponse(t, responsesJSON, &responsePayload)
	require.Equal(t, float64(1000), responsePayload["usage"].(map[string]any)["input_tokens"])
	require.Equal(t, float64(100), responsePayload["usage"].(map[string]any)["output_tokens"])
	require.True(t, strings.HasPrefix(responsePayload["id"].(string), "resp_mock_"))

	chatSSE := postJSON(t, server.URL+"/chat/completions", `{"model":"gpt-4o-mini","stream":true}`)
	require.Equal(t, http.StatusOK, chatSSE.StatusCode)
	chatSSEBody, err := io.ReadAll(chatSSE.Body)
	require.NoError(t, err)
	require.NoError(t, chatSSE.Body.Close())
	require.Contains(t, string(chatSSEBody), `"prompt_tokens":1000`)
	require.Contains(t, string(chatSSEBody), `"completion_tokens":100`)
	require.Contains(t, string(chatSSEBody), "data: [DONE]")

	responsesResponse := postJSON(t, server.URL+"/responses", `{"model":"gpt-4o-mini","stream":true}`)
	require.Equal(t, http.StatusOK, responsesResponse.StatusCode)
	require.Equal(t, "text/event-stream", responsesResponse.Header.Get("Content-Type"))
	responseRequestID := responsesResponse.Header.Get("X-Request-ID")
	require.NotEqual(t, chatRequestID, responseRequestID)
	body, err := io.ReadAll(responsesResponse.Body)
	require.NoError(t, err)
	require.NoError(t, responsesResponse.Body.Close())
	require.Contains(t, string(body), "event: response.created")
	require.Contains(t, string(body), "event: response.output_text.done")
	require.Contains(t, string(body), "event: response.completed")
	require.Contains(t, string(body), `"input_tokens":1000`)
	require.Contains(t, string(body), `"output_tokens":100`)

	chatID, ok := chat["id"].(string)
	require.True(t, ok)
	require.True(t, strings.HasPrefix(chatID, "chatcmpl_mock_"))
}

func TestNamedAndNextControlFailures(t *testing.T) {
	handler := newMockServer()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	configure := func(body string) {
		req, err := http.NewRequest(http.MethodPost, server.URL+"/__control", strings.NewReader(body))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "127.0.0.1:1234"
		response := serveDirect(handler, req)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	}

	configure(`{"mode":"named","name":"fail-twice","input_tokens":7,"output_tokens":3,"output":"named output","status":429,"failures":2}`)
	for _, wantStatus := range []int{429, 429, 200} {
		response := postJSON(t, server.URL+"/v1/responses", `{"model":"fail-twice"}`)
		require.Equal(t, wantStatus, response.StatusCode)
		_ = response.Body.Close()
	}
	headerRequest, err := http.NewRequest(http.MethodPost, server.URL+"/v1/responses", strings.NewReader(`{"model":"gpt-4o-mini"}`))
	require.NoError(t, err)
	headerRequest.Header.Set("X-Mock-Case", "fail-twice")
	headerResponse, err := http.DefaultClient.Do(headerRequest)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, headerResponse.StatusCode)
	var headerPayload map[string]any
	decodeResponse(t, headerResponse, &headerPayload)
	require.Equal(t, float64(7), headerPayload["usage"].(map[string]any)["input_tokens"])

	configure(`{"mode":"next","name":"next-retry","input_tokens":9,"output_tokens":4,"status":503,"failures":1}`)
	first := postJSON(t, server.URL+"/v1/responses", `{"model":"gpt-4o-mini"}`)
	require.Equal(t, http.StatusServiceUnavailable, first.StatusCode)
	_ = first.Body.Close()
	second := postJSON(t, server.URL+"/v1/responses", `{"model":"gpt-4o-mini"}`)
	require.Equal(t, http.StatusOK, second.StatusCode)
	var payload map[string]any
	decodeResponse(t, second, &payload)
	usage := payload["usage"].(map[string]any)
	require.Equal(t, float64(9), usage["input_tokens"])
	require.Equal(t, float64(4), usage["output_tokens"])
	third := postJSON(t, server.URL+"/v1/responses", `{"model":"gpt-4o-mini"}`)
	require.Equal(t, http.StatusOK, third.StatusCode)
	decodeResponse(t, third, &payload)
	require.Equal(t, float64(1000), payload["usage"].(map[string]any)["input_tokens"])
}

func TestControlLimitsAndPrivatePeer(t *testing.T) {
	handler := newMockServer()

	publicRequest := httptest.NewRequest(http.MethodPost, "/__control", strings.NewReader(`{"mode":"next"}`))
	publicRequest.RemoteAddr = "203.0.113.9:4444"
	publicResponse := serveDirect(handler, publicRequest)
	require.Equal(t, http.StatusForbidden, publicResponse.Code)

	invalidDelay := httptest.NewRequest(http.MethodPost, "/__control", strings.NewReader(`{"mode":"next","delay_ms":10001}`))
	invalidDelay.RemoteAddr = "127.0.0.1:4444"
	invalidResponse := serveDirect(handler, invalidDelay)
	require.Equal(t, http.StatusBadRequest, invalidResponse.Code)

	invalidFailureMode := httptest.NewRequest(http.MethodPost, "/__control", strings.NewReader(`{"mode":"next","ws_failure_mode":"close"}`))
	invalidFailureMode.RemoteAddr = "127.0.0.1:4444"
	require.Equal(t, http.StatusBadRequest, serveDirect(handler, invalidFailureMode).Code)

	invalidFailureModeType := httptest.NewRequest(http.MethodPost, "/__control", strings.NewReader(`{"mode":"next","ws_failure_mode":1}`))
	invalidFailureModeType.RemoteAddr = "127.0.0.1:4444"
	require.Equal(t, http.StatusBadRequest, serveDirect(handler, invalidFailureModeType).Code)

	oversizedControl := httptest.NewRequest(http.MethodPost, "/__control", strings.NewReader(`{"mode":"next","output":"`+strings.Repeat("x", maxControlBodyBytes)+`"}`))
	oversizedControl.RemoteAddr = "127.0.0.1:4444"
	oversizedControlResponse := serveDirect(handler, oversizedControl)
	require.Equal(t, http.StatusRequestEntityTooLarge, oversizedControlResponse.Code)

	oversized := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-4o-mini","input":"`+strings.Repeat("x", maxRequestBodyBytes)+`"}`))
	oversizedResponse := serveDirect(handler, oversized)
	require.Equal(t, http.StatusRequestEntityTooLarge, oversizedResponse.Code)
}

func TestStatsSnapshotAndResetDoNotExposeRequestContent(t *testing.T) {
	handler := newMockServer()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	secretMarker := "must-not-appear-in-stats"
	req, err := http.NewRequest(http.MethodPost, server.URL+"/v1/chat/completions", strings.NewReader(fmt.Sprintf(`{"model":"gpt-4o-mini","messages":[{"content":%q}]}`, secretMarker)))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer another-must-not-appear")
	response, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	_ = response.Body.Close()
	unknownResponse, err := http.Get(server.URL + "/" + secretMarker)
	require.NoError(t, err)
	require.Equal(t, http.StatusNotFound, unknownResponse.StatusCode)
	_ = unknownResponse.Body.Close()

	statsRequest := httptest.NewRequest(http.MethodGet, "/__stats?reset=1", nil)
	statsRequest.RemoteAddr = "127.0.0.1:5555"
	statsResponse := serveDirect(handler, statsRequest)
	require.Equal(t, http.StatusOK, statsResponse.Code)
	require.NotContains(t, statsResponse.Body.String(), secretMarker)
	require.NotContains(t, statsResponse.Body.String(), "another-must-not-appear")
	require.Contains(t, statsResponse.Body.String(), `"/v1/chat/completions":1`)
	require.Contains(t, statsResponse.Body.String(), `"unknown":1`)

	afterReset := httptest.NewRequest(http.MethodGet, "/__stats", nil)
	afterReset.RemoteAddr = "127.0.0.1:5555"
	afterResetResponse := serveDirect(handler, afterReset)
	require.Contains(t, afterResetResponse.Body.String(), `"requests":0`)
}

func TestResponsesWebSocketSupportsMultipleTurns(t *testing.T) {
	server := httptest.NewServer(newMockServer())
	t.Cleanup(server.Close)

	dialCtx, cancelDial := context.WithTimeout(context.Background(), 3*time.Second)
	conn, _, err := coderws.Dial(dialCtx, "ws"+strings.TrimPrefix(server.URL, "http")+"/v1/responses", nil)
	cancelDial()
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.CloseNow() })

	responseIDs := make([]string, 0, 2)
	for _, input := range []string{"first synthetic turn", "second synthetic turn"} {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		err = conn.Write(ctx, coderws.MessageText, []byte(fmt.Sprintf(`{"type":"response.create","model":"gpt-4o-mini","input":%q}`, input)))
		cancel()
		require.NoError(t, err)

		var createdID string
		for {
			ctx, cancel = context.WithTimeout(context.Background(), 3*time.Second)
			_, payload, readErr := conn.Read(ctx)
			cancel()
			require.NoError(t, readErr)
			var event map[string]any
			require.NoError(t, json.Unmarshal(payload, &event))
			if event["type"] == "response.created" {
				createdID = event["response"].(map[string]any)["id"].(string)
			}
			if event["type"] != "response.completed" {
				continue
			}
			completed := event["response"].(map[string]any)
			require.Equal(t, createdID, completed["id"])
			usage := completed["usage"].(map[string]any)
			require.Equal(t, float64(1000), usage["input_tokens"])
			require.Equal(t, float64(100), usage["output_tokens"])
			responseIDs = append(responseIDs, createdID)
			break
		}
	}
	require.Len(t, responseIDs, 2)
	require.NotEqual(t, responseIDs[0], responseIDs[1])
}

func TestResponsesWebSocketCanOmitUsageWithoutChangingDefault(t *testing.T) {
	handler := newMockServer()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	control := httptest.NewRequest(http.MethodPost, "/__control", strings.NewReader(`{"mode":"named","name":"missing-usage","omit_usage":true}`))
	control.RemoteAddr = "127.0.0.1:4444"
	require.Equal(t, http.StatusOK, serveDirect(handler, control).Code)

	dialCtx, cancelDial := context.WithTimeout(context.Background(), 3*time.Second)
	conn, _, err := coderws.Dial(dialCtx, "ws"+strings.TrimPrefix(server.URL, "http")+"/v1/responses", nil)
	cancelDial()
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.CloseNow() })

	for _, testCase := range []struct {
		model     string
		wantUsage bool
	}{{model: "missing-usage", wantUsage: false}, {model: defaultModel, wantUsage: true}} {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		require.NoError(t, conn.Write(ctx, coderws.MessageText, []byte(fmt.Sprintf(`{"type":"response.create","model":%q}`, testCase.model))))
		cancel()
		for {
			ctx, cancel = context.WithTimeout(context.Background(), 3*time.Second)
			_, payload, readErr := conn.Read(ctx)
			cancel()
			require.NoError(t, readErr)
			var event map[string]any
			require.NoError(t, json.Unmarshal(payload, &event))
			if event["type"] != "response.completed" {
				continue
			}
			response := event["response"].(map[string]any)
			_, hasUsage := response["usage"]
			require.Equal(t, testCase.wantUsage, hasUsage)
			break
		}
	}
}

func TestResponsesWebSocketFailureModeIsOptIn(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		controlJSON string
		wantEvent   string
	}{
		{name: "default", controlJSON: `{"mode":"named","name":"ws-failure","status":429,"failures":1}`, wantEvent: "response.failed"},
		{name: "rate limit failover", controlJSON: `{"mode":"named","name":"ws-failure","status":429,"failures":1,"ws_failure_mode":"rate_limit_error"}`, wantEvent: "error"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			handler := newMockServer()
			server := httptest.NewServer(handler)
			t.Cleanup(server.Close)
			control := httptest.NewRequest(http.MethodPost, "/__control", strings.NewReader(testCase.controlJSON))
			control.RemoteAddr = "127.0.0.1:4444"
			require.Equal(t, http.StatusOK, serveDirect(handler, control).Code)

			dialCtx, cancelDial := context.WithTimeout(context.Background(), 3*time.Second)
			conn, _, err := coderws.Dial(dialCtx, "ws"+strings.TrimPrefix(server.URL, "http")+"/v1/responses", nil)
			cancelDial()
			require.NoError(t, err)
			t.Cleanup(func() { _ = conn.CloseNow() })

			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			require.NoError(t, conn.Write(ctx, coderws.MessageText, []byte(`{"type":"response.create","model":"ws-failure"}`)))
			cancel()
			ctx, cancel = context.WithTimeout(context.Background(), 3*time.Second)
			_, payload, err := conn.Read(ctx)
			cancel()
			require.NoError(t, err)
			var event map[string]any
			require.NoError(t, json.Unmarshal(payload, &event))
			require.Equal(t, testCase.wantEvent, event["type"])
		})
	}
}

func TestStatsExposeOnlySanitizedSyntheticAccountOutcomes(t *testing.T) {
	handler := newMockServer()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	control := httptest.NewRequest(http.MethodPost, "/__control", strings.NewReader(`{"mode":"named","name":"account-outcome","status":429,"failures":1,"ws_failure_mode":"rate_limit_error"}`))
	control.RemoteAddr = "127.0.0.1:4444"
	require.Equal(t, http.StatusOK, serveDirect(handler, control).Code)

	firstAuthorization := "Bearer synthetic-0123456789abcdef-1"
	secondAuthorization := "Bearer synthetic-0123456789abcdef-2"
	require.Equal(t, "error", runMockWSTurn(t, server.URL, firstAuthorization, "account-outcome"))
	require.Equal(t, "response.completed", runMockWSTurn(t, server.URL, secondAuthorization, "account-outcome"))

	statsRequest := httptest.NewRequest(http.MethodGet, "/__stats", nil)
	statsRequest.RemoteAddr = "127.0.0.1:5555"
	statsResponse := serveDirect(handler, statsRequest)
	require.Equal(t, http.StatusOK, statsResponse.Code)
	require.NotContains(t, statsResponse.Body.String(), firstAuthorization)
	require.NotContains(t, statsResponse.Body.String(), secondAuthorization)
	var payload struct {
		Stats statsSnapshot `json:"stats"`
	}
	require.NoError(t, json.Unmarshal(statsResponse.Body.Bytes(), &payload))
	require.Equal(t, uint64(1), payload.Stats.ByCaseSyntheticAccountOutcome["account-outcome"]["account_1:429"])
	require.Equal(t, uint64(1), payload.Stats.ByCaseSyntheticAccountOutcome["account-outcome"]["account_2:200"])
}

func runMockWSTurn(t *testing.T, serverURL, authorization, model string) string {
	t.Helper()
	header := make(http.Header)
	header.Set("Authorization", authorization)
	dialCtx, cancelDial := context.WithTimeout(context.Background(), 3*time.Second)
	conn, _, err := coderws.Dial(dialCtx, "ws"+strings.TrimPrefix(serverURL, "http")+"/v1/responses", &coderws.DialOptions{HTTPHeader: header})
	cancelDial()
	require.NoError(t, err)
	defer conn.CloseNow()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	require.NoError(t, conn.Write(ctx, coderws.MessageText, []byte(fmt.Sprintf(`{"type":"response.create","model":%q}`, model))))
	cancel()
	for seen := 0; seen < 16; seen++ {
		ctx, cancel = context.WithTimeout(context.Background(), 3*time.Second)
		_, raw, readErr := conn.Read(ctx)
		cancel()
		require.NoError(t, readErr)
		var event map[string]any
		require.NoError(t, json.Unmarshal(raw, &event))
		eventType, _ := event["type"].(string)
		if eventType == "error" || eventType == "response.failed" || eventType == "response.completed" {
			return eventType
		}
	}
	t.Fatal("terminal WebSocket event not received")
	return ""
}

func postJSON(t *testing.T, url, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return response
}

func decodeResponse(t *testing.T, response *http.Response, target any) {
	t.Helper()
	defer response.Body.Close()
	require.NoError(t, json.NewDecoder(response.Body).Decode(target))
}

func serveDirect(handler http.Handler, request *http.Request) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}
