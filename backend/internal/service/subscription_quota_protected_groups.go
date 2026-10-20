package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var ErrSubscriptionQuotaProtectedUnavailable = infraerrors.ServiceUnavailable("SUBSCRIPTION_QUOTA_PROTECTED_UNAVAILABLE", "This group is temporarily unavailable while subscription billing is being prepared")

// These records protect the old billing source during a controlled conversion.
// They are not term admissions, term receipts, or a replacement billing ledger.
type SubscriptionQuotaProtectedAdmission struct {
	RequestID        string
	UserID           int64
	APIKeyID         int64
	GroupID          int64
	SubscriptionType string
}

type SubscriptionQuotaProtectedGroupRepository interface {
	AdmitProtectedGroup(context.Context, SubscriptionQuotaProtectedAdmission) error
	RecordProtectedGroupUsage(context.Context, string, json.RawMessage) error
	FinishProtectedGroup(context.Context, string, string, string) error
	CountProtectedGroupUnfinished(context.Context, int64) (int64, error)
}

type SubscriptionQuotaProtectedGroupService struct {
	repo   SubscriptionQuotaProtectedGroupRepository
	groups map[int64]struct{}
}

func NewSubscriptionQuotaProtectedGroupService(repo SubscriptionQuotaProtectedGroupRepository, groupIDs []int64) *SubscriptionQuotaProtectedGroupService {
	s := &SubscriptionQuotaProtectedGroupService{repo: repo, groups: make(map[int64]struct{}, len(groupIDs))}
	for _, id := range groupIDs {
		if id > 0 {
			s.groups[id] = struct{}{}
		}
	}
	return s
}

func (s *SubscriptionQuotaProtectedGroupService) Protects(groupID int64) bool {
	if s == nil {
		return false
	}
	_, ok := s.groups[groupID]
	return ok
}

// Called only after account waits, immediately before a real upstream attempt.
// Both the original authenticated group and the final billing group are checked:
// a scheduler/fallback must not escape protection by replacing the Key's Group.
func (s *SubscriptionQuotaProtectedGroupService) Admit(ctx context.Context, original, current *APIKey, platform string) (*SubscriptionQuotaProtectedRequest, error) {
	if s == nil || (!s.Protects(protectedGroupID(original)) && !s.Protects(protectedGroupID(current))) {
		return nil, nil
	}
	if s.repo == nil || original == nil || current == nil || current.Group == nil ||
		original.ID != current.ID || original.UserID != current.UserID ||
		protectedGroupID(original) != protectedGroupID(current) || current.Group.ID != protectedGroupID(current) ||
		original.Group == nil || original.Group.ID != protectedGroupID(original) ||
		original.Group.Platform != PlatformAnthropic || current.Group.Platform != PlatformAnthropic || platform != PlatformAnthropic {
		return nil, ErrSubscriptionQuotaProtectedUnavailable
	}
	if forced, _ := ctx.Value(ctxkey.ForcePlatform).(string); strings.TrimSpace(forced) != "" {
		return nil, ErrSubscriptionQuotaProtectedUnavailable
	}
	if target, ok := ResolvedTargetPlatformFromContext(ctx); ok && target != PlatformAnthropic {
		return nil, ErrSubscriptionQuotaProtectedUnavailable
	}
	input := SubscriptionQuotaProtectedAdmission{RequestID: "protected:" + generateRequestID(), UserID: current.UserID, APIKeyID: current.ID, GroupID: protectedGroupID(current), SubscriptionType: current.Group.SubscriptionType}
	prior, _ := ctx.Value(protectedConcurrencyContextKey{}).(*protectedConcurrencyState)
	if prior != nil {
		if prior.userID != input.UserID || prior.keyID != input.APIKeyID || prior.groupID != input.GroupID {
			return nil, ErrSubscriptionQuotaProtectedUnavailable
		}
		input.RequestID = prior.request.id
	}
	admitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := s.repo.AdmitProtectedGroup(admitCtx, input); err != nil {
		return nil, ErrSubscriptionQuotaProtectedUnavailable.WithCause(err)
	}
	if prior != nil {
		prior.mu.Lock()
		prior.attempted = true
		prior.mu.Unlock()
		return prior.request, nil
	}
	return &SubscriptionQuotaProtectedRequest{repo: s.repo, id: input.RequestID}, nil
}

func (s *SubscriptionQuotaProtectedGroupService) CountUnfinished(ctx context.Context, groupID int64) (int64, error) {
	if s == nil || s.repo == nil || !s.Protects(groupID) {
		return 0, ErrSubscriptionQuotaProtectedUnavailable
	}
	return s.repo.CountProtectedGroupUnfinished(ctx, groupID)
}

func protectedGroupID(key *APIKey) int64 {
	if key == nil || key.GroupID == nil {
		return 0
	}
	return *key.GroupID
}

