package service

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var (
	ErrSubscriptionQuotaRequired       = infraerrors.BadRequest("QUOTA_REQUIRED", "provide a positive allowance or a plan with a default allowance")
	ErrSubscriptionQuotaInvalid        = infraerrors.BadRequest("INVALID_QUOTA", "allowance must be a positive decimal with at most eight fractional digits")
	ErrSubscriptionQuotaInput          = infraerrors.BadRequest("INVALID_SUBSCRIPTION_QUOTA_INPUT", "invalid subscription quota input")
	ErrSubscriptionQuotaNotFound       = infraerrors.NotFound("SUBSCRIPTION_QUOTA_NOT_FOUND", "subscription quota record not found")
	ErrSubscriptionQuotaRevision       = infraerrors.Conflict("QUOTA_REVISION_CONFLICT", "schedule changed; preview again before submitting")
	ErrSubscriptionQuotaIdempotency    = infraerrors.Conflict("IDEMPOTENCY_CONFLICT", "idempotency key was already used with different input")
	ErrSubscriptionQuotaSchedule       = infraerrors.Conflict("TERM_SCHEDULE_CONFLICT", "an unfinished card already exists; use renewal to append a card")
	ErrSubscriptionQuotaNotCancellable = infraerrors.Conflict("TERM_NOT_CANCELLABLE", "this card has already ended or was cancelled")
)

type SubscriptionQuotaService struct {
	repo SubscriptionQuotaRepository
	cfg  *config.Config
}

func NewSubscriptionQuotaService(repo SubscriptionQuotaRepository, cfg *config.Config) *SubscriptionQuotaService {
	return &SubscriptionQuotaService{repo: repo, cfg: cfg}
}

