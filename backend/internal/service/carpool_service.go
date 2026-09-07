package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/shopspring/decimal"
)

type CarpoolService struct {
	repo           CarpoolRepositoryAPI
	resetReader    CarpoolResetWindowReader
	billingApplier UsageBillingRepository

	mu                   sync.Mutex
	cancel               context.CancelFunc
	done                 chan struct{}
	maintenanceInterval  time.Duration
	maintenanceTimeout   time.Duration
	recoveryScanTimeout  time.Duration
	recoveryApplyTimeout time.Duration
	recoveryMarkTimeout  time.Duration
}

func NewCarpoolService(repo CarpoolRepositoryAPI) *CarpoolService {
	return &CarpoolService{
		repo:                 repo,
		maintenanceInterval:  30 * time.Second,
		maintenanceTimeout:   20 * time.Second,
		recoveryScanTimeout:  20 * time.Second,
		recoveryApplyTimeout: 5 * time.Second,
		recoveryMarkTimeout:  5 * time.Second,
	}
}

func (s *CarpoolService) SetResetWindowReader(reader CarpoolResetWindowReader) {
	s.resetReader = reader
}

func (s *CarpoolService) SetUsageBillingApplier(applier UsageBillingRepository) {
	s.billingApplier = applier
}

func (s *CarpoolService) Start(parent context.Context) {
	if parent == nil {
		parent = context.Background()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	s.cancel = cancel
	s.done = done
	go s.run(ctx, done)
}

func (s *CarpoolService) run(ctx context.Context, done chan struct{}) {
	defer close(done)
	interval := s.maintenanceInterval
	if interval <= 0 {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		s.maintain(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *CarpoolService) maintain(parent context.Context) {
	var work sync.WaitGroup
	work.Add(2)
	go func() {
		defer work.Done()
		timeout := s.maintenanceTimeout
		if timeout <= 0 {
			timeout = 20 * time.Second
		}
		ctx, cancel := context.WithTimeout(parent, timeout)
		defer cancel()
		now, err := s.repo.DatabaseNow(ctx)
		if err == nil {
			_ = s.repo.MaintainCycles(ctx, now, 200)
		}
	}()
	go func() {
		defer work.Done()
		s.recoverKnownUsage(parent, 100)
	}()
	work.Wait()
}

func (s *CarpoolService) recoverKnownUsage(ctx context.Context, limit int) {
	if s.billingApplier == nil {
		return
	}
	scanTimeout := s.recoveryScanTimeout
	if scanTimeout <= 0 {
		scanTimeout = 20 * time.Second
	}
	scanCtx, scanCancel := context.WithTimeout(ctx, scanTimeout)
	receipts, err := s.repo.RecoverPendingReceipts(scanCtx, limit)
	scanCancel()
	if err != nil {
		return
	}
	for i := range receipts {
		if ctx.Err() != nil {
			return
		}
		receipt := receipts[i]
		cmd, decodeErr := DecodeCarpoolUsageBillingReceipt(receipt.BillingPayload)
		if decodeErr != nil || !recoveryCommandMatchesReceipt(cmd, receipt) {
			s.markKnownUsageReconcileRequired(ctx, receipt.Snapshot, "invalid_durable_billing_command")
			continue
		}
		// RecoverPendingReceipts returns detached values; Apply owns its own
		// transaction and lock order, so no receipt lock surrounds this call.
		applyTimeout := s.recoveryApplyTimeout
		if applyTimeout <= 0 {
			applyTimeout = 5 * time.Second
		}
		applyCtx, applyCancel := context.WithTimeout(ctx, applyTimeout)
		_, applyErr := s.billingApplier.Apply(applyCtx, cmd)
		applyCancel()
		if applyErr != nil {
			if ctx.Err() != nil {
				return
			}
			if category := knownUsageRecoveryFailureCategory(applyErr, receipt.RetryCount); category != "" {
				s.markKnownUsageReconcileRequired(ctx, receipt.Snapshot, category)
			}
		}
	}
}

func (s *CarpoolService) markKnownUsageReconcileRequired(ctx context.Context, snapshot domain.CarpoolBillingSnapshot, category string) {
	if ctx.Err() != nil {
		return
	}
	timeout := s.recoveryMarkTimeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	markCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	_ = s.repo.MarkKnownUsageReconcileRequired(markCtx, snapshot, category)
}

const carpoolKnownUsageMaxRetryCount = 3

func knownUsageRecoveryFailureCategory(err error, retryCount int) string {
	for _, permanent := range []error{
		ErrUsageBillingRequestIDRequired,
		ErrUsageBillingRequestConflict,
		ErrUsageBillingSourceConflict,
		ErrUsageBillingCarpoolSnapshotInvalid,
		ErrUserNotFound,
		ErrAPIKeyNotFound,
		ErrAccountNotFound,
		ErrGroupNotFound,
		ErrSubscriptionNotFound,
		ErrCarpoolNotFound,
		ErrCarpoolInvalidRelationship,
		ErrCarpoolIdempotencyConflict,
		ErrCarpoolAdmissionReplay,
	} {
		if errors.Is(err, permanent) {
			return "automatic_settlement_permanent_failure"
		}
	}
	if retryCount >= carpoolKnownUsageMaxRetryCount {
		return "automatic_settlement_retry_exhausted"
	}
	return ""
}

func recoveryCommandMatchesReceipt(cmd *UsageBillingCommand, receipt domain.CarpoolKnownUsage) bool {
	if cmd == nil || cmd.CarpoolSnapshot == nil || !cmd.CarpoolCost.Equal(receipt.ActualCostUSD) {
		return false
	}
	snapshot := cmd.CarpoolSnapshot
	stored := receipt.Snapshot
	return snapshot.BillingRequestID == stored.BillingRequestID &&
		snapshot.RequestID == stored.RequestID &&
		snapshot.UserID == stored.UserID &&
		snapshot.APIKeyID == stored.APIKeyID &&
		snapshot.GroupID == stored.GroupID &&
		snapshot.TermID == stored.TermID &&
		snapshot.CycleID == stored.CycleID &&
		snapshot.AdmittedAt.Equal(stored.AdmittedAt)
}

func (s *CarpoolService) Stop() {
	s.mu.Lock()
	cancel, done := s.cancel, s.done
	s.cancel, s.done = nil, nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
}

func carpoolFingerprint(kind string, actorID int64, target any, payload any) (string, error) {
	body, err := json.Marshal(struct {
		Kind    string `json:"kind"`
		ActorID int64  `json:"actor_id"`
		Target  any    `json:"target"`
		Payload any    `json:"payload"`
	}{kind, actorID, target, payload})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}

type CarpoolOpenInput struct {
	PlanID   int64                        `json:"plan_id"`
	GroupID  int64                        `json:"group_id"`
	StartsAt *time.Time                   `json:"starts_at"`
	Mode     string                       `json:"mode"`
	Takeover *domain.CarpoolTakeoverInput `json:"takeover"`
	Notes    *string                      `json:"notes"`
	Payment  *domain.CarpoolPaymentInput  `json:"payment"`
}

type CarpoolRenewInput struct {
	PlanID  int64                       `json:"plan_id"`
	Notes   *string                     `json:"notes"`
	Payment *domain.CarpoolPaymentInput `json:"payment"`
}
type CarpoolTerminateInput struct {
	Reason      string     `json:"reason"`
	EffectiveAt *time.Time `json:"effective_at"`
}
type CarpoolAdjustmentInput struct {
	Bucket           string          `json:"bucket"`
	DeltaUSD         decimal.Decimal `json:"delta_usd"`
	Reason           string          `json:"reason"`
	ReversesLedgerID *int64          `json:"reverses_ledger_id"`
}
type CarpoolPlanVersionInput struct {
	Name           string          `json:"name"`
	ListPriceCNY   decimal.Decimal `json:"list_price_cny"`
	WeeklyQuotaUSD decimal.Decimal `json:"weekly_quota_usd"`
	BoostRatio     decimal.Decimal `json:"boost_ratio"`
	BoostCount     int             `json:"boost_count"`
	Enabled        bool            `json:"enabled"`
}
type CarpoolBillingReconcileInput struct {
	Confirmed     bool             `json:"confirmed"`
	Resolution    string           `json:"resolution"`
	ActualCostUSD *decimal.Decimal `json:"actual_cost_usd"`
	Reason        string           `json:"reason"`
}

func (s *CarpoolService) Preview(ctx context.Context, userID, planID int64, startsAt *time.Time, mode string, takeover *domain.CarpoolTakeoverInput) (*domain.CarpoolTermPreview, error) {
	if mode == "" {
		mode = "new"
	}
	if mode != "new" && mode != "takeover" {
		return nil, fmt.Errorf("invalid opening mode")
	}
	if mode == "takeover" && takeover == nil {
		return nil, fmt.Errorf("takeover details are required")
	}
	if mode == "new" && takeover != nil {
		return nil, fmt.Errorf("takeover details are not valid for new opening")
	}
	if takeover != nil {
		quantized := takeover.Quantized()
		takeover = &quantized
		if takeover.NextNaturalResetAt != nil {
			value := takeover.NextNaturalResetAt.UTC()
			takeover.NextNaturalResetAt = &value
		}
		if takeover.CurrentBoostBalanceUSD.IsNegative() || takeover.CurrentManualBalanceUSD.IsNegative() {
			return nil, infraerrors.BadRequest("CARPOOL_TAKEOVER_BUCKET_INVALID", "takeover boost and manual balances cannot be negative")
		}
		if !takeover.OrdinaryBalanceTransferUSD.IsZero() {
			return nil, infraerrors.BadRequest("CARPOOL_ORDINARY_BALANCE_READ_ONLY", "ordinary balance is read-only during carpool takeover")
		}
		if takeover.HistoricalUsedUSD != nil && takeover.HistoricalUsedUSD.IsNegative() {
			return nil, fmt.Errorf("historical_used_usd cannot be negative")
		}
	}
	now, err := s.repo.DatabaseNow(ctx)
	if err != nil {
		return nil, err
	}
	start := now
	if startsAt != nil {
		start = startsAt.UTC()
	}
	plan, err := s.repo.GetPreviewPlan(ctx, userID, planID)
	if err != nil {
		return nil, err
	}
	snapshot := plan.Snapshot()
	expiresAt := start.Add(time.Duration(snapshot.DurationDays) * 24 * time.Hour)
	if takeover != nil && (takeover.BoostUsed < 0 || takeover.BoostUsed > snapshot.BoostCount) {
		return nil, fmt.Errorf("invalid boost_used")
	}
	if takeover != nil && takeover.NextNaturalResetAt != nil {
		deadline := *takeover.NextNaturalResetAt
		if snapshot.EffectiveResetMode() != domain.CarpoolResetModeRolling || now.Before(start) || !now.Before(expiresAt) ||
			!deadline.After(now) || deadline.After(now.Add(time.Duration(snapshot.CycleDays)*24*time.Hour)) {
			return nil, ErrCarpoolInvalidNaturalReset
		}
	}
	preview := &domain.CarpoolTermPreview{CalculatedAt: now, Mode: mode, Plan: snapshot, StartsAt: start, ExpiresAt: expiresAt, Warnings: []string{}, OrdinaryBalanceDeductionUSD: decimal.Zero}
	if takeover != nil {
		if takeover.HistoryComplete && (takeover.HistoricalUsedUSD == nil || takeover.StatisticsSince == nil) {
			return nil, fmt.Errorf("complete takeover history requires historical fields")
		}
	}
	var takeoverDeadline *time.Time
	if takeover != nil {
		takeoverDeadline = takeover.NextNaturalResetAt
	}
	for _, spec := range domain.BuildCarpoolOpeningCycles(start, now, snapshot, takeoverDeadline) {
		action := "scheduled"
		if !now.Before(spec.EndsAt) {
			action = "missed"
		} else if !now.Before(spec.StartsAt) {
			if mode == "takeover" {
				action = "import"
			} else {
				action = "grant"
			}
		}
		preview.Cycles = append(preview.Cycles, domain.CarpoolPreviewCycle{CycleNo: spec.CycleNo, StartsAt: spec.StartsAt, EndsAt: spec.EndsAt, BaseQuotaUSD: spec.BaseQuotaUSD, InitialAction: action})
		if preview.NextNaturalResetAt == nil && now.Before(expiresAt) && spec.EndsAt.Before(expiresAt) && (now.Before(spec.StartsAt) || now.Before(spec.EndsAt)) {
			deadline := spec.EndsAt
			preview.NextNaturalResetAt = &deadline
		}
	}
	return preview, nil
}

func (s *CarpoolService) Open(ctx context.Context, userID, actorID int64, input CarpoolOpenInput, key string) (*domain.CarpoolAdminTerm, error) {
	if input.Takeover != nil {
		quantized := input.Takeover.Quantized()
		input.Takeover = &quantized
	}
	fp, err := carpoolFingerprint("open_term", actorID, userID, input)
	if err != nil {
		return nil, err
	}
	term, cycles, err := s.repo.CreateTerm(ctx, domain.CreateCarpoolTermParams{UserID: userID, ScopeID: domain.CarpoolGlobalScopeID, GroupID: input.GroupID, PlanID: input.PlanID, ActorID: actorID, StartsAt: input.StartsAt, Mode: input.Mode, Takeover: input.Takeover, Notes: input.Notes, Payment: input.Payment, Operation: domain.CarpoolOperation{Kind: "open_term", ActorID: actorID, Key: key, Fingerprint: fp}})
	if err != nil {
		return nil, err
	}
	return adminTermFrom(term, cycles), nil
}

func (s *CarpoolService) Renew(ctx context.Context, termID, actorID int64, input CarpoolRenewInput, key string) (*domain.CarpoolAdminTerm, error) {
	fp, err := carpoolFingerprint("renew_term", actorID, termID, input)
	if err != nil {
		return nil, err
	}
	term, cycles, err := s.repo.CreateTerm(ctx, domain.CreateCarpoolTermParams{ScopeID: domain.CarpoolGlobalScopeID, PlanID: input.PlanID, ActorID: actorID, RenewFromTermID: termID, Mode: "new", Notes: input.Notes, Payment: input.Payment, Operation: domain.CarpoolOperation{Kind: "renew_term", ActorID: actorID, Key: key, Fingerprint: fp}})
	if err != nil {
		return nil, err
	}
	return adminTermFrom(term, cycles), nil
}

func adminTermFrom(term *domain.CarpoolTerm, cycles []domain.CarpoolCycle) *domain.CarpoolAdminTerm {
	term.PlanSnapshot.NormalizeResetMode()
	result := &domain.CarpoolAdminTerm{ID: term.ID, UserID: term.UserID, ScopeID: term.ScopeID, GroupID: term.GroupID, PlanID: term.PlanID, PlanSnapshot: term.PlanSnapshot, StartsAt: term.StartsAt, ExpiresAt: term.ExpiresAt, Status: term.Status, BoostUsed: term.BoostUsed, BoostRemaining: term.PlanSnapshot.BoostCount - term.BoostUsed, HistoryComplete: term.HistoryComplete, StatisticsSince: term.StatisticsSince, Payments: term.OperationPayments, CreatedAt: term.CreatedAt, NextNaturalResetAt: domain.CarpoolProjectedNextNaturalResetAt(term.Status, term.ExpiresAt, cycles)}
	for _, cycle := range cycles {
		adminCycle := adminCycleFrom(term.UserID, cycle)
		result.Cycles = append(result.Cycles, adminCycle)
		if cycle.State == domain.CarpoolCycleActive {
			current := adminCycle
			result.CurrentCycle = &current
		}
	}
	for _, payment := range result.Payments {
		if payment.PaymentKind == "refund" {
			result.PaymentNetCNY = result.PaymentNetCNY.Sub(payment.AmountCNY)
		} else {
			result.PaymentNetCNY = result.PaymentNetCNY.Add(payment.AmountCNY)
		}
	}
	return result
}
func adminCycleFrom(userID int64, c domain.CarpoolCycle) domain.CarpoolAdminCycle {
	return domain.CarpoolAdminCycle{ID: c.ID, TermID: c.TermID, UserID: userID, CycleNo: c.CycleNo, StartsAt: c.StartsAt, EndsAt: c.EndsAt, BaseQuotaUSD: c.BaseQuotaUSD, BaseBalanceUSD: c.BaseBalanceUSD, BoostBalanceUSD: c.BoostBalanceUSD, ManualBalanceUSD: c.ManualBalanceUSD, AvailableUSD: c.AvailableUSD(), State: c.State, Revision: c.Revision}
}

func (s *CarpoolService) Terminate(ctx context.Context, termID, actorID int64, input CarpoolTerminateInput, key string) (*domain.CarpoolAdminTerm, error) {
	fp, err := carpoolFingerprint("terminate_term", actorID, termID, input)
	if err != nil {
		return nil, err
	}
	term, err := s.repo.TerminateTerm(ctx, termID, actorID, input.Reason, input.EffectiveAt, domain.CarpoolOperation{Kind: "terminate_term", ActorID: actorID, Key: key, Fingerprint: fp})
	if err != nil {
		return nil, err
	}
	return adminTermFrom(term, term.OperationCycles), nil
}
func (s *CarpoolService) CreatePlanVersion(ctx context.Context, oldID, actorID int64, input CarpoolPlanVersionInput, key string) (*domain.CarpoolPlan, error) {
	input.ListPriceCNY = input.ListPriceCNY.Round(2)
	input.WeeklyQuotaUSD = input.WeeklyQuotaUSD.Round(8)
	input.BoostRatio = input.BoostRatio.Round(8)
	fp, err := carpoolFingerprint("plan_version", actorID, oldID, input)
	if err != nil {
		return nil, err
	}
	return s.repo.CreatePlanVersion(ctx, oldID, domain.CarpoolPlan{Name: input.Name, ListPriceCNY: input.ListPriceCNY, WeeklyQuotaUSD: input.WeeklyQuotaUSD, BoostRatio: input.BoostRatio, BoostCount: input.BoostCount, Enabled: input.Enabled}, domain.CarpoolOperation{Kind: "plan_version", ActorID: actorID, Key: key, Fingerprint: fp})
}
func (s *CarpoolService) RecordPayment(ctx context.Context, termID, actorID int64, input domain.CarpoolPaymentInput, key string) (*domain.CarpoolPayment, error) {
	fp, err := carpoolFingerprint("payment", actorID, termID, input)
	if err != nil {
		return nil, err
	}
	return s.repo.RecordPayment(ctx, termID, actorID, input, domain.CarpoolOperation{Kind: "payment", ActorID: actorID, Key: key, Fingerprint: fp})
}
func (s *CarpoolService) Adjust(ctx context.Context, cycleID, actorID int64, input CarpoolAdjustmentInput, key string) ([]domain.CarpoolLedgerEntry, error) {
	input.DeltaUSD = input.DeltaUSD.Round(8)
	fp, err := carpoolFingerprint("adjustment", actorID, cycleID, input)
	if err != nil {
		return nil, err
	}
	return s.repo.AdjustCycle(ctx, cycleID, actorID, input.Bucket, input.DeltaUSD, input.Reason, input.ReversesLedgerID, domain.CarpoolOperation{Kind: "adjustment", ActorID: actorID, Key: key, Fingerprint: fp})
}
func (s *CarpoolService) ClaimBoost(ctx context.Context, userID int64, key string) (*domain.CarpoolBoostResult, error) {
	fp, err := carpoolFingerprint("boost", userID, userID, struct{}{})
	if err != nil {
		return nil, err
	}
	return s.repo.ClaimBoost(ctx, userID, key, fp)
}
func (s *CarpoolService) BoostStatus(ctx context.Context, userID int64) (*domain.CarpoolBoostStatus, error) {
	return s.repo.GetBoostStatus(ctx, userID)
}
func (s *CarpoolService) Details(ctx context.Context, userID int64) (*domain.CarpoolUserDetails, error) {
	details, err := s.repo.GetUserDetails(ctx, userID)
	if err != nil {
		return nil, err
	}
	if s.resetReader != nil {
		window, readErr := s.resetReader.UserResetWindow(ctx, userID, details.ServerNow)
		if readErr != nil {
			return nil, readErr
		}
		details.ResetWindow = window
	}
	return details, nil
}
func (s *CarpoolService) Plans(ctx context.Context) ([]domain.CarpoolPlan, error) {
	return s.repo.ListPlans(ctx, false)
}
func (s *CarpoolService) Terms(ctx context.Context, f domain.CarpoolTermFilters) ([]domain.CarpoolAdminTerm, int64, error) {
	items, total, err := s.repo.ListAdminTerms(ctx, f)
	if err == nil && items == nil {
		items = make([]domain.CarpoolAdminTerm, 0)
	}
	return items, total, err
}
func (s *CarpoolService) Cycles(ctx context.Context, f domain.CarpoolCycleFilters) ([]domain.CarpoolAdminCycle, int64, error) {
	items, total, err := s.repo.ListAdminCycles(ctx, f)
	if err == nil && items == nil {
		items = make([]domain.CarpoolAdminCycle, 0)
	}
	return items, total, err
}
func (s *CarpoolService) Ledger(ctx context.Context, f domain.CarpoolLedgerFilters) ([]domain.CarpoolLedgerEntry, int64, error) {
	items, total, err := s.repo.ListAdminLedger(ctx, f)
	if err == nil && items == nil {
		items = make([]domain.CarpoolLedgerEntry, 0)
	}
	return items, total, err
}
func (s *CarpoolService) Payments(ctx context.Context, termID int64, page, pageSize int) ([]domain.CarpoolPayment, int64, error) {
	items, total, err := s.repo.ListPayments(ctx, termID, page, pageSize)
	if err == nil && items == nil {
		items = make([]domain.CarpoolPayment, 0)
	}
	return items, total, err
}
func (s *CarpoolService) BillingExceptions(ctx context.Context, filters domain.CarpoolBillingExceptionFilters) ([]domain.CarpoolBillingException, int64, error) {
	return s.repo.ListBillingExceptions(ctx, filters)
}
func (s *CarpoolService) ReconcileBillingException(ctx context.Context, billingRequestID, actorID int64, input CarpoolBillingReconcileInput, key string) (*domain.CarpoolBillingException, error) {
	input.Resolution = strings.TrimSpace(input.Resolution)
	input.Reason = strings.TrimSpace(input.Reason)
	if !input.Confirmed {
		return nil, infraerrors.BadRequest("CARPOOL_RECONCILIATION_CONFIRMATION_REQUIRED", "explicit reconciliation confirmation is required")
	}
	if input.Reason == "" || len([]rune(input.Reason)) > 1000 {
		return nil, infraerrors.BadRequest("CARPOOL_RECONCILIATION_REASON_INVALID", "reconciliation reason is required and must not exceed 1000 characters")
	}
	var cost decimal.Decimal
	switch input.Resolution {
	case domain.CarpoolBillingResolutionNoCost:
		if input.ActualCostUSD != nil && !input.ActualCostUSD.IsZero() {
			return nil, infraerrors.BadRequest("CARPOOL_RECONCILIATION_COST_INVALID", "no_cost cannot include a nonzero actual cost")
		}
	case domain.CarpoolBillingResolutionActualCost:
		if input.ActualCostUSD == nil {
			return nil, infraerrors.BadRequest("CARPOOL_RECONCILIATION_COST_REQUIRED", "actual_cost requires actual_cost_usd")
		}
		cost = input.ActualCostUSD.Round(8)
		if !cost.IsPositive() {
			return nil, infraerrors.BadRequest("CARPOOL_RECONCILIATION_COST_INVALID", "actual_cost_usd must be positive after rounding to 8 decimal places")
		}
	default:
		return nil, infraerrors.BadRequest("CARPOOL_RECONCILIATION_RESOLUTION_INVALID", "resolution must be no_cost or actual_cost")
	}
	normalized := input
	if input.Resolution == domain.CarpoolBillingResolutionActualCost {
		normalized.ActualCostUSD = &cost
	}
	fp, err := carpoolFingerprint("billing_reconcile", actorID, billingRequestID, normalized)
	if err != nil {
		return nil, err
	}
	return s.repo.ReconcileBillingException(ctx, billingRequestID, actorID, input.Resolution, normalized.ActualCostUSD, input.Reason, domain.CarpoolOperation{Kind: "billing_reconcile", ActorID: actorID, Key: key, Fingerprint: fp})
}

func (s *CarpoolService) Admit(ctx context.Context, userID, apiKeyID, groupID int64, requestID string, admittedAt time.Time) (*domain.CarpoolBillingSnapshot, error) {
	if !strings.HasPrefix(requestID, "carpool:") {
		return nil, ErrCarpoolInvalidRelationship
	}
	return s.repo.Admit(ctx, userID, apiKeyID, groupID, requestID, admittedAt)
}
func (s *CarpoolService) PersistKnownUsage(ctx context.Context, snapshot *domain.CarpoolBillingSnapshot, cost decimal.Decimal, payload json.RawMessage) error {
	if snapshot == nil {
		return ErrCarpoolAdmissionRequired
	}
	return s.repo.PersistKnownUsage(ctx, *snapshot, cost, payload)
}
func (s *CarpoolService) RecoverPendingReceipts(ctx context.Context, limit int) ([]domain.CarpoolKnownUsage, error) {
	return s.repo.RecoverPendingReceipts(ctx, limit)
}
func (s *CarpoolService) MarkReconcileRequired(ctx context.Context, snapshot *domain.CarpoolBillingSnapshot, reason string) error {
	if snapshot == nil {
		return ErrCarpoolAdmissionRequired
	}
	return s.repo.MarkReconcileRequired(ctx, *snapshot, reason)
}
