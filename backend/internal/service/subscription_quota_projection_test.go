//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
)

type quotaProjectionLegacyRepo struct {
	UserSubscriptionRepository
	rows []UserSubscription
}

func (r quotaProjectionLegacyRepo) ListByUserID(context.Context, int64) ([]UserSubscription, error) {
	return r.rows, nil
}
func (r quotaProjectionLegacyRepo) ListActiveByUserID(context.Context, int64) ([]UserSubscription, error) {
	return r.rows, nil
}
func (r quotaProjectionLegacyRepo) GetByID(context.Context, int64) (*UserSubscription, error) {
	row := r.rows[0]
	return &row, nil
}

type quotaProjectionGroupRepo struct {
	GroupRepository
	group *Group
	calls int
}

func (r *quotaProjectionGroupRepo) GetByID(context.Context, int64) (*Group, error) {
	r.calls++
	return r.group, nil
}

type quotaProjectionReader struct {
	summary     *domain.SubscriptionQuotaSummary
	err         error
	calls       int
	user, group int64
}

type quotaProjectionHistoricalRepo struct {
	quotaProjectionLegacyRepo
	mode string
}

func (r quotaProjectionHistoricalRepo) GetSubscriptionQuotaMode(context.Context, int64) (string, error) {
	return r.mode, nil
}

func TestSubscriptionQuotaLegacyProjectionDeletedGroupUsesStoredMode(t *testing.T) {
	reader := &quotaProjectionReader{err: errors.New("windows history must not touch term storage")}
	repo := quotaProjectionHistoricalRepo{quotaProjectionLegacyRepo: quotaProjectionLegacyRepo{rows: []UserSubscription{{ID: 1, UserID: 11, GroupID: 6}}}, mode: "windows"}
	s := &SubscriptionService{userSubRepo: repo, quotaSummaryReader: reader}
	rows, err := s.ListUserSubscriptions(context.Background(), 11)
	require.NoError(t, err)
	require.Equal(t, "windows", rows[0].QuotaMode)
	require.Nil(t, rows[0].Group)
	require.Zero(t, reader.calls)
	repo.mode = "term"
	s.userSubRepo = repo
	_, err = s.ListUserSubscriptions(context.Background(), 11)
	require.Error(t, err)
	require.Equal(t, 1, reader.calls)
}

func (r *quotaProjectionReader) GetSummary(_ context.Context, user, group int64) (*domain.SubscriptionQuotaSummary, error) {
	r.calls++
	r.user, r.group = user, group
	return r.summary, r.err
}

func TestSubscriptionQuotaLegacyProjectionUsesCanonicalTerm(t *testing.T) {
	now := time.Now().UTC()
	group := &Group{ID: 6, Name: "Claude", Platform: PlatformAnthropic, SubscriptionType: SubscriptionTypeSubscription, SubscriptionQuotaMode: "term"}
	reader := &quotaProjectionReader{summary: &domain.SubscriptionQuotaSummary{GroupID: 6, QuotaMode: "term", ServerTime: now, CurrentTerm: &domain.SubscriptionQuotaTerm{ID: 40, Status: "reconciling", QuotaUSD: "100.00000000", UsedUSD: "30.00000000", PendingUSD: "1.25000000", RemainingUSD: "68.75000000", StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(167 * time.Hour)}}}
	groups := &quotaProjectionGroupRepo{group: group}
	legacy := quotaProjectionLegacyRepo{rows: []UserSubscription{{ID: 3, UserID: 11, GroupID: 6, Status: "active", ExpiresAt: now.Add(1000 * time.Hour), DailyUsageUSD: 999, WeeklyUsageUSD: 999, MonthlyUsageUSD: 999}}}
	s := &SubscriptionService{userSubRepo: legacy, groupRepo: groups, quotaSummaryReader: reader}
	rows, err := s.ListUserSubscriptions(context.Background(), 11)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "term", rows[0].QuotaMode)
	require.Equal(t, "reconciling", rows[0].Status)
	require.Equal(t, reader.summary.CurrentTerm.ExpiresAt, rows[0].ExpiresAt)
	require.Zero(t, rows[0].DailyUsageUSD)
	require.Zero(t, rows[0].WeeklyUsageUSD)
	require.Zero(t, rows[0].MonthlyUsageUSD)
	require.Equal(t, int64(11), reader.user)
	require.Equal(t, int64(6), reader.group)
	require.Equal(t, 1, groups.calls)
	progress, err := s.GetSubscriptionProgress(context.Background(), 3)
	require.NoError(t, err)
	require.Equal(t, "term", progress.QuotaMode)
	require.Equal(t, "30.00000000", progress.QuotaSummary.CurrentTerm.UsedUSD)
	require.Nil(t, progress.Daily)
	require.Nil(t, progress.Weekly)
	require.Nil(t, progress.Monthly)
	require.Equal(t, float64(999), legacy.rows[0].DailyUsageUSD, "projection must not mutate stored parent facts")
}

