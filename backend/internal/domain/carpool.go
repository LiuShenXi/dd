package domain

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/shopspring/decimal"
)

const (
	CarpoolTimezone            = "Asia/Shanghai"
	CarpoolGlobalScopeID int64 = 1

	CarpoolTermPending    = "pending"
	CarpoolTermActive     = "active"
	CarpoolTermExpired    = "expired"
	CarpoolTermTerminated = "terminated"

	CarpoolCycleScheduled = "scheduled"
	CarpoolCycleActive    = "active"
	CarpoolCycleMissed    = "missed"
	CarpoolCycleClosing   = "closing"
	CarpoolCycleClosed    = "closed"

	CarpoolBucketBase   = "base"
	CarpoolBucketBoost  = "boost"
	CarpoolBucketManual = "manual"

	CarpoolBillingResolutionNoCost     = "no_cost"
	CarpoolBillingResolutionActualCost = "actual_cost"

	CarpoolResetModeFixed   = "fixed"
	CarpoolResetModeRolling = "rolling"
)

type CarpoolPlanSnapshot struct {
	PlanID                int64           `json:"plan_id"`
	Code                  string          `json:"code"`
	Name                  string          `json:"name"`
	Version               int             `json:"version"`
	ListPriceCNY          decimal.Decimal `json:"list_price_cny"`
	WeeklyQuotaUSD        decimal.Decimal `json:"weekly_quota_usd"`
	Cycle5QuotaUSD        decimal.Decimal `json:"cycle_5_quota_usd"`
	DurationDays          int             `json:"duration_days"`
	CycleDays             int             `json:"cycle_days"`
	BoostRatio            decimal.Decimal `json:"boost_ratio"`
	BoostAmountUSD        decimal.Decimal `json:"boost_amount_usd"`
	BoostCount            int             `json:"boost_count"`
	RoundingMode          string          `json:"rounding_mode"`
	Enabled               bool            `json:"enabled"`
	ResetMode             string          `json:"reset_mode"`
	WeeklyQuotaCustomized bool            `json:"weekly_quota_customized,omitempty"`
	DurationCustomized    bool            `json:"duration_customized,omitempty"`
}

// Overrides belong to the member snapshot, never to the shared plan version.
func (s CarpoolPlanSnapshot) WithWeeklyQuotaOverride(quota decimal.Decimal) CarpoolPlanSnapshot {
	s.WeeklyQuotaCustomized = true
	s.WeeklyQuotaUSD = quota.Round(8)
	s.Cycle5QuotaUSD = s.WeeklyQuotaUSD.Mul(decimal.NewFromInt(2)).Div(decimal.NewFromInt(7)).Round(0)
	s.BoostAmountUSD = s.WeeklyQuotaUSD.Mul(s.BoostRatio).Round(8)
	return s
}

func (s CarpoolPlanSnapshot) WithDurationOverride(durationDays int) CarpoolPlanSnapshot {
	s.DurationCustomized = true
	s.DurationDays = durationDays
	s.ResetMode = resetModeForNewSnapshot(durationDays)
	return s
}

func (s CarpoolPlanSnapshot) WithRenewalOverrides(previous CarpoolPlanSnapshot) CarpoolPlanSnapshot {
	if previous.WeeklyQuotaCustomized {
		s = s.WithWeeklyQuotaOverride(previous.WeeklyQuotaUSD)
	}
	if previous.DurationCustomized {
		s = s.WithDurationOverride(previous.DurationDays)
	}
	return s
}

func (s CarpoolPlanSnapshot) EffectiveResetMode() string {
	if s.ResetMode == CarpoolResetModeRolling {
		return CarpoolResetModeRolling
	}
	return CarpoolResetModeFixed
}

func (s *CarpoolPlanSnapshot) NormalizeResetMode() {
	if s != nil {
		s.ResetMode = s.EffectiveResetMode()
	}
}

