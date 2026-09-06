package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/internal/util/logredact"
	"github.com/shopspring/decimal"
)

const ledgerSelect = `SELECT id,user_id,term_id,cycle_id,event_type,bucket,delta_usd::text,event_key,request_id,api_key_id,reset_batch_id,boost_slot,actor_id,reverses_ledger_id,reason,effective_at,recorded_at FROM carpool_ledger`

func scanLedgerRows(rows *sql.Rows) ([]domain.CarpoolLedgerEntry, error) {
	out := make([]domain.CarpoolLedgerEntry, 0)
	for rows.Next() {
		var entry domain.CarpoolLedgerEntry
		var delta string
		if err := rows.Scan(&entry.ID, &entry.UserID, &entry.TermID, &entry.CycleID, &entry.EventType, &entry.Bucket, &delta, &entry.EventKey, &entry.RequestID, &entry.APIKeyID, &entry.ResetBatchID, &entry.BoostSlot, &entry.ActorID, &entry.ReversesLedgerID, &entry.Reason, &entry.EffectiveAt, &entry.RecordedAt); err != nil {
			return nil, err
		}
		var err error
		entry.DeltaUSD, err = decimal.NewFromString(delta)
		if err != nil {
			return nil, err
		}
		out = append(out, entry)
	}
	return out, rows.Err()
}