func (s *SubscriptionQuotaService) openParams(userID, actorID int64, in SubscriptionQuotaOpenInput, mutation bool) (domain.SubscriptionQuotaOpenParams, error) {
	p := domain.SubscriptionQuotaOpenParams{UserID: userID, ActorID: actorID, GroupID: in.GroupID, Operation: in.Operation, PlanID: in.PlanID, Notes: in.Notes}
	if userID <= 0 || actorID <= 0 || in.GroupID <= 0 || (in.Operation != "open" && in.Operation != "renew") || (in.PlanID != nil && *in.PlanID <= 0) || utf8.RuneCountInString(in.Notes) > 500 {
		return p, ErrSubscriptionQuotaInput
	}
	if mutation {
		if in.ExpectedRevision == nil || *in.ExpectedRevision < 0 {
			return p, ErrSubscriptionQuotaInput
		}
		p.ExpectedRevision = *in.ExpectedRevision
	}
	if in.QuotaUSD != nil {
		value, err := domain.NormalizeSubscriptionQuotaMoney(*in.QuotaUSD)
		if err != nil {
			return p, ErrSubscriptionQuotaInvalid
		}
		p.QuotaUSD = &value
	} else if in.PlanID == nil {
		return p, ErrSubscriptionQuotaRequired
	}
	if s != nil && s.cfg != nil && s.cfg.RunMode != config.RunModeSimple && s.cfg.SubscriptionQuota.IssueEnabled {
		for _, id := range s.cfg.SubscriptionQuota.EnabledGroupIDs {
			if id == in.GroupID {
				p.IssueAllowed = true
				break
			}
		}
	}
	return p, nil
}
func validSubscriptionQuotaKey(key string) bool {
	return len(key) >= 16 && len(key) <= 128 && strings.TrimSpace(key) == key && !strings.ContainsAny(key, "\r\n\t\x00")
}
func (s *SubscriptionQuotaService) available() error {
	if s == nil || s.repo == nil {
		return ErrSubscriptionQuotaBillingUnavailable
	}
	return nil
}
func (s *SubscriptionQuotaService) Preview(ctx context.Context, userID, actorID int64, in SubscriptionQuotaOpenInput) (*domain.SubscriptionQuotaPreview, error) {
	p, err := s.openParams(userID, actorID, in, false)
	if err != nil {
		return nil, err
	}
	if err = s.available(); err != nil {
		return nil, err
	}
	return s.repo.PreviewTerm(ctx, p)
}
func (s *SubscriptionQuotaService) OpenTerm(ctx context.Context, userID, actorID int64, in SubscriptionQuotaOpenInput, key string) (*domain.SubscriptionQuotaMutationResult, error) {
	p, err := s.openParams(userID, actorID, in, true)
	if err != nil {
		return nil, err
	}
	if !validSubscriptionQuotaKey(key) {
		return nil, ErrSubscriptionQuotaInput
	}
	p.IdempotencyKey = key
	if err = s.available(); err != nil {
		return nil, err
	}
	return s.repo.OpenTerm(ctx, p)
}
func (s *SubscriptionQuotaService) CancelTerm(ctx context.Context, termID, actorID int64, in SubscriptionQuotaCancelInput, key string) (*domain.SubscriptionQuotaMutationResult, error) {
	reason := strings.TrimSpace(in.Reason)
	if termID <= 0 || actorID <= 0 || in.ExpectedRevision == nil || *in.ExpectedRevision < 0 || reason == "" || utf8.RuneCountInString(reason) > 500 || !validSubscriptionQuotaKey(key) {
		return nil, ErrSubscriptionQuotaInput
	}
	if err := s.available(); err != nil {
		return nil, err
	}
	return s.repo.CancelTerm(ctx, domain.SubscriptionQuotaCancelParams{TermID: termID, ActorID: actorID, ExpectedRevision: *in.ExpectedRevision, Reason: reason, IdempotencyKey: key})
}
func subscriptionQuotaPage(in SubscriptionQuotaPageInput) (domain.SubscriptionQuotaPageParams, error) {
	p := domain.SubscriptionQuotaPageParams{Limit: in.Limit}
	if p.Limit == 0 {
		p.Limit = 20
	}
	if p.Limit < 1 || p.Limit > 100 {
		return p, ErrSubscriptionQuotaInput
	}
	var err error
	p.BeforeID, err = domain.DecodeSubscriptionQuotaCursor(in.Cursor)
	if err != nil {
		return p, ErrSubscriptionQuotaInput
	}
	return p, nil
}
func (s *SubscriptionQuotaService) GetSummary(ctx context.Context, userID, groupID int64) (*domain.SubscriptionQuotaSummary, error) {
	if userID <= 0 || groupID <= 0 {
		return nil, ErrSubscriptionQuotaInput
	}
	if err := s.available(); err != nil {
		return nil, err
	}
	return s.repo.GetSummary(ctx, userID, groupID)
}
func (s *SubscriptionQuotaService) ListSummaries(ctx context.Context, userID int64) ([]domain.SubscriptionQuotaSummary, error) {
	if userID <= 0 {
		return nil, ErrSubscriptionQuotaInput
	}
	if err := s.available(); err != nil {
		return nil, err
	}
	return s.repo.ListSummaries(ctx, userID)
}
func (s *SubscriptionQuotaService) ListTerms(ctx context.Context, userID, groupID int64, page SubscriptionQuotaPageInput) (*domain.SubscriptionQuotaTermPage, error) {
	if userID <= 0 || groupID <= 0 {
		return nil, ErrSubscriptionQuotaInput
	}
	p, err := subscriptionQuotaPage(page)
	if err != nil {
		return nil, err
	}
	if err = s.available(); err != nil {
		return nil, err
	}
	return s.repo.ListTerms(ctx, userID, groupID, p)
}
func (s *SubscriptionQuotaService) ListUsage(ctx context.Context, userID, termID int64, page SubscriptionQuotaPageInput) (*domain.SubscriptionQuotaUsagePage, error) {
	if userID <= 0 || termID <= 0 {
		return nil, ErrSubscriptionQuotaInput
	}
	p, err := subscriptionQuotaPage(page)
	if err != nil {
		return nil, err
	}
	if err = s.available(); err != nil {
		return nil, err
	}
	return s.repo.ListUsage(ctx, userID, termID, p)
}
