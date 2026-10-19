package service

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

var ErrSubscriptionQuotaEndpointUnsupported = infraerrors.Forbidden("TERM_ENDPOINT_UNSUPPORTED", "This endpoint or upstream adapter is not supported for term quota")

type subscriptionQuotaForwardContextKey struct{}

// SubscriptionQuotaForwardState belongs to one inbound HTTP call. Its immutable
// snapshot is copied on access, and it never grants a second real upstream send.
type SubscriptionQuotaForwardState struct {
	mu              sync.Mutex
	billing         SubscriptionQuotaGatewayBilling
	key             APIKey
	channel         ChannelUsageFields
	requestID       string
	snapshot        *domain.SubscriptionQuotaBillingSnapshot
	attempted       bool
	queued          bool
	finalized       bool
	rejectedStatus  int
	heartbeatCancel context.CancelFunc
	heartbeatDone   <-chan struct{}
}

func ContextWithSubscriptionQuotaForward(ctx context.Context, key *APIKey, billing SubscriptionQuotaGatewayBilling, channel ChannelUsageFields) (context.Context, *SubscriptionQuotaForwardState, error) {
	if state, ok := SubscriptionQuotaForwardStateFromContext(ctx); ok {
		if key == nil || key.GroupID == nil || *key.GroupID != state.key.Group.ID {
			return ctx, state, ErrSubscriptionQuotaEndpointUnsupported
		}
		return ctx, state, nil
	}
	if key == nil || key.Group == nil || !key.Group.HasTermQuotaMode() {
		return ctx, nil, nil
	}
	if billing == nil || !key.Group.IsTermQuota() {
		return ctx, nil, ErrSubscriptionQuotaBillingUnavailable
	}
	copyKey := *key
	copyGroup := *key.Group
	copyKey.Group = &copyGroup
	state := &SubscriptionQuotaForwardState{billing: billing, key: copyKey, channel: channel, requestID: NewSubscriptionQuotaAdmissionRequestID()}
	return context.WithValue(ctx, subscriptionQuotaForwardContextKey{}, state), state, nil
}

func SubscriptionQuotaForwardStateFromContext(ctx context.Context) (*SubscriptionQuotaForwardState, bool) {
	if ctx == nil {
		return nil, false
	}
	state, ok := ctx.Value(subscriptionQuotaForwardContextKey{}).(*SubscriptionQuotaForwardState)
	return state, ok && state != nil
}

func PreserveSubscriptionQuotaForwardState(ctx, source context.Context) context.Context {
	if state, ok := SubscriptionQuotaForwardStateFromContext(source); ok {
		return context.WithValue(ctx, subscriptionQuotaForwardContextKey{}, state)
	}
	return ctx
}

func SubscriptionQuotaForwardBillingSnapshot(ctx context.Context) *domain.SubscriptionQuotaBillingSnapshot {
	if snapshot, ok := SubscriptionQuotaBillingSnapshotFromContext(ctx); ok {
		return snapshot
	}
	if state, ok := SubscriptionQuotaForwardStateFromContext(ctx); ok {
		return state.Snapshot()
	}
	return nil
}

func (s *SubscriptionQuotaForwardState) Snapshot() *domain.SubscriptionQuotaBillingSnapshot {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.snapshot == nil {
		return nil
	}
	copy := s.snapshot.Clone()
	return &copy
}

func (s *SubscriptionQuotaForwardState) MarkUsageQueued() {
	if s != nil {
		s.mu.Lock()
		s.queued = true
		s.mu.Unlock()
	}
}

// FinishForward runs after the handler's forward/error path. Queued usage owns
// its own durable transition; unknown or rejected attempts are persisted here.
func (s *SubscriptionQuotaForwardState) FinishForward() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	if s.finalized || s.queued || s.snapshot == nil {
		s.mu.Unlock()
		return nil
	}
	s.finalized = true
	copy := s.snapshot.Clone()
	status := s.rejectedStatus
	s.mu.Unlock()
	defer s.StopHeartbeat()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if status != 0 {
		return s.billing.MarkNoCharge(ctx, &copy, domain.SubscriptionQuotaNoChargeEvidence{Kind: "upstream_rejected", Reference: fmt.Sprintf("http:%d", status)})
	}
	return s.billing.MarkReconcileRequired(ctx, &copy, "upstream_usage_unknown")
}

