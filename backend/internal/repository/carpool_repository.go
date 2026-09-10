package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/shopspring/decimal"
)

type CarpoolRepository struct{ db *sql.DB }

var errCarpoolCycleBoundaryMoved = errors.New("carpool cycle boundary moved while waiting for locks")

const carpoolScopeMembershipLockNamespace int32 = 1129467980 // "CRPL"

type createTermOperationResponse struct {
	Term     domain.CarpoolTerm      `json:"term"`
	Cycles   []domain.CarpoolCycle   `json:"cycles"`
	Payments []domain.CarpoolPayment `json:"payments"`
}

func NewCarpoolRepository(db *sql.DB) *CarpoolRepository { return &CarpoolRepository{db: db} }

func (r *CarpoolRepository) DatabaseNow(ctx context.Context) (time.Time, error) {
	var now time.Time
	err := r.db.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now)
	return now, err
}

func (r *CarpoolRepository) GetPlan(ctx context.Context, planID int64) (*domain.CarpoolPlan, error) {
	row := r.db.QueryRowContext(ctx, `SELECT id, code, name, list_price_cny::text, weekly_quota_usd::text, duration_days, cycle_days, boost_ratio::text, boost_count, enabled, version FROM carpool_plans WHERE id=$1`, planID)
	return scanCarpoolPlan(row)
}

