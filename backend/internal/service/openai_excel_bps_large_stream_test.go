package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestExcelBPSLargeTerminalEvent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			opaque := strings.Repeat("r", (20<<20)+17)
			raw, err := json.Marshal(map[string]any{"type": "response.completed", "response": map[string]any{"id": "resp_large", "status": "completed", "model": "gpt-6-astra", "output": []any{map[string]any{"type": "reasoning", "encrypted_content": opaque}}, "usage": map[string]any{"input_tokens": 17, "output_tokens": 3}}})
			require.NoError(t, err)
			wire := io.MultiReader(strings.NewReader("event: response.completed\ndata: "), strings.NewReader(string(raw)), strings.NewReader("\n\n"))
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(wire)}}
			svc := openAIClientToolsTestService(upstream)
			body := []byte(fmt.Sprintf(`{"model":"gpt-6-astra","stream":%v,"input":"test"}`, stream))
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(body)))
			result, err := svc.Forward(context.Background(), c, excelAccount(), body)
			require.NoError(t, err)
			require.Equal(t, 200, rec.Code)
			require.Equal(t, "response.completed", result.UpstreamTerminalEvent)
			require.Equal(t, "resp_large", result.ResponseID)
			require.Equal(t, 17, result.Usage.InputTokens)
			require.Equal(t, 3, result.Usage.OutputTokens)
			var terminal string
			if stream {
				for _, line := range strings.Split(rec.Body.String(), "\n") {
					if strings.HasPrefix(line, "data: ") {
						event := strings.TrimPrefix(line, "data: ")
						require.NotEqual(t, "response.failed", gjson.Get(event, "type").String())
						if gjson.Get(event, "type").String() == "response.completed" {
							terminal = gjson.Get(event, "response").Raw
						}
					}
				}
			} else {
				terminal = rec.Body.String()
			}
			require.Equal(t, len(opaque), len(gjson.Get(terminal, "output.0.encrypted_content").String()))
			require.True(t, gjson.Get(terminal, "output.0.encrypted_content").String() == opaque, "opaque reasoning was changed")
		})
	}
}
