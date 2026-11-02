package service

import (
	"context"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
)

type SubscriptionQuotaSummaryReader interface {
	GetSummary(context.Context, int64, int64) (*domain.SubscriptionQuotaSummary, error)
}

func (s *SubscriptionService) SetSubscriptionQuotaSummaryReader(reader SubscriptionQuotaSummaryReader) {
	s.quotaSummaryReader = reader
}

func ProvideSubscriptionService(groups GroupRepository, subscriptions UserSubscriptionRepository, cache *BillingCacheService, ent *dbent.Client, cfg *config.Config, quota *SubscriptionQuotaService) *SubscriptionService {
	s := NewSubscriptionService(groups, subscriptions, cache, ent, cfg)
	s.SetSubscriptionQuotaSummaryReader(quota)
	return s
}

func (s *SubscriptionService) GetTermQuotaSummary(ctx context.Context, userID, groupID int64) (*domain.SubscriptionQuotaSummary, error) {
	if s == nil || s.quotaSummaryReader == nil {
		return nil, ErrSubscriptionQuotaBillingUnavailable
	}
	summary, err := s.quotaSummaryReader.GetSummary(ctx, userID, groupID)
	if err != nil {
		return nil, err
	}
	if summary == nil || summary.GroupID != groupID || summary.QuotaMode != domain.SubscriptionQuotaModeTerm {
		return nil, ErrSubscriptionQuotaBillingUnavailable
	}
	return summary, nil
}

// Display projection only. Authorization continues to use the final database
// admission/current-term reader, never these parent rows or their expiry.
func (s *SubscriptionService) projectQuotaRows(ctx context.Context, rows []UserSubscription, activeOnly bool) ([]UserSubscription, error) {
	out := make([]UserSubscription, 0, len(rows))
	for _, row := range rows {
		mode := row.QuotaMode
		if row.Group != nil {
			mode = row.Group.EffectiveSubscriptionQuotaMode()
		}
		if row.Group == nil {
			// Native history may intentionally omit a soft-deleted Group edge.
			// Read only its persisted mode without reviving that edge or assuming
			// that an absent relationship means a legacy windows subscription.
			if reader, ok := s.userSubRepo.(interface {
				GetSubscriptionQuotaMode(context.Context, int64) (string, error)
			}); ok {
				var err error
				mode, err = reader.GetSubscriptionQuotaMode(ctx, row.GroupID)
				if err != nil {
					return nil, ErrSubscriptionQuotaBillingUnavailable
				}
			} else {
				if s.groupRepo == nil {
					return nil, ErrSubscriptionQuotaBillingUnavailable
				}
				group, err := s.groupRepo.GetByID(ctx, row.GroupID)
				if err != nil || group == nil {
					return nil, ErrSubscriptionQuotaBillingUnavailable
				}
				row.Group, mode = group, group.EffectiveSubscriptionQuotaMode()
			}
		}
		if mode != domain.SubscriptionQuotaModeWindows && mode != domain.SubscriptionQuotaModeTerm {
			return nil, ErrSubscriptionQuotaBillingUnavailable
		}
		row.QuotaMode = domain.SubscriptionQuotaModeWindows
		row.QuotaSummary = nil
		if mode == domain.SubscriptionQuotaModeTerm {
			summary, err := s.GetTermQuotaSummary(ctx, row.UserID, row.GroupID)
			if err != nil {
				return nil, err
			}
			row.QuotaMode, row.QuotaSummary = domain.SubscriptionQuotaModeTerm, summary
			row.DailyWindowStart, row.WeeklyWindowStart, row.MonthlyWindowStart = nil, nil, nil
			row.DailyUsageUSD, row.WeeklyUsageUSD, row.MonthlyUsageUSD = 0, 0, 0
			if summary.CurrentTerm != nil {
				row.Status = summary.CurrentTerm.Status
				row.StartsAt, row.ExpiresAt = summary.CurrentTerm.StartsAt, summary.CurrentTerm.ExpiresAt
			} else {
				if activeOnly {
					continue
				}
				row.Status = "not_active"
				if len(summary.NextTerms) > 0 {
					row.Status = "scheduled"
				} else if summary.LastTerm != nil {
					row.Status = summary.LastTerm.Status
				}
			}
		}
		out = append(out, row)
	}
	return out, nil
}
