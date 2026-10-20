package domain

import (
	"encoding/base64"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

const SubscriptionQuotaDuration = 168 * time.Hour

var subscriptionQuotaMoneyPattern = regexp.MustCompile(`^(0|[1-9][0-9]{0,11})(\.[0-9]{1,8})?$`)

// NormalizeSubscriptionQuotaMoney accepts only exact positive NUMERIC(20,8)
// text. It never rounds an administrator's requested allowance.
func NormalizeSubscriptionQuotaMoney(value string) (string, error) {
	if !subscriptionQuotaMoneyPattern.MatchString(value) {
		return "", fmt.Errorf("invalid quota")
	}
	integer, _, _ := strings.Cut(value, ".")
	if len(strings.TrimLeft(integer, "0")) > 12 {
		return "", fmt.Errorf("quota exceeds NUMERIC(20,8)")
	}
	amount, err := decimal.NewFromString(value)
	if err != nil || !amount.IsPositive() {
		return "", fmt.Errorf("quota must be positive")
	}
	return amount.StringFixed(8), nil
}

func EncodeSubscriptionQuotaCursor(id int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(id, 10)))
}
func DecodeSubscriptionQuotaCursor(cursor string) (int64, error) {
	if cursor == "" {
		return 0, nil
	}
	data, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, fmt.Errorf("invalid cursor")
	}
	id, err := strconv.ParseInt(string(data), 10, 64)
	if err != nil || id <= 0 || EncodeSubscriptionQuotaCursor(id) != cursor {
		return 0, fmt.Errorf("invalid cursor")
	}
	return id, nil
}

type SubscriptionQuotaTerm struct {
	ID                     int64      `json:"id"`
	GroupID                int64      `json:"group_id"`
	SubscriptionID         int64      `json:"subscription_id"`
	TermNo                 int64      `json:"term_no"`
	QuotaUSD               string     `json:"quota_usd"`
	UsedUSD                string     `json:"used_usd"`
	PendingUSD             string     `json:"pending_usd"`
	RemainingUSD           string     `json:"remaining_usd"`
	Status                 string     `json:"status"`
	LifecycleState         string     `json:"lifecycle_state"`
	StartsAt               time.Time  `json:"starts_at"`
	ExpiresAt              time.Time  `json:"expires_at"`
	CancelledAt            *time.Time `json:"cancelled_at"`
	Revision               int64      `json:"revision"`
	UnsettledRequestCount  int64      `json:"unsettled_request_count"`
	UserID                 int64      `json:"-"`
	ReconcileRequiredCount int64      `json:"-"`
	RawOverageUSD          string     `json:"-"`
}

// ProjectSubscriptionQuotaTerm is shared by all reads and exact-boundary tests.
// Stored lifecycle flags never grant rights outside the half-open interval.
func ProjectSubscriptionQuotaTerm(term SubscriptionQuotaTerm, now time.Time) SubscriptionQuotaTerm {
	quota, _ := decimal.NewFromString(term.QuotaUSD)
	used, _ := decimal.NewFromString(term.UsedUSD)
	pending, _ := decimal.NewFromString(term.PendingUSD)
	term.QuotaUSD, term.UsedUSD, term.PendingUSD = quota.StringFixed(8), used.StringFixed(8), pending.StringFixed(8)
	remaining := quota.Sub(used).Sub(pending)
	term.RemainingUSD = decimal.Max(decimal.Zero, remaining).StringFixed(8)
	term.RawOverageUSD = decimal.Max(decimal.Zero, used.Sub(quota)).StringFixed(8)
	switch {
	case term.CancelledAt != nil:
		term.Status = "cancelled"
	case now.Before(term.StartsAt):
		term.Status = "scheduled"
	case !now.Before(term.ExpiresAt):
		term.Status = "expired"
	case term.ReconcileRequiredCount > 0:
		term.Status = "reconciling"
	case !remaining.IsPositive():
		term.Status = "exhausted"
	default:
		term.Status = "active"
	}
	if term.Status == "cancelled" || term.Status == "scheduled" || term.Status == "expired" {
		term.RemainingUSD = decimal.Zero.StringFixed(8)
	}
	return term
}