type CarpoolPlan struct {
	ID             int64
	Code           string
	Name           string
	ListPriceCNY   decimal.Decimal
	WeeklyQuotaUSD decimal.Decimal
	DurationDays   int
	CycleDays      int
	BoostRatio     decimal.Decimal
	BoostCount     int
	Enabled        bool
	Version        int
}

type CarpoolAdminPlan struct {
	CarpoolPlanSnapshot
	IsLatest bool `json:"is_latest"`
}

func (p CarpoolPlan) Snapshot() CarpoolPlanSnapshot {
	return CarpoolPlanSnapshot{
		PlanID:         p.ID,
		Code:           p.Code,
		Name:           p.Name,
		Version:        p.Version,
		ListPriceCNY:   p.ListPriceCNY,
		WeeklyQuotaUSD: p.WeeklyQuotaUSD,
		Cycle5QuotaUSD: p.WeeklyQuotaUSD.Mul(decimal.NewFromInt(2)).Div(decimal.NewFromInt(7)).Round(0),
		DurationDays:   p.DurationDays,
		CycleDays:      p.CycleDays,
		BoostRatio:     p.BoostRatio,
		BoostAmountUSD: p.WeeklyQuotaUSD.Mul(p.BoostRatio).Round(8),
		BoostCount:     p.BoostCount,
		RoundingMode:   "half_up",
		Enabled:        p.Enabled,
		ResetMode:      resetModeForNewSnapshot(p.DurationDays),
	}
}

func resetModeForNewSnapshot(durationDays int) string {
	if durationDays == 28 {
		return CarpoolResetModeRolling
	}
	return CarpoolResetModeFixed
}

func (p CarpoolPlan) AdminProjection() CarpoolAdminPlan {
	return CarpoolAdminPlan{CarpoolPlanSnapshot: p.Snapshot(), IsLatest: true}
}

type CarpoolCycleSpec struct {
	CycleNo      int             `json:"cycle_no"`
	StartsAt     time.Time       `json:"starts_at"`
	EndsAt       time.Time       `json:"ends_at"`
	BaseQuotaUSD decimal.Decimal `json:"base_quota_usd"`
}

func BuildCarpoolCycles(startsAt time.Time, snapshot CarpoolPlanSnapshot) []CarpoolCycleSpec {
	if snapshot.DurationDays <= 0 || snapshot.CycleDays <= 0 {
		return nil
	}
	cycleCount := (snapshot.DurationDays + snapshot.CycleDays - 1) / snapshot.CycleDays
	cycles := make([]CarpoolCycleSpec, 0, cycleCount)
	expiresAt := startsAt.Add(time.Duration(snapshot.DurationDays) * 24 * time.Hour)
	for cycleNo := 1; cycleNo <= cycleCount; cycleNo++ {
		cycleStart := startsAt.Add(time.Duration((cycleNo-1)*snapshot.CycleDays) * 24 * time.Hour)
		cycleEnd := cycleStart.Add(time.Duration(snapshot.CycleDays) * 24 * time.Hour)
		if cycleEnd.After(expiresAt) {
			cycleEnd = expiresAt
		}
		quota := snapshot.WeeklyQuotaUSD
		cycleDurationDays := int(cycleEnd.Sub(cycleStart) / (24 * time.Hour))
		if cycleDurationDays < snapshot.CycleDays {
			quota = snapshot.WeeklyQuotaUSD.Mul(decimal.NewFromInt(int64(cycleDurationDays))).Div(decimal.NewFromInt(int64(snapshot.CycleDays))).Round(0)
			if cycleNo == 5 && !snapshot.Cycle5QuotaUSD.IsZero() {
				quota = snapshot.Cycle5QuotaUSD
			}
		}
		cycles = append(cycles, CarpoolCycleSpec{CycleNo: cycleNo, StartsAt: cycleStart, EndsAt: cycleEnd, BaseQuotaUSD: quota.Round(8)})
	}
	return cycles
}