func TestSubscriptionQuotaLegacyActiveUsesCurrentCardAndWindowsNeverReadsTerms(t *testing.T) {
	now := time.Now()
	windowGroup := &Group{ID: 7, SubscriptionType: SubscriptionTypeSubscription}
	termGroup := &Group{ID: 6, SubscriptionType: SubscriptionTypeSubscription, SubscriptionQuotaMode: "term"}
	reader := &quotaProjectionReader{summary: &domain.SubscriptionQuotaSummary{GroupID: 6, QuotaMode: "term", ServerTime: now, NextTerms: []domain.SubscriptionQuotaTerm{{ID: 40, StartsAt: now.Add(time.Hour)}}}}
	legacy := quotaProjectionLegacyRepo{rows: []UserSubscription{{ID: 1, UserID: 11, GroupID: 7, Group: windowGroup, Status: "active", ExpiresAt: now.Add(time.Hour), WeeklyUsageUSD: 5}, {ID: 2, UserID: 11, GroupID: 6, Group: termGroup, Status: "active", ExpiresAt: now.Add(1000 * time.Hour)}}}
	s := &SubscriptionService{userSubRepo: legacy, quotaSummaryReader: reader}
	rows, err := s.ListActiveUserSubscriptions(context.Background(), 11)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, int64(7), rows[0].GroupID)
	require.Equal(t, "windows", rows[0].QuotaMode)
	require.Equal(t, 1, reader.calls)
	s.userSubRepo = quotaProjectionLegacyRepo{rows: legacy.rows[:1]}
	reader.err = errors.New("must not query term reader for GPT/windows")
	reader.calls = 0
	_, err = s.ListUserSubscriptions(context.Background(), 11)
	require.NoError(t, err)
	_, err = s.ListActiveUserSubscriptions(context.Background(), 11)
	require.NoError(t, err)
	_, err = s.GetByID(context.Background(), 1)
	require.NoError(t, err)
	_, err = s.GetSubscriptionProgress(context.Background(), 1)
	require.NoError(t, err)
	require.Zero(t, reader.calls)
}

func TestSubscriptionQuotaLegacyProjectionUnavailableCannotBecomeWindows(t *testing.T) {
	legacy := quotaProjectionLegacyRepo{rows: []UserSubscription{{ID: 1, UserID: 11, GroupID: 6}}}
	s := &SubscriptionService{userSubRepo: legacy}
	_, err := s.ListUserSubscriptions(context.Background(), 11)
	require.ErrorIs(t, err, ErrSubscriptionQuotaBillingUnavailable)
	s.groupRepo = &quotaProjectionGroupRepo{group: &Group{ID: 6, SubscriptionQuotaMode: "term"}}
	_, err = s.ListUserSubscriptions(context.Background(), 11)
	require.ErrorIs(t, err, ErrSubscriptionQuotaBillingUnavailable)
	s.quotaSummaryReader = &quotaProjectionReader{summary: &domain.SubscriptionQuotaSummary{GroupID: 7, QuotaMode: "term"}}
	_, err = s.ListUserSubscriptions(context.Background(), 11)
	require.ErrorIs(t, err, ErrSubscriptionQuotaBillingUnavailable)
}
