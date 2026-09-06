package handler

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

const carpoolHTTPAdmissionStateKey = "carpool_http_admission_state"

var errCarpoolUsageRecordTaskPanicked = errors.New("carpool usage record task panicked")

func carpoolWebSocketUsageReconcileReason(err error) string {
	if errors.Is(err, service.ErrCarpoolUsageUnknown) {
		return "websocket turn ended without durable known usage"
	}
	return "websocket known usage persistence or settlement failed"
}

type carpoolHTTPAdmissionState struct {
	snapshot        *domain.CarpoolBillingSnapshot
	usageDurable    bool
	reconcileMarked bool
}

func carpoolHTTPState(c *gin.Context) *carpoolHTTPAdmissionState {
	if c == nil {
		return nil
	}
	if value, ok := c.Get(carpoolHTTPAdmissionStateKey); ok {
		if state, ok := value.(*carpoolHTTPAdmissionState); ok {
			return state
		}
	}
	state := &carpoolHTTPAdmissionState{}
	c.Set(carpoolHTTPAdmissionStateKey, state)
	return state
}

func (h *OpenAIGatewayHandler) admitCarpoolForForward(c *gin.Context, apiKey *service.APIKey) bool {
	if apiKey == nil || apiKey.Group == nil || !apiKey.Group.IsCarpoolType() {
		return true
	}
	if snapshot, ok := service.CarpoolBillingSnapshotFromContext(c.Request.Context()); ok {
		if state := carpoolHTTPState(c); state != nil && state.snapshot == nil {
			copySnapshot := *snapshot
			state.snapshot = &copySnapshot
		}
		return true
	}
	if h == nil || h.billingCacheService == nil || h.billingCacheService.CarpoolGatewayBilling() == nil {
		response.ErrorFrom(c, service.ErrCarpoolBillingUnavailable)
		return false
	}
	snapshot, err := h.billingCacheService.CarpoolGatewayBilling().Admit(
		c.Request.Context(), apiKey.UserID, apiKey.ID, apiKey.Group.ID,
		service.NewCarpoolAdmissionRequestID(), timezone.Now(),
	)
	if err != nil {
		response.ErrorFrom(c, err)
		return false
	}
	if snapshot == nil {
		response.ErrorFrom(c, service.ErrCarpoolBillingUnavailable)
		return false
	}
	c.Request = c.Request.WithContext(service.ContextWithCarpoolBillingSnapshot(c.Request.Context(), snapshot))
	if state := carpoolHTTPState(c); state != nil {
		copySnapshot := *snapshot
		state.snapshot = &copySnapshot
	}
	return true
}

func (h *OpenAIGatewayHandler) recordCarpoolHTTPUsageResult(c *gin.Context, ctx context.Context, err error, reason string) {
	if _, ok := service.CarpoolBillingSnapshotFromContext(ctx); !ok {
		return
	}
	state := carpoolHTTPState(c)
	if state == nil || state.snapshot == nil {
		return
	}
	if err == nil {
		state.usageDurable = true
		return
	}
	if state.reconcileMarked {
		return
	}
	state.reconcileMarked = true
	h.markCarpoolUsageUnknown(ctx, state.snapshot, reason)
}

func (h *OpenAIGatewayHandler) reconcileCarpoolHTTPUsage(c *gin.Context, reason string) {
	state := carpoolHTTPState(c)
	if state == nil || state.snapshot == nil || state.usageDurable || state.reconcileMarked {
		return
	}
	state.reconcileMarked = true
	ctx := context.Background()
	if c != nil && c.Request != nil {
		ctx = c.Request.Context()
	}
	h.markCarpoolUsageUnknown(ctx, state.snapshot, reason)
}

func executeCarpoolUsageTaskSynchronously(parent context.Context, task service.UsageRecordTask) (handled bool, taskErr error) {
	if task == nil {
		return false, nil
	}
	if _, ok := service.CarpoolBillingSnapshotFromContext(parent); !ok {
		return false, nil
	}
	task = wrapUsageRecordTaskContext(parent, task)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	handled = true
	defer func() {
		if recover() != nil {
			taskErr = errCarpoolUsageRecordTaskPanicked
			logger.L().Error("carpool.usage_record_task_panic_recovered")
		}
	}()
	task(ctx)
	return handled, nil
}