func BuildCarpoolOpeningCycles(startsAt, at time.Time, snapshot CarpoolPlanSnapshot, nextNaturalResetAt *time.Time) []CarpoolCycleSpec {
	if snapshot.EffectiveResetMode() != CarpoolResetModeRolling {
		return BuildCarpoolCycles(startsAt, snapshot)
	}
	period := time.Duration(snapshot.CycleDays) * 24 * time.Hour
	expiresAt := startsAt.Add(time.Duration(snapshot.DurationDays) * 24 * time.Hour)
	if period <= 0 || !startsAt.Before(expiresAt) {
		return nil
	}
	endsAt := startsAt.Add(period)
	if endsAt.After(expiresAt) {
		endsAt = expiresAt
	}
	specs := []CarpoolCycleSpec{{CycleNo: 1, StartsAt: startsAt, EndsAt: endsAt, BaseQuotaUSD: snapshot.WeeklyQuotaUSD.Round(8)}}
	if at.Before(startsAt) || !at.Before(expiresAt) {
		return specs
	}
	for !at.Before(specs[len(specs)-1].EndsAt) && specs[len(specs)-1].EndsAt.Before(expiresAt) {
		start := specs[len(specs)-1].EndsAt
		end := start.Add(period)
		if end.After(expiresAt) {
			end = expiresAt
		}
		specs = append(specs, CarpoolCycleSpec{CycleNo: len(specs) + 1, StartsAt: start, EndsAt: end, BaseQuotaUSD: snapshot.WeeklyQuotaUSD.Round(8)})
	}
	if nextNaturalResetAt != nil {
		deadline := nextNaturalResetAt.UTC()
		if deadline.After(expiresAt) {
			deadline = expiresAt
		}
		specs[len(specs)-1].EndsAt = deadline
	}
	return specs
}

// A takeover anchor records a known current period without inventing earlier
// cycles or changing the membership's registration-based expiry.
func BuildCarpoolTakeoverCycles(startsAt, at time.Time, snapshot CarpoolPlanSnapshot, takeover *CarpoolTakeoverInput) ([]CarpoolCycleSpec, error) {
	var deadline *time.Time
	if takeover != nil {
		deadline = takeover.NextNaturalResetAt
	}
	period := time.Duration(snapshot.CycleDays) * 24 * time.Hour
	expiresAt := startsAt.Add(time.Duration(snapshot.DurationDays) * 24 * time.Hour)
	if takeover == nil || takeover.CurrentCycleStartsAt == nil {
		if deadline != nil && (snapshot.EffectiveResetMode() != CarpoolResetModeRolling || at.Before(startsAt) || !at.Before(expiresAt) ||
			!deadline.After(at) || deadline.After(at.Add(period))) {
			return nil, fmt.Errorf("invalid takeover next natural reset deadline")
		}
		return BuildCarpoolOpeningCycles(startsAt, at, snapshot, deadline), nil
	}
	anchor := takeover.CurrentCycleStartsAt.UTC()
	end := anchor.Add(period)
	if end.After(expiresAt) {
		end = expiresAt
	}
	if period <= 0 || anchor.Before(startsAt) || anchor.After(at) || !at.Before(end) {
		return nil, fmt.Errorf("takeover current cycle must cover the takeover instant within the term")
	}
	if deadline != nil {
		requested := deadline.UTC()
		if requested.After(expiresAt) {
			requested = expiresAt
		}
		if !requested.Equal(end) {
			return nil, fmt.Errorf("takeover deadline must match the anchored current cycle end")
		}
	}
	specs := []CarpoolCycleSpec{{CycleNo: 1, StartsAt: anchor, EndsAt: end, BaseQuotaUSD: snapshot.WeeklyQuotaUSD.Round(8)}}
	if snapshot.EffectiveResetMode() == CarpoolResetModeFixed {
		for end.Before(expiresAt) {
			start := end
			end = start.Add(period)
			if end.After(expiresAt) {
				end = expiresAt
			}
			specs = append(specs, CarpoolCycleSpec{CycleNo: len(specs) + 1, StartsAt: start, EndsAt: end, BaseQuotaUSD: snapshot.WeeklyQuotaUSD.Round(8)})
		}
	}
	return specs, nil
}

