package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type carpoolWSBillingStub struct {
	mu                  sync.Mutex
	canonicalStart      time.Time
	admitCalls          int
	failAdmissionAt     int
	admissionErr        error
	admissionRequestIDs []string
	snapshots           []domain.CarpoolBillingSnapshot
	persisted           []domain.CarpoolBillingSnapshot
	marked              []domain.CarpoolBillingSnapshot
	markedReasons       []string
}

func (s *carpoolWSBillingStub) Admit(_ context.Context, userID, apiKeyID, groupID int64, requestID string, _ time.Time) (*domain.CarpoolBillingSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.admitCalls++
	s.admissionRequestIDs = append(s.admissionRequestIDs, requestID)
	if s.failAdmissionAt == s.admitCalls {
		if s.admissionErr != nil {
			return nil, s.admissionErr
		}
		return nil, service.ErrCarpoolUnavailable
	}
	canonical := s.canonicalStart.Add(time.Duration(s.admitCalls) * time.Second)
	snapshot := domain.CarpoolBillingSnapshot{
		BillingRequestID: int64(9000 + s.admitCalls),
		RequestID:        requestID,
		UserID:           userID,
		APIKeyID:         apiKeyID,
		GroupID:          groupID,
		TermID:           9100,
		CycleID:          int64(9200 + s.admitCalls),
		AdmittedAt:       canonical,
	}
	s.snapshots = append(s.snapshots, snapshot)
	copySnapshot := snapshot
	return &copySnapshot, nil
}

func (s *carpoolWSBillingStub) PersistKnownUsage(_ context.Context, snapshot *domain.CarpoolBillingSnapshot, _ decimal.Decimal, _ json.RawMessage) error {
	if snapshot == nil {
		return service.ErrCarpoolAdmissionRequired
	}
	s.mu.Lock()
	s.persisted = append(s.persisted, *snapshot)
	s.mu.Unlock()
	return nil
}

func (*carpoolWSBillingStub) RecoverPendingReceipts(context.Context, int) ([]domain.CarpoolKnownUsage, error) {
	return nil, nil
}

func (s *carpoolWSBillingStub) MarkReconcileRequired(_ context.Context, snapshot *domain.CarpoolBillingSnapshot, reason string) error {
	if snapshot == nil {
		return service.ErrCarpoolAdmissionRequired
	}
	s.mu.Lock()
	s.marked = append(s.marked, *snapshot)
	s.markedReasons = append(s.markedReasons, reason)
	s.mu.Unlock()
	return nil
}

func (s *carpoolWSBillingStub) state() (int, []string, []domain.CarpoolBillingSnapshot, []domain.CarpoolBillingSnapshot, []domain.CarpoolBillingSnapshot, []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.admitCalls,
		append([]string(nil), s.admissionRequestIDs...),
		append([]domain.CarpoolBillingSnapshot(nil), s.snapshots...),
		append([]domain.CarpoolBillingSnapshot(nil), s.persisted...),
		append([]domain.CarpoolBillingSnapshot(nil), s.marked...),
		append([]string(nil), s.markedReasons...)
}

type carpoolWSUsageBillingRepoStub struct {
	mu       sync.Mutex
	commands []*service.UsageBillingCommand
}

func (s *carpoolWSUsageBillingRepoStub) Apply(_ context.Context, cmd *service.UsageBillingCommand) (*service.UsageBillingApplyResult, error) {
	if cmd == nil {
		return nil, errors.New("nil usage billing command")
	}
	copyCmd := *cmd
	if cmd.CarpoolSnapshot != nil {
		copySnapshot := *cmd.CarpoolSnapshot
		copyCmd.CarpoolSnapshot = &copySnapshot
	}
	s.mu.Lock()
	s.commands = append(s.commands, &copyCmd)
	s.mu.Unlock()
	return &service.UsageBillingApplyResult{Applied: true}, nil
}

func (*carpoolWSUsageBillingRepoStub) ReserveBatchImageBalance(context.Context, *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
	return nil, nil
}

func (*carpoolWSUsageBillingRepoStub) CaptureBatchImageBalance(context.Context, *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
	return nil, nil
}

func (*carpoolWSUsageBillingRepoStub) ReleaseBatchImageBalance(context.Context, *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
	return nil, nil
}

func (s *carpoolWSUsageBillingRepoStub) appliedCommands() []*service.UsageBillingCommand {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*service.UsageBillingCommand(nil), s.commands...)
}

func TestCarpoolWebSocketUsageReconcileReason(t *testing.T) {
	require.Equal(t, "websocket turn ended without durable known usage", carpoolWebSocketUsageReconcileReason(service.ErrCarpoolUsageUnknown))
	require.Equal(t, "websocket known usage persistence or settlement failed", carpoolWebSocketUsageReconcileReason(errors.New("usage write failed")))
}

