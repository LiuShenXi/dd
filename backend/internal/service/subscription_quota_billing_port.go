package service

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

var (
	ErrSubscriptionQuotaNotActive       = infraerrors.Forbidden("TERM_NOT_ACTIVE", "there is no current Claude quota card")
	ErrSubscriptionQuotaExpired         = infraerrors.Forbidden("TERM_EXPIRED", "the Claude quota card has expired")
	ErrSubscriptionQuotaCancelled       = infraerrors.Forbidden("TERM_CANCELLED", "the Claude quota card was cancelled")
	ErrSubscriptionQuotaExhausted       = infraerrors.New(429, "TERM_QUOTA_EXHAUSTED", "the current Claude quota card is exhausted")
	ErrSubscriptionQuotaReconciliation  = infraerrors.ServiceUnavailable("TERM_RECONCILIATION_PENDING", "the current card has an unresolved consumption request")
	ErrSubscriptionQuotaAdmissionRetry  = infraerrors.ServiceUnavailable("TERM_ADMISSION_RETRY", "card boundary changed while waiting; retry the request")
	ErrSubscriptionQuotaReceiptConflict = infraerrors.Conflict("TERM_RECEIPT_CONFLICT", "subscription consumption facts conflict")
	ErrSubscriptionQuotaUsageUnknown    = infraerrors.Conflict("TERM_USAGE_UNKNOWN", "complete trusted usage is unavailable")
)

type SubscriptionQuotaGatewayBilling interface {
	Heartbeat(context.Context, *domain.SubscriptionQuotaBillingSnapshot) error
	Admit(context.Context, domain.SubscriptionQuotaAdmissionParams) (*domain.SubscriptionQuotaBillingSnapshot, error)
	PersistKnownUsage(context.Context, *domain.SubscriptionQuotaBillingSnapshot, decimal.Decimal, json.RawMessage, json.RawMessage) error
	MarkNoCharge(context.Context, *domain.SubscriptionQuotaBillingSnapshot, domain.SubscriptionQuotaNoChargeEvidence) error
	MarkReconcileRequired(context.Context, *domain.SubscriptionQuotaBillingSnapshot, string) error
}

type SubscriptionQuotaVerifiedUsage struct {
	RequestedModel        string `json:"requested_model,omitempty"`
	UpstreamModel         string `json:"upstream_model,omitempty"`
	ResponseModel         string `json:"response_model,omitempty"`
	InputTokens           int64  `json:"input_tokens"`
	OutputTokens          int64  `json:"output_tokens"`
	CacheCreation5mTokens int64  `json:"cache_creation_5m_tokens"`
	CacheCreation1hTokens int64  `json:"cache_creation_1h_tokens"`
	CacheReadTokens       int64  `json:"cache_read_tokens"`
	ServiceTier           string `json:"service_tier,omitempty"`
	SearchCallCount       int64  `json:"search_call_count,omitempty"`
}
type SubscriptionQuotaBillingRepository interface {
	Heartbeat(context.Context, *domain.SubscriptionQuotaBillingSnapshot) error
	AdmitRequest(context.Context, domain.SubscriptionQuotaAdmissionParams) (*domain.SubscriptionQuotaBillingSnapshot, error)
	PersistKnownUsage(context.Context, domain.SubscriptionQuotaKnownUsage) error
	MarkNoCharge(context.Context, *domain.SubscriptionQuotaBillingSnapshot, domain.SubscriptionQuotaNoChargeEvidence) error
	MarkReconcileRequired(context.Context, *domain.SubscriptionQuotaBillingSnapshot, string) error
	ClaimRecoverable(context.Context, domain.SubscriptionQuotaRecoveryClaim) ([]domain.SubscriptionQuotaKnownUsage, error)
	FinishRecoveryAttempt(context.Context, int64, string, string, time.Duration) error
	MarkStaleAdmissions(context.Context, time.Duration, int) error
	ListPendingUsageLogs(context.Context, int) ([]domain.SubscriptionQuotaKnownUsage, error)
	MarkUsageLogRecorded(context.Context, int64, string) error
}

type subscriptionQuotaBillingSnapshotContextKey struct{}

func ContextWithSubscriptionQuotaBillingSnapshot(ctx context.Context, snapshot *domain.SubscriptionQuotaBillingSnapshot) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if snapshot == nil {
		return ctx
	}
	copy := snapshot.Clone()
	return context.WithValue(ctx, subscriptionQuotaBillingSnapshotContextKey{}, &copy)
}
func SubscriptionQuotaBillingSnapshotFromContext(ctx context.Context) (*domain.SubscriptionQuotaBillingSnapshot, bool) {
	if ctx == nil {
		return nil, false
	}
	snapshot, ok := ctx.Value(subscriptionQuotaBillingSnapshotContextKey{}).(*domain.SubscriptionQuotaBillingSnapshot)
	if !ok || snapshot == nil {
		return nil, false
	}
	copy := snapshot.Clone()
	return &copy, true
}
func NewSubscriptionQuotaAdmissionRequestID() string { return "subquota:" + uuid.NewString() }

func SubscriptionQuotaAdmissionEnabled(cfg *config.Config, groupID int64) bool {
	if cfg == nil || cfg.RunMode == config.RunModeSimple || !cfg.SubscriptionQuota.AdmissionEnabled {
		return false
	}
	for _, id := range cfg.SubscriptionQuota.EnabledGroupIDs {
		if id == groupID {
			return true
		}
	}
	return false
}
func SubscriptionQuotaGenerationRouteAllowed(method, path string) bool {
	if method != http.MethodPost {
		return false
	}
	switch path {
	case "/v1/messages", "/v1/chat/completions", "/chat/completions", "/v1/responses", "/responses", "/backend-api/codex/responses":
		return true
	}
	return false
}
func (s *APIKeyService) SetSubscriptionQuotaGatewayBilling(billing SubscriptionQuotaGatewayBilling) {
	if s != nil {
		s.subscriptionQuotaGatewayBilling = billing
	}
}
func (s *APIKeyService) SubscriptionQuotaGatewayBilling() SubscriptionQuotaGatewayBilling {
	if s == nil {
		return nil
	}
	return s.subscriptionQuotaGatewayBilling
}

func (s *BillingCacheService) SetSubscriptionQuotaGatewayBilling(billing SubscriptionQuotaGatewayBilling) {
	if s != nil {
		s.subscriptionQuotaGatewayBilling = billing
	}
}
func (s *BillingCacheService) SubscriptionQuotaGatewayBilling() SubscriptionQuotaGatewayBilling {
	if s == nil {
		return nil
	}
	return s.subscriptionQuotaGatewayBilling
}