type CarpoolTerm struct {
	ID                int64
	UserID            int64
	ScopeID           int64
	GroupID           int64
	PlanID            int64
	PlanSnapshot      CarpoolPlanSnapshot
	StartsAt          time.Time
	ExpiresAt         time.Time
	Status            string
	BoostUsed         int
	HistoryComplete   bool
	StatisticsSince   *time.Time
	CreatedBy         int64
	Notes             *string
	CreatedAt         time.Time
	OperationCycles   []CarpoolCycle   `json:"-"`
	OperationPayments []CarpoolPayment `json:"-"`
}

type CarpoolCycle struct {
	ID               int64
	TermID           int64
	CycleNo          int
	StartsAt         time.Time
	EndsAt           time.Time
	BaseQuotaUSD     decimal.Decimal
	BaseBalanceUSD   decimal.Decimal
	BoostBalanceUSD  decimal.Decimal
	ManualBalanceUSD decimal.Decimal
	State            string
	Revision         int64
}

func (c CarpoolCycle) AvailableUSD() decimal.Decimal {
	total := c.BaseBalanceUSD.Add(c.BoostBalanceUSD).Add(c.ManualBalanceUSD)
	if total.IsNegative() {
		return decimal.Zero
	}
	return total.Round(8)
}

type CarpoolBillingSnapshot struct {
	BillingRequestID int64     `json:"billing_request_id"`
	RequestID        string    `json:"request_id"`
	UserID           int64     `json:"user_id"`
	APIKeyID         int64     `json:"api_key_id"`
	GroupID          int64     `json:"group_id"`
	TermID           int64     `json:"term_id"`
	CycleID          int64     `json:"cycle_id"`
	AdmittedAt       time.Time `json:"admitted_at"`
}

type CarpoolKnownUsage struct {
	Snapshot       CarpoolBillingSnapshot
	ActualCostUSD  decimal.Decimal
	BillingPayload json.RawMessage
	RetryCount     int
}

type CarpoolUsageDebit struct {
	Snapshot      CarpoolBillingSnapshot
	ActualCostUSD decimal.Decimal
	EventKey      string
	ActorID       *int64
}

type CarpoolLedgerEntry struct {
	ID               int64           `json:"id"`
	UserID           int64           `json:"user_id"`
	UserEmail        string          `json:"user_email,omitempty"`
	TermID           int64           `json:"term_id"`
	CycleID          int64           `json:"cycle_id"`
	EventType        string          `json:"event_type"`
	Bucket           string          `json:"bucket"`
	DeltaUSD         decimal.Decimal `json:"delta_usd"`
	EventKey         string          `json:"event_key"`
	RequestID        *string         `json:"request_id"`
	APIKeyID         *int64          `json:"api_key_id"`
	ResetBatchID     *int64          `json:"reset_batch_id"`
	BoostSlot        *int            `json:"boost_slot"`
	ActorID          *int64          `json:"actor_id"`
	ReversesLedgerID *int64          `json:"reverses_ledger_id"`
	Reason           *string         `json:"reason"`
	EffectiveAt      time.Time       `json:"effective_at"`
	RecordedAt       time.Time       `json:"recorded_at"`
}

type CarpoolTakeoverInput struct {
	CurrentBaseBalanceUSD      decimal.Decimal  `json:"current_base_balance_usd"`
	CurrentBoostBalanceUSD     decimal.Decimal  `json:"current_boost_balance_usd"`
	CurrentManualBalanceUSD    decimal.Decimal  `json:"current_manual_balance_usd"`
	OrdinaryBalanceTransferUSD decimal.Decimal  `json:"ordinary_balance_transfer_usd"`
	HistoricalUsedUSD          *decimal.Decimal `json:"historical_used_usd"`
	StatisticsSince            *time.Time       `json:"statistics_since"`
	BoostUsed                  int              `json:"boost_used"`
	HistoryComplete            bool             `json:"history_complete"`
	NextNaturalResetAt         *time.Time       `json:"next_natural_reset_at"`
	CurrentCycleStartsAt       *time.Time       `json:"current_cycle_starts_at,omitempty"`
}

