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

type terminateTermOperationResponse struct {
	Term     domain.CarpoolTerm      `json:"term"`
	Cycles   []domain.CarpoolCycle   `json:"cycles"`
	Payments []domain.CarpoolPayment `json:"payments"`
}

func operationKeyHash(key string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(key)))
	return hex.EncodeToString(sum[:])
}

func operationRequestID(operation domain.CarpoolOperation) string {
	return operationKeyHash(fmt.Sprintf("%s\n%d\n%s", operation.Kind, operation.ActorID, strings.TrimSpace(operation.Key)))
}

func lockOperationTx(ctx context.Context, tx *sql.Tx, operation domain.CarpoolOperation) error {
	identity := fmt.Sprintf("carpool:operation:%s:%d:%s", operation.Kind, operation.ActorID, operationKeyHash(operation.Key))
	_, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, identity)
	return err
}

func validateOperation(operation domain.CarpoolOperation, kinds ...string) error {
	if operation.ActorID <= 0 || strings.TrimSpace(operation.Key) == "" || strings.TrimSpace(operation.Fingerprint) == "" {
		return fmt.Errorf("carpool operation identity is required")
	}
	for _, kind := range kinds {
		if operation.Kind == kind {
			return nil
		}
	}
	return fmt.Errorf("invalid carpool operation kind")
}

func lookupOperationTx(ctx context.Context, tx *sql.Tx, operation domain.CarpoolOperation) (int64, json.RawMessage, bool, error) {
	var resourceID int64
	var fingerprint, response string
	err := tx.QueryRowContext(ctx, `SELECT resource_id,request_fingerprint,response::text FROM carpool_operations WHERE kind=$1 AND actor_id=$2 AND key_hash=$3`, operation.Kind, operation.ActorID, operationKeyHash(operation.Key)).Scan(&resourceID, &fingerprint, &response)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil, false, nil
	}
	if err != nil {
		return 0, nil, false, err
	}
	if fingerprint != operation.Fingerprint {
		return 0, nil, false, service.ErrCarpoolIdempotencyConflict
	}
	return resourceID, json.RawMessage(response), true, nil
}

func replayOperationResponse(response json.RawMessage, target any) error {
	if len(response) == 0 {
		return fmt.Errorf("carpool operation response is empty")
	}
	return json.Unmarshal(response, target)
}

