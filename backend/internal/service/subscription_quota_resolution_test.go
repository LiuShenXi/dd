//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
)

type quotaResolutionRepoStub struct {
	calls  int
	params domain.SubscriptionQuotaResolutionParams
}

func (r *quotaResolutionRepoStub) ResolveRequest(_ context.Context, p domain.SubscriptionQuotaResolutionParams) (*domain.SubscriptionQuotaResolveResult, error) {
	r.calls++
	r.params = p
	return &domain.SubscriptionQuotaResolveResult{Request: domain.SubscriptionQuotaRequest{ID: p.RequestRowID, Status: "no_charge"}}, nil
}

type quotaResolutionCalculatorStub struct {
	calls   int
	usage   SubscriptionQuotaVerifiedUsage
	failure error
}

func (c *quotaResolutionCalculatorStub) CalculateSubscriptionQuotaVerifiedCost(_ json.RawMessage, in SubscriptionQuotaVerifiedUsage, _ time.Time) (*SubscriptionQuotaCalculatedCost, error) {
	c.calls++
	c.usage = in
	return nil, c.failure
}

func TestSubscriptionQuotaResolutionValidationAndNoCharge(t *testing.T) {
	revision := int64(0)
	valid := SubscriptionQuotaResolveInput{ExpectedRequestRevision: &revision, Resolution: "no_charge", EvidenceReference: " provider-audit-42 ", Reason: " confirmed no consumption "}
	repo := &quotaResolutionRepoStub{}
	svc := NewSubscriptionQuotaResolutionService(repo, nil)
	result, err := svc.ResolveRequest(context.Background(), 7, 11, valid, "resolution-key-0001")
	require.NoError(t, err)
	require.EqualValues(t, 11, result.Request.ID)
	require.EqualValues(t, 7, repo.params.ActorID)
	require.EqualValues(t, 0, repo.params.ExpectedRequestRevision)
	require.Equal(t, "provider-audit-42", repo.params.EvidenceReference)
	require.Equal(t, "confirmed no consumption", repo.params.Reason)
	require.Nil(t, repo.params.PrepareKnown)
	for _, mutate := range []func(*SubscriptionQuotaResolveInput){func(in *SubscriptionQuotaResolveInput) { in.ExpectedRequestRevision = nil }, func(in *SubscriptionQuotaResolveInput) { negative := int64(-1); in.ExpectedRequestRevision = &negative }, func(in *SubscriptionQuotaResolveInput) { in.Resolution = "settled" }, func(in *SubscriptionQuotaResolveInput) { in.EvidenceReference = " " }, func(in *SubscriptionQuotaResolveInput) { in.Reason = " " }, func(in *SubscriptionQuotaResolveInput) { in.VerifiedUsage = &SubscriptionQuotaVerifiedUsage{} }, func(in *SubscriptionQuotaResolveInput) { in.Resolution = "verified_usage" }} {
		invalid := valid
		mutate(&invalid)
		_, err = svc.ResolveRequest(context.Background(), 7, 11, invalid, "resolution-key-0002")
		require.ErrorIs(t, err, ErrSubscriptionQuotaInput)
	}
	require.Equal(t, 1, repo.calls, "invalid resolutions make no repository call")
}

func TestSubscriptionQuotaResolutionVerifiedUsesStoredSnapshotAndCopiedEvidence(t *testing.T) {
	revision := int64(3)
	verified := &SubscriptionQuotaVerifiedUsage{RequestedModel: "original", InputTokens: 13, OutputTokens: 5}
	repo := &quotaResolutionRepoStub{}
	failure := errors.New("snapshot pricing proof")
	calculator := &quotaResolutionCalculatorStub{failure: failure}
	svc := NewSubscriptionQuotaResolutionService(repo, calculator)
	_, err := svc.ResolveRequest(context.Background(), 7, 11, SubscriptionQuotaResolveInput{ExpectedRequestRevision: &revision, Resolution: "verified_usage", EvidenceReference: "audit", Reason: "provider verified", VerifiedUsage: verified}, "resolution-key-0001")
	require.NoError(t, err)
	require.NotNil(t, repo.params.PrepareKnown)
	require.Zero(t, calculator.calls, "calculation waits until repository guards and replay checks finish")
	verified.InputTokens = 999
	verified.RequestedModel = "mutated"
	revision = 99
	_, err = repo.params.PrepareKnown(domain.SubscriptionQuotaKnownUsage{Snapshot: domain.SubscriptionQuotaBillingSnapshot{PricingSnapshot: json.RawMessage(`{"frozen":true}`), PricingAt: time.Now()}})
	require.ErrorIs(t, err, failure)
	require.EqualValues(t, 13, calculator.usage.InputTokens)
	require.Equal(t, "original", calculator.usage.RequestedModel)
	require.EqualValues(t, 3, repo.params.ExpectedRequestRevision)
}