func (r *CarpoolRepository) GetPreviewPlan(ctx context.Context, userID, planID int64) (*domain.CarpoolPlan, error) {
	var userExists bool
	if err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND deleted_at IS NULL)`, userID).Scan(&userExists); err != nil {
		return nil, err
	}
	if !userExists {
		return nil, service.ErrCarpoolNotFound
	}
	plan, err := r.GetPlan(ctx, planID)
	if err != nil {
		return nil, err
	}
	var latestID int64
	if err = r.db.QueryRowContext(ctx, `SELECT id FROM carpool_plans WHERE code=$1 ORDER BY version DESC,id DESC LIMIT 1`, plan.Code).Scan(&latestID); err != nil {
		return nil, err
	}
	if latestID != plan.ID || !plan.Enabled {
		return nil, service.ErrCarpoolUnavailable
	}
	return plan, nil
}

func (r *CarpoolRepository) ListPlans(ctx context.Context, enabledOnly bool) ([]domain.CarpoolPlan, error) {
	query := `SELECT id,code,name,list_price_cny::text,weekly_quota_usd::text,duration_days,cycle_days,boost_ratio::text,boost_count,enabled,version FROM (SELECT DISTINCT ON (code) id,code,name,list_price_cny,weekly_quota_usd,duration_days,cycle_days,boost_ratio,boost_count,enabled,version FROM carpool_plans ORDER BY code,version DESC,id DESC) latest`
	if enabledOnly {
		query += ` WHERE enabled=TRUE`
	}
	query += ` ORDER BY weekly_quota_usd ASC,id ASC`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var plans []domain.CarpoolPlan
	for rows.Next() {
		plan, err := scanCarpoolPlan(rows)
		if err != nil {
			return nil, err
		}
		plans = append(plans, *plan)
	}
	return plans, rows.Err()
}

type carpoolRowScanner interface{ Scan(...any) error }

func scanCarpoolPlan(row carpoolRowScanner) (*domain.CarpoolPlan, error) {
	var plan domain.CarpoolPlan
	var price, weekly, ratio string
	if err := row.Scan(&plan.ID, &plan.Code, &plan.Name, &price, &weekly, &plan.DurationDays, &plan.CycleDays, &ratio, &plan.BoostCount, &plan.Enabled, &plan.Version); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, service.ErrCarpoolNotFound
		}
		return nil, err
	}
	var err error
	if plan.ListPriceCNY, err = decimal.NewFromString(price); err != nil {
		return nil, err
	}
	if plan.WeeklyQuotaUSD, err = decimal.NewFromString(weekly); err != nil {
		return nil, err
	}
	if plan.BoostRatio, err = decimal.NewFromString(ratio); err != nil {
		return nil, err
	}
	return &plan, nil
}

func (r *CarpoolRepository) CreateTerm(ctx context.Context, params domain.CreateCarpoolTermParams) (_ *domain.CarpoolTerm, _ []domain.CarpoolCycle, err error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = tx.Rollback() }()

	if params.ScopeID == 0 {
		params.ScopeID = domain.CarpoolGlobalScopeID
	}
	if params.ScopeID != domain.CarpoolGlobalScopeID {
		return nil, nil, service.ErrCarpoolInvalidRelationship
	}
	if params.Mode == "" {
		params.Mode = "new"
	}
	if params.Mode != "new" && params.Mode != "takeover" {
		return nil, nil, fmt.Errorf("invalid carpool opening mode")
	}
	if params.Mode == "takeover" && params.Takeover == nil {
		return nil, nil, fmt.Errorf("takeover details are required")
	}
	if params.Mode == "new" && params.Takeover != nil {
		return nil, nil, fmt.Errorf("takeover details are not valid for new opening")
	}
	if params.Takeover != nil {
		quantized := params.Takeover.Quantized()
		params.Takeover = &quantized
		if params.Takeover.NextNaturalResetAt != nil {
			value := params.Takeover.NextNaturalResetAt.UTC()
			params.Takeover.NextNaturalResetAt = &value
		}
		if params.Takeover.CurrentBoostBalanceUSD.IsNegative() || params.Takeover.CurrentManualBalanceUSD.IsNegative() {
			return nil, nil, fmt.Errorf("takeover boost and manual balances cannot be negative")
		}
		if !params.Takeover.OrdinaryBalanceTransferUSD.IsZero() {
			return nil, nil, fmt.Errorf("ordinary balance is read-only during carpool takeover")
		}
	}
	if err = validateOperation(params.Operation, "open_term", "renew_term"); err != nil {
		return nil, nil, err
	}
	if params.ActorID != params.Operation.ActorID {
		return nil, nil, fmt.Errorf("carpool operation actor mismatch")
	}
	if params.Operation.Kind == "renew_term" && params.RenewFromTermID <= 0 {
		return nil, nil, fmt.Errorf("renewal source term is required")
	}
	if params.Operation.Kind == "open_term" && params.RenewFromTermID != 0 {
		return nil, nil, fmt.Errorf("renewal source term is not valid for opening")
	}
	if err = lockOperationTx(ctx, tx, params.Operation); err != nil {
		return nil, nil, err
	}
	if _, response, replayed, lookupErr := lookupOperationTx(ctx, tx, params.Operation); lookupErr != nil {
		return nil, nil, lookupErr
	} else if replayed {
		var cached createTermOperationResponse
		if loadErr := replayOperationResponse(response, &cached); loadErr != nil {
			return nil, nil, loadErr
		}
		cached.Term.OperationCycles = cached.Cycles
		cached.Term.OperationPayments = cached.Payments
		return &cached.Term, cached.Cycles, nil
	}
	if err = lockCarpoolScopeMembershipTx(ctx, tx, params.ScopeID); err != nil {
		return nil, nil, err
	}
	var renewalSource *domain.CarpoolTerm
	if params.Operation.Kind == "renew_term" {
		renewalSource, err = scanCarpoolTerm(tx.QueryRowContext(ctx, termSelect+` WHERE id=$1 FOR UPDATE`, params.RenewFromTermID))
		if err != nil {
			return nil, nil, err
		}
		if renewalSource.ScopeID != params.ScopeID {
			return nil, nil, service.ErrCarpoolInvalidRelationship
		}
		params.UserID = renewalSource.UserID
		params.GroupID = renewalSource.GroupID
		params.StartsAt = &renewalSource.ExpiresAt
	}

	var now time.Time
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return nil, nil, err
	}
	startsAt := now
	if params.StartsAt != nil {
		startsAt = params.StartsAt.UTC()
	}

	var plan *domain.CarpoolPlan
	inheritRenewalOverrides := renewalSource != nil && params.PlanID == 0
	if inheritRenewalOverrides {
		plan, err = scanCarpoolPlan(tx.QueryRowContext(ctx, `SELECT id,code,name,list_price_cny::text,weekly_quota_usd::text,duration_days,cycle_days,boost_ratio::text,boost_count,enabled,version FROM carpool_plans WHERE code=$1 ORDER BY version DESC,id DESC LIMIT 1 FOR SHARE`, renewalSource.PlanSnapshot.Code))
	} else {
		plan, err = getPlanTx(ctx, tx, params.PlanID)
	}
	if err != nil {
		return nil, nil, err
	}
	if err = validateLatestEnabledPlanTx(ctx, tx, plan); err != nil {
		return nil, nil, service.ErrCarpoolUnavailable
	}
	params.PlanID = plan.ID
	if err = prepareCarpoolUserGroupTx(ctx, tx, params); err != nil {
		return nil, nil, err
	}
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, fmt.Sprintf("carpool:%d:%d", params.UserID, params.ScopeID)); err != nil {
		return nil, nil, err
	}

	snapshot := plan.Snapshot()
	if inheritRenewalOverrides {
		snapshot = snapshot.WithRenewalOverrides(renewalSource.PlanSnapshot)
	}
	expiresAt := startsAt.Add(time.Duration(snapshot.DurationDays) * 24 * time.Hour)
	var overlapID int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM carpool_terms WHERE user_id=$1 AND scope_id=$2 AND status <> 'terminated' AND starts_at < $4 AND expires_at > $3 LIMIT 1 FOR UPDATE`, params.UserID, params.ScopeID, startsAt, expiresAt).Scan(&overlapID)
	if err == nil {
		return nil, nil, service.ErrCarpoolOverlap
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, nil, err
	}

	status := domain.CarpoolTermPending
	if !now.Before(startsAt) && now.Before(expiresAt) {
		status = domain.CarpoolTermActive
	}
	if !now.Before(expiresAt) {
		status = domain.CarpoolTermExpired
	}
	specs, cycleErr := domain.BuildCarpoolTakeoverCycles(startsAt, now, snapshot, params.Takeover)
	if cycleErr != nil {
		return nil, nil, service.ErrCarpoolInvalidNaturalReset
	}
	historyComplete := params.Mode != "takeover"
	boostUsed := 0
	if params.Takeover != nil {
		historyComplete, boostUsed = params.Takeover.HistoryComplete, params.Takeover.BoostUsed
		if historyComplete && (params.Takeover.HistoricalUsedUSD == nil || params.Takeover.StatisticsSince == nil) {
			return nil, nil, fmt.Errorf("complete takeover history requires historical_used_usd and statistics_since")
		}
		if params.Takeover.HistoricalUsedUSD != nil && params.Takeover.HistoricalUsedUSD.IsNegative() {
			return nil, nil, fmt.Errorf("historical_used_usd cannot be negative")
		}
	}
	if boostUsed < 0 || boostUsed > snapshot.BoostCount {
		return nil, nil, fmt.Errorf("invalid boost_used")
	}
	snapshotJSON, err := json.Marshal(snapshot)
	if err != nil {
		return nil, nil, err
	}

	var statisticsSince *time.Time
	if params.Takeover != nil {
		statisticsSince = params.Takeover.StatisticsSince
		if statisticsSince == nil {
			// Unknown pre-takeover history starts at the database-authoritative
			// takeover instant; it must never be presented as zero all-time usage.
			statisticsSince = &now
		}
	}
	term := &domain.CarpoolTerm{UserID: params.UserID, ScopeID: params.ScopeID, GroupID: params.GroupID, PlanID: params.PlanID, PlanSnapshot: snapshot, StartsAt: startsAt, ExpiresAt: expiresAt, Status: status, BoostUsed: boostUsed, HistoryComplete: historyComplete, StatisticsSince: statisticsSince, CreatedBy: params.ActorID, Notes: params.Notes}
	err = tx.QueryRowContext(ctx, `INSERT INTO carpool_terms(user_id,scope_id,group_id,plan_id,plan_snapshot,starts_at,expires_at,status,boost_used,source_mode,history_complete,statistics_since,created_by,notes) VALUES($1,$2,$3,$4,$5::jsonb,$6,$7,$8,$9,$10,$11,$12,$13,$14) RETURNING id,created_at`, params.UserID, params.ScopeID, params.GroupID, params.PlanID, string(snapshotJSON), startsAt, expiresAt, status, boostUsed, params.Mode, historyComplete, statisticsSince, params.ActorID, params.Notes).Scan(&term.ID, &term.CreatedAt)
	if err != nil {
		return nil, nil, err
	}

	cycles := make([]domain.CarpoolCycle, 0, len(specs))
	for _, spec := range specs {
		state := domain.CarpoolCycleScheduled
		base, boost, manual := decimal.Zero, decimal.Zero, decimal.Zero
		if !now.Before(spec.EndsAt) {
			state = domain.CarpoolCycleMissed
		}
		if !now.Before(spec.StartsAt) && now.Before(spec.EndsAt) {
			state = domain.CarpoolCycleActive
			if params.Mode == "takeover" && params.Takeover != nil {
				base, boost, manual = params.Takeover.CurrentBaseBalanceUSD, params.Takeover.CurrentBoostBalanceUSD, params.Takeover.CurrentManualBalanceUSD
			} else {
				base = spec.BaseQuotaUSD
			}
		}
		cycle := domain.CarpoolCycle{TermID: term.ID, CycleNo: spec.CycleNo, StartsAt: spec.StartsAt, EndsAt: spec.EndsAt, BaseQuotaUSD: spec.BaseQuotaUSD, BaseBalanceUSD: base, BoostBalanceUSD: boost, ManualBalanceUSD: manual, State: state}
		var activatedAt *time.Time
		if state == domain.CarpoolCycleActive {
			activatedAt = &now
		}
		err = tx.QueryRowContext(ctx, `INSERT INTO carpool_cycles(term_id,cycle_no,starts_at,ends_at,base_quota_usd,base_balance_usd,boost_balance_usd,manual_balance_usd,state,activated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id`, term.ID, spec.CycleNo, spec.StartsAt, spec.EndsAt, spec.BaseQuotaUSD.StringFixed(8), base.StringFixed(8), boost.StringFixed(8), manual.StringFixed(8), state, activatedAt).Scan(&cycle.ID)
		if err != nil {
			return nil, nil, err
		}
		if state == domain.CarpoolCycleActive {
			if params.Mode != "takeover" {
				err = insertLedgerTx(ctx, tx, term.UserID, term.ID, cycle.ID, "cycle_initial", domain.CarpoolBucketBase, spec.BaseQuotaUSD, fmt.Sprintf("cycle_initial:%d", cycle.ID), nil, nil, nil, nil, &params.ActorID, nil, "initial cycle grant", now)
			}
			if err != nil {
				return nil, nil, err
			}
		}
		cycles = append(cycles, cycle)
	}
	if params.Takeover != nil {
		if err = importTakeoverTx(ctx, tx, term, cycles, *params.Takeover, params.ActorID, now); err != nil {
			return nil, nil, err
		}
	}
	var payments []domain.CarpoolPayment
	if params.Payment != nil {
		payment, paymentErr := createPaymentTx(ctx, tx, term.ID, params.ActorID, operationRequestID(params.Operation), params.Operation.Fingerprint, *params.Payment)
		if paymentErr != nil {
			return nil, nil, paymentErr
		}
		if payment != nil {
			payments = append(payments, *payment)
		}
	}
	term.OperationCycles = cycles
	term.OperationPayments = payments
	response := createTermOperationResponse{Term: *term, Cycles: cycles, Payments: payments}
	if err = recordOperationTx(ctx, tx, params.Operation, "term", term.ID, response); err != nil {
		return nil, nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, nil, err
	}
	return term, cycles, nil
}