func (s *GatewayService) beforeSubscriptionQuotaSend(ctx context.Context, c *gin.Context, account *Account, model string, body []byte) error {
	state, ok := SubscriptionQuotaForwardStateFromContext(ctx)
	if !ok {
		return nil
	}
	if err := rejectUnsupportedSubscriptionQuotaTools(ctx, body); err != nil {
		return err
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.attempted || state.snapshot != nil {
		return fmt.Errorf("term requests permit one upstream attempt: %w", ErrSubscriptionQuotaBillingUnavailable)
	}
	if account == nil || account.Platform != PlatformAnthropic {
		return ErrSubscriptionQuotaEndpointUnsupported
	}
	tier := ""
	if speed := anthropicSpeedServiceTier(account, gjson.GetBytes(body, "speed").String(), model); speed != nil {
		tier = *speed
	}
	effort := gjson.GetBytes(body, "output_config.effort").String()
	if effort == "" && gjson.GetBytes(body, "thinking.type").String() == "enabled" {
		if value := DefaultEffortForThinkingEnabled(model); value != nil {
			effort = *value
		}
	}
	factory, err := s.PrepareSubscriptionQuotaPricing(ctx, SubscriptionQuotaPricingInput{APIKey: &state.key, Account: account, RequestedModel: subscriptionQuotaRequestModel(state, model), UpstreamModel: model, ChannelFields: state.channel, ServiceTier: tier, ReasoningEffort: effort, ForceCacheBilling: IsForceCacheBilling(ctx)})
	if err != nil {
		return err
	}
	snapshot, err := state.billing.Admit(ctx, domain.SubscriptionQuotaAdmissionParams{UserID: state.key.UserID, APIKeyID: state.key.ID, GroupID: state.key.Group.ID, AccountID: account.ID, ExpectedAccountType: account.Type, RequestID: state.requestID, FreezePricing: factory})
	if err != nil {
		return err
	}
	if snapshot == nil {
		return ErrSubscriptionQuotaBillingUnavailable
	}
	copy := snapshot.Clone()
	state.snapshot = &copy
	state.attempted = true
	state.startHeartbeatLocked(10 * time.Second)
	return nil
}

func (s *SubscriptionQuotaForwardState) startHeartbeatLocked(interval time.Duration) {
	if s.heartbeatCancel != nil || s.snapshot == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	s.heartbeatCancel = cancel
	s.heartbeatDone = done
	snapshot := s.snapshot.Clone()
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				attempt, cancel := context.WithTimeout(ctx, 5*time.Second)
				err := s.billing.Heartbeat(attempt, &snapshot)
				cancel()
				if err != nil && ctx.Err() == nil {
					slog.Error("term forward heartbeat failed; durable request remains held", "request_id", snapshot.RequestID, "error", err)
				}
			}
		}
	}()
}

// StopHeartbeat is called after the mandatory usage task has made its durable
// transition attempt, or after an unqueued forward is classified for recovery.
func (s *SubscriptionQuotaForwardState) StopHeartbeat() {
	if s == nil {
		return
	}
	s.mu.Lock()
	cancel, done := s.heartbeatCancel, s.heartbeatDone
	s.heartbeatCancel = nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
}

func observeSubscriptionQuotaHTTP(ctx context.Context, resp *http.Response, err error) {
	state, ok := SubscriptionQuotaForwardStateFromContext(ctx)
	if !ok {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if err != nil || resp == nil {
		return
	}
	// These statuses are protocol-level authentication/validation/throttling
	// rejections. A generic 5xx or transport failure is never evidence of zero.
	switch resp.StatusCode {
	case 400, 401, 403, 404, 409, 413, 415, 422, 429:
		state.rejectedStatus = resp.StatusCode
	}
}

// Only client-executed tools have token-only pricing in this first version.
// Reject server/unknown builtins before conversion can discard their type.
func rejectUnsupportedSubscriptionQuotaTools(ctx context.Context, body []byte) error {
	if _, term := SubscriptionQuotaForwardStateFromContext(ctx); !term {
		return nil
	}
	for _, tool := range gjson.GetBytes(body, "tools").Array() {
		kind := tool.Get("type").String()
		if kind == "" || kind == "function" || kind == "custom" || kind == "local_shell" || kind == "shell" || kind == "apply_patch" || strings.HasPrefix(kind, "bash_") || strings.HasPrefix(kind, "computer_") || strings.HasPrefix(kind, "text_editor_") || strings.HasPrefix(kind, "memory_") {
			continue
		}
		return ErrSubscriptionQuotaEndpointUnsupported
	}
	return nil
}

func rejectUnsupportedSubscriptionQuotaAdapter(ctx context.Context, unsupported bool) error {
	if _, term := SubscriptionQuotaForwardStateFromContext(ctx); term && unsupported {
		return ErrSubscriptionQuotaEndpointUnsupported
	}
	return nil
}

func subscriptionQuotaRequestModel(state *SubscriptionQuotaForwardState, fallback string) string {
	if value := strings.TrimSpace(state.channel.OriginalModel); value != "" {
		return value
	}
	return fallback
}