func (h *OpenAIGatewayHandler) handleCarpoolUsageRecordTask(parent context.Context, task service.UsageRecordTask) bool {
	handled, err := executeCarpoolUsageTaskSynchronously(parent, task)
	if !handled || err == nil {
		return handled
	}
	if snapshot, ok := service.CarpoolBillingSnapshotFromContext(parent); ok {
		h.markCarpoolUsageUnknown(parent, snapshot, "usage record task panicked")
	}
	return true
}

func (h *OpenAIGatewayHandler) admitCarpoolTurn(ctx context.Context, apiKey *service.APIKey) (context.Context, *domain.CarpoolBillingSnapshot, error) {
	if apiKey == nil || apiKey.Group == nil || !apiKey.Group.IsCarpoolType() {
		return ctx, nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if snapshot, ok := service.CarpoolBillingSnapshotFromContext(ctx); ok {
		return ctx, snapshot, nil
	}
	if h == nil || h.billingCacheService == nil || h.billingCacheService.CarpoolGatewayBilling() == nil {
		return ctx, nil, service.ErrCarpoolBillingUnavailable
	}
	snapshot, err := h.billingCacheService.CarpoolGatewayBilling().Admit(
		ctx, apiKey.UserID, apiKey.ID, apiKey.Group.ID,
		service.NewCarpoolAdmissionRequestID(), timezone.Now(),
	)
	if err != nil {
		return ctx, nil, err
	}
	if snapshot == nil {
		return ctx, nil, service.ErrCarpoolBillingUnavailable
	}
	return service.ContextWithCarpoolBillingSnapshot(ctx, snapshot), snapshot, nil
}

type carpoolWSTurnAdmissionStore struct {
	mu        sync.Mutex
	snapshots map[int]*domain.CarpoolBillingSnapshot
}

func (s *carpoolWSTurnAdmissionStore) load(turn int) (*domain.CarpoolBillingSnapshot, bool) {
	if s == nil || turn <= 0 {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot, ok := s.snapshots[turn]
	if !ok || snapshot == nil {
		return nil, false
	}
	copySnapshot := *snapshot
	return &copySnapshot, true
}

func (s *carpoolWSTurnAdmissionStore) store(turn int, snapshot *domain.CarpoolBillingSnapshot) {
	if s == nil || turn <= 0 || snapshot == nil {
		return
	}
	s.mu.Lock()
	if s.snapshots == nil {
		s.snapshots = make(map[int]*domain.CarpoolBillingSnapshot, 4)
	}
	copySnapshot := *snapshot
	s.snapshots[turn] = &copySnapshot
	s.mu.Unlock()
}

func (s *carpoolWSTurnAdmissionStore) delete(turn int) {
	if s == nil || turn <= 0 {
		return
	}
	s.mu.Lock()
	delete(s.snapshots, turn)
	s.mu.Unlock()
}

// remapLatestTo preserves one logical turn's admission when WS failover
// restarts the forwarding adapter and its local turn counter at one.
func (s *carpoolWSTurnAdmissionStore) remapLatestTo(turn int) {
	if s == nil || turn <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	latestTurn := 0
	for candidate := range s.snapshots {
		if candidate > latestTurn {
			latestTurn = candidate
		}
	}
	if latestTurn == 0 || latestTurn == turn {
		return
	}
	s.snapshots[turn] = s.snapshots[latestTurn]
	delete(s.snapshots, latestTurn)
}

func (s *carpoolWSTurnAdmissionStore) drain() []*domain.CarpoolBillingSnapshot {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*domain.CarpoolBillingSnapshot, 0, len(s.snapshots))
	for turn, snapshot := range s.snapshots {
		if snapshot != nil {
			copySnapshot := *snapshot
			out = append(out, &copySnapshot)
		}
		delete(s.snapshots, turn)
	}
	return out
}

func (h *OpenAIGatewayHandler) markCarpoolUsageUnknown(ctx context.Context, snapshot *domain.CarpoolBillingSnapshot, reason string) {
	if snapshot == nil || h == nil || h.billingCacheService == nil {
		return
	}
	billing := h.billingCacheService.CarpoolGatewayBilling()
	if billing == nil {
		return
	}
	detached := context.Background()
	if ctx != nil {
		detached = context.WithoutCancel(ctx)
	}
	reconcileCtx, cancel := context.WithTimeout(detached, 10*time.Second)
	defer cancel()
	if err := billing.MarkReconcileRequired(reconcileCtx, snapshot, reason); err != nil {
		logger.L().Error("carpool.mark_reconcile_required_failed",
			zap.Int64("billing_request_id", snapshot.BillingRequestID),
			zap.String("request_id", snapshot.RequestID),
			zap.Error(err),
		)
	}
}