func lockCarpoolScopeMembershipTx(ctx context.Context, tx *sql.Tx, scopeID int64) error {
	if scopeID <= 0 || scopeID > 2147483647 {
		return service.ErrCarpoolInvalidRelationship
	}
	_, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1,$2)`, carpoolScopeMembershipLockNamespace, int32(scopeID))
	return err
}

func getPlanTx(ctx context.Context, tx *sql.Tx, planID int64) (*domain.CarpoolPlan, error) {
	return scanCarpoolPlan(tx.QueryRowContext(ctx, `SELECT id,code,name,list_price_cny::text,weekly_quota_usd::text,duration_days,cycle_days,boost_ratio::text,boost_count,enabled,version FROM carpool_plans WHERE id=$1 FOR SHARE`, planID))
}

func validateLatestEnabledPlanTx(ctx context.Context, tx *sql.Tx, plan *domain.CarpoolPlan) error {
	if plan == nil || !plan.Enabled {
		return service.ErrCarpoolUnavailable
	}
	var latestID int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM carpool_plans WHERE code=$1 ORDER BY version DESC,id DESC LIMIT 1`, plan.Code).Scan(&latestID); err != nil {
		return err
	}
	if latestID != plan.ID {
		return service.ErrCarpoolUnavailable
	}
	return nil
}

const carpoolEffectiveGroupTypeSQL = `CASE WHEN b.user_id IS NOT NULL THEN CASE WHEN g.subscription_type='standard' AND g.platform='openai' AND b.group_id=g.id THEN 'carpool' ELSE '' END ELSE g.subscription_type END`