func recordOperationTx(ctx context.Context, tx *sql.Tx, operation domain.CarpoolOperation, resourceType string, resourceID int64, response any) error {
	body, err := json.Marshal(response)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO carpool_operations(kind,actor_id,key_hash,request_fingerprint,resource_type,resource_id,response) VALUES($1,$2,$3,$4,$5,$6,$7::jsonb)`, operation.Kind, operation.ActorID, operationKeyHash(operation.Key), operation.Fingerprint, resourceType, resourceID, string(body))
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "unique") {
		return service.ErrCarpoolIdempotencyConflict
	}
	return err
}

func (r *CarpoolRepository) GetTerm(ctx context.Context, termID int64) (*domain.CarpoolTerm, error) {
	return scanCarpoolTerm(r.db.QueryRowContext(ctx, termSelect+` WHERE id=$1`, termID))
}

const termSelect = `SELECT id,user_id,scope_id,group_id,plan_id,plan_snapshot::text,starts_at,expires_at,status,boost_used,history_complete,statistics_since,created_by,notes,created_at FROM carpool_terms`

func getTermTx(ctx context.Context, tx *sql.Tx, termID int64) (*domain.CarpoolTerm, error) {
	return scanCarpoolTerm(tx.QueryRowContext(ctx, termSelect+` WHERE id=$1`, termID))
}

func scanCarpoolTerm(row carpoolRowScanner) (*domain.CarpoolTerm, error) {
	var term domain.CarpoolTerm
	var snapshot string
	if err := row.Scan(&term.ID, &term.UserID, &term.ScopeID, &term.GroupID, &term.PlanID, &snapshot, &term.StartsAt, &term.ExpiresAt, &term.Status, &term.BoostUsed, &term.HistoryComplete, &term.StatisticsSince, &term.CreatedBy, &term.Notes, &term.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, service.ErrCarpoolNotFound
		}
		return nil, err
	}
	if err := json.Unmarshal([]byte(snapshot), &term.PlanSnapshot); err != nil {
		return nil, err
	}
	return &term, nil
}

func (r *CarpoolRepository) ListTermCycles(ctx context.Context, termID int64) ([]domain.CarpoolCycle, error) {
	return listTermCycles(r.db.QueryContext(ctx, cycleSelect+` WHERE term_id=$1 ORDER BY cycle_no`, termID))
}

const cycleSelect = `SELECT id,term_id,cycle_no,starts_at,ends_at,base_quota_usd::text,base_balance_usd::text,boost_balance_usd::text,manual_balance_usd::text,state,revision FROM carpool_cycles`

func listTermCyclesTx(ctx context.Context, tx *sql.Tx, termID int64) ([]domain.CarpoolCycle, error) {
	return listTermCycles(tx.QueryContext(ctx, cycleSelect+` WHERE term_id=$1 ORDER BY cycle_no`, termID))
}

func listTermCycles(rows *sql.Rows, err error) ([]domain.CarpoolCycle, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.CarpoolCycle
	for rows.Next() {
		cycle, scanErr := scanCarpoolCycle(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, *cycle)
	}
	return out, rows.Err()
}

func (r *CarpoolRepository) CreatePlanVersion(ctx context.Context, oldID int64, next domain.CarpoolPlan, operation domain.CarpoolOperation) (result *domain.CarpoolPlan, err error) {
	if err = validateOperation(operation, "plan_version"); err != nil {
		return nil, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err = lockOperationTx(ctx, tx, operation); err != nil {
		return nil, err
	}
	if _, response, replayed, lookupErr := lookupOperationTx(ctx, tx, operation); lookupErr != nil {
		return nil, lookupErr
	} else if replayed {
		var cached domain.CarpoolPlan
		if err = replayOperationResponse(response, &cached); err != nil {
			return nil, err
		}
		return &cached, nil
	}
	old, err := scanCarpoolPlan(tx.QueryRowContext(ctx, `SELECT id,code,name,list_price_cny::text,weekly_quota_usd::text,duration_days,cycle_days,boost_ratio::text,boost_count,enabled,version FROM carpool_plans WHERE id=$1 FOR UPDATE`, oldID))
	if err != nil {
		return nil, err
	}
	var latestID int64
	if err = tx.QueryRowContext(ctx, `SELECT id FROM carpool_plans WHERE code=$1 ORDER BY version DESC,id DESC LIMIT 1 FOR UPDATE`, old.Code).Scan(&latestID); err != nil {
		return nil, err
	}
	if latestID != old.ID {
		return nil, service.ErrCarpoolIdempotencyConflict
	}
	if next.Name == "" {
		next.Name = old.Name
	}
	next.Code = old.Code
	next.DurationDays = 28
	next.CycleDays = 7
	next.Version = old.Version + 1
	next.ListPriceCNY = next.ListPriceCNY.Round(2)
	next.WeeklyQuotaUSD = next.WeeklyQuotaUSD.Round(8)
	next.BoostRatio = next.BoostRatio.Round(8)
	if !next.ListPriceCNY.IsPositive() || !next.WeeklyQuotaUSD.IsPositive() || !next.BoostRatio.Equal(decimal.New(1, -1)) || (next.BoostCount != 2 && next.BoostCount != 3) {
		return nil, fmt.Errorf("invalid carpool plan values")
	}
	if _, err = tx.ExecContext(ctx, `UPDATE carpool_plans SET enabled=FALSE,updated_at=NOW() WHERE id=$1`, oldID); err != nil {
		return nil, err
	}
	err = tx.QueryRowContext(ctx, `INSERT INTO carpool_plans(code,name,list_price_cny,weekly_quota_usd,duration_days,cycle_days,boost_ratio,boost_count,enabled,version) VALUES($1,$2,$3,$4,28,7,$5,$6,$7,$8) RETURNING id`, next.Code, next.Name, next.ListPriceCNY.StringFixed(2), next.WeeklyQuotaUSD.StringFixed(8), next.BoostRatio.StringFixed(8), next.BoostCount, next.Enabled, next.Version).Scan(&next.ID)
	if err != nil {
		return nil, err
	}
	if err = recordOperationTx(ctx, tx, operation, "plan", next.ID, &next); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return &next, nil
}

func (r *CarpoolRepository) TerminateTerm(ctx context.Context, termID, actorID int64, reason string, effectiveAt *time.Time, operation domain.CarpoolOperation) (term *domain.CarpoolTerm, err error) {
	if strings.TrimSpace(reason) == "" {
		return nil, fmt.Errorf("termination reason is required")
	}
	if err = validateOperation(operation, "terminate_term"); err != nil {
		return nil, err
	}
	if actorID != operation.ActorID {
		return nil, fmt.Errorf("carpool operation actor mismatch")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err = lockOperationTx(ctx, tx, operation); err != nil {
		return nil, err
	}
	if _, response, replayed, lookupErr := lookupOperationTx(ctx, tx, operation); lookupErr != nil {
		return nil, lookupErr
	} else if replayed {
		var cached terminateTermOperationResponse
		if err = replayOperationResponse(response, &cached); err != nil {
			return nil, err
		}
		cached.Term.OperationCycles = cached.Cycles
		cached.Term.OperationPayments = cached.Payments
		return &cached.Term, nil
	}
	var now time.Time
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return nil, err
	}
	when := now
	if effectiveAt != nil {
		when = effectiveAt.UTC()
		if when.After(now) {
			return nil, fmt.Errorf("future termination is not supported")
		}
	}
	res, err := tx.ExecContext(ctx, `UPDATE carpool_terms SET status='terminated',terminated_at=$1,terminated_by=$2,termination_reason=$3,updated_at=NOW() WHERE id=$4 AND status <> 'terminated'`, when, actorID, reason, termID)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, service.ErrCarpoolNotFound
	}
	if _, err = tx.ExecContext(ctx, `UPDATE carpool_cycles SET state=CASE WHEN state='active' THEN 'closing' WHEN state='scheduled' THEN 'missed' ELSE state END,updated_at=NOW(),revision=revision+1 WHERE term_id=$1 AND state IN ('active','scheduled')`, termID); err != nil {
		return nil, err
	}
	term, err = getTermTx(ctx, tx, termID)
	if err != nil {
		return nil, err
	}
	cycles, err := listTermCyclesTx(ctx, tx, termID)
	if err != nil {
		return nil, err
	}
	payments, err := listTermPaymentsTx(ctx, tx, termID)
	if err != nil {
		return nil, err
	}
	term.OperationCycles = cycles
	term.OperationPayments = payments
	response := terminateTermOperationResponse{Term: *term, Cycles: cycles, Payments: payments}
	if err = recordOperationTx(ctx, tx, operation, "term", termID, response); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return term, nil
}

func createPaymentTx(ctx context.Context, tx *sql.Tx, termID, actorID int64, requestID, fingerprint string, input domain.CarpoolPaymentInput) (*domain.CarpoolPayment, error) {
	if !input.AmountCNY.IsPositive() || (input.PaymentKind != "payment" && input.PaymentKind != "refund") || strings.TrimSpace(input.Channel) == "" || input.PaidAt.IsZero() {
		return nil, fmt.Errorf("invalid payment")
	}
	p := &domain.CarpoolPayment{TermID: termID, AmountCNY: input.AmountCNY.Round(2), PaymentKind: input.PaymentKind, PaidAt: input.PaidAt, Channel: input.Channel, ExternalOrderNo: input.ExternalOrderNo, RecordedBy: actorID, Notes: input.Notes}
	err := tx.QueryRowContext(ctx, `INSERT INTO carpool_payments(term_id,amount_cny,payment_kind,paid_at,channel,external_order_no,request_id,request_fingerprint,recorded_by,notes) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id,recorded_at`, termID, p.AmountCNY.StringFixed(2), p.PaymentKind, p.PaidAt, p.Channel, p.ExternalOrderNo, requestID, fingerprint, actorID, p.Notes).Scan(&p.ID, &p.RecordedAt)
	return p, err
}

func (r *CarpoolRepository) RecordPayment(ctx context.Context, termID, actorID int64, input domain.CarpoolPaymentInput, operation domain.CarpoolOperation) (p *domain.CarpoolPayment, err error) {
	if err = validateOperation(operation, "payment"); err != nil {
		return nil, err
	}
	if actorID != operation.ActorID {
		return nil, fmt.Errorf("carpool operation actor mismatch")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err = lockOperationTx(ctx, tx, operation); err != nil {
		return nil, err
	}
	if _, response, replayed, lookupErr := lookupOperationTx(ctx, tx, operation); lookupErr != nil {
		return nil, lookupErr
	} else if replayed {
		var cached domain.CarpoolPayment
		if err = replayOperationResponse(response, &cached); err != nil {
			return nil, err
		}
		return &cached, nil
	}
	if _, err = getTermTx(ctx, tx, termID); err != nil {
		return nil, err
	}
	p, err = createPaymentTx(ctx, tx, termID, actorID, operationRequestID(operation), operation.Fingerprint, input)
	if err != nil {
		return nil, err
	}
	if err = recordOperationTx(ctx, tx, operation, "payment", p.ID, p); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return p, nil
}

const paymentSelect = `SELECT id,term_id,amount_cny::text,payment_kind,paid_at,channel,external_order_no,recorded_by,notes,recorded_at FROM carpool_payments`

func scanPayment(row carpoolRowScanner) (*domain.CarpoolPayment, error) {
	var p domain.CarpoolPayment
	var amount string
	if err := row.Scan(&p.ID, &p.TermID, &amount, &p.PaymentKind, &p.PaidAt, &p.Channel, &p.ExternalOrderNo, &p.RecordedBy, &p.Notes, &p.RecordedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, service.ErrCarpoolNotFound
		}
		return nil, err
	}
	var err error
	p.AmountCNY, err = decimal.NewFromString(amount)
	return &p, err
}

func listTermPaymentsTx(ctx context.Context, tx *sql.Tx, termID int64) ([]domain.CarpoolPayment, error) {
	rows, err := tx.QueryContext(ctx, paymentSelect+` WHERE term_id=$1 ORDER BY paid_at DESC,id DESC`, termID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.CarpoolPayment, 0)
	for rows.Next() {
		payment, scanErr := scanPayment(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, *payment)
	}
	return out, rows.Err()
}

func (r *CarpoolRepository) ListPayments(ctx context.Context, termID int64, page, pageSize int) ([]domain.CarpoolPayment, int64, error) {
	normalizePage(&page, &pageSize)
	var total int64
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM carpool_payments WHERE term_id=$1`, termID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.db.QueryContext(ctx, paymentSelect+` WHERE term_id=$1 ORDER BY paid_at DESC,id DESC LIMIT $2 OFFSET $3`, termID, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]domain.CarpoolPayment, 0)
	for rows.Next() {
		p, e := scanPayment(rows)
		if e != nil {
			return nil, 0, e
		}
		out = append(out, *p)
	}
	return out, total, rows.Err()
}

