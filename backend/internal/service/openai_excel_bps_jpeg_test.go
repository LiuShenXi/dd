package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestExcelBPSAttachmentCanonicalImageFilenames(t *testing.T) {
	for _, tc := range []struct{ mediaType, filename string }{
		{"image/jpeg", "image.jpg"},
		{"image/png", "image.png"},
		{"image/gif", "image.gif"},
		{"image/webp", "image.webp"},
	} {
		t.Run(tc.mediaType, func(t *testing.T) {
			data := []byte("unchanged-image-bytes")
			req, err := newExcelBPSAttachmentRequest(context.Background(), tc.mediaType, data, "token", "account")
			require.NoError(t, err)
			reader, err := req.MultipartReader()
			require.NoError(t, err)
			part, err := reader.NextPart()
			require.NoError(t, err)
			require.Equal(t, "file", part.FormName())
			require.Equal(t, tc.filename, part.FileName())
			require.Equal(t, tc.mediaType, part.Header.Get("Content-Type"))
			got, err := io.ReadAll(part)
			require.NoError(t, err)
			require.Equal(t, data, got)
		})
	}
}

func TestExcelBPSJPEGUserAndToolHistoryForwarding(t *testing.T) {
	var encoded bytes.Buffer
	require.NoError(t, jpeg.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 3, 2)), nil))
	dataURL := "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(encoded.Bytes())
	for _, tool := range []bool{false, true} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("tool=%v/stream=%v", tool, stream), func(t *testing.T) {
				screenshot := map[string]any{"type": "input_image", "image_url": dataURL, "detail": "high"}
				input := []any{map[string]any{"role": "user", "content": []any{screenshot}}}
				if tool {
					input = []any{
						map[string]any{"role": "user", "content": "Inspect the screenshot."},
						map[string]any{"type": "function_call", "name": "view_image", "call_id": "call_jpeg", "arguments": "{}"},
						map[string]any{"type": "function_call_output", "call_id": "call_jpeg", "output": []any{map[string]any{"type": "input_text", "text": "Window screenshot"}, screenshot}},
						map[string]any{"role": "user", "content": "Continue from the previous screenshot."},
					}
				}
				source := map[string]any{"model": "gpt-6-astra", "stream": stream, "input": input}
				if tool {
					source["tools"] = []any{map[string]any{"type": "function", "name": "view_image", "parameters": map[string]any{"type": "object", "properties": map[string]any{}}}}
				}
				body, err := json.Marshal(source)
				require.NoError(t, err)
				upstream := &httpUpstreamRecorder{responses: []*http.Response{
					{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"openai_file_id":"file-jpeg"}`))},
					{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_jpeg\",\"status\":\"completed\",\"model\":\"gpt-6-astra\",\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"JPEG read\"}]}],\"usage\":{\"input_tokens\":10,\"output_tokens\":2}}}\n\n"))},
				}}
				svc := openAIClientToolsTestService(upstream)
				account := excelAccount()
				account.Credentials["access_token"] = "jpeg-test-" + t.Name()
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
				result, err := svc.Forward(context.Background(), c, account, body)
				require.NoError(t, err)
				require.Equal(t, "response.completed", result.UpstreamTerminalEvent)
				require.Len(t, upstream.requests, 2)
				upload := upstream.requests[0]
				upload.Body = io.NopCloser(bytes.NewReader(upstream.bodies[0]))
				reader, err := upload.MultipartReader()
				require.NoError(t, err)
				part, err := reader.NextPart()
				require.NoError(t, err)
				require.Equal(t, "image.jpg", part.FileName())
				require.Equal(t, "image/jpeg", part.Header.Get("Content-Type"))
				got, err := io.ReadAll(part)
				require.NoError(t, err)
				require.Equal(t, encoded.Bytes(), got)
				require.NotContains(t, string(upstream.lastBody), "data:image")
				foundImage, foundTool := false, false
				for _, item := range gjson.GetBytes(upstream.lastBody, "input").Array() {
					if item.Get("type").String() == "function_call_output" && item.Get("call_id").String() == "call_jpeg" {
						foundTool = true
						require.Contains(t, item.Get("output").String(), "Window screenshot")
						require.Contains(t, item.Get("output").String(), "file-jpeg")
					}
					for _, content := range item.Get("content").Array() {
						if content.Get("type").String() == "input_image" {
							foundImage = true
							require.Equal(t, "file-jpeg", content.Get("file_id").String())
							require.Equal(t, "high", content.Get("detail").String())
						}
					}
				}
				require.True(t, foundImage)
				require.Equal(t, tool, foundTool)
				require.Contains(t, rec.Body.String(), "JPEG read")
			})
		}
	}
}
