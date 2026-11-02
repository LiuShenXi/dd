package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func excelImageFixture(t *testing.T) ([]byte, string) {
	t.Helper()
	var imageBytes bytes.Buffer
	require.NoError(t, png.Encode(&imageBytes, image.NewRGBA(image.Rect(0, 0, 3, 2))))
	return imageBytes.Bytes(), "data:image/png;base64," + base64.StdEncoding.EncodeToString(imageBytes.Bytes())
}

func TestExcelBPSInlineImageForwarding(t *testing.T) {
	imageBytes, dataURL := excelImageFixture(t)
	for _, stream := range []bool{false, true} {
		for _, path := range []string{"/v1/responses", "/v1/responses/compact"} {
			t.Run(fmt.Sprintf("%v/%s", stream, path), func(t *testing.T) {
				upstream := &httpUpstreamRecorder{responses: []*http.Response{
					{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"openai_file_id":"file-uploaded"}`))},
					{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_image\",\"status\":\"completed\",\"model\":\"gpt-6-astra\",\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"image read\"}]}],\"usage\":{\"input_tokens\":10,\"output_tokens\":2}}}\n\n"))},
				}}
				svc := openAIClientToolsTestService(upstream)
				account := excelAccount()
				account.Credentials["access_token"] = "token-" + t.Name()
				account.Proxy = &Proxy{Protocol: "socks5", Host: "127.0.0.1", Port: 1080, Username: "proxy-user", Password: "proxy-secret"}
				body, err := json.Marshal(map[string]any{"model": "gpt-6-astra", "stream": stream, "input": []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_text", "text": "describe"}, map[string]any{"type": "input_image", "image_url": dataURL, "detail": "high"}}}}})
				require.NoError(t, err)
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
				result, err := svc.Forward(context.Background(), c, account, body)
				require.NoError(t, err)
				require.Equal(t, "response.completed", result.UpstreamTerminalEvent)
				require.Len(t, upstream.requests, 2)
				require.Equal(t, basispoints.AttachmentsURL, upstream.requests[0].URL.String())
				require.Equal(t, basispoints.ResponsesURL, upstream.requests[1].URL.String())
				require.Equal(t, account.Proxy.URL(), upstream.lastProxyURL)
				upload := upstream.requests[0]
				require.Equal(t, "Bearer token-"+t.Name(), upload.Header.Get("Authorization"))
				require.Equal(t, "test-account", upload.Header.Get("Chatgpt-Account-Id"))
				require.True(t, HTTPUpstreamRedirectsDisabled(upload.Context()))
				upload.Body = io.NopCloser(bytes.NewReader(upstream.bodies[0]))
				reader, err := upload.MultipartReader()
				require.NoError(t, err)
				part, err := reader.NextPart()
				require.NoError(t, err)
				got, err := io.ReadAll(part)
				require.NoError(t, err)
				require.Equal(t, "file", part.FormName())
				require.Equal(t, "image/png", part.Header.Get("Content-Type"))
				require.Equal(t, imageBytes, got)
				require.NotContains(t, string(upstream.lastBody), "data:image")
				items := gjson.GetBytes(upstream.lastBody, "input").Array()
				found := false
				for _, item := range items {
					for _, part := range item.Get("content").Array() {
						if part.Get("type").String() == "input_image" {
							found = true
							require.Equal(t, "file-uploaded", part.Get("file_id").String())
							require.Equal(t, "high", part.Get("detail").String())
						}
					}
				}
				require.True(t, found)
				require.Contains(t, rec.Body.String(), "image read")
			})
		}
	}
}

func TestExcelBPSAttachmentFailureDoesNotSubmitInference(t *testing.T) {
	_, dataURL := excelImageFixture(t)
	for _, tc := range []struct {
		status int
		body   string
		want   int
	}{
		{401, `{"error":"private-image-data test-token"}`, 401},
		{413, `{"error":"private-image-data"}`, 413},
		{429, `{"error":"rate limit"}`, 429},
		{503, `{"error":"unavailable"}`, 503},
		{302, "redirect", 502},
		{200, `{"id":"wrong-field"}`, 502},
		{200, `{"openai_file_id":" "}`, 502},
	} {
		t.Run(fmt.Sprint(tc.status, tc.want, tc.body), func(t *testing.T) {
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: tc.status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(tc.body))}}
			svc := openAIClientToolsTestService(upstream)
			account := excelAccount()
			account.Credentials["access_token"] = "failure-token-" + t.Name()
			body, _ := json.Marshal(map[string]any{"model": "gpt-6-astra", "input": []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_image", "image_url": dataURL}}}}})
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
			_, err := svc.Forward(context.Background(), c, account, body)
			require.Error(t, err)
			require.Len(t, upstream.requests, 1)
			require.Equal(t, tc.want, rec.Code)
			require.True(t, IsResponseCommitted(c))
			require.True(t, json.Valid(rec.Body.Bytes()))
			require.NotContains(t, rec.Body.String(), "private-image-data")
			require.NotContains(t, rec.Body.String(), "test-token")
			require.True(t, account.Schedulable)
			var failover *UpstreamFailoverError
			require.NotErrorAs(t, err, &failover)
		})
	}
}