func TestOpenAIResponsesWebSocket_CarpoolTwoTurnsUseFreshDurableSnapshots(t *testing.T) {
	for _, mode := range []string{service.OpenAIWSIngressModeCtxPool, service.OpenAIWSIngressModePassthrough} {
		t.Run(mode, func(t *testing.T) {
			billing := &carpoolWSBillingStub{
				canonicalStart: time.Date(2026, 9, 6, 10, 0, 0, 123456000, time.UTC),
			}
			billingRepo := &carpoolWSUsageBillingRepoStub{}
			result := runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
				firstPayload:     `{"type":"response.create","model":"gpt-5.1","input":"turn one"}`,
				secondPayload:    `{"type":"response.create","model":"gpt-5.1","input":"turn two"}`,
				ingressMode:      mode,
				carpoolBilling:   billing,
				usageBillingRepo: billingRepo,
			})

			admitCalls, requestIDs, snapshots, persisted, marked, _ := billing.state()
			require.Equal(t, 2, admitCalls)
			require.Len(t, requestIDs, 2)
			require.NotEqual(t, requestIDs[0], requestIDs[1])
			require.Len(t, snapshots, 2)
			require.NotEqual(t, snapshots[0].BillingRequestID, snapshots[1].BillingRequestID)
			require.NotEqual(t, snapshots[0].CycleID, snapshots[1].CycleID)
			require.Equal(t, snapshots, persisted)
			require.Empty(t, marked)

			commands := billingRepo.appliedCommands()
			require.Len(t, commands, 2)
			require.Len(t, result.logs, 2)
			for i := range snapshots {
				require.NotNil(t, commands[i].CarpoolSnapshot)
				require.Equal(t, snapshots[i], *commands[i].CarpoolSnapshot)
				require.NotNil(t, result.logs[i].CarpoolTermID)
				require.Equal(t, snapshots[i].TermID, *result.logs[i].CarpoolTermID)
				require.NotNil(t, result.logs[i].CarpoolCycleID)
				require.Equal(t, snapshots[i].CycleID, *result.logs[i].CarpoolCycleID)
				require.NotNil(t, result.logs[i].CarpoolAdmittedAt)
				require.Equal(t, snapshots[i].AdmittedAt, *result.logs[i].CarpoolAdmittedAt)
			}
		})
	}
}

func TestOpenAIResponsesWebSocketV2Passthrough_CarpoolSecondTurnAdmissionFailureDoesNotReachUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstreamDone := make(chan struct{})
	secondUpstreamFrame := make(chan []byte, 1)
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(upstreamDone)
		conn, err := coderws.Accept(w, r, &coderws.AcceptOptions{CompressionMode: coderws.CompressionContextTakeover})
		require.NoError(t, err)
		defer func() { _ = conn.CloseNow() }()

		readCtx, cancelRead := context.WithTimeout(r.Context(), 3*time.Second)
		_, _, err = conn.Read(readCtx)
		cancelRead()
		require.NoError(t, err)

		completed := []byte(`{"type":"response.completed","response":{"id":"resp_carpool_admitted","model":"gpt-5.1","usage":{"input_tokens":2,"output_tokens":1}}}`)
		writeCtx, cancelWrite := context.WithTimeout(r.Context(), 3*time.Second)
		err = conn.Write(writeCtx, coderws.MessageText, completed)
		cancelWrite()
		require.NoError(t, err)

		readCtx, cancelRead = context.WithTimeout(r.Context(), 3*time.Second)
		_, second, readErr := conn.Read(readCtx)
		cancelRead()
		if readErr == nil {
			secondUpstreamFrame <- append([]byte(nil), second...)
		}
	}))
	defer upstreamServer.Close()

	billing := &carpoolWSBillingStub{
		canonicalStart:  time.Date(2026, 9, 6, 11, 0, 0, 123456000, time.UTC),
		failAdmissionAt: 2,
	}
	billingRepo := &carpoolWSUsageBillingRepoStub{}
	harness := newOpenAIWSPassthroughHandlerHarnessWithBilling(t, upstreamServer.URL, billing, billingRepo)

	firstPayload := `{"type":"response.create","model":"gpt-5.1","input":"turn one"}`
	writeCtx, cancelWrite := context.WithTimeout(context.Background(), 3*time.Second)
	err := harness.clientConn.Write(writeCtx, coderws.MessageText, []byte(firstPayload))
	cancelWrite()
	require.NoError(t, err)

	readCtx, cancelRead := context.WithTimeout(context.Background(), 3*time.Second)
	_, event, err := harness.clientConn.Read(readCtx)
	cancelRead()
	require.NoError(t, err)
	require.Equal(t, "resp_carpool_admitted", gjson.GetBytes(event, "response.id").String())

	secondPayload := `{"type":"response.create","model":"gpt-5.1","input":"turn two"}`
	writeCtx, cancelWrite = context.WithTimeout(context.Background(), 3*time.Second)
	err = harness.clientConn.Write(writeCtx, coderws.MessageText, []byte(secondPayload))
	cancelWrite()
	require.NoError(t, err)

	readCtx, cancelRead = context.WithTimeout(context.Background(), 3*time.Second)
	_, _, err = harness.clientConn.Read(readCtx)
	cancelRead()
	var closeErr coderws.CloseError
	require.ErrorAs(t, err, &closeErr)
	require.Equal(t, coderws.StatusTryAgainLater, closeErr.Code)
	require.Equal(t, "carpool admission failed", closeErr.Reason)

	select {
	case <-harness.handlerDone:
	case <-time.After(3 * time.Second):
		t.Fatal("websocket handler did not exit after carpool admission failure")
	}
	select {
	case <-upstreamDone:
	case <-time.After(3 * time.Second):
		t.Fatal("upstream websocket did not exit after carpool admission failure")
	}
	select {
	case second := <-secondUpstreamFrame:
		t.Fatalf("unadmitted second turn reached upstream: %s", second)
	default:
	}

	admitCalls, requestIDs, snapshots, persisted, marked, _ := billing.state()
	require.Equal(t, 2, admitCalls)
	require.Len(t, requestIDs, 2)
	require.Len(t, snapshots, 1)
	require.Equal(t, snapshots, persisted)
	require.Empty(t, marked)
	require.Len(t, billingRepo.appliedCommands(), 1)
}