func (t CarpoolTakeoverInput) Quantized() CarpoolTakeoverInput {
	t.CurrentBaseBalanceUSD = t.CurrentBaseBalanceUSD.Round(8)
	t.CurrentBoostBalanceUSD = t.CurrentBoostBalanceUSD.Round(8)
	t.CurrentManualBalanceUSD = t.CurrentManualBalanceUSD.Round(8)
	t.OrdinaryBalanceTransferUSD = t.OrdinaryBalanceTransferUSD.Round(8)
	if t.CurrentCycleStartsAt != nil {
		value := t.CurrentCycleStartsAt.UTC()
		t.CurrentCycleStartsAt = &value
	}
	if t.HistoricalUsedUSD != nil {
		value := t.HistoricalUsedUSD.Round(8)
		t.HistoricalUsedUSD = &value
	}
	return t
}

type CreateCarpoolTermParams struct {
	UserID, ScopeID, GroupID, PlanID, ActorID int64
	RenewFromTermID                           int64
	StartsAt                                  *time.Time
	Mode                                      string
	Takeover                                  *CarpoolTakeoverInput
	Notes                                     *string
	Payment                                   *CarpoolPaymentInput
	Operation                                 CarpoolOperation
}

type CarpoolOperation struct {
	Kind        string
	ActorID     int64
	Key         string
	Fingerprint string
}

type CarpoolPaymentInput struct {
	AmountCNY       decimal.Decimal `json:"amount_cny"`
	PaymentKind     string          `json:"payment_kind"`
	PaidAt          time.Time       `json:"paid_at"`
	Channel         string          `json:"channel"`
	ExternalOrderNo *string         `json:"external_order_no"`
	Notes           *string         `json:"notes"`
}

type CarpoolPayment struct {
	ID              int64           `json:"id"`
	TermID          int64           `json:"term_id"`
	AmountCNY       decimal.Decimal `json:"amount_cny"`
	PaymentKind     string          `json:"payment_kind"`
	PaidAt          time.Time       `json:"paid_at"`
	Channel         string          `json:"channel"`
	ExternalOrderNo *string         `json:"external_order_no,omitempty"`
	RecordedBy      int64           `json:"recorded_by"`
	Notes           *string         `json:"notes,omitempty"`
	RecordedAt      time.Time       `json:"recorded_at"`
}

type CarpoolBoostStatus struct {
	Eligible          bool            `json:"eligible"`
	Remaining         int             `json:"remaining"`
	Total             int             `json:"total"`
	AmountUSD         decimal.Decimal `json:"amount_usd"`
	HelpText          string          `json:"help_text"`
	UnavailableReason *string         `json:"unavailable_reason"`
}

type CarpoolBoostResult = CarpoolBoostStatus

type CarpoolUserUsage struct {
	TotalUsedUSD    decimal.Decimal `json:"total_used_usd"`
	HistoryComplete bool            `json:"history_complete"`
	StatisticsSince *time.Time      `json:"statistics_since"`
}

type CarpoolUserCycle struct {
	CycleNo  int       `json:"cycle_no"`
	StartsAt time.Time `json:"starts_at"`
	EndsAt   time.Time `json:"ends_at"`
	Status   string    `json:"status"`
}

type CarpoolUserTerm struct {
	Status             string                  `json:"status"`
	StartsAt           time.Time               `json:"starts_at"`
	ExpiresAt          time.Time               `json:"expires_at"`
	ResetCount         int                     `json:"reset_count"`
	ResetCountBasis    string                  `json:"reset_count_basis"`
	ResetEvents        []CarpoolUserResetEvent `json:"reset_events"`
	CurrentCycleNo     *int                    `json:"current_cycle_no"`
	Cycles             []CarpoolUserCycle      `json:"cycles"`
	ResetMode          string                  `json:"reset_mode"`
	NextNaturalResetAt *time.Time              `json:"next_natural_reset_at"`
}