func prepareCarpoolUserGroupTx(ctx context.Context, tx *sql.Tx, params domain.CreateCarpoolTermParams) error {
	var groupType, platform, groupStatus string
	err := tx.QueryRowContext(ctx, `SELECT subscription_type,platform,status FROM groups WHERE id=$1 AND deleted_at IS NULL FOR SHARE`, params.GroupID).Scan(&groupType, &platform, &groupStatus)
	if errors.Is(err, sql.ErrNoRows) {
		return service.ErrCarpoolInvalidRelationship
	}
	if err != nil {
		return err
	}
	if groupType != service.SubscriptionTypeStandard {
		return validateCarpoolUserGroupTx(ctx, tx, params.UserID, params.GroupID)
	}
	if platform != service.PlatformOpenAI || groupStatus != service.StatusActive {
		return service.ErrCarpoolInvalidRelationship
	}
	var userStatus, balance, frozen string
	err = tx.QueryRowContext(ctx, `SELECT status,balance::text,COALESCE(frozen_balance,0)::text FROM users WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, params.UserID).Scan(&userStatus, &balance, &frozen)
	if errors.Is(err, sql.ErrNoRows) {
		return service.ErrCarpoolInvalidRelationship
	}
	if err != nil {
		return err
	}
	if userStatus != service.StatusActive {
		return service.ErrCarpoolInvalidRelationship
	}
	var boundGroupID int64
	err = tx.QueryRowContext(ctx, `SELECT group_id FROM carpool_billing_bindings WHERE user_id=$1 FOR UPDATE`, params.UserID).Scan(&boundGroupID)
	firstBinding := errors.Is(err, sql.ErrNoRows)
	if err != nil && !firstBinding {
		return err
	}
	if !firstBinding && boundGroupID != params.GroupID {
		return service.ErrCarpoolInvalidRelationship
	}
	if firstBinding {
		if params.Operation.Kind == "renew_term" {
			return service.ErrCarpoolInvalidRelationship
		}
		ordinary, parseErr := decimal.NewFromString(balance)
		if parseErr != nil {
			return parseErr
		}
		reserved, parseErr := decimal.NewFromString(frozen)
		if parseErr != nil {
			return parseErr
		}
		if !ordinary.IsZero() || !reserved.IsZero() {
			return service.ErrCarpoolOpeningBalance
		}
	}
	// Lock every existing Key without changing its identity, group, quota, or status.
	rows, err := tx.QueryContext(ctx, `SELECT group_id FROM api_keys WHERE user_id=$1 AND deleted_at IS NULL ORDER BY id FOR UPDATE`, params.UserID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var keyGroup sql.NullInt64
		if err = rows.Scan(&keyGroup); err != nil {
			rows.Close()
			return err
		}
		if !keyGroup.Valid || keyGroup.Int64 != params.GroupID {
			rows.Close()
			return service.ErrCarpoolOpeningKeyGroup
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if firstBinding {
		if _, err = tx.ExecContext(ctx, `INSERT INTO carpool_billing_bindings(user_id,group_id) VALUES($1,$2)`, params.UserID, params.GroupID); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE users SET restrict_public_groups=TRUE,updated_at=clock_timestamp() WHERE id=$1 AND NOT restrict_public_groups`, params.UserID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM user_allowed_groups WHERE user_id=$1 AND group_id<>$2`, params.UserID, params.GroupID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO user_allowed_groups(user_id,group_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, params.UserID, params.GroupID); err != nil {
		return err
	}
	// These permission changes are not all covered by the legacy invalidation triggers.
	_, err = tx.ExecContext(ctx, `INSERT INTO auth_cache_invalidation_outbox(cache_key) SELECT encode(sha256(convert_to(key,'UTF8')),'hex') FROM api_keys WHERE user_id=$1 AND deleted_at IS NULL`, params.UserID)
	return err
}

func validateCarpoolUserGroupTx(ctx context.Context, tx *sql.Tx, userID, groupID int64) error {
	var groupType string
	err := tx.QueryRowContext(ctx, `SELECT `+carpoolEffectiveGroupTypeSQL+` FROM users u CROSS JOIN groups g LEFT JOIN carpool_billing_bindings b ON b.user_id=u.id WHERE u.id=$1 AND u.deleted_at IS NULL AND g.id=$2 AND g.deleted_at IS NULL`, userID, groupID).Scan(&groupType)
	if errors.Is(err, sql.ErrNoRows) {
		return service.ErrCarpoolInvalidRelationship
	}
	if err != nil {
		return err
	}
	if !strings.EqualFold(groupType, "carpool") {
		return service.ErrCarpoolInvalidRelationship
	}
	return nil
}

func importTakeoverTx(ctx context.Context, tx *sql.Tx, term *domain.CarpoolTerm, cycles []domain.CarpoolCycle, takeover domain.CarpoolTakeoverInput, actorID int64, now time.Time) error {
	var current *domain.CarpoolCycle
	for i := range cycles {
		if cycles[i].State == domain.CarpoolCycleActive {
			current = &cycles[i]
			break
		}
	}
	if current == nil {
		if !takeover.CurrentBaseBalanceUSD.IsZero() || !takeover.CurrentBoostBalanceUSD.IsZero() || !takeover.CurrentManualBalanceUSD.IsZero() {
			return service.ErrCarpoolUnavailable
		}
		if len(cycles) > 0 {
			current = &cycles[len(cycles)-1]
		} else {
			return service.ErrCarpoolUnavailable
		}
	}
	for _, grant := range []struct {
		bucket string
		amount decimal.Decimal
	}{{domain.CarpoolBucketBase, takeover.CurrentBaseBalanceUSD}, {domain.CarpoolBucketBoost, takeover.CurrentBoostBalanceUSD}, {domain.CarpoolBucketManual, takeover.CurrentManualBalanceUSD}} {
		if grant.amount.IsZero() {
			continue
		}
		if err := insertLedgerTx(ctx, tx, term.UserID, term.ID, current.ID, "takeover_opening", grant.bucket, grant.amount, fmt.Sprintf("takeover:%d:%s:opening", term.ID, grant.bucket), nil, nil, nil, nil, &actorID, nil, "historical takeover opening balance", now); err != nil {
			return err
		}
	}
	if takeover.HistoricalUsedUSD != nil && takeover.HistoricalUsedUSD.IsPositive() {
		used := takeover.HistoricalUsedUSD.Round(8)
		if err := insertLedgerTx(ctx, tx, term.UserID, term.ID, current.ID, "takeover_historical_source", domain.CarpoolBucketBase, used, fmt.Sprintf("takeover:%d:history:source", term.ID), nil, nil, nil, nil, &actorID, nil, "historical usage source balancing entry", now); err != nil {
			return err
		}
		if err := insertLedgerTx(ctx, tx, term.UserID, term.ID, current.ID, "takeover_historical_usage", domain.CarpoolBucketBase, used.Neg(), fmt.Sprintf("takeover:%d:history:usage", term.ID), nil, nil, nil, nil, &actorID, nil, "imported historical usage", now); err != nil {
			return err
		}
	}
	return nil
}

func insertLedgerTx(ctx context.Context, tx *sql.Tx, userID, termID, cycleID int64, eventType, bucket string, delta decimal.Decimal, eventKey string, requestID *string, apiKeyID, resetBatchID *int64, boostSlot *int, actorID, reversesID *int64, reason string, effectiveAt time.Time) error {
	if delta.IsZero() {
		return nil
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO carpool_ledger(user_id,term_id,cycle_id,event_type,bucket,delta_usd,event_key,request_id,api_key_id,reset_batch_id,boost_slot,actor_id,reverses_ledger_id,reason,effective_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`, userID, termID, cycleID, eventType, bucket, delta.StringFixed(8), eventKey, requestID, apiKeyID, resetBatchID, boostSlot, actorID, reversesID, reason, effectiveAt)
	return err
}

func (r *CarpoolRepository) EnsureCurrentCycle(ctx context.Context, termID int64, at time.Time) (*domain.CarpoolCycle, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	cycle, err := ensureCurrentCycleTx(ctx, tx, termID, at)
	if err != nil && !errors.Is(err, service.ErrCarpoolUnavailable) {
		_ = tx.Rollback()
		return nil, err
	}
	ensureErr := err
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return cycle, ensureErr
}

