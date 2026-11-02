package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/shopspring/decimal"
)

// This list follows the existing prepareUsageLogInsert argument contract. Term
// logs use its normalization but bypass best-effort batching and require a
// settled durable receipt before publishing a successful statistics row.
const subscriptionQuotaUsageLogColumns = `user_id,api_key_id,account_id,request_id,model,requested_model,upstream_model,upstream_response_model,upstream_model_mismatch,group_id,subscription_id,carpool_term_id,carpool_cycle_id,carpool_admitted_at,input_tokens,output_tokens,cache_creation_tokens,cache_read_tokens,cache_creation_5m_tokens,cache_creation_1h_tokens,image_output_tokens,image_output_cost,image_input_tokens,image_input_cost,input_cost,output_cost,cache_creation_cost,cache_read_cost,total_cost,actual_cost,rate_multiplier,account_rate_multiplier,billing_type,request_type,stream,openai_ws_mode,duration_ms,first_token_ms,user_agent,ip_address,image_count,image_size,image_input_size,image_output_size,image_size_source,image_size_breakdown,video_count,video_resolution,video_duration_seconds,service_tier,reasoning_effort,requested_reasoning_effort,inbound_endpoint,upstream_endpoint,cache_ttl_overridden,long_context_billing_applied,channel_id,model_mapping_chain,billing_tier,billing_mode,account_stats_cost,upstream_request_id,session_id,native_compaction_v2,created_at`

func (r *usageLogRepository) createSubscriptionQuotaUsageLog(ctx context.Context, log *service.UsageLog) (bool, error) {
	if r == nil || r.db == nil || log.SubscriptionTermID == nil || log.SubscriptionQuotaRequestID == nil {
		return false, service.ErrSubscriptionQuotaBillingUnavailable
	}
	// Discover immutable routing without a row lock, then follow the financial
	// scope order. An INSERT's FK checks must not take key/account locks before
	// waiting on the parent/term held by a simultaneous Apply.
	discovered, err := subscriptionQuotaScanRequest(r.db.QueryRowContext(ctx, `SELECT `+subscriptionQuotaRequestColumns+` FROM subscription_quota_requests WHERE id=$1`, *log.SubscriptionQuotaRequestID))
	if err != nil {
		return false, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	if err = lockDashboardAggregationWriter(ctx, tx); err != nil {
		return false, err
	}
	stored, err := subscriptionQuotaLockRequest(ctx, tx, discovered.known.Snapshot)
	if err != nil {
		return false, err
	}
	if stored.status != "settled" {
		return false, service.ErrSubscriptionQuotaReceiptConflict
	}
	command, err := service.DecodeSubscriptionQuotaUsageBillingReceipt(stored.known.BillingPayload)
	if err != nil {
		return false, err
	}
	if err = service.ValidateSubscriptionQuotaUsageLogPayload(stored.known.UsageLogPayload, command); err != nil {
		return false, err
	}
	if log.RequestID != stored.known.Snapshot.RequestID || log.UserID != stored.known.Snapshot.UserID || log.APIKeyID != stored.known.Snapshot.APIKeyID || log.AccountID != stored.known.Snapshot.AccountID || *log.SubscriptionTermID != stored.known.Snapshot.TermID {
		return false, service.ErrSubscriptionQuotaReceiptConflict
	}
	// A stale pending-list reader may resume after another worker acknowledged
	// publication and retention removed the log. The durable acknowledgement
	// closes publication permanently; never reconstruct an intentionally deleted row.
	if stored.logRecorded {
		return false, nil
	}
	canonicalLog, err := service.DecodeSubscriptionQuotaUsageLogPayload(stored.known.UsageLogPayload)
	if err != nil {
		return false, err
	}
	*log = *canonicalLog
	amounts, err := service.SubscriptionQuotaUsageLogExactAmounts(stored.known.UsageLogPayload)
	if err != nil {
		return false, err
	}
	prepared := prepareUsageLogInsert(log)
	if len(strings.Split(subscriptionQuotaUsageLogColumns, ",")) != len(prepared.args) {
		return false, fmt.Errorf("quota usage statistics column contract changed")
	}
	args := append(append([]any(nil), prepared.args...), *log.SubscriptionTermID, *log.SubscriptionQuotaRequestID)
	columns := strings.Split(subscriptionQuotaUsageLogColumns, ",")
	for index, column := range columns {
		if exact, ok := amounts[column]; ok {
			args[index] = exact
		}
	}
	placeholders := make([]string, len(args))
	for i := range placeholders {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
	}
	query := `INSERT INTO usage_logs (` + subscriptionQuotaUsageLogColumns + `,subscription_term_id,subscription_quota_request_id) VALUES (` + strings.Join(placeholders, ",") + `) ON CONFLICT(request_id,api_key_id) DO NOTHING RETURNING id,created_at`
	inserted := true
	err = tx.QueryRowContext(ctx, query, args...).Scan(&log.ID, &log.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		inserted = false
	} else if err != nil {
		return false, err
	}
	var actual decimal.Decimal
	var userID, accountID, groupID, parentID, termID, requestID int64
	var input, output, creation, read int
	err = tx.QueryRowContext(ctx, `SELECT id,created_at,user_id,account_id,group_id,subscription_id,subscription_term_id,subscription_quota_request_id,actual_cost::text,input_tokens,output_tokens,cache_creation_tokens,cache_read_tokens FROM usage_logs WHERE request_id=$1 AND api_key_id=$2`, log.RequestID, log.APIKeyID).Scan(&log.ID, &log.CreatedAt, &userID, &accountID, &groupID, &parentID, &termID, &requestID, &actual, &input, &output, &creation, &read)
	if err != nil {
		return false, err
	}
	s := command.SubscriptionTermSnapshot
	if userID != s.UserID || accountID != s.AccountID || groupID != s.GroupID || parentID != s.SubscriptionID || termID != s.TermID || requestID != s.RequestRowID || !actual.Equal(command.SubscriptionTermCost) || input != command.InputTokens || output != command.OutputTokens || creation != command.CacheCreationTokens || read != command.CacheReadTokens {
		return false, service.ErrSubscriptionQuotaReceiptConflict
	}
	if inserted {
		if err = subscriptionQuotaPublishHistoricalDashboardTx(ctx, tx, log, stored.known.UsageLogPayload); err != nil {
			return false, err
		}
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	return inserted, nil
}
