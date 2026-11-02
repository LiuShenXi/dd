package backgroundjobs

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestPostgresLeadershipRetainsSessionUntilRelease(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectQuery(`SELECT pg_try_advisory_lock`).WithArgs(advisoryKey).WillReturnRows(sqlmock.NewRows([]string{"locked"}).AddRow(true))
	lease, err := (PostgresLocker{DB: db}).Acquire(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, db.Stats().InUse)
	require.NoError(t, mock.ExpectationsWereMet())
	mock.ExpectQuery(`SELECT pg_advisory_unlock`).WithArgs(advisoryKey).WillReturnRows(sqlmock.NewRows([]string{"unlocked"}).AddRow(true))
	lease.Release()
	lease.Release()
	require.Zero(t, db.Stats().InUse)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPostgresLeadershipUnavailableFailsClosed(t *testing.T) {
	for _, fail := range []bool{false, true} {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		query := mock.ExpectQuery(`SELECT pg_try_advisory_lock`).WithArgs(advisoryKey)
		if fail {
			query.WillReturnError(errors.New("database failure"))
		} else {
			query.WillReturnRows(sqlmock.NewRows([]string{"locked"}).AddRow(false))
		}
		// Discarding prevents an uncertain lock from escaping into the pool.
		mock.ExpectClose()
		lease, err := (PostgresLocker{DB: db}).Acquire(context.Background())
		require.Error(t, err)
		require.Nil(t, lease)
		require.Zero(t, db.Stats().InUse)
		require.NoError(t, mock.ExpectationsWereMet())
		_ = db.Close()
	}
}
