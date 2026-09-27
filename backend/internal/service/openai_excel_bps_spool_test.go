package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type bpsRepeatedReader struct{}

func (bpsRepeatedReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'r'
	}
	return len(p), nil
}

func TestExcelBPSPreservesProgressiveUsageBeforeUnexpectedEOF(t *testing.T) {
	gin.SetMode(gin.TestMode)
	wire := "data: {\"type\":\"response.in_progress\",\"usage\":{\"input_tokens\":17,\"output_tokens\":3}}\n\n"
	upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(wire))}}
	svc := openAIClientToolsTestService(upstream)
	body := []byte("{\"model\":\"gpt-6-astra\",\"stream\":true,\"input\":\"test\"}")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(body)))
	result, err := svc.Forward(context.Background(), c, excelAccount(), body)
	require.Error(t, err)
	require.Equal(t, 17, result.Usage.InputTokens)
	require.Equal(t, 3, result.Usage.OutputTokens)
}

func TestExcelBPSCancelClosesBlockedUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	reader, writer := io.Pipe()
	defer writer.Close()
	upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: reader}}
	svc := openAIClientToolsTestService(upstream)
	body := []byte("{\"model\":\"gpt-6-astra\",\"stream\":false,\"input\":\"test\"}")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", strings.NewReader(string(body)))
	requestCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := svc.Forward(requestCtx, c, excelAccount(), body); done <- err }()
	// Successful pipe write proves the transform goroutine has entered its read.
	_, err := io.WriteString(writer, ": upstream waiting\n")
	require.NoError(t, err)
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(3 * time.Second):
		t.Fatal("cancelled gateway did not release upstream reader")
	}
	_, err = writer.Write([]byte("next"))
	require.ErrorIs(t, err, io.ErrClosedPipe)
}

type bpsDiscardHTTPWriter struct {
	header http.Header
	bytes  int64
	status int
	fail   bool
}

func (w *bpsDiscardHTTPWriter) Header() http.Header { return w.header }
func (w *bpsDiscardHTTPWriter) WriteHeader(n int)   { w.status = n }
func (w *bpsDiscardHTTPWriter) Write(p []byte) (int, error) {
	if w.fail {
		return 0, io.ErrClosedPipe
	}
	w.bytes += int64(len(p))
	return len(p), nil
}
func (w *bpsDiscardHTTPWriter) Flush() {}

func TestExcelBPSDiskBackedCompaction(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct{ stream, fail bool }{{true, false}, {false, false}, {true, true}} {
		t.Run(fmt.Sprintf("stream_%t_disconnect_%t", tc.stream, tc.fail), func(t *testing.T) {
			const size = 128 << 20
			prefix := "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_spooled\",\"status\":\"completed\",\"model\":\"gpt-6-astra\",\"output\":[{\"type\":\"compaction\",\"encrypted_content\":\""
			suffix := "\"}],\"usage\":{\"input_tokens\":17,\"output_tokens\":3}}}\n\n"
			wire := io.MultiReader(strings.NewReader(prefix), io.LimitReader(bpsRepeatedReader{}, size), strings.NewReader(suffix))
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(wire)}}
			svc := openAIClientToolsTestService(upstream)
			body := []byte(fmt.Sprintf("{\"model\":\"gpt-6-astra\",\"stream\":%v,\"input\":\"test\"}", tc.stream))
			rec := &bpsDiscardHTTPWriter{header: make(http.Header), fail: tc.fail}
			c, _ := gin.CreateTestContext(rec)
			path := "/v1/responses"
			if !tc.stream {
				path += "/compact"
			}
			c.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(body)))
			runtime.GC()
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			result, err := svc.Forward(context.Background(), c, excelAccount(), body)
			runtime.ReadMemStats(&after)
			if tc.fail {
				require.Error(t, err)
				require.True(t, result.ClientDisconnect)
			} else {
				require.NoError(t, err)
				require.Greater(t, rec.bytes, int64(size))
				require.Equal(t, 200, rec.status)
			}
			require.Equal(t, "response.completed", result.UpstreamTerminalEvent)
			require.Equal(t, "resp_spooled", result.ResponseID)
			require.Equal(t, "gpt-6-astra", result.UpstreamResponseModel)
			require.Equal(t, 17, result.Usage.InputTokens)
			require.Equal(t, 3, result.Usage.OutputTokens)
			allocated := after.TotalAlloc - before.TotalAlloc
			t.Logf("gateway 128 MiB compaction; total allocated %.2f MiB", float64(allocated)/(1<<20))
			require.Less(t, allocated, uint64(32<<20))
		})
	}
}
