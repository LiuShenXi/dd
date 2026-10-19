//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
)

type subscriptionQuotaPortStub struct {
	SubscriptionQuotaRepository
	calls  int
	open   domain.SubscriptionQuotaOpenParams
	cancel domain.SubscriptionQuotaCancelParams
}

func (s *subscriptionQuotaPortStub) OpenTerm(_ context.Context, p domain.SubscriptionQuotaOpenParams) (*domain.SubscriptionQuotaMutationResult, error) {
	s.calls++
	s.open = p
	return &domain.SubscriptionQuotaMutationResult{Replayed: !p.IssueAllowed}, nil
}
func (s *subscriptionQuotaPortStub) CancelTerm(_ context.Context, p domain.SubscriptionQuotaCancelParams) (*domain.SubscriptionQuotaMutationResult, error) {
	s.calls++
	s.cancel = p
	return &domain.SubscriptionQuotaMutationResult{}, nil
}

func TestSubscriptionQuotaServiceInvalidInputNeverCallsRepository(t *testing.T) {
	repo := &subscriptionQuotaPortStub{}
	svc := NewSubscriptionQuotaService(repo, &config.Config{})
	rev := int64(0)
	for _, input := range []string{"", "0", "-1", "NaN", "Infinity", "1e2", "01", "1.000000001"} {
		_, err := svc.OpenTerm(context.Background(), 1, 2, SubscriptionQuotaOpenInput{GroupID: 3, Operation: "open", QuotaUSD: &input, ExpectedRevision: &rev}, "valid-test-operation-key")
		require.ErrorIs(t, err, ErrSubscriptionQuotaInvalid, input)
	}
	quota := "1"
	_, err := svc.OpenTerm(context.Background(), 1, 2, SubscriptionQuotaOpenInput{GroupID: 3, Operation: "open", QuotaUSD: &quota}, "valid-test-operation-key")
	require.ErrorIs(t, err, ErrSubscriptionQuotaInput)
	_, err = svc.OpenTerm(context.Background(), 1, 2, SubscriptionQuotaOpenInput{GroupID: 3, Operation: "open", ExpectedRevision: &rev}, "valid-test-operation-key")
	require.ErrorIs(t, err, ErrSubscriptionQuotaRequired)
	require.Zero(t, repo.calls)
}
func TestSubscriptionQuotaServiceGateAllowsRepositoryReplayButNoNewIssue(t *testing.T) {
	repo := &subscriptionQuotaPortStub{}
	cfg := &config.Config{}
	svc := NewSubscriptionQuotaService(repo, cfg)
	rev := int64(0)
	quota := "12.5"
	in := SubscriptionQuotaOpenInput{GroupID: 3, Operation: "open", QuotaUSD: &quota, ExpectedRevision: &rev}
	result, err := svc.OpenTerm(context.Background(), 1, 2, in, "valid-test-operation-key")
	require.NoError(t, err)
	require.True(t, result.Replayed)
	require.False(t, repo.open.IssueAllowed)
	require.Equal(t, "12.50000000", *repo.open.QuotaUSD)
	cfg.SubscriptionQuota.IssueEnabled = true
	cfg.SubscriptionQuota.EnabledGroupIDs = []int64{3}
	_, err = svc.OpenTerm(context.Background(), 1, 2, in, "valid-test-operation-key")
	require.NoError(t, err)
	require.True(t, repo.open.IssueAllowed)
	cfg.RunMode = config.RunModeSimple
	_, err = svc.OpenTerm(context.Background(), 1, 2, in, "valid-test-operation-key")
	require.NoError(t, err)
	require.False(t, repo.open.IssueAllowed)
	_, err = svc.CancelTerm(context.Background(), 10, 2, SubscriptionQuotaCancelInput{ExpectedRevision: &rev, Reason: " cancel future card "}, "valid-cancel-operation-key")
	require.NoError(t, err)
	require.Equal(t, "cancel future card", repo.cancel.Reason)
}
