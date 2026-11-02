package dto

import (
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
)

// SubscriptionQuotaTerm is the public card projection. Administrator notes,
// source snapshots and internal accounting buckets are intentionally absent.
type SubscriptionQuotaTerm struct {
	ID                    int64      `json:"id"`
	GroupID               int64      `json:"group_id"`
	SubscriptionID        int64      `json:"subscription_id"`
	TermNo                int64      `json:"term_no"`
	QuotaUSD              string     `json:"quota_usd"`
	UsedUSD               string     `json:"used_usd"`
	PendingUSD            string     `json:"pending_usd"`
	RemainingUSD          string     `json:"remaining_usd"`
	Status                string     `json:"status"`
	LifecycleState        string     `json:"lifecycle_state"`
	StartsAt              time.Time  `json:"starts_at"`
	ExpiresAt             time.Time  `json:"expires_at"`
	CancelledAt           *time.Time `json:"cancelled_at"`
	Revision              int64      `json:"revision"`
	UnsettledRequestCount int64      `json:"unsettled_request_count"`
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

type SubscriptionQuotaTermPage struct {
	Items      []SubscriptionQuotaTerm `json:"items"`
	NextCursor *string                 `json:"next_cursor"`
	HasMore    bool                    `json:"has_more"`
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

type SubscriptionQuotaUsagePage struct {
	Items      []SubscriptionQuotaUsage `json:"items"`
	NextCursor *string                  `json:"next_cursor"`
	HasMore    bool                     `json:"has_more"`
}

func SubscriptionQuotaTermFromDomain(t *domain.SubscriptionQuotaTerm) *SubscriptionQuotaTerm {
	if t == nil {
		return nil
	}
	return &SubscriptionQuotaTerm{ID: t.ID, GroupID: t.GroupID, SubscriptionID: t.SubscriptionID, TermNo: t.TermNo, QuotaUSD: t.QuotaUSD, UsedUSD: t.UsedUSD, PendingUSD: t.PendingUSD, RemainingUSD: t.RemainingUSD, Status: t.Status, LifecycleState: t.LifecycleState, StartsAt: t.StartsAt, ExpiresAt: t.ExpiresAt, CancelledAt: t.CancelledAt, Revision: t.Revision, UnsettledRequestCount: t.UnsettledRequestCount}
}

func quotaTerms(terms []domain.SubscriptionQuotaTerm) []SubscriptionQuotaTerm {
	items := make([]SubscriptionQuotaTerm, 0, len(terms))
	for i := range terms {
		items = append(items, *SubscriptionQuotaTermFromDomain(&terms[i]))
	}
	return items
}

func SubscriptionQuotaSummaryFromDomain(s *domain.SubscriptionQuotaSummary) *SubscriptionQuotaSummary {
	if s == nil {
		return nil
	}
	return &SubscriptionQuotaSummary{GroupID: s.GroupID, QuotaMode: s.QuotaMode, Revision: s.Revision, ServerTime: s.ServerTime, CurrentTerm: SubscriptionQuotaTermFromDomain(s.CurrentTerm), LastTerm: SubscriptionQuotaTermFromDomain(s.LastTerm), NextTerms: quotaTerms(s.NextTerms)}
}

func SubscriptionQuotaPreviewFromDomain(p *domain.SubscriptionQuotaPreview) *SubscriptionQuotaPreview {
	if p == nil {
		return nil
	}
	warnings := append([]string{}, p.Warnings...)
	return &SubscriptionQuotaPreview{GroupID: p.GroupID, EffectiveQuotaUSD: p.EffectiveQuotaUSD, ProposedStartsAt: p.ProposedStartsAt, ProposedExpiresAt: p.ProposedExpiresAt, ServerTime: p.ServerTime, CurrentRevision: p.CurrentRevision, Warnings: warnings, CurrentTerm: SubscriptionQuotaTermFromDomain(p.CurrentTerm), NextTerms: quotaTerms(p.NextTerms)}
}

func SubscriptionQuotaMutationFromDomain(r *domain.SubscriptionQuotaMutationResult) *SubscriptionQuotaMutationResult {
	if r == nil {
		return nil
	}
	return &SubscriptionQuotaMutationResult{Term: *SubscriptionQuotaTermFromDomain(&r.Term), Summary: *SubscriptionQuotaSummaryFromDomain(&r.Summary), Replayed: r.Replayed}
}

func SubscriptionQuotaTermPageFromDomain(p *domain.SubscriptionQuotaTermPage) *SubscriptionQuotaTermPage {
	if p == nil {
		return nil
	}
	return &SubscriptionQuotaTermPage{Items: quotaTerms(p.Items), NextCursor: p.NextCursor, HasMore: p.HasMore}
}

func SubscriptionQuotaUsagePageFromDomain(p *domain.SubscriptionQuotaUsagePage) *SubscriptionQuotaUsagePage {
	if p == nil {
		return nil
	}
	items := make([]SubscriptionQuotaUsage, 0, len(p.Items))
	for _, u := range p.Items {
		items = append(items, SubscriptionQuotaUsage{RequestID: u.RequestID, APIKeyID: u.APIKeyID, TermID: u.TermID, Model: u.Model, ActualCostUSD: u.ActualCostUSD, SettledAt: u.SettledAt, InputTokens: u.InputTokens, OutputTokens: u.OutputTokens})
	}
	return &SubscriptionQuotaUsagePage{Items: items, NextCursor: p.NextCursor, HasMore: p.HasMore}
}