func TestOpenAIResponsesWebSocketV2Passthrough_CarpoolCompletedWithoutUsageMarksReceiptMissing(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstreamDone := make(chan struct{})
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(upstreamDone)
		conn, err := coderws.Accept(w, r, &coderws.AcceptOptions{CompressionMode: coderws.CompressionContextTakeover})
		require.NoError(t, err)
		defer func() { _ = conn.CloseNow() }()

		readCtx, cancelRead := context.WithTimeout(r.Context(), 3*time.Second)
		_, _, err = conn.Read(readCtx)
		cancelRead()
		require.NoError(t, err)

		completed := []byte(`{"type":"response.completed","response":{"id":"resp_carpool_missing_usage","model":"gpt-5.1","status":"completed"}}`)
		writeCtx, cancelWrite := context.WithTimeout(r.Context(), 3*time.Second)
		err = conn.Write(writeCtx, coderws.MessageText, completed)
		cancelWrite()
		require.NoError(t, err)
	}))
	defer upstreamServer.Close()

	billing := &carpoolWSBillingStub{
		canonicalStart: time.Date(2026, 9, 6, 12, 0, 0, 123456000, time.UTC),
	}
	billingRepo := &carpoolWSUsageBillingRepoStub{}
	harness := newOpenAIWSPassthroughHandlerHarnessWithBilling(t, upstreamServer.URL, billing, billingRepo)

	writeCtx, cancelWrite := context.WithTimeout(context.Background(), 3*time.Second)
	err := harness.clientConn.Write(writeCtx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.1","input":"unknown usage"}`))
	cancelWrite()
	require.NoError(t, err)

	readCtx, cancelRead := context.WithTimeout(context.Background(), 3*time.Second)
	_, event, err := harness.clientConn.Read(readCtx)
	cancelRead()
	require.NoError(t, err)
	require.Equal(t, "resp_carpool_missing_usage", gjson.GetBytes(event, "response.id").String())

	readCtx, cancelRead = context.WithTimeout(context.Background(), 3*time.Second)
	_, _, err = harness.clientConn.Read(readCtx)
	cancelRead()
	require.Error(t, err)

	select {
	case <-harness.handlerDone:
	case <-time.After(3 * time.Second):
		t.Fatal("websocket handler did not exit after completed response without usage")
	}
	select {
	case <-upstreamDone:
	case <-time.After(3 * time.Second):
		t.Fatal("upstream websocket did not exit")
	}

	admitCalls, _, snapshots, persisted, marked, reasons := billing.state()
	require.Equal(t, 1, admitCalls)
	require.Len(t, snapshots, 1)
	require.Empty(t, persisted)
	require.Equal(t, snapshots, marked)
	require.Equal(t, []string{"websocket turn ended without durable known usage"}, reasons)
	require.Empty(t, billingRepo.appliedCommands())
}