type SubscriptionQuotaSummary struct {
	GroupID     int64                   `json:"group_id"`
	QuotaMode   string                  `json:"quota_mode"`
	Revision    int64                   `json:"revision"`
	ServerTime  time.Time               `json:"server_time"`
	CurrentTerm *SubscriptionQuotaTerm  `json:"current_term"`
	LastTerm    *SubscriptionQuotaTerm  `json:"last_term"`
	NextTerms   []SubscriptionQuotaTerm `json:"next_terms"`
}
type SubscriptionQuotaPreview struct {
	GroupID           int64                   `json:"group_id"`
	EffectiveQuotaUSD string                  `json:"effective_quota_usd"`
	ProposedStartsAt  time.Time               `json:"proposed_starts_at"`
	ProposedExpiresAt time.Time               `json:"proposed_expires_at"`
	ServerTime        time.Time               `json:"server_time"`
	CurrentRevision   int64                   `json:"current_revision"`
	Warnings          []string                `json:"warnings"`
	CurrentTerm       *SubscriptionQuotaTerm  `json:"current_term"`
	NextTerms         []SubscriptionQuotaTerm `json:"next_terms"`
}
type SubscriptionQuotaMutationResult struct {
	Term     SubscriptionQuotaTerm    `json:"term"`
	Summary  SubscriptionQuotaSummary `json:"summary"`
	Replayed bool                     `json:"replayed"`
}
type SubscriptionQuotaUsage struct {
	RequestID     string    `json:"request_id"`
	APIKeyID      int64     `json:"api_key_id"`
	TermID        int64     `json:"term_id"`
	Model         string    `json:"model"`
	ActualCostUSD string    `json:"actual_cost_usd"`
	SettledAt     time.Time `json:"settled_at"`
	InputTokens   int64     `json:"input_tokens"`
	OutputTokens  int64     `json:"output_tokens"`
}
type SubscriptionQuotaTermPage struct {
	Items      []SubscriptionQuotaTerm `json:"items"`
	NextCursor *string                 `json:"next_cursor"`
	HasMore    bool                    `json:"has_more"`
}
type SubscriptionQuotaUsagePage struct {
	Items      []SubscriptionQuotaUsage `json:"items"`
	NextCursor *string                  `json:"next_cursor"`
	HasMore    bool                     `json:"has_more"`
}

// Normalized intent is durable alongside the original resolution. Replay never
// consults today's plan/default or rewrites the originally returned timestamps.
type SubscriptionQuotaOpenParams struct {
	UserID                   int64   `json:"user_id"`
	ActorID                  int64   `json:"actor_id"`
	GroupID                  int64   `json:"group_id"`
	Operation                string  `json:"operation"`
	QuotaUSD                 *string `json:"quota_usd"`
	PlanID                   *int64  `json:"plan_id"`
	ExpectedRevision         int64   `json:"expected_revision"`
	Notes                    string  `json:"notes"`
	IdempotencyKey           string  `json:"-"`
	IssueAllowed             bool    `json:"-"`
	DefaultClaudeConcurrency int     `json:"-"`
}
type SubscriptionQuotaCancelParams struct {
	TermID           int64  `json:"term_id"`
	ActorID          int64  `json:"actor_id"`
	ExpectedRevision int64  `json:"expected_revision"`
	Reason           string `json:"reason"`
	IdempotencyKey   string `json:"-"`
}
type SubscriptionQuotaPageParams struct {
	BeforeID int64
	Limit    int
}
type SubscriptionQuotaSourceSnapshot struct {
	SchemaVersion       int        `json:"schema_version"`
	QuotaUSD            string     `json:"quota_usd"`
	DurationHours       int        `json:"duration_hours"`
	QuotaSource         string     `json:"quota_source"`
	PlanID              *int64     `json:"plan_id"`
	PlanName            string     `json:"plan_name,omitempty"`
	PlanUpdatedAt       *time.Time `json:"plan_updated_at,omitempty"`
	PlanDefaultQuotaUSD *string    `json:"plan_default_quota_usd,omitempty"`
}