// Keep the preflight allowlist shared with API-key middleware. Wildcard response
// subpaths, WS, forced platforms, Google and media have no R1 final gate.
func SubscriptionQuotaProtectedRouteAllowed(method, path string) bool {
	if method == http.MethodPost {
		switch path {
		case "/v1/messages", "/v1/chat/completions", "/chat/completions", "/v1/responses", "/responses", "/backend-api/codex/responses", "/v1/messages/count_tokens", "/messages/count_tokens":
			return true
		}
	}
	if method == http.MethodGet {
		switch path {
		case "/models", "/v1/models", "/backend-api/codex/models", "/v1/usage", "/v1/sub2api/billing":
			return true
		}
		for _, prefix := range []string{"/models/", "/v1/models/"} {
			if tail, ok := strings.CutPrefix(path, prefix); ok && tail != "" && !strings.Contains(tail, "/") {
				return true
			}
		}
	}
	return false
}

// State survives handler return, cancelled request contexts and process restart.
// No deferred release or age-based cleanup is permitted to declare it drained.
type SubscriptionQuotaProtectedRequest struct {
	repo     SubscriptionQuotaProtectedGroupRepository
	id       string
	mu       sync.Mutex
	queued   bool
	prepared bool
	known    bool
}

func (r *SubscriptionQuotaProtectedRequest) persist(fn func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return fn(ctx)
}

// PrepareUsage persists only non-secret metering facts before submitting work.
// A zero struct cannot prove known zero usage in the legacy ForwardResult type;
// it stays held for reconciliation instead of silently permitting a conversion.
func (r *SubscriptionQuotaProtectedRequest) PrepareUsage(result *ForwardResult, accountID int64, pricingAt time.Time) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.queued = true
	if result == nil {
		return r.persist(func(ctx context.Context) error {
			return r.repo.FinishProtectedGroup(ctx, r.id, "reconcile_required", "usage_missing")
		})
	}
	u := result.Usage
	r.known = u.InputTokens >= 0 && u.OutputTokens >= 0 && u.CacheCreationInputTokens >= 0 && u.CacheReadInputTokens >= 0 &&
		u.CacheCreation5mTokens >= 0 && u.CacheCreation1hTokens >= 0 &&
		(u.InputTokens > 0 || u.OutputTokens > 0 || u.CacheCreationInputTokens > 0 || u.CacheReadInputTokens > 0)
	payload, err := json.Marshal(struct {
		Version           int         `json:"version"`
		AccountID         int64       `json:"account_id"`
		UpstreamRequestID string      `json:"upstream_request_id"`
		Model             string      `json:"model"`
		UpstreamModel     string      `json:"upstream_model"`
		Usage             ClaudeUsage `json:"usage"`
		PricingAt         time.Time   `json:"pricing_at"`
		ClientDisconnect  bool        `json:"client_disconnect"`
	}{1, accountID, result.RequestID, result.Model, result.UpstreamModel, u, pricingAt, result.ClientDisconnect})
	if err != nil {
		return err
	}
	if err := r.persist(func(ctx context.Context) error { return r.repo.RecordProtectedGroupUsage(ctx, r.id, payload) }); err != nil {
		return err
	}
	r.prepared = true
	return nil
}

func (r *SubscriptionQuotaProtectedRequest) FinishUsage(billingErr error) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	state, reason := "reconcile_required", "usage_settlement_failed"
	if billingErr == nil && r.prepared && r.known {
		state, reason = "completed", "legacy_billing_completed"
	}
	if !r.known {
		reason = "usage_missing_or_ambiguous"
	}
	return r.persist(func(ctx context.Context) error { return r.repo.FinishProtectedGroup(ctx, r.id, state, reason) })
}

func (r *SubscriptionQuotaProtectedRequest) ForwardEnded() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.queued {
		return nil
	}
	return r.persist(func(ctx context.Context) error {
		return r.repo.FinishProtectedGroup(ctx, r.id, "reconcile_required", "forward_ended_without_usage")
	})
}

func (r *SubscriptionQuotaProtectedRequest) FinishCountTokens() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.queued = true
	return r.persist(func(ctx context.Context) error {
		return r.repo.FinishProtectedGroup(ctx, r.id, "no_charge", "count_tokens_does_not_bill_entitlement")
	})
}

// A protected old-source request must not report a successful settlement after
// the legacy multi-write fallback merely logged an error and returned nil.
func (s *GatewayService) RecordProtectedGroupUsage(ctx context.Context, input *RecordUsageInput, protection *SubscriptionQuotaProtectedRequest) error {
	if protection != nil && (s == nil || s.usageBillingRepo == nil) {
		return fmt.Errorf("protected group requires atomic usage billing repository")
	}
	return s.RecordUsage(ctx, input)
}
