//go:build unit

package repository

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSubscriptionQuotaDashboardProtectedPartitionKeepsDropFastPath(t *testing.T) {
	for _, protected := range []bool{false, true} {
		t.Run(map[bool]string{true: "pending_term_keeps_partition", false: "unprotected_month_drops"}[protected], func(t *testing.T) {
			setGroupUsageRollupTestTimezone(t)
			db, mock := newSQLMock(t)
			month := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
			mock.ExpectBegin()
			mock.ExpectQuery(`SELECT id FROM usage_group_rollup_state.*FOR UPDATE`).
				WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
			mock.ExpectQuery(`(?s)SELECT.*status IN \('admitted','usage_known','reconcile_required'\).*status = 'settled'.*usage_log_recorded_at IS NULL.*OR EXISTS.*NOT EXISTS`).
				WithArgs(month, month.AddDate(0, 1, 0)).
				WillReturnRows(sqlmock.NewRows([]string{"protected"}).AddRow(protected))
			if !protected {
				mock.ExpectExec(`UPDATE usage_group_rollup_state`).WithArgs(month, "Asia/Shanghai").WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectExec(`DROP TABLE IF EXISTS "usage_logs_202606"`).WillReturnResult(sqlmock.NewResult(0, 0))
			}
			mock.ExpectCommit()
			require.NoError(t, dropUsageLogsPartitionWithRollupInvalidation(context.Background(), db, "usage_logs_202606", month))
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestSubscriptionQuotaDashboardBatchCleanupRequiresDurableAcknowledgement(t *testing.T) {
	db, mock := newSQLMock(t)
	cutoff := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT id FROM usage_group_rollup_state.*FOR UPDATE`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectQuery(`(?s)WITH victims AS.*SELECT tableoid, ctid.*subscription_quota_request_id IS NULL OR EXISTS.*qr.id = usage_logs.subscription_quota_request_id.*qr.usage_log_recorded_at IS NOT NULL.*ORDER BY.*DELETE FROM usage_logs.*WHERE \(tableoid, ctid\) IN \(SELECT tableoid, ctid FROM victims\).*subscription_quota_request_id IS NULL OR EXISTS.*qr.usage_log_recorded_at IS NOT NULL.*RETURNING created_at`).
		WithArgs(cutoff, usageLogsCleanupBatchSize).WillReturnRows(sqlmock.NewRows([]string{"created_at"}))
	mock.ExpectCommit()
	n, err := cleanupUsageLogsBatchWithRollupInvalidation(context.Background(), db, cutoff)
	require.NoError(t, err)
	require.Zero(t, n)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSubscriptionQuotaDashboardAggregateLocksBeforeSourceSnapshot(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := newDashboardAggregationRepositoryWithSQL(db)
	start := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	mock.ExpectBegin()
	mock.ExpectExec(`SELECT pg_advisory_xact_lock\(1936811121, 1\)`).WillReturnResult(sqlmock.NewResult(0, 1))
	for _, query := range []string{`INSERT INTO usage_dashboard_hourly_users`, `INSERT INTO usage_dashboard_daily_users`, `INSERT INTO usage_dashboard_hourly`, `INSERT INTO usage_dashboard_daily`} {
		mock.ExpectExec(query).WillReturnResult(sqlmock.NewResult(0, 1))
	}
	mock.ExpectCommit()
	require.NoError(t, repo.AggregateRange(context.Background(), start, start.Add(time.Minute)))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSubscriptionQuotaDashboardHistoricalPublicationUsesExactInsertedContribution(t *testing.T) {
	for _, published := range []bool{false, true} {
		t.Run(map[bool]string{true: "published_history_increment", false: "unpublished_recent_normal_scheduler"}[published], func(t *testing.T) {
			setGroupUsageRollupTestTimezone(t)
			db, mock := newSQLMock(t)
			requestID, termID := int64(11), int64(12)
			log := &service.UsageLog{ID: 10, CreatedAt: time.Date(2026, 6, 1, 3, 0, 0, 0, time.UTC), SubscriptionQuotaRequestID: &requestID, SubscriptionTermID: &termID}
			mock.ExpectBegin()
			tx, err := db.BeginTx(context.Background(), nil)
			require.NoError(t, err)
			mock.ExpectQuery(`(?s)SELECT EXISTS.*usage_logs l.*last_aggregated_at.*OR EXISTS.*usage_dashboard_hourly.*OR EXISTS.*usage_dashboard_daily`).
				WithArgs(log.ID, log.CreatedAt, requestID, termID, "Asia/Shanghai").
				WillReturnRows(sqlmock.NewRows([]string{"published"}).AddRow(published))
			if published {
				for _, table := range []string{"hourly", "daily"} {
					mock.ExpectExec(`(?s)WITH contribution AS.*COALESCE\(account_stats_cost,total_cost\)\*COALESCE\(account_rate_multiplier,1\).*new_user AS.*INSERT INTO usage_dashboard_`+table+`_users.*ON CONFLICT DO NOTHING RETURNING 1.*INSERT INTO usage_dashboard_`+table+` .*total_requests=usage_dashboard_`+table+`.total_requests\+EXCLUDED.total_requests.*active_users=usage_dashboard_`+table+`.active_users\+EXCLUDED.active_users`).
						WithArgs(log.ID, log.CreatedAt, "Asia/Shanghai").WillReturnResult(sqlmock.NewResult(0, 1))
				}
			}
			require.NoError(t, subscriptionQuotaPublishHistoricalDashboardTx(context.Background(), tx, log, nil))
			mock.ExpectRollback()
			require.NoError(t, tx.Rollback())
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestSubscriptionQuotaDashboardHistoricalPublicationFailureIsNotAcknowledged(t *testing.T) {
	db, mock := newSQLMock(t)
	mock.ExpectBegin()
	tx, err := db.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	requestID, termID := int64(11), int64(12)
	log := &service.UsageLog{ID: 10, SubscriptionQuotaRequestID: &requestID, SubscriptionTermID: &termID}
	mock.ExpectQuery(`SELECT EXISTS`).WillReturnRows(sqlmock.NewRows([]string{"published"}).AddRow(true))
	mock.ExpectExec(`WITH contribution AS`).WillReturnError(sql.ErrConnDone)
	err = subscriptionQuotaPublishHistoricalDashboardTx(context.Background(), tx, log, nil)
	require.ErrorIs(t, err, sql.ErrConnDone)
	mock.ExpectRollback()
	require.NoError(t, tx.Rollback())
	require.NoError(t, mock.ExpectationsWereMet())
	require.Error(t, subscriptionQuotaPublishHistoricalDashboardTx(context.Background(), nil, log, nil))
}

func TestSubscriptionQuotaDashboardWriterLockFailureStopsAggregation(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := newDashboardAggregationRepositoryWithSQL(db)
	lockErr := errors.New("writer lock canceled")
	mock.ExpectBegin()
	mock.ExpectExec(`SELECT pg_advisory_xact_lock\(1936811121, 1\)`).WillReturnError(lockErr)
	mock.ExpectRollback()
	now := time.Now()
	require.ErrorIs(t, repo.AggregateRange(context.Background(), now, now.Add(time.Minute)), lockErr)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSubscriptionQuotaDashboardAggregateRetentionSerializesBucketsAndUserDedup(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := newDashboardAggregationRepositoryWithSQL(db)
	hourCutoff := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	dayCutoff := hourCutoff.AddDate(-1, 0, 0)
	mock.ExpectBegin()
	mock.ExpectExec(`SELECT pg_advisory_xact_lock\(1936811121, 1\)`).WillReturnResult(sqlmock.NewResult(0, 1))
	for _, table := range []string{"usage_dashboard_hourly", "usage_dashboard_hourly_users", "usage_dashboard_daily", "usage_dashboard_daily_users"} {
		cutoff := hourCutoff
		if table == "usage_dashboard_daily" || table == "usage_dashboard_daily_users" {
			cutoff = dayCutoff
		}
		mock.ExpectExec(`DELETE FROM ` + table + ` WHERE`).WithArgs(cutoff).WillReturnResult(sqlmock.NewResult(0, 1))
	}
	mock.ExpectCommit()
	require.NoError(t, repo.CleanupAggregates(context.Background(), hourCutoff, dayCutoff))
	require.NoError(t, mock.ExpectationsWereMet())
}