type CarpoolUserResetEvent struct {
	CycleNo        int             `json:"cycle_no"`
	OccurredAt     time.Time       `json:"occurred_at"`
	TargetQuotaUSD decimal.Decimal `json:"target_quota_usd"`
}

type CarpoolUserQuota struct {
	AvailableUSD     decimal.Decimal  `json:"available_usd"`
	RemainingPercent *decimal.Decimal `json:"remaining_percent"`
}

type CarpoolUserResetWindow struct {
	Status           string     `json:"status"`
	ScheduledAt      *time.Time `json:"scheduled_at"`
	ScheduleRevision int64      `json:"schedule_revision"`
	EligibleForMe    bool       `json:"eligible_for_me"`
	IneligibleReason *string    `json:"ineligible_reason"`
}

type CarpoolUserDetails struct {
	BillingMode string                 `json:"billing_mode"`
	ServerNow   time.Time              `json:"server_now"`
	Timezone    string                 `json:"timezone"`
	Usage       CarpoolUserUsage       `json:"usage"`
	Quota       *CarpoolUserQuota      `json:"quota"`
	Term        *CarpoolUserTerm       `json:"term"`
	ResetWindow CarpoolUserResetWindow `json:"reset_window"`
}

type CarpoolAdminTerm struct {
	ID                 int64               `json:"id"`
	UserID             int64               `json:"user_id"`
	UserEmail          string              `json:"user_email,omitempty"`
	ScopeID            int64               `json:"scope_id"`
	GroupID            int64               `json:"group_id"`
	PlanID             int64               `json:"plan_id"`
	PlanSnapshot       CarpoolPlanSnapshot `json:"plan_snapshot"`
	StartsAt           time.Time           `json:"starts_at"`
	ExpiresAt          time.Time           `json:"expires_at"`
	Status             string              `json:"status"`
	BoostUsed          int                 `json:"boost_used"`
	BoostRemaining     int                 `json:"boost_remaining"`
	HistoryComplete    bool                `json:"history_complete"`
	StatisticsSince    *time.Time          `json:"statistics_since"`
	PaymentNetCNY      decimal.Decimal     `json:"payment_net_cny"`
	CurrentCycle       *CarpoolAdminCycle  `json:"current_cycle"`
	Cycles             []CarpoolAdminCycle `json:"cycles,omitempty"`
	Payments           []CarpoolPayment    `json:"payments,omitempty"`
	CreatedAt          time.Time           `json:"created_at"`
	NextNaturalResetAt *time.Time          `json:"next_natural_reset_at"`
}

type CarpoolAdminCycle struct {
	ID                int64           `json:"id"`
	TermID            int64           `json:"term_id"`
	UserID            int64           `json:"user_id"`
	UserEmail         string          `json:"user_email,omitempty"`
	CycleNo           int             `json:"cycle_no"`
	StartsAt          time.Time       `json:"starts_at"`
	EndsAt            time.Time       `json:"ends_at"`
	BaseQuotaUSD      decimal.Decimal `json:"base_quota_usd"`
	BaseBalanceUSD    decimal.Decimal `json:"base_balance_usd"`
	BoostBalanceUSD   decimal.Decimal `json:"boost_balance_usd"`
	ManualBalanceUSD  decimal.Decimal `json:"manual_balance_usd"`
	AvailableUSD      decimal.Decimal `json:"available_usd"`
	InitialGrantedUSD decimal.Decimal `json:"initial_granted_usd"`
	ResetGrantedUSD   decimal.Decimal `json:"reset_granted_usd"`
	BoostGrantedUSD   decimal.Decimal `json:"boost_granted_usd"`
	AdjustmentNetUSD  decimal.Decimal `json:"adjustment_net_usd"`
	UsedUSD           decimal.Decimal `json:"used_usd"`
	ExpiredUSD        decimal.Decimal `json:"expired_usd"`
	State             string          `json:"state"`
	Revision          int64           `json:"revision"`
}