func ensureCurrentCycleTx(ctx context.Context, tx *sql.Tx, termID int64, at time.Time) (*domain.CarpoolCycle, error) {
	var userID int64
	var status string
	var startsAt, expiresAt time.Time
	var snapshotJSON string
	if err := tx.QueryRowContext(ctx, `SELECT user_id,status,starts_at,expires_at,plan_snapshot::text FROM carpool_terms WHERE id=$1 FOR UPDATE`, termID).Scan(&userID, &status, &startsAt, &expiresAt, &snapshotJSON); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, service.ErrCarpoolNotFound
		}
		return nil, err
	}
	terminal := status == domain.CarpoolTermTerminated || !at.Before(expiresAt)
	if terminal {
		if _, err := tx.ExecContext(ctx, `UPDATE carpool_cycles SET state='missed',updated_at=NOW(),revision=revision+1 WHERE term_id=$1 AND state='scheduled'`, termID); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE carpool_cycles SET state='closing',updated_at=NOW(),revision=revision+1 WHERE term_id=$1 AND state='active'`, termID); err != nil {
			return nil, err
		}
		if status != domain.CarpoolTermTerminated {
			if _, err := tx.ExecContext(ctx, `UPDATE carpool_terms SET status='expired',updated_at=NOW() WHERE id=$1`, termID); err != nil {
				return nil, err
			}
		}
		return nil, service.ErrCarpoolUnavailable
	}
	if at.Before(startsAt) {
		return nil, service.ErrCarpoolUnavailable
	}
	var snapshot domain.CarpoolPlanSnapshot
	if err := json.Unmarshal([]byte(snapshotJSON), &snapshot); err != nil {
		return nil, err
	}
	snapshot.NormalizeResetMode()
	if _, err := tx.ExecContext(ctx, `UPDATE carpool_cycles SET state='missed',updated_at=NOW(),revision=revision+1 WHERE term_id=$1 AND state='scheduled' AND ends_at <= $2`, termID, at); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE carpool_cycles SET state='closing',updated_at=NOW(),revision=revision+1 WHERE term_id=$1 AND state='active' AND ends_at <= $2`, termID, at); err != nil {
		return nil, err
	}

	cycle, err := scanCarpoolCycle(tx.QueryRowContext(ctx, cycleSelect+` WHERE term_id=$1 AND starts_at <= $2 AND ends_at > $2 FOR UPDATE`, termID, at))
	if errors.Is(err, service.ErrCarpoolNotFound) && snapshot.EffectiveResetMode() == domain.CarpoolResetModeRolling {
		cycle, err = createCurrentRollingCycleTx(ctx, tx, termID, userID, expiresAt, at, snapshot)
	}
	if err != nil {
		return nil, err
	}
	if cycle.State == domain.CarpoolCycleScheduled {
		res, err := tx.ExecContext(ctx, `UPDATE carpool_cycles SET state='active',base_balance_usd=base_balance_usd+base_quota_usd,activated_at=$2,updated_at=NOW(),revision=revision+1 WHERE id=$1 AND state='scheduled'`, cycle.ID, at)
		if err != nil {
			return nil, err
		}
		if n, _ := res.RowsAffected(); n == 1 {
			if err = insertLedgerTx(ctx, tx, userID, termID, cycle.ID, "cycle_initial", domain.CarpoolBucketBase, cycle.BaseQuotaUSD, fmt.Sprintf("cycle_initial:%d", cycle.ID), nil, nil, nil, nil, nil, nil, "initial cycle grant", at); err != nil {
				return nil, err
			}
			cycle.State, cycle.BaseBalanceUSD, cycle.Revision = domain.CarpoolCycleActive, cycle.BaseBalanceUSD.Add(cycle.BaseQuotaUSD), cycle.Revision+1
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE carpool_terms SET status='active',updated_at=NOW() WHERE id=$1 AND status='pending'`, termID); err != nil {
		return nil, err
	}
	return cycle, nil
}

func createCurrentRollingCycleTx(ctx context.Context, tx *sql.Tx, termID, userID int64, expiresAt, at time.Time, snapshot domain.CarpoolPlanSnapshot) (*domain.CarpoolCycle, error) {
	last, err := scanCarpoolCycle(tx.QueryRowContext(ctx, cycleSelect+` WHERE term_id=$1 ORDER BY cycle_no DESC LIMIT 1 FOR UPDATE`, termID))
	if err != nil {
		return nil, err
	}
	period := time.Duration(snapshot.CycleDays) * 24 * time.Hour
	if period <= 0 {
		return nil, service.ErrCarpoolUnavailable
	}
	for !at.Before(last.EndsAt) && last.EndsAt.Before(expiresAt) {
		startsAt := last.EndsAt
		endsAt := startsAt.Add(period)
		if endsAt.After(expiresAt) {
			endsAt = expiresAt
		}
		state := domain.CarpoolCycleActive
		base := snapshot.WeeklyQuotaUSD.Round(8)
		var activatedAt *time.Time
		if !at.Before(endsAt) {
			state = domain.CarpoolCycleMissed
			base = decimal.Zero
		} else {
			activated := at
			activatedAt = &activated
		}
		next := &domain.CarpoolCycle{
			TermID: termID, CycleNo: last.CycleNo + 1, StartsAt: startsAt, EndsAt: endsAt,
			BaseQuotaUSD: snapshot.WeeklyQuotaUSD.Round(8), BaseBalanceUSD: base, State: state,
		}
		if err = tx.QueryRowContext(ctx, `INSERT INTO carpool_cycles(term_id,cycle_no,starts_at,ends_at,base_quota_usd,base_balance_usd,boost_balance_usd,manual_balance_usd,state,activated_at) VALUES($1,$2,$3,$4,$5,$6,0,0,$7,$8) RETURNING id`, termID, next.CycleNo, startsAt, endsAt, next.BaseQuotaUSD.StringFixed(8), base.StringFixed(8), state, activatedAt).Scan(&next.ID); err != nil {
			return nil, err
		}
		if state == domain.CarpoolCycleActive {
			if err = insertLedgerTx(ctx, tx, userID, termID, next.ID, "cycle_initial", domain.CarpoolBucketBase, next.BaseQuotaUSD, fmt.Sprintf("cycle_initial:%d", next.ID), nil, nil, nil, nil, nil, nil, "natural rolling refill", startsAt); err != nil {
				return nil, err
			}
			return next, nil
		}
		last = next
	}
	return nil, service.ErrCarpoolNotFound
}

func scanCarpoolCycle(row carpoolRowScanner) (*domain.CarpoolCycle, error) {
	var c domain.CarpoolCycle
	var quota, base, boost, manual string
	if err := row.Scan(&c.ID, &c.TermID, &c.CycleNo, &c.StartsAt, &c.EndsAt, &quota, &base, &boost, &manual, &c.State, &c.Revision); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, service.ErrCarpoolNotFound
		}
		return nil, err
	}
	var err error
	if c.BaseQuotaUSD, err = decimal.NewFromString(quota); err != nil {
		return nil, err
	}
	if c.BaseBalanceUSD, err = decimal.NewFromString(base); err != nil {
		return nil, err
	}
	if c.BoostBalanceUSD, err = decimal.NewFromString(boost); err != nil {
		return nil, err
	}
	if c.ManualBalanceUSD, err = decimal.NewFromString(manual); err != nil {
		return nil, err
	}
	return &c, nil
}

func applyGrantTx(ctx context.Context, tx *sql.Tx, userID, termID, cycleID int64, bucket string, amount decimal.Decimal, eventType, eventKey string, requestID *string, boostSlot *int, actorID, reversesID *int64, reason string, now time.Time) ([]domain.CarpoolLedgerEntry, error) {
	if !amount.IsPositive() {
		return nil, fmt.Errorf("grant amount must be positive")
	}
	column := map[string]string{domain.CarpoolBucketBase: "base_balance_usd", domain.CarpoolBucketBoost: "boost_balance_usd", domain.CarpoolBucketManual: "manual_balance_usd"}[bucket]
	if column == "" {
		return nil, fmt.Errorf("invalid bucket")
	}
	var base string
	if err := tx.QueryRowContext(ctx, `SELECT base_balance_usd::text FROM carpool_cycles WHERE id=$1 AND term_id=$2 FOR UPDATE`, cycleID, termID).Scan(&base); err != nil {
		return nil, err
	}
	baseBalance, err := decimal.NewFromString(base)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, fmt.Sprintf(`UPDATE carpool_cycles SET %s=%s+$1,updated_at=NOW(),revision=revision+1 WHERE id=$2`, column, column), amount.StringFixed(8), cycleID); err != nil {
		return nil, err
	}
	if err = insertLedgerTx(ctx, tx, userID, termID, cycleID, eventType, bucket, amount, eventKey, requestID, nil, nil, boostSlot, actorID, reversesID, reason, now); err != nil {
		return nil, err
	}
	entries := []domain.CarpoolLedgerEntry{{UserID: userID, TermID: termID, CycleID: cycleID, EventType: eventType, Bucket: bucket, DeltaUSD: amount, EventKey: eventKey, RequestID: requestID, EffectiveAt: now}}
	if bucket != domain.CarpoolBucketBase && baseBalance.IsNegative() {
		offset := decimal.Min(amount, baseBalance.Abs())
		if offset.IsPositive() {
			if _, err = tx.ExecContext(ctx, fmt.Sprintf(`UPDATE carpool_cycles SET base_balance_usd=base_balance_usd+$1,%s=%s-$1,updated_at=NOW(),revision=revision+1 WHERE id=$2`, column, column), offset.StringFixed(8), cycleID); err != nil {
				return nil, err
			}
			if err = insertLedgerTx(ctx, tx, userID, termID, cycleID, "debt_offset", bucket, offset.Neg(), eventKey+":offset:source", requestID, nil, nil, nil, actorID, nil, "grant transferred to base debt", now); err != nil {
				return nil, err
			}
			if err = insertLedgerTx(ctx, tx, userID, termID, cycleID, "debt_offset", domain.CarpoolBucketBase, offset, eventKey+":offset:base", requestID, nil, nil, nil, actorID, nil, "grant offset base debt", now); err != nil {
				return nil, err
			}
		}
	}
	return entries, nil
}

func admissionFingerprint(requestID string, userID, apiKeyID, groupID int64) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("v1|%s|%d|%d|%d", strings.TrimSpace(requestID), userID, apiKeyID, groupID)))
	return hex.EncodeToString(sum[:])
}

func (r *CarpoolRepository) Admit(ctx context.Context, userID, apiKeyID, groupID int64, requestID string, _ time.Time) (*domain.CarpoolBillingSnapshot, error) {
	for attempt := 0; attempt < 4; attempt++ {
		snapshot, err := r.admitOnce(ctx, userID, apiKeyID, groupID, requestID)
		if !errors.Is(err, errCarpoolCycleBoundaryMoved) {
			return snapshot, err
		}
	}
	return nil, service.ErrCarpoolUnavailable
}

func (r *CarpoolRepository) admitOnce(ctx context.Context, userID, apiKeyID, groupID int64, requestID string) (*domain.CarpoolBillingSnapshot, error) {
	if strings.TrimSpace(requestID) == "" {
		return nil, fmt.Errorf("request_id is required")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	fingerprint := admissionFingerprint(requestID, userID, apiKeyID, groupID)
	admissionIdentity := fmt.Sprintf("carpool:admission:%d:%s", apiKeyID, strings.TrimSpace(requestID))
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, admissionIdentity); err != nil {
		return nil, err
	}
	if existing, findErr := findBillingSnapshotTx(ctx, tx, requestID, apiKeyID); findErr == nil {
		if existing.UserID != userID || existing.GroupID != groupID {
			return nil, service.ErrCarpoolIdempotencyConflict
		}
		return nil, service.ErrCarpoolAdmissionReplay
	} else if !errors.Is(findErr, service.ErrCarpoolNotFound) {
		return nil, findErr
	}
	var keyUser, keyGroup int64
	var groupType string
	if err = tx.QueryRowContext(ctx, `SELECT k.user_id,k.group_id,`+carpoolEffectiveGroupTypeSQL+` FROM api_keys k JOIN groups g ON g.id=k.group_id AND g.deleted_at IS NULL LEFT JOIN carpool_billing_bindings b ON b.user_id=k.user_id WHERE k.id=$1 AND k.deleted_at IS NULL`, apiKeyID).Scan(&keyUser, &keyGroup, &groupType); err != nil {
		return nil, service.ErrCarpoolInvalidRelationship
	}
	if keyUser != userID || keyGroup != groupID || !strings.EqualFold(groupType, "carpool") {
		return nil, service.ErrCarpoolInvalidRelationship
	}
	var termID int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM carpool_terms WHERE user_id=$1 AND scope_id=$2 AND group_id=$3 AND status IN ('pending','active') AND starts_at <= clock_timestamp() AND expires_at > clock_timestamp() ORDER BY starts_at DESC LIMIT 1 FOR UPDATE`, userID, domain.CarpoolGlobalScopeID, groupID).Scan(&termID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrCarpoolUnavailable
	}
	if err != nil {
		return nil, err
	}
	var checkAt time.Time
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&checkAt); err != nil {
		return nil, err
	}
	cycle, err := ensureCurrentCycleTx(ctx, tx, termID, checkAt)
	if err != nil {
		if errors.Is(err, service.ErrCarpoolUnavailable) {
			if commitErr := tx.Commit(); commitErr != nil {
				return nil, commitErr
			}
		}
		return nil, err
	}
	// Settlement locks cycle before it updates API-key counters. Admission uses
	// the same order and then revalidates all mutable authorization fields from
	// locked key/user/group rows rather than trusting the middleware cache.
	var keyStatus, userStatus, groupStatus string
	var keyExpiresAt, window5hStart, window1dStart, window7dStart *time.Time
	var quota, quotaUsed, rateLimit5h, rateLimit1d, rateLimit7d, usage5h, usage1d, usage7d float64
	if err = tx.QueryRowContext(ctx, `SELECT k.user_id,k.group_id,k.status,k.expires_at,k.quota,k.quota_used,k.rate_limit_5h,k.rate_limit_1d,k.rate_limit_7d,k.usage_5h,k.usage_1d,k.usage_7d,k.window_5h_start,k.window_1d_start,k.window_7d_start,u.status,g.status,`+carpoolEffectiveGroupTypeSQL+` FROM api_keys k JOIN users u ON u.id=k.user_id AND u.deleted_at IS NULL JOIN groups g ON g.id=k.group_id AND g.deleted_at IS NULL LEFT JOIN carpool_billing_bindings b ON b.user_id=k.user_id WHERE k.id=$1 AND k.deleted_at IS NULL FOR SHARE OF k,u,g`, apiKeyID).Scan(
		&keyUser, &keyGroup, &keyStatus, &keyExpiresAt, &quota, &quotaUsed,
		&rateLimit5h, &rateLimit1d, &rateLimit7d, &usage5h, &usage1d, &usage7d,
		&window5hStart, &window1dStart, &window7dStart, &userStatus, &groupStatus, &groupType,
	); err != nil {
		return nil, service.ErrCarpoolInvalidRelationship
	}
	if keyUser != userID || keyGroup != groupID || !strings.EqualFold(groupType, "carpool") {
		return nil, service.ErrCarpoolInvalidRelationship
	}
	var admittedAt time.Time
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&admittedAt); err != nil {
		return nil, err
	}
	if admittedAt.Before(cycle.StartsAt) || !admittedAt.Before(cycle.EndsAt) {
		return nil, errCarpoolCycleBoundaryMoved
	}
	if keyStatus != service.StatusAPIKeyActive || userStatus != domain.StatusActive || groupStatus != domain.StatusActive ||
		(keyExpiresAt != nil && !admittedAt.Before(*keyExpiresAt)) ||
		(quota > 0 && quotaUsed >= quota) ||
		!carpoolRateWindowEligible(rateLimit5h, usage5h, window5hStart, service.RateLimitWindow5h, admittedAt) ||
		!carpoolRateWindowEligible(rateLimit1d, usage1d, window1dStart, service.RateLimitWindow1d, admittedAt) ||
		!carpoolRateWindowEligible(rateLimit7d, usage7d, window7dStart, service.RateLimitWindow7d, admittedAt) {
		return nil, service.ErrCarpoolUnavailable
	}
	if cycle == nil || cycle.State != domain.CarpoolCycleActive {
		return nil, service.ErrCarpoolUnavailable
	}
	if !cycle.AvailableUSD().IsPositive() {
		return nil, service.ErrCarpoolQuotaExhausted
	}
	s := &domain.CarpoolBillingSnapshot{RequestID: requestID, UserID: userID, APIKeyID: apiKeyID, GroupID: groupID, TermID: termID, CycleID: cycle.ID, AdmittedAt: admittedAt}
	err = tx.QueryRowContext(ctx, `INSERT INTO carpool_billing_requests(request_id,api_key_id,user_id,group_id,term_id,cycle_id,admitted_at,status,request_fingerprint) VALUES($1,$2,$3,$4,$5,$6,$7,'admitted',$8) RETURNING id,admitted_at`, requestID, apiKeyID, userID, groupID, termID, cycle.ID, admittedAt, fingerprint).Scan(&s.BillingRequestID, &s.AdmittedAt)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return s, nil
}