func (r *CarpoolRepository) GetUserDetails(ctx context.Context, userID int64) (*domain.CarpoolUserDetails, error) {
	details := &domain.CarpoolUserDetails{Timezone: domain.CarpoolTimezone, ResetWindow: domain.CarpoolUserResetWindow{Status: "none"}}
	var used string
	err := r.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(CASE WHEN l.event_type IN ('usage','takeover_historical_usage') AND l.delta_usd < 0 THEN -l.delta_usd WHEN l.reverses_ledger_id IS NOT NULL AND l.delta_usd > 0 AND reversed.event_type IN ('usage','takeover_historical_usage') AND reversed.delta_usd < 0 THEN -l.delta_usd ELSE 0 END),0)::text,COALESCE(BOOL_AND(t.history_complete),TRUE),MIN(t.statistics_since) FROM carpool_terms t LEFT JOIN carpool_ledger l ON l.term_id=t.id LEFT JOIN carpool_ledger reversed ON reversed.id=l.reverses_ledger_id WHERE t.user_id=$1`, userID).Scan(&used, &details.Usage.HistoryComplete, &details.Usage.StatisticsSince)
	if err != nil {
		return nil, err
	}
	details.Usage.TotalUsedUSD, err = decimal.NewFromString(used)
	if err != nil {
		return nil, err
	}
	var term *domain.CarpoolTerm
	var effectiveStatus string
	selected := false
	for attempt := 0; attempt < 4; attempt++ {
		if err = r.db.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&details.ServerNow); err != nil {
			return nil, err
		}
		term, err = scanCarpoolTerm(r.db.QueryRowContext(ctx, termSelect+` WHERE user_id=$1 ORDER BY CASE WHEN starts_at <= $2 AND expires_at > $2 AND status <> 'terminated' THEN 0 WHEN starts_at > $2 AND status <> 'terminated' THEN 1 ELSE 2 END,CASE WHEN starts_at > $2 AND status <> 'terminated' THEN starts_at END ASC,starts_at DESC LIMIT 1`, userID, details.ServerNow))
		if errors.Is(err, service.ErrCarpoolNotFound) {
			return details, nil
		}
		if err != nil {
			return nil, err
		}
		effectiveStatus = term.Status
		if term.Status != domain.CarpoolTermTerminated {
			switch {
			case details.ServerNow.Before(term.StartsAt):
				effectiveStatus = domain.CarpoolTermPending
			case !details.ServerNow.Before(term.ExpiresAt):
				effectiveStatus = domain.CarpoolTermExpired
			default:
				effectiveStatus = domain.CarpoolTermActive
			}
		}
		if effectiveStatus != domain.CarpoolTermActive {
			selected = true
			break
		}

		_, details.ServerNow, err = r.ensureCurrentCycleForUserDetails(ctx, term.ID)
		if errors.Is(err, errCarpoolCycleBoundaryMoved) || errors.Is(err, service.ErrCarpoolUnavailable) {
			continue
		}
		if err != nil {
			return nil, err
		}
		term, err = r.GetTerm(ctx, term.ID)
		if err != nil {
			return nil, err
		}
		if term.Status == domain.CarpoolTermTerminated || term.Status == domain.CarpoolTermExpired || details.ServerNow.Before(term.StartsAt) || !details.ServerNow.Before(term.ExpiresAt) {
			continue
		}
		effectiveStatus = domain.CarpoolTermActive
		selected = true
		break
	}
	if !selected {
		return nil, fmt.Errorf("read carpool user details: %w", errCarpoolCycleBoundaryMoved)
	}
	cycles, err := r.ListTermCycles(ctx, term.ID)
	if err != nil {
		return nil, err
	}
	userTerm := &domain.CarpoolUserTerm{Status: effectiveStatus, StartsAt: term.StartsAt, ExpiresAt: term.ExpiresAt, ResetCountBasis: "current_term", ResetEvents: make([]domain.CarpoolUserResetEvent, 0), Cycles: make([]domain.CarpoolUserCycle, 0, len(cycles))}
	for _, cycle := range cycles {
		userTerm.Cycles = append(userTerm.Cycles, domain.CarpoolUserCycle{CycleNo: cycle.CycleNo, StartsAt: cycle.StartsAt, EndsAt: cycle.EndsAt, Status: cycle.State})
		if effectiveStatus == domain.CarpoolTermActive && cycle.State == domain.CarpoolCycleActive && !details.ServerNow.Before(cycle.StartsAt) && details.ServerNow.Before(cycle.EndsAt) {
			n := cycle.CycleNo
			userTerm.CurrentCycleNo = &n
			details.Quota = &domain.CarpoolUserQuota{AvailableUSD: cycle.AvailableUSD()}
		}
	}
	resetRows, err := r.db.QueryContext(ctx, `
		SELECT cycle.cycle_no,target.executed_at,cycle.base_quota_usd::text
		FROM carpool_reset_targets target
		JOIN carpool_reset_batches batch ON batch.id=target.batch_id AND batch.status='completed'
		JOIN carpool_terms term ON term.id=target.term_id AND term.user_id=$2
		JOIN carpool_cycles cycle ON cycle.id=target.cycle_id AND cycle.term_id=term.id
		WHERE target.term_id=$1 AND target.status='succeeded'
		ORDER BY target.executed_at ASC,target.batch_id ASC,target.id ASC`, term.ID, userID)
	if err != nil {
		return nil, err
	}
	defer resetRows.Close()
	for resetRows.Next() {
		var event domain.CarpoolUserResetEvent
		var targetQuota string
		if err = resetRows.Scan(&event.CycleNo, &event.OccurredAt, &targetQuota); err != nil {
			return nil, err
		}
		event.TargetQuotaUSD, err = decimal.NewFromString(targetQuota)
		if err != nil {
			return nil, err
		}
		userTerm.ResetEvents = append(userTerm.ResetEvents, event)
	}
	if err = resetRows.Err(); err != nil {
		return nil, err
	}
	userTerm.ResetCount = len(userTerm.ResetEvents)
	details.Term = userTerm
	return details, nil
}

func (r *CarpoolRepository) ensureCurrentCycleForUserDetails(ctx context.Context, termID int64) (*domain.CarpoolCycle, time.Time, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, time.Time{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var lockedTermID int64
	if err = tx.QueryRowContext(ctx, `SELECT id FROM carpool_terms WHERE id=$1 FOR UPDATE`, termID).Scan(&lockedTermID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, time.Time{}, service.ErrCarpoolNotFound
		}
		return nil, time.Time{}, err
	}
	var ensureAt time.Time
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&ensureAt); err != nil {
		return nil, time.Time{}, err
	}
	cycle, ensureErr := ensureCurrentCycleTx(ctx, tx, termID, ensureAt)
	if ensureErr != nil && !errors.Is(ensureErr, service.ErrCarpoolUnavailable) {
		return nil, time.Time{}, ensureErr
	}
	var projectionAt time.Time
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&projectionAt); err != nil {
		return nil, time.Time{}, err
	}
	if cycle != nil && (projectionAt.Before(cycle.StartsAt) || !projectionAt.Before(cycle.EndsAt)) {
		return nil, projectionAt, errCarpoolCycleBoundaryMoved
	}
	if err = tx.Commit(); err != nil {
		return nil, time.Time{}, err
	}
	return cycle, projectionAt, ensureErr
}

func appendFilter(where *[]string, args *[]any, expression string, value any) {
	*args = append(*args, value)
	*where = append(*where, fmt.Sprintf(expression, len(*args)))
}

func (r *CarpoolRepository) ListAdminTerms(ctx context.Context, filters domain.CarpoolTermFilters) ([]domain.CarpoolAdminTerm, int64, error) {
	normalizePage(&filters.Page, &filters.PageSize)
	where := []string{"1=1"}
	args := []any{}
	if filters.UserID != nil {
		appendFilter(&where, &args, "t.user_id=$%d", *filters.UserID)
	}
	if filters.PlanID != nil {
		appendFilter(&where, &args, "t.plan_id=$%d", *filters.PlanID)
	}
	if filters.Status != "" {
		appendFilter(&where, &args, "t.status=$%d", filters.Status)
	}
	if filters.StartsFrom != nil {
		appendFilter(&where, &args, "t.starts_at >= $%d", *filters.StartsFrom)
	}
	if filters.StartsTo != nil {
		appendFilter(&where, &args, "t.starts_at < $%d", *filters.StartsTo)
	}
	predicate := strings.Join(where, " AND ")
	var total int64
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM carpool_terms t WHERE `+predicate, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, filters.PageSize, (filters.Page-1)*filters.PageSize)
	query := `SELECT t.id,t.user_id,t.scope_id,t.group_id,t.plan_id,t.plan_snapshot::text,t.starts_at,t.expires_at,t.status,t.boost_used,t.history_complete,t.statistics_since,COALESCE(SUM(CASE WHEN p.payment_kind='payment' THEN p.amount_cny ELSE -p.amount_cny END),0)::text,t.created_at FROM carpool_terms t LEFT JOIN carpool_payments p ON p.term_id=t.id WHERE ` + predicate + fmt.Sprintf(` GROUP BY t.id ORDER BY t.starts_at DESC,t.id DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args))
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	out := make([]domain.CarpoolAdminTerm, 0)
	for rows.Next() {
		var item domain.CarpoolAdminTerm
		var snapshot, payment string
		if err = rows.Scan(&item.ID, &item.UserID, &item.ScopeID, &item.GroupID, &item.PlanID, &snapshot, &item.StartsAt, &item.ExpiresAt, &item.Status, &item.BoostUsed, &item.HistoryComplete, &item.StatisticsSince, &payment, &item.CreatedAt); err != nil {
			return nil, 0, err
		}
		if err = json.Unmarshal([]byte(snapshot), &item.PlanSnapshot); err != nil {
			return nil, 0, err
		}
		item.BoostRemaining = item.PlanSnapshot.BoostCount - item.BoostUsed
		item.PaymentNetCNY, err = decimal.NewFromString(payment)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, item)
	}
	if err = rows.Err(); err != nil {
		_ = rows.Close()
		return nil, 0, err
	}
	if err = rows.Close(); err != nil {
		return nil, 0, err
	}
	for i := range out {
		termID := out[i].ID
		active := domain.CarpoolCycleActive
		cycles, _, listErr := r.ListAdminCycles(ctx, domain.CarpoolCycleFilters{Page: 1, PageSize: 1, TermID: &termID, State: active})
		if listErr != nil {
			return nil, 0, listErr
		}
		if len(cycles) == 1 {
			current := cycles[0]
			out[i].CurrentCycle = &current
		}
	}
	return out, total, nil
}

func (r *CarpoolRepository) ListAdminCycles(ctx context.Context, filters domain.CarpoolCycleFilters) ([]domain.CarpoolAdminCycle, int64, error) {
	normalizePage(&filters.Page, &filters.PageSize)
	where := []string{"1=1"}
	args := []any{}
	if filters.UserID != nil {
		appendFilter(&where, &args, "t.user_id=$%d", *filters.UserID)
	}
	if filters.TermID != nil {
		appendFilter(&where, &args, "c.term_id=$%d", *filters.TermID)
	}
	if filters.CycleNo != nil {
		appendFilter(&where, &args, "c.cycle_no=$%d", *filters.CycleNo)
	}
	if filters.State != "" {
		appendFilter(&where, &args, "c.state=$%d", filters.State)
	}
	if filters.StartsFrom != nil {
		appendFilter(&where, &args, "c.starts_at >= $%d", *filters.StartsFrom)
	}
	if filters.StartsTo != nil {
		appendFilter(&where, &args, "c.starts_at < $%d", *filters.StartsTo)
	}
	predicate := strings.Join(where, " AND ")
	var total int64
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM carpool_cycles c JOIN carpool_terms t ON t.id=c.term_id WHERE `+predicate, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, filters.PageSize, (filters.Page-1)*filters.PageSize)
	rows, err := r.db.QueryContext(ctx, `SELECT c.id,c.term_id,t.user_id,c.cycle_no,c.starts_at,c.ends_at,c.base_quota_usd::text,c.base_balance_usd::text,c.boost_balance_usd::text,c.manual_balance_usd::text,GREATEST(0,c.base_balance_usd+c.boost_balance_usd+c.manual_balance_usd)::text,a.initial_granted_usd::text,a.reset_granted_usd::text,a.boost_granted_usd::text,a.adjustment_net_usd::text,a.used_usd::text,a.expired_usd::text,c.state,c.revision FROM carpool_cycles c JOIN carpool_terms t ON t.id=c.term_id LEFT JOIN LATERAL (SELECT COALESCE(SUM(CASE WHEN l.event_type IN ('cycle_initial','takeover_opening','takeover_transfer_funded') AND l.delta_usd > 0 THEN l.delta_usd ELSE 0 END),0) AS initial_granted_usd,COALESCE(SUM(CASE WHEN l.event_type='reset' AND l.delta_usd > 0 THEN l.delta_usd ELSE 0 END),0) AS reset_granted_usd,COALESCE(SUM(CASE WHEN l.event_type='boost' AND l.delta_usd > 0 THEN l.delta_usd ELSE 0 END),0) AS boost_granted_usd,COALESCE(SUM(CASE WHEN l.event_type='adjustment' THEN l.delta_usd ELSE 0 END),0) AS adjustment_net_usd,COALESCE(SUM(CASE WHEN l.event_type='usage' AND l.delta_usd < 0 THEN -l.delta_usd WHEN l.reverses_ledger_id IS NOT NULL AND l.delta_usd > 0 AND reversed.event_type='usage' AND reversed.delta_usd < 0 THEN -l.delta_usd ELSE 0 END),0) AS used_usd,COALESCE(SUM(CASE WHEN l.event_type='expiry' AND l.delta_usd < 0 THEN -l.delta_usd ELSE 0 END),0) AS expired_usd FROM carpool_ledger l LEFT JOIN carpool_ledger reversed ON reversed.id=l.reverses_ledger_id WHERE l.cycle_id=c.id) a ON TRUE WHERE `+predicate+fmt.Sprintf(` ORDER BY c.starts_at DESC,c.id DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]domain.CarpoolAdminCycle, 0)
	for rows.Next() {
		var item domain.CarpoolAdminCycle
		var quota, base, boost, manual, available, initial, reset, boostGranted, adjustment, used, expired string
		if err = rows.Scan(&item.ID, &item.TermID, &item.UserID, &item.CycleNo, &item.StartsAt, &item.EndsAt, &quota, &base, &boost, &manual, &available, &initial, &reset, &boostGranted, &adjustment, &used, &expired, &item.State, &item.Revision); err != nil {
			return nil, 0, err
		}
		for target, raw := range map[*decimal.Decimal]string{&item.BaseQuotaUSD: quota, &item.BaseBalanceUSD: base, &item.BoostBalanceUSD: boost, &item.ManualBalanceUSD: manual, &item.AvailableUSD: available, &item.InitialGrantedUSD: initial, &item.ResetGrantedUSD: reset, &item.BoostGrantedUSD: boostGranted, &item.AdjustmentNetUSD: adjustment, &item.UsedUSD: used, &item.ExpiredUSD: expired} {
			*target, err = decimal.NewFromString(raw)
			if err != nil {
				return nil, 0, err
			}
		}
		out = append(out, item)
	}
	return out, total, rows.Err()
}

func (r *CarpoolRepository) ListAdminLedger(ctx context.Context, filters domain.CarpoolLedgerFilters) ([]domain.CarpoolLedgerEntry, int64, error) {
	normalizePage(&filters.Page, &filters.PageSize)
	where := []string{"1=1"}
	args := []any{}
	if filters.UserID != nil {
		appendFilter(&where, &args, "user_id=$%d", *filters.UserID)
	}
	if filters.TermID != nil {
		appendFilter(&where, &args, "term_id=$%d", *filters.TermID)
	}
	if filters.CycleID != nil {
		appendFilter(&where, &args, "cycle_id=$%d", *filters.CycleID)
	}
	if filters.EventType != "" {
		appendFilter(&where, &args, "event_type=$%d", filters.EventType)
	}
	if filters.Bucket != "" {
		appendFilter(&where, &args, "bucket=$%d", filters.Bucket)
	}
	predicate := strings.Join(where, " AND ")
	var total int64
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM carpool_ledger WHERE `+predicate, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, filters.PageSize, (filters.Page-1)*filters.PageSize)
	rows, err := r.db.QueryContext(ctx, ledgerSelect+` WHERE `+predicate+fmt.Sprintf(` ORDER BY recorded_at DESC,id DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out, err := scanLedgerRows(rows)
	return out, total, err
}

func sanitizeBillingExceptionDiagnostic(raw string) *string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	category := carpoolBillingDiagnosticCategory(raw)
	return &category
}

func sanitizeBillingExceptionReason(raw string) *string {
	const maxRunes = 512
	redacted := strings.TrimSpace(logredact.RedactText(raw, "api_key", "apikey", "token", "secret", "key", "authorization"))
	if redacted == "" {
		return nil
	}
	runes := []rune(redacted)
	if len(runes) > maxRunes {
		redacted = string(runes[:maxRunes-3]) + "..."
	}
	return &redacted
}

func (r *CarpoolRepository) ListBillingExceptions(ctx context.Context, filters domain.CarpoolBillingExceptionFilters) ([]domain.CarpoolBillingException, int64, error) {
	normalizePage(&filters.Page, &filters.PageSize)
	status := strings.TrimSpace(filters.Status)
	where := []string{}
	if status == "settled" {
		where = append(where, "b.status='settled'", "b.resolution IS NOT NULL")
	} else {
		where = append(where, "b.status <> 'settled'", "(b.status='reconcile_required' OR b.last_error IS NOT NULL)")
	}
	args := []any{}
	if filters.UserID != nil {
		appendFilter(&where, &args, "b.user_id=$%d", *filters.UserID)
	}
	if filters.TermID != nil {
		appendFilter(&where, &args, "b.term_id=$%d", *filters.TermID)
	}
	if filters.CycleID != nil {
		appendFilter(&where, &args, "b.cycle_id=$%d", *filters.CycleID)
	}
	if status != "" && status != "settled" {
		appendFilter(&where, &args, "b.status=$%d", status)
	}
	predicate := strings.Join(where, " AND ")
	var total int64
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM carpool_billing_requests b WHERE `+predicate, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, filters.PageSize, (filters.Page-1)*filters.PageSize)
	rows, err := r.db.QueryContext(ctx, `SELECT b.id,b.request_id,b.api_key_id,b.user_id,b.group_id,b.term_id,b.cycle_id,b.admitted_at,b.status,b.actual_cost_usd::text,b.retry_count,b.last_error,b.updated_at,b.resolution,b.resolved_at,b.resolved_by,b.resolution_reason FROM carpool_billing_requests b WHERE `+predicate+fmt.Sprintf(` ORDER BY b.updated_at DESC,b.id DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]domain.CarpoolBillingException, 0)
	for rows.Next() {
		var item domain.CarpoolBillingException
		var knownCost, lastError, resolution, reason sql.NullString
		if err = rows.Scan(&item.ID, &item.RequestID, &item.APIKeyID, &item.UserID, &item.GroupID, &item.TermID, &item.CycleID, &item.AdmittedAt, &item.Status, &knownCost, &item.RetryCount, &lastError, &item.UpdatedAt, &resolution, &item.ResolvedAt, &item.ResolvedBy, &reason); err != nil {
			return nil, 0, err
		}
		if knownCost.Valid {
			cost, parseErr := decimal.NewFromString(knownCost.String)
			if parseErr != nil {
				return nil, 0, parseErr
			}
			item.KnownCostUSD = &cost
		}
		if lastError.Valid {
			item.SanitizedError = sanitizeBillingExceptionDiagnostic(lastError.String)
		}
		if resolution.Valid {
			item.Resolution = &resolution.String
			item.ActualCostUSD = item.KnownCostUSD
		}
		if reason.Valid {
			item.Reason = sanitizeBillingExceptionReason(reason.String)
		}
		out = append(out, item)
	}
	return out, total, rows.Err()
}