func (r *CarpoolRepository) ClaimBoost(ctx context.Context, userID int64, key, fingerprint string) (result *domain.CarpoolBoostResult, err error) {
	for attempt := 0; attempt < 4; attempt++ {
		result, err = r.claimBoostOnce(ctx, userID, key, fingerprint)
		if !errors.Is(err, errCarpoolCycleBoundaryMoved) {
			return result, err
		}
	}
	return nil, service.ErrCarpoolUnavailable
}

func (r *CarpoolRepository) claimBoostOnce(ctx context.Context, userID int64, key, fingerprint string) (result *domain.CarpoolBoostResult, err error) {
	op := domain.CarpoolOperation{Kind: "boost", ActorID: userID, Key: key, Fingerprint: fingerprint}
	if err = validateOperation(op, "boost"); err != nil {
		return nil, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err = lockOperationTx(ctx, tx, op); err != nil {
		return nil, err
	}
	if _, response, replayed, lookupErr := lookupOperationTx(ctx, tx, op); lookupErr != nil {
		return nil, lookupErr
	} else if replayed {
		var cached domain.CarpoolBoostResult
		if e := json.Unmarshal(response, &cached); e != nil {
			return nil, e
		}
		return &cached, nil
	}
	var termID int64
	var snapshotJSON string
	var status string
	var used int
	var startsAt, expiresAt time.Time
	for attempt := 0; attempt < 2; attempt++ {
		var selectAt time.Time
		if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&selectAt); err != nil {
			return nil, err
		}
		err = tx.QueryRowContext(ctx, `SELECT id,plan_snapshot::text,boost_used,status,starts_at,expires_at FROM carpool_terms WHERE user_id=$1 AND scope_id=$2 AND status IN ('pending','active') ORDER BY CASE WHEN starts_at <= $3 AND expires_at > $3 THEN 0 WHEN starts_at > $3 THEN 1 ELSE 2 END,CASE WHEN starts_at > $3 THEN starts_at END ASC,starts_at DESC LIMIT 1 FOR UPDATE`, userID, domain.CarpoolGlobalScopeID, selectAt).Scan(&termID, &snapshotJSON, &used, &status, &startsAt, &expiresAt)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, service.ErrCarpoolUnavailable
		}
		if err != nil {
			return nil, err
		}
		var lockedAt time.Time
		if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&lockedAt); err != nil {
			return nil, err
		}
		if status != domain.CarpoolTermTerminated && !lockedAt.Before(startsAt) && lockedAt.Before(expiresAt) {
			break
		}
		termID = 0
	}
	if termID == 0 {
		return nil, service.ErrCarpoolUnavailable
	}
	var now time.Time
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return nil, err
	}
	if now.Before(startsAt) || !now.Before(expiresAt) {
		return nil, service.ErrCarpoolUnavailable
	}
	var snapshot domain.CarpoolPlanSnapshot
	if err = json.Unmarshal([]byte(snapshotJSON), &snapshot); err != nil {
		return nil, err
	}
	if used >= snapshot.BoostCount {
		return nil, service.ErrCarpoolBoostExhausted
	}
	cycle, err := ensureCurrentCycleTx(ctx, tx, termID, now)
	if err != nil {
		return nil, err
	}
	if cycle.State != domain.CarpoolCycleActive {
		return nil, service.ErrCarpoolUnavailable
	}
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return nil, err
	}
	if now.Before(startsAt) || !now.Before(expiresAt) {
		return nil, service.ErrCarpoolUnavailable
	}
	if now.Before(cycle.StartsAt) || !now.Before(cycle.EndsAt) {
		return nil, errCarpoolCycleBoundaryMoved
	}
	slot := used + 1
	requestID := operationRequestID(op)
	eventKey := fmt.Sprintf("boost:%d:%d", termID, slot)
	if _, err = applyGrantTx(ctx, tx, userID, termID, cycle.ID, domain.CarpoolBucketBoost, snapshot.BoostAmountUSD, "boost", eventKey, &requestID, &slot, &userID, nil, "member boost", now); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE carpool_terms SET boost_used=$1,updated_at=NOW() WHERE id=$2`, slot, termID); err != nil {
		return nil, err
	}
	result = &domain.CarpoolBoostResult{Eligible: slot < snapshot.BoostCount, Remaining: snapshot.BoostCount - slot, Total: snapshot.BoostCount, AmountUSD: snapshot.BoostAmountUSD, HelpText: boostHelpText(snapshot)}
	if result.Remaining == 0 {
		reason := "boosts_exhausted"
		result.UnavailableReason = &reason
	}
	if err = recordOperationTx(ctx, tx, op, "term", termID, result); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}

func boostHelpText(snapshot domain.CarpoolPlanSnapshot) string {
	return fmt.Sprintf("This %d-day term includes %d boosts. Boost quota expires with the current cycle.", snapshot.DurationDays, snapshot.BoostCount)
}

func (r *CarpoolRepository) GetBoostStatus(ctx context.Context, userID int64) (*domain.CarpoolBoostStatus, error) {
	var now time.Time
	if err := r.db.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return nil, err
	}
	var termID int64
	var snapshotJSON, status string
	var used int
	var startsAt, expiresAt time.Time
	err := r.db.QueryRowContext(ctx, `SELECT id,plan_snapshot::text,boost_used,status,starts_at,expires_at FROM carpool_terms WHERE user_id=$1 AND scope_id=$2 ORDER BY CASE WHEN starts_at <= $3 AND expires_at > $3 AND status <> 'terminated' THEN 0 WHEN starts_at > $3 AND status <> 'terminated' THEN 1 ELSE 2 END,CASE WHEN starts_at > $3 THEN starts_at END ASC,starts_at DESC LIMIT 1`, userID, domain.CarpoolGlobalScopeID, now).Scan(&termID, &snapshotJSON, &used, &status, &startsAt, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return unavailableBoost("no_term"), nil
	}
	if err != nil {
		return nil, err
	}
	var snapshot domain.CarpoolPlanSnapshot
	if err = json.Unmarshal([]byte(snapshotJSON), &snapshot); err != nil {
		return nil, err
	}
	remaining := snapshot.BoostCount - used
	result := &domain.CarpoolBoostStatus{Eligible: true, Remaining: remaining, Total: snapshot.BoostCount, AmountUSD: snapshot.BoostAmountUSD, HelpText: boostHelpText(snapshot)}
	if status == domain.CarpoolTermTerminated {
		return unavailableBoostWithSnapshot("term_terminated", snapshot, remaining), nil
	}
	if now.Before(startsAt) {
		return unavailableBoostWithSnapshot("term_not_started", snapshot, remaining), nil
	}
	if !now.Before(expiresAt) || status == domain.CarpoolTermExpired {
		return unavailableBoostWithSnapshot("term_expired", snapshot, remaining), nil
	}
	if remaining <= 0 {
		return unavailableBoostWithSnapshot("boosts_exhausted", snapshot, 0), nil
	}
	cycle, ensureErr := r.EnsureCurrentCycle(ctx, termID, now)
	if errors.Is(ensureErr, service.ErrCarpoolUnavailable) || errors.Is(ensureErr, service.ErrCarpoolNotFound) {
		return unavailableBoostWithSnapshot("no_active_cycle", snapshot, remaining), nil
	}
	if ensureErr != nil {
		return nil, ensureErr
	}
	if cycle == nil || cycle.State != domain.CarpoolCycleActive {
		return unavailableBoostWithSnapshot("no_active_cycle", snapshot, remaining), nil
	}
	return result, nil
}

func unavailableBoost(reason string) *domain.CarpoolBoostStatus {
	return &domain.CarpoolBoostStatus{Eligible: false, Remaining: 0, Total: 0, AmountUSD: decimal.Zero, HelpText: "", UnavailableReason: &reason}
}
func unavailableBoostWithSnapshot(reason string, snapshot domain.CarpoolPlanSnapshot, remaining int) *domain.CarpoolBoostStatus {
	return &domain.CarpoolBoostStatus{Eligible: false, Remaining: remaining, Total: snapshot.BoostCount, AmountUSD: snapshot.BoostAmountUSD, HelpText: boostHelpText(snapshot), UnavailableReason: &reason}
}

func (r *CarpoolRepository) AdjustCycle(ctx context.Context, cycleID, actorID int64, bucket string, delta decimal.Decimal, reason string, reversesID *int64, operation domain.CarpoolOperation) (entries []domain.CarpoolLedgerEntry, err error) {
	if err = validateOperation(operation, "adjustment"); err != nil {
		return nil, err
	}
	if actorID != operation.ActorID {
		return nil, fmt.Errorf("carpool operation actor mismatch")
	}
	delta = delta.Round(8)
	if delta.IsZero() || strings.TrimSpace(reason) == "" {
		return nil, fmt.Errorf("non-zero adjustment and reason are required")
	}
	column := map[string]string{domain.CarpoolBucketBase: "base_balance_usd", domain.CarpoolBucketBoost: "boost_balance_usd", domain.CarpoolBucketManual: "manual_balance_usd"}[bucket]
	if column == "" {
		return nil, fmt.Errorf("invalid bucket")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err = lockOperationTx(ctx, tx, operation); err != nil {
		return nil, err
	}
	requestID := operationRequestID(operation)
	if _, response, replayed, lookupErr := lookupOperationTx(ctx, tx, operation); lookupErr != nil {
		return nil, lookupErr
	} else if replayed {
		if err = replayOperationResponse(response, &entries); err != nil {
			return nil, err
		}
		return entries, nil
	}
	var now time.Time
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return nil, err
	}
	var termID int64
	if err = tx.QueryRowContext(ctx, `SELECT term_id FROM carpool_cycles WHERE id=$1`, cycleID).Scan(&termID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, service.ErrCarpoolNotFound
		}
		return nil, err
	}
	var userID int64
	if err = tx.QueryRowContext(ctx, `SELECT user_id FROM carpool_terms WHERE id=$1 FOR UPDATE`, termID).Scan(&userID); err != nil {
		return nil, err
	}
	cycle, err := scanCarpoolCycle(tx.QueryRowContext(ctx, cycleSelect+` WHERE id=$1 AND term_id=$2 FOR UPDATE`, cycleID, termID))
	if err != nil {
		return nil, err
	}
	if reversesID != nil {
		var originalCycle int64
		var originalBucket, originalDelta string
		var originalReversal *int64
		if err = tx.QueryRowContext(ctx, `SELECT cycle_id,bucket,delta_usd::text,reverses_ledger_id FROM carpool_ledger WHERE id=$1 FOR UPDATE`, *reversesID).Scan(&originalCycle, &originalBucket, &originalDelta, &originalReversal); err != nil {
			return nil, err
		}
		parsed, parseErr := decimal.NewFromString(originalDelta)
		if parseErr != nil {
			return nil, parseErr
		}
		if originalCycle != cycleID || originalBucket != bucket || !delta.Equal(parsed.Neg()) {
			return nil, fmt.Errorf("adjustment reversal must exactly reverse the referenced entry")
		}
		if originalReversal != nil {
			return nil, fmt.Errorf("reversing a reversal is not supported")
		}
		var existing int64
		err = tx.QueryRowContext(ctx, `SELECT id FROM carpool_ledger WHERE reverses_ledger_id=$1 LIMIT 1`, *reversesID).Scan(&existing)
		if err == nil {
			return nil, service.ErrCarpoolIdempotencyConflict
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}
	eventKey := "adjustment:" + requestID + ":" + bucket
	if delta.IsPositive() {
		_, err = applyGrantTx(ctx, tx, userID, cycle.TermID, cycleID, bucket, delta.Round(8), "adjustment", eventKey, &requestID, nil, &actorID, reversesID, reason, now)
	} else {
		amount := delta.Abs().Round(8)
		targetDebit, baseDebit := amount, decimal.Zero
		if bucket == domain.CarpoolBucketBase {
			if _, err = tx.ExecContext(ctx, `UPDATE carpool_cycles SET base_balance_usd=base_balance_usd-$1,updated_at=NOW(),revision=revision+1 WHERE id=$2`, amount.StringFixed(8), cycleID); err == nil {
				err = insertLedgerTx(ctx, tx, userID, cycle.TermID, cycleID, "adjustment", bucket, amount.Neg(), eventKey, &requestID, nil, nil, nil, &actorID, reversesID, reason, now)
			}
		} else {
			balance := cycle.BoostBalanceUSD
			if bucket == domain.CarpoolBucketManual {
				balance = cycle.ManualBalanceUSD
			}
			targetDebit = decimal.Min(decimal.Max(balance, decimal.Zero), amount)
			baseDebit = amount.Sub(targetDebit)
			if _, err = tx.ExecContext(ctx, fmt.Sprintf(`UPDATE carpool_cycles SET %s=%s-$1,base_balance_usd=base_balance_usd-$2,updated_at=NOW(),revision=revision+1 WHERE id=$3`, column, column), targetDebit.StringFixed(8), baseDebit.StringFixed(8), cycleID); err == nil {
				linked := reversesID
				if targetDebit.IsPositive() {
					err = insertLedgerTx(ctx, tx, userID, cycle.TermID, cycleID, "adjustment", bucket, targetDebit.Neg(), eventKey, &requestID, nil, nil, nil, &actorID, linked, reason, now)
					linked = nil
				}
				if err == nil && baseDebit.IsPositive() {
					err = insertLedgerTx(ctx, tx, userID, cycle.TermID, cycleID, "adjustment", domain.CarpoolBucketBase, baseDebit.Neg(), eventKey+":debt", &requestID, nil, nil, nil, &actorID, linked, reason, now)
				}
			}
		}
	}
	if err != nil {
		return nil, err
	}
	entries, err = listLedgerByRequestTx(ctx, tx, requestID)
	if err != nil {
		return nil, err
	}
	if err = recordOperationTx(ctx, tx, operation, "cycle", cycleID, entries); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return entries, nil
}

func listLedgerByRequestTx(ctx context.Context, tx *sql.Tx, requestID string) ([]domain.CarpoolLedgerEntry, error) {
	rows, err := tx.QueryContext(ctx, ledgerSelect+` WHERE request_id=$1 ORDER BY id`, requestID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanLedgerRows(rows)
}

func normalizePage(page, pageSize *int) {
	if *page < 1 {
		*page = 1
	}
	if *pageSize < 1 {
		*pageSize = 20
	}
	if *pageSize > 200 {
		*pageSize = 200
	}
}

func (r *CarpoolRepository) MaintainCycles(ctx context.Context, now time.Time, limit int) error {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	rows, err := r.db.QueryContext(ctx, `SELECT t.id FROM carpool_terms t WHERE (t.status='pending' AND t.starts_at <= $1) OR (t.status='active' AND (t.expires_at <= $1 OR EXISTS (SELECT 1 FROM carpool_cycles c WHERE c.term_id=t.id AND ((c.state='scheduled' AND c.starts_at <= $1) OR (c.state='active' AND c.ends_at <= $1))))) ORDER BY t.starts_at,t.id LIMIT $2`, now, limit)
	if err != nil {
		return err
	}
	var termIDs []int64
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			_ = rows.Close()
			return err
		}
		termIDs = append(termIDs, id)
	}
	if err = rows.Close(); err != nil {
		return err
	}
	for _, termID := range termIDs {
		_, ensureErr := r.EnsureCurrentCycle(ctx, termID, now)
		if ensureErr != nil && !errors.Is(ensureErr, service.ErrCarpoolUnavailable) {
			return ensureErr
		}
	}
	closingRows, err := r.db.QueryContext(ctx, `SELECT c.id FROM carpool_cycles c WHERE c.state='closing' AND NOT EXISTS (SELECT 1 FROM carpool_billing_requests b WHERE b.cycle_id=c.id AND b.status IN ('admitted','usage_known','settling','reconcile_required')) ORDER BY c.term_id,c.id LIMIT $1`, limit)
	if err != nil {
		return err
	}
	var cycleIDs []int64
	for closingRows.Next() {
		var id int64
		if err = closingRows.Scan(&id); err != nil {
			_ = closingRows.Close()
			return err
		}
		cycleIDs = append(cycleIDs, id)
	}
	if err = closingRows.Close(); err != nil {
		return err
	}
	for _, cycleID := range cycleIDs {
		if err = closeCarpoolCycle(ctx, r.db, cycleID, now); err != nil {
			return err
		}
	}
	return nil
}

func closeCarpoolCycle(ctx context.Context, db *sql.DB, cycleID int64, now time.Time) (err error) {
	var termID int64
	if err = db.QueryRowContext(ctx, `SELECT term_id FROM carpool_cycles WHERE id=$1`, cycleID).Scan(&termID); err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var userID int64
	if err = tx.QueryRowContext(ctx, `SELECT user_id FROM carpool_terms WHERE id=$1 FOR UPDATE`, termID).Scan(&userID); err != nil {
		return err
	}
	cycle, err := scanCarpoolCycle(tx.QueryRowContext(ctx, cycleSelect+` WHERE id=$1 AND term_id=$2 FOR UPDATE`, cycleID, termID))
	if err != nil {
		return err
	}
	if cycle.State != domain.CarpoolCycleClosing {
		return tx.Commit()
	}
	var unresolved int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM carpool_billing_requests WHERE cycle_id=$1 AND status IN ('admitted','usage_known','settling','reconcile_required')`, cycleID).Scan(&unresolved); err != nil {
		return err
	}
	if unresolved > 0 {
		return tx.Commit()
	}
	base, boost, manual := cycle.BaseBalanceUSD, cycle.BoostBalanceUSD, cycle.ManualBalanceUSD
	for _, source := range []struct {
		name  string
		value *decimal.Decimal
	}{{domain.CarpoolBucketBoost, &boost}, {domain.CarpoolBucketManual, &manual}} {
		if base.IsNegative() && source.value.IsPositive() {
			offset := decimal.Min(base.Abs(), *source.value)
			base = base.Add(offset)
			*source.value = source.value.Sub(offset)
			prefix := fmt.Sprintf("cycle_close:%d:offset:%s", cycleID, source.name)
			if err = insertLedgerTx(ctx, tx, userID, termID, cycleID, "debt_offset", source.name, offset.Neg(), prefix+":source", nil, nil, nil, nil, nil, nil, "normalize buckets before cycle close", now); err != nil {
				return err
			}
			if err = insertLedgerTx(ctx, tx, userID, termID, cycleID, "debt_offset", domain.CarpoolBucketBase, offset, prefix+":base", nil, nil, nil, nil, nil, nil, "normalize buckets before cycle close", now); err != nil {
				return err
			}
		}
	}
	for _, part := range []struct {
		name  string
		value *decimal.Decimal
	}{{domain.CarpoolBucketBase, &base}, {domain.CarpoolBucketBoost, &boost}, {domain.CarpoolBucketManual, &manual}} {
		if part.value.IsPositive() {
			amount := part.value.Neg()
			if err = insertLedgerTx(ctx, tx, userID, termID, cycleID, "expiry", part.name, amount, fmt.Sprintf("cycle_expiry:%d:%s", cycleID, part.name), nil, nil, nil, nil, nil, nil, "unused quota expired at cycle close", now); err != nil {
				return err
			}
			*part.value = decimal.Zero
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE carpool_cycles SET base_balance_usd=$1,boost_balance_usd=$2,manual_balance_usd=$3,state='closed',closed_at=$4,updated_at=NOW(),revision=revision+1 WHERE id=$5`, base.StringFixed(8), boost.StringFixed(8), manual.StringFixed(8), now, cycleID); err != nil {
		return err
	}
	return tx.Commit()
}