func carpoolRateWindowEligible(limit, used float64, startsAt *time.Time, duration time.Duration, now time.Time) bool {
	return limit <= 0 || startsAt == nil || !now.Before(startsAt.Add(duration)) || used < limit
}

func findBillingSnapshotTx(ctx context.Context, tx *sql.Tx, requestID string, apiKeyID int64) (*domain.CarpoolBillingSnapshot, error) {
	var s domain.CarpoolBillingSnapshot
	err := tx.QueryRowContext(ctx, `SELECT id,request_id,user_id,api_key_id,group_id,term_id,cycle_id,admitted_at FROM carpool_billing_requests WHERE request_id=$1 AND api_key_id=$2`, requestID, apiKeyID).Scan(&s.BillingRequestID, &s.RequestID, &s.UserID, &s.APIKeyID, &s.GroupID, &s.TermID, &s.CycleID, &s.AdmittedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrCarpoolNotFound
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *CarpoolRepository) PersistKnownUsage(ctx context.Context, s domain.CarpoolBillingSnapshot, cost decimal.Decimal, payload json.RawMessage) error {
	if cost.IsNegative() {
		return fmt.Errorf("actual cost cannot be negative")
	}
	cost = cost.Round(8)
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	res, err := r.db.ExecContext(ctx, `UPDATE carpool_billing_requests SET status=CASE WHEN status='settled' THEN 'settled' ELSE 'usage_known' END,actual_cost_usd=$1,billing_payload=$2::jsonb,receipt_recorded_at=COALESCE(receipt_recorded_at,NOW()),last_error=NULL,updated_at=NOW() WHERE id=$3 AND request_id=$4 AND api_key_id=$5 AND user_id=$6 AND group_id=$7 AND term_id=$8 AND cycle_id=$9 AND admitted_at=$10 AND (status='admitted' OR (status='reconcile_required' AND actual_cost_usd IS NULL AND billing_payload IS NULL) OR (status IN ('usage_known','settling','settled') AND actual_cost_usd=$1 AND COALESCE(billing_payload,'{}'::jsonb)=$2::jsonb))`, cost.StringFixed(8), string(payload), s.BillingRequestID, s.RequestID, s.APIKeyID, s.UserID, s.GroupID, s.TermID, s.CycleID, s.AdmittedAt)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return service.ErrCarpoolIdempotencyConflict
	}
	return nil
}

