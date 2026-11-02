package service

import (
	"context"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var (
	ErrSubscriptionQuotaRolloutBlocked = infraerrors.Conflict("SUBSCRIPTION_QUOTA_ROLLOUT_BLOCKED", "conversion has unresolved blockers; refresh the preview")
	ErrSubscriptionQuotaRolloutStale   = infraerrors.Conflict("SUBSCRIPTION_QUOTA_ROLLOUT_STALE", "conversion preview changed; preview and confirm again")
)

type SubscriptionQuotaRolloutRepository interface {
	RegisterReplica(context.Context, domain.SubscriptionQuotaReplica) error
	GetRollout(context.Context, int64, domain.SubscriptionQuotaRolloutPolicy) (*domain.SubscriptionQuotaRolloutState, error)
	MaintainRollout(context.Context, domain.SubscriptionQuotaMaintenanceParams) (*domain.SubscriptionQuotaRolloutState, error)
	PreviewRollout(context.Context, domain.SubscriptionQuotaRolloutParams) (*domain.SubscriptionQuotaRolloutState, error)
	ConvertRollout(context.Context, domain.SubscriptionQuotaRolloutParams) (*domain.SubscriptionQuotaRolloutResult, error)
}
type SubscriptionQuotaMaintenanceInput struct {
	Enabled          bool   `json:"enabled"`
	ExpectedRevision *int64 `json:"expected_revision"`
	Reason           string `json:"reason"`
}
type SubscriptionQuotaConversionInput struct {
	ExpectedRevision *int64                               `json:"expected_revision"`
	PreviewToken     string                               `json:"preview_token"`
	Allocations      []domain.SubscriptionQuotaAllocation `json:"allocations"`
}
type SubscriptionQuotaRolloutService struct {
	instanceToken string
	repo          SubscriptionQuotaRolloutRepository
	cfg           *config.Config
	cancel        context.CancelFunc
	done          chan struct{}
	stop          sync.Once
}

func NewSubscriptionQuotaRolloutService(repo SubscriptionQuotaRolloutRepository, cfg *config.Config) *SubscriptionQuotaRolloutService {
	return &SubscriptionQuotaRolloutService{repo: repo, cfg: cfg, instanceToken: generateRequestID()}
}
func (s *SubscriptionQuotaRolloutService) policy() domain.SubscriptionQuotaRolloutPolicy {
	p := domain.SubscriptionQuotaRolloutPolicy{}
	if s != nil && s.cfg != nil {
		p.ExpectedReplicaIDs = append([]string{}, s.cfg.SubscriptionQuota.RolloutExpectedReplicaIDs...)
		sort.Strings(p.ExpectedReplicaIDs)
	}
	return p
}
func (s *SubscriptionQuotaRolloutService) Get(ctx context.Context, groupID int64) (*domain.SubscriptionQuotaRolloutState, error) {
	if s == nil || s.repo == nil {
		return nil, ErrSubscriptionQuotaBillingUnavailable
	}
	if groupID <= 0 {
		return nil, ErrSubscriptionQuotaInput
	}
	return s.repo.GetRollout(ctx, groupID, s.policy())
}
func (s *SubscriptionQuotaRolloutService) Maintain(ctx context.Context, groupID, actorID int64, in SubscriptionQuotaMaintenanceInput, key string) (*domain.SubscriptionQuotaRolloutState, error) {
	if s == nil || s.repo == nil {
		return nil, ErrSubscriptionQuotaBillingUnavailable
	}
	if groupID <= 0 || actorID <= 0 || in.ExpectedRevision == nil || *in.ExpectedRevision < 0 || !validSubscriptionQuotaKey(key) || strings.TrimSpace(in.Reason) == "" || len(in.Reason) > 500 {
		return nil, ErrSubscriptionQuotaInput
	}
	return s.repo.MaintainRollout(ctx, domain.SubscriptionQuotaMaintenanceParams{GroupID: groupID, ActorID: actorID, ExpectedRevision: *in.ExpectedRevision, IdempotencyKey: key, Enabled: in.Enabled, Reason: strings.TrimSpace(in.Reason), Policy: s.policy()})
}
func NormalizeSubscriptionQuotaAllocations(in []domain.SubscriptionQuotaAllocation) ([]domain.SubscriptionQuotaAllocation, error) {
	if in == nil || len(in) > 1000 {
		return nil, ErrSubscriptionQuotaInput
	}
	out := append([]domain.SubscriptionQuotaAllocation{}, in...)
	sort.Slice(out, func(i, j int) bool { return out[i].UserID < out[j].UserID })
	for i := range out {
		if out[i].UserID <= 0 || i > 0 && out[i].UserID == out[i-1].UserID {
			return nil, ErrSubscriptionQuotaInput
		}
		quota, err := domain.NormalizeSubscriptionQuotaMoney(out[i].QuotaUSD)
		if err != nil {
			return nil, ErrSubscriptionQuotaInvalid
		}
		out[i].QuotaUSD = quota
	}
	return out, nil
}
func (s *SubscriptionQuotaRolloutService) Preview(ctx context.Context, groupID, actorID int64, allocations []domain.SubscriptionQuotaAllocation) (*domain.SubscriptionQuotaRolloutState, error) {
	if s == nil || s.repo == nil {
		return nil, ErrSubscriptionQuotaBillingUnavailable
	}
	if groupID <= 0 || actorID <= 0 {
		return nil, ErrSubscriptionQuotaInput
	}
	normalized, err := NormalizeSubscriptionQuotaAllocations(allocations)
	if err != nil {
		return nil, err
	}
	return s.repo.PreviewRollout(ctx, domain.SubscriptionQuotaRolloutParams{GroupID: groupID, ActorID: actorID, Allocations: normalized, Policy: s.policy()})
}
func (s *SubscriptionQuotaRolloutService) Convert(ctx context.Context, groupID, actorID int64, in SubscriptionQuotaConversionInput, key string) (*domain.SubscriptionQuotaRolloutResult, error) {
	if s == nil || s.repo == nil {
		return nil, ErrSubscriptionQuotaBillingUnavailable
	}
	if groupID <= 0 || actorID <= 0 || in.ExpectedRevision == nil || *in.ExpectedRevision < 0 || len(in.PreviewToken) != 64 || !validSubscriptionQuotaKey(key) {
		return nil, ErrSubscriptionQuotaInput
	}
	normalized, err := NormalizeSubscriptionQuotaAllocations(in.Allocations)
	if err != nil {
		return nil, err
	}
	return s.repo.ConvertRollout(ctx, domain.SubscriptionQuotaRolloutParams{GroupID: groupID, ActorID: actorID, ExpectedRevision: *in.ExpectedRevision, IdempotencyKey: key, PreviewToken: in.PreviewToken, Allocations: normalized, Policy: s.policy()})
}

// Heartbeats originate from running application configuration, never an admin
// request body. The explicit deployment roster also detects absent old replicas.
func (s *SubscriptionQuotaRolloutService) RegisterRuntime(ctx context.Context) error {
	if s == nil || s.repo == nil || s.cfg == nil || s.cfg.SubscriptionQuota.RolloutReplicaID == "" {
		return nil
	}
	q := s.cfg.SubscriptionQuota
	return s.repo.RegisterReplica(ctx, domain.SubscriptionQuotaReplica{InstanceToken: s.instanceToken, ReplicaID: q.RolloutReplicaID, CapabilityVersion: domain.SubscriptionQuotaRolloutCapabilityVersion, ReceiptVersion: 1, LeaseProtocolVersion: domain.SubscriptionQuotaLeaseProtocolVersion, ClaudeDefaultConcurrency: s.cfg.Default.ClaudeSubscriptionConcurrency, IssueEnabled: q.IssueEnabled, AdmissionEnabled: q.AdmissionEnabled, RecoveryEnabled: q.RecoveryEnabled, ProtectedGroupIDs: append([]int64{}, q.ProtectedGroupIDs...), EnabledGroupIDs: append([]int64{}, q.EnabledGroupIDs...), ExpectedReplicaIDs: s.policy().ExpectedReplicaIDs})
}
func ProvideSubscriptionQuotaRolloutService(repo SubscriptionQuotaRolloutRepository, cfg *config.Config) *SubscriptionQuotaRolloutService {
	s := NewSubscriptionQuotaRolloutService(repo, cfg)
	if cfg == nil || cfg.SubscriptionQuota.RolloutReplicaID == "" {
		return s
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.done = make(chan struct{})
	go func() {
		defer close(s.done)
		timer := time.NewTicker(10 * time.Second)
		defer timer.Stop()
		lastFailure := ""
		for {
			callCtx, done := context.WithTimeout(ctx, 5*time.Second)
			err := s.RegisterRuntime(callCtx)
			if err != nil && ctx.Err() == nil {
				if err.Error() != lastFailure {
					slog.Warn("subscription quota replica registration failed", "replica_id", cfg.SubscriptionQuota.RolloutReplicaID, "error", err)
				}
				lastFailure = err.Error()
			} else if err == nil && lastFailure != "" {
				slog.Info("subscription quota replica registration recovered", "replica_id", cfg.SubscriptionQuota.RolloutReplicaID)
				lastFailure = ""
			}
			done()
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
			}
		}
	}()
	return s
}
func (s *SubscriptionQuotaRolloutService) Stop() {
	if s != nil && s.cancel != nil {
		s.stop.Do(func() { s.cancel(); <-s.done })
	}
}