type CarpoolTermFilters struct {
	Page, PageSize       int
	UserID, PlanID       *int64
	Status               string
	StartsFrom, StartsTo *time.Time
}

type CarpoolCycleFilters struct {
	Page, PageSize       int
	UserID, TermID       *int64
	CycleNo              *int
	State                string
	StartsFrom, StartsTo *time.Time
}

type CarpoolLedgerFilters struct {
	Page, PageSize          int
	UserID, TermID, CycleID *int64
	EventType, Bucket       string
}

type CarpoolBillingException struct {
	ID                          int64            `json:"id"`
	RequestID                   string           `json:"request_id"`
	APIKeyID                    int64            `json:"api_key_id"`
	UserID                      int64            `json:"user_id"`
	UserEmail                   string           `json:"user_email,omitempty"`
	GroupID                     int64            `json:"group_id"`
	TermID                      int64            `json:"term_id"`
	CycleID                     int64            `json:"cycle_id"`
	AdmittedAt                  time.Time        `json:"admitted_at"`
	Status                      string           `json:"status"`
	KnownCostUSD                *decimal.Decimal `json:"known_cost_usd"`
	RetryCount                  int              `json:"retry_count"`
	SanitizedError              *string          `json:"sanitized_error"`
	UpdatedAt                   time.Time        `json:"updated_at"`
	Resolution                  *string          `json:"resolution"`
	ResolvedAt                  *time.Time       `json:"resolved_at"`
	ResolvedBy                  *int64           `json:"resolved_by"`
	Reason                      *string          `json:"reason"`
	ActualCostUSD               *decimal.Decimal `json:"actual_cost_usd"`
	AuxiliaryUsageReconstructed bool             `json:"auxiliary_usage_reconstructed"`
}

type CarpoolBillingExceptionFilters struct {
	Page, PageSize          int
	UserID, TermID, CycleID *int64
	Status                  string
}

type CarpoolPreviewCycle struct {
	CycleNo       int             `json:"cycle_no"`
	StartsAt      time.Time       `json:"starts_at"`
	EndsAt        time.Time       `json:"ends_at"`
	BaseQuotaUSD  decimal.Decimal `json:"base_quota_usd"`
	InitialAction string          `json:"initial_action"`
}

type CarpoolTermPreview struct {
	CalculatedAt                time.Time             `json:"calculated_at"`
	Mode                        string                `json:"mode"`
	Plan                        CarpoolPlanSnapshot   `json:"plan"`
	StartsAt                    time.Time             `json:"starts_at"`
	ExpiresAt                   time.Time             `json:"expires_at"`
	Cycles                      []CarpoolPreviewCycle `json:"cycles"`
	Warnings                    []string              `json:"warnings"`
	OrdinaryBalanceDeductionUSD decimal.Decimal       `json:"ordinary_balance_deduction_usd"`
	NextNaturalResetAt          *time.Time            `json:"next_natural_reset_at"`
}

func CarpoolNextNaturalResetAt(status string, expiresAt, at time.Time, cycles []CarpoolCycle) *time.Time {
	if status == CarpoolTermExpired || status == CarpoolTermTerminated || !at.Before(expiresAt) {
		return nil
	}
	for _, cycle := range cycles {
		if cycle.EndsAt.Before(expiresAt) && (at.Before(cycle.StartsAt) || (at.Before(cycle.EndsAt) && !at.Before(cycle.StartsAt))) {
			deadline := cycle.EndsAt
			return &deadline
		}
	}
	return nil
}

func CarpoolProjectedNextNaturalResetAt(status string, expiresAt time.Time, cycles []CarpoolCycle) *time.Time {
	if status == CarpoolTermExpired || status == CarpoolTermTerminated {
		return nil
	}
	for _, cycle := range cycles {
		if (cycle.State == CarpoolCycleActive || cycle.State == CarpoolCycleScheduled) && cycle.EndsAt.Before(expiresAt) {
			deadline := cycle.EndsAt
			return &deadline
		}
	}
	return nil
}