func (r *CarpoolRepository) RecoverPendingReceipts(ctx context.Context, limit int) ([]domain.CarpoolKnownUsage, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	if _, err := r.db.ExecContext(ctx, `WITH stale AS (SELECT id FROM carpool_billing_requests WHERE status='admitted' AND updated_at < NOW()-INTERVAL '1 hour' ORDER BY updated_at,id FOR UPDATE SKIP LOCKED LIMIT $1) UPDATE carpool_billing_requests b SET status='reconcile_required',retry_count=b.retry_count+1,last_error='receipt_missing_timeout',updated_at=NOW() FROM stale WHERE b.id=stale.id`, limit); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `WITH pending AS (SELECT id FROM carpool_billing_requests WHERE status='usage_known' ORDER BY updated_at,id FOR UPDATE SKIP LOCKED LIMIT $1) UPDATE carpool_billing_requests b SET retry_count=b.retry_count+1,last_error='automatic_settlement_retry_pending',updated_at=NOW() FROM pending WHERE b.id=pending.id RETURNING b.id,b.request_id,b.user_id,b.api_key_id,b.group_id,b.term_id,b.cycle_id,b.admitted_at,b.actual_cost_usd::text,COALESCE(b.billing_payload,'{}'::jsonb)::text,b.retry_count`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.CarpoolKnownUsage
	for rows.Next() {
		var item domain.CarpoolKnownUsage
		var cost, payload string
		if err = rows.Scan(&item.Snapshot.BillingRequestID, &item.Snapshot.RequestID, &item.Snapshot.UserID, &item.Snapshot.APIKeyID, &item.Snapshot.GroupID, &item.Snapshot.TermID, &item.Snapshot.CycleID, &item.Snapshot.AdmittedAt, &cost, &payload, &item.RetryCount); err != nil {
			return nil, err
		}
		item.ActualCostUSD, err = decimal.NewFromString(cost)
		if err != nil {
			return nil, err
		}
		item.BillingPayload = json.RawMessage(payload)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *CarpoolRepository) MarkReconcileRequired(ctx context.Context, s domain.CarpoolBillingSnapshot, reason string) error {
	reason = carpoolBillingDiagnosticCategory(reason)
	res, err := r.db.ExecContext(ctx, `UPDATE carpool_billing_requests SET status=CASE WHEN status='admitted' THEN 'reconcile_required' ELSE status END,last_error=$1,updated_at=NOW() WHERE id=$2 AND request_id=$3 AND api_key_id=$4 AND status <> 'settled'`, reason, s.BillingRequestID, s.RequestID, s.APIKeyID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return service.ErrCarpoolNotFound
	}
	return nil
}

func (r *CarpoolRepository) MarkKnownUsageReconcileRequired(ctx context.Context, s domain.CarpoolBillingSnapshot, reason string) error {
	reason = carpoolBillingDiagnosticCategory(reason)
	res, err := r.db.ExecContext(ctx, `UPDATE carpool_billing_requests SET status='reconcile_required',last_error=$1,updated_at=NOW() WHERE id=$2 AND request_id=$3 AND api_key_id=$4 AND status='usage_known'`, reason, s.BillingRequestID, s.RequestID, s.APIKeyID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return service.ErrCarpoolNotFound
	}
	return nil
}

func carpoolBillingDiagnosticCategory(reason string) string {
	switch strings.TrimSpace(reason) {
	case "usage receipt missing after recovery timeout", "receipt_missing_timeout":
		return "receipt_missing_timeout"
	case "automatic settlement retry pending", "automatic_settlement_retry_pending":
		return "automatic_settlement_retry_pending"
	case "automatic settlement retry failed", "automatic_settlement_retry_failed":
		return "automatic_settlement_retry_failed"
	case "automatic settlement permanent failure", "automatic_settlement_permanent_failure":
		return "automatic_settlement_permanent_failure"
	case "automatic settlement retry exhausted", "automatic_settlement_retry_exhausted":
		return "automatic_settlement_retry_exhausted"
	case "invalid durable carpool billing command", "invalid_durable_billing_command":
		return "invalid_durable_billing_command"
	case "alpha search request ended without durable known usage",
		"chat completions request ended without durable known usage",
		"embeddings request ended without durable known usage",
		"responses request ended without durable known usage",
		"messages request ended without durable known usage",
		"images request ended without durable known usage",
		"websocket turn ended without durable known usage",
		"usage_receipt_missing":
		return "usage_receipt_missing"
	case "alpha search usage persistence or settlement failed",
		"chat completions usage persistence or settlement failed",
		"embeddings usage persistence or settlement failed",
		"responses usage persistence or settlement failed",
		"messages usage persistence or settlement failed",
		"images usage persistence or settlement failed",
		"carpool cyber usage persistence or settlement failed",
		"websocket cyber usage persistence or settlement failed",
		"websocket known usage persistence or settlement failed",
		"usage_persistence_or_settlement_failed":
		return "usage_persistence_or_settlement_failed"
	default:
		return "usage_reconciliation_required"
	}
}

func (r *CarpoolRepository) ApplyUsageDebitTx(ctx context.Context, tx *sql.Tx, debit *domain.CarpoolUsageDebit) ([]domain.CarpoolLedgerEntry, error) {
	if debit == nil || debit.ActualCostUSD.IsNegative() {
		return nil, fmt.Errorf("invalid carpool debit")
	}
	cost := debit.ActualCostUSD.Round(8)
	cycle, err := scanCarpoolCycle(tx.QueryRowContext(ctx, `SELECT id,term_id,cycle_no,starts_at,ends_at,base_quota_usd::text,base_balance_usd::text,boost_balance_usd::text,manual_balance_usd::text,state,revision FROM carpool_cycles WHERE id=$1 FOR UPDATE`, debit.Snapshot.CycleID))
	if err != nil {
		return nil, err
	}
	var userID, termID, cycleID int64
	var status, receiptCost string
	var admittedAt time.Time
	if err := tx.QueryRowContext(ctx, `SELECT user_id,term_id,cycle_id,status,COALESCE(actual_cost_usd,0)::text,admitted_at FROM carpool_billing_requests WHERE id=$1 AND request_id=$2 AND api_key_id=$3 FOR UPDATE`, debit.Snapshot.BillingRequestID, debit.Snapshot.RequestID, debit.Snapshot.APIKeyID).Scan(&userID, &termID, &cycleID, &status, &receiptCost, &admittedAt); err != nil {
		return nil, err
	}
	storedCost, parseErr := decimal.NewFromString(receiptCost)
	if parseErr != nil {
		return nil, parseErr
	}
	if userID != debit.Snapshot.UserID || termID != debit.Snapshot.TermID || cycleID != debit.Snapshot.CycleID || cycle.TermID != termID || !admittedAt.Equal(debit.Snapshot.AdmittedAt) || !storedCost.Equal(cost) {
		return nil, service.ErrCarpoolInvalidRelationship
	}
	if status == "settled" {
		return nil, nil
	}
	if status != "usage_known" && status != "settling" {
		return nil, service.ErrCarpoolUnavailable
	}
	if admittedAt.Before(cycle.StartsAt) || !admittedAt.Before(cycle.EndsAt) {
		return nil, service.ErrCarpoolInvalidRelationship
	}
	remaining := cost
	takeBase := decimal.Min(decimal.Max(cycle.BaseBalanceUSD, decimal.Zero), remaining)
	remaining = remaining.Sub(takeBase)
	takeBoost := decimal.Min(decimal.Max(cycle.BoostBalanceUSD, decimal.Zero), remaining)
	remaining = remaining.Sub(takeBoost)
	takeManual := decimal.Min(decimal.Max(cycle.ManualBalanceUSD, decimal.Zero), remaining)
	remaining = remaining.Sub(takeManual)
	baseDelta := takeBase.Add(remaining).Neg()
	boostDelta := takeBoost.Neg()
	manualDelta := takeManual.Neg()
	if _, err = tx.ExecContext(ctx, `UPDATE carpool_cycles SET base_balance_usd=base_balance_usd+$1,boost_balance_usd=boost_balance_usd+$2,manual_balance_usd=manual_balance_usd+$3,updated_at=NOW(),revision=revision+1 WHERE id=$4`, baseDelta.StringFixed(8), boostDelta.StringFixed(8), manualDelta.StringFixed(8), cycleID); err != nil {
		return nil, err
	}
	requestID := debit.Snapshot.RequestID
	apiKeyID := debit.Snapshot.APIKeyID
	eventPrefix := debit.EventKey
	if eventPrefix == "" {
		eventPrefix = fmt.Sprintf("usage:%d", debit.Snapshot.BillingRequestID)
	}
	var entries []domain.CarpoolLedgerEntry
	for _, part := range []struct {
		bucket string
		delta  decimal.Decimal
	}{{domain.CarpoolBucketBase, baseDelta}, {domain.CarpoolBucketBoost, boostDelta}, {domain.CarpoolBucketManual, manualDelta}} {
		if part.delta.IsZero() {
			continue
		}
		key := eventPrefix + ":" + part.bucket
		if err = insertLedgerTx(ctx, tx, userID, termID, cycleID, "usage", part.bucket, part.delta, key, &requestID, &apiKeyID, nil, nil, debit.ActorID, nil, "actual gateway usage", admittedAt); err != nil {
			return nil, err
		}
		entries = append(entries, domain.CarpoolLedgerEntry{UserID: userID, TermID: termID, CycleID: cycleID, EventType: "usage", Bucket: part.bucket, DeltaUSD: part.delta, EventKey: key, RequestID: &requestID, EffectiveAt: admittedAt})
	}
	return entries, nil
}

func (r *CarpoolRepository) MarkBillingSettledTx(ctx context.Context, tx *sql.Tx, billingRequestID int64, actualCost decimal.Decimal) error {
	res, err := tx.ExecContext(ctx, `UPDATE carpool_billing_requests SET status='settled',actual_cost_usd=$1,settled_at=COALESCE(settled_at,NOW()),last_error=NULL,updated_at=NOW() WHERE id=$2 AND status IN ('usage_known','settling','settled') AND actual_cost_usd=$1`, actualCost.Round(8).StringFixed(8), billingRequestID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return service.ErrCarpoolIdempotencyConflict
	}
	return nil
}
