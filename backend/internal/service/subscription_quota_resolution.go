package service

import (
	"context"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var (
	ErrSubscriptionQuotaRequestRevision = infraerrors.Conflict("REQUEST_REVISION_CONFLICT", "request changed; inspect it again before resolving")
	ErrSubscriptionQuotaNotResolvable   = infraerrors.Conflict("TERM_REQUEST_NOT_RESOLVABLE", "only an unknown request without known consumption can be resolved")
)

type SubscriptionQuotaResolutionService struct {
	repo       SubscriptionQuotaResolutionRepository
	calculator SubscriptionQuotaVerifiedCostCalculator
}

func NewSubscriptionQuotaResolutionService(repo SubscriptionQuotaResolutionRepository, calculator SubscriptionQuotaVerifiedCostCalculator) *SubscriptionQuotaResolutionService {
	return &SubscriptionQuotaResolutionService{repo: repo, calculator: calculator}
}

func (s *SubscriptionQuotaResolutionService) ResolveRequest(ctx context.Context, actorID, rowID int64, in SubscriptionQuotaResolveInput, key string) (*domain.SubscriptionQuotaResolveResult, error) {
	in.EvidenceReference = strings.TrimSpace(in.EvidenceReference)
	in.Reason = strings.TrimSpace(in.Reason)
	if actorID <= 0 || rowID <= 0 || in.ExpectedRequestRevision == nil || *in.ExpectedRequestRevision < 0 || !validSubscriptionQuotaKey(key) || in.EvidenceReference == "" || in.Reason == "" || utf8.RuneCountInString(in.EvidenceReference) > 500 || utf8.RuneCountInString(in.Reason) > 500 {
		return nil, ErrSubscriptionQuotaInput
	}
	switch in.Resolution {
	case "no_charge":
		if in.VerifiedUsage != nil {
			return nil, ErrSubscriptionQuotaInput
		}
	case "verified_usage":
		if in.VerifiedUsage == nil {
			return nil, ErrSubscriptionQuotaInput
		}
		copy := *in.VerifiedUsage
		in.VerifiedUsage = &copy
	default:
		return nil, ErrSubscriptionQuotaInput
	}
	if s == nil || s.repo == nil {
		return nil, ErrSubscriptionQuotaBillingUnavailable
	}
	if in.Resolution == "verified_usage" && s.calculator == nil {
		return nil, ErrSubscriptionQuotaBillingUnavailable
	}
	revision := *in.ExpectedRequestRevision
	in.ExpectedRequestRevision = &revision
	intent, err := json.Marshal(in)
	if err != nil {
		return nil, ErrSubscriptionQuotaInput
	}
	p := domain.SubscriptionQuotaResolutionParams{RequestRowID: rowID, ActorID: actorID, ExpectedRequestRevision: revision, Resolution: in.Resolution, EvidenceReference: in.EvidenceReference, Reason: in.Reason, IdempotencyKey: key, NormalizedIntent: intent}
	if in.VerifiedUsage != nil {
		verified := *in.VerifiedUsage
		p.PrepareKnown = func(stored domain.SubscriptionQuotaKnownUsage) (domain.SubscriptionQuotaKnownUsage, error) {
			calculated, err := s.calculator.CalculateSubscriptionQuotaVerifiedCost(stored.Snapshot.PricingSnapshot, verified, stored.Snapshot.PricingAt)
			if err != nil {
				return domain.SubscriptionQuotaKnownUsage{}, err
			}
			_, known, err := PrepareSubscriptionQuotaKnownUsage(&stored.Snapshot, calculated, nil)
			return known, err
		}
	}
	return s.repo.ResolveRequest(ctx, p)
}
