package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

type renewalPreviewRepository struct {
	CarpoolRepositoryAPI
	term domain.CarpoolTerm
	plan domain.CarpoolPlan
	now  time.Time
}

func (r *renewalPreviewRepository) GetTerm(context.Context, int64) (*domain.CarpoolTerm, error) {
	return &r.term, nil
}
func (r *renewalPreviewRepository) ListPlans(context.Context, bool) ([]domain.CarpoolPlan, error) {
	return []domain.CarpoolPlan{r.plan}, nil
}
func (r *renewalPreviewRepository) GetPreviewPlan(_ context.Context, _, planID int64) (*domain.CarpoolPlan, error) {
	if planID != r.plan.ID || !r.plan.Enabled {
		return nil, ErrCarpoolUnavailable
	}
	return &r.plan, nil
}
func (r *renewalPreviewRepository) DatabaseNow(context.Context) (time.Time, error) {
	return r.now, nil
}

func TestCarpoolRenewalPreviewPreservesOnlyDefaultOverrides(t *testing.T) {
	for _, quota := range []int64{450, 1000} {
		now := time.Date(2026, 9, 8, 3, 0, 0, 0, time.UTC)
		plan := domain.CarpoolPlan{ID: 2, Code: "four_seat", Version: 2, Enabled: true, DurationDays: 28, CycleDays: 7, WeeklyQuotaUSD: decimal.NewFromInt(550), BoostRatio: decimal.RequireFromString("0.1"), BoostCount: 2}
		repo := &renewalPreviewRepository{plan: plan, now: now, term: domain.CarpoolTerm{ID: 4, UserID: 8, ScopeID: 1, ExpiresAt: now.Add(3*24*time.Hour + 123456*time.Microsecond), PlanSnapshot: plan.Snapshot().WithWeeklyQuotaOverride(decimal.NewFromInt(quota)).WithDurationOverride(7)}}
		service := NewCarpoolService(repo)
		preview, err := service.PreviewRenewal(context.Background(), 8, 4, 0)
		require.NoError(t, err)
		require.Equal(t, repo.term.ExpiresAt, preview.StartsAt)
		require.Equal(t, repo.term.ExpiresAt.Add(7*24*time.Hour), preview.ExpiresAt)
		require.True(t, decimal.NewFromInt(quota).Equal(preview.Plan.WeeklyQuotaUSD))
		require.True(t, decimal.NewFromInt(quota).Mul(plan.BoostRatio).Equal(preview.Plan.BoostAmountUSD))
		require.Len(t, preview.Cycles, 1)
		require.Equal(t, "scheduled", preview.Cycles[0].InitialAction)
		require.Equal(t, preview.ExpiresAt, preview.Cycles[0].EndsAt)
		explicit, err := service.PreviewRenewal(context.Background(), 8, 4, 2)
		require.NoError(t, err)
		require.Equal(t, 28, explicit.Plan.DurationDays)
		require.False(t, explicit.Plan.WeeklyQuotaCustomized)
		require.False(t, explicit.Plan.DurationCustomized)
		require.True(t, plan.WeeklyQuotaUSD.Equal(explicit.Plan.WeeklyQuotaUSD))
		_, err = service.PreviewRenewal(context.Background(), 9, 4, 0)
		require.ErrorIs(t, err, ErrCarpoolInvalidRelationship)
		repo.plan.Enabled = false
		_, err = service.PreviewRenewal(context.Background(), 8, 4, 0)
		require.ErrorIs(t, err, ErrCarpoolUnavailable)
	}
}
