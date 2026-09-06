package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestCarpoolRecoverPendingReceiptsBoundsAndLocksStaleAdmissions(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectExec(`WITH stale AS \(SELECT id FROM carpool_billing_requests WHERE status='admitted'.*FOR UPDATE SKIP LOCKED LIMIT \$1\) UPDATE carpool_billing_requests b`).
		WithArgs(7).
		WillReturnResult(sqlmock.NewResult(0, 7))

	admittedAt := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`WITH pending AS .*FOR UPDATE SKIP LOCKED LIMIT \$1.*RETURNING .*b.retry_count`).
		WithArgs(7).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "request_id", "user_id", "api_key_id", "group_id", "term_id", "cycle_id",
			"admitted_at", "actual_cost_usd", "billing_payload", "retry_count",
		}).AddRow(1, "request-1", 2, 3, 4, 5, 6, admittedAt, "1.25000000", `{}`, 2))

	receipts, err := NewCarpoolRepository(db).RecoverPendingReceipts(context.Background(), 7)
	require.NoError(t, err)
	require.Len(t, receipts, 1)
	require.Equal(t, 2, receipts[0].RetryCount)
	require.NoError(t, mock.ExpectationsWereMet())
}
