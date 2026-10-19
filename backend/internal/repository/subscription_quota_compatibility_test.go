package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func TestSubscriptionQuotaCarpoolProjectionAllowsOnlyExplicitTermGroup(t *testing.T) {
	for _, test := range []struct {
		platform, kind, mode string
		allowed              bool
	}{
		{"anthropic", "subscription", "term", true},
		{"anthropic", "subscription", "windows", false},
		{"anthropic", "standard", "term", false},
		{"openai", "subscription", "term", false},
		{"anthropic", "standard", "windows", false},
	} {
		t.Run(test.platform+test.kind+test.mode, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			mock.ExpectQuery(`SELECT group_id FROM carpool_billing_bindings`).WithArgs(int64(7)).WillReturnRows(sqlmock.NewRows([]string{"group_id"}).AddRow(2))
			groupID := int64(6)
			group := &service.Group{ID: groupID, Platform: test.platform, SubscriptionType: test.kind, SubscriptionQuotaMode: test.mode}
			key := &service.APIKey{UserID: 7, GroupID: &groupID, Group: group}
			got, err := (&apiKeyRepository{sql: db}).projectCarpoolBilling(context.Background(), key)
			if test.allowed {
				require.NoError(t, err)
				require.Same(t, group, got.Group)
			} else {
				require.ErrorIs(t, err, service.ErrCarpoolInvalidRelationship)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestSubscriptionQuotaOnlineIndexValidatorIsNarrow(t *testing.T) {
	content, err := migrations.FS.ReadFile("247_subscription_quota_usage_index_notx.sql")
	require.NoError(t, err)
	nonTx, err := validateMigrationExecutionMode("247_subscription_quota_usage_index_notx.sql", string(content))
	require.NoError(t, err)
	require.True(t, nonTx)
	for _, sql := range []string{"SET lock_timeout = '0';", "SET statement_timeout = '0';", "SET search_path = 'other';", "RESET ALL;", "SELECT 1;"} {
		_, err := validateMigrationExecutionMode("247_subscription_quota_usage_index_notx.sql", sql)
		require.Error(t, err)
	}
	_, err = validateMigrationExecutionMode("999_unrelated_notx.sql", "SET lock_timeout = '5s';")
	require.Error(t, err)
}

func TestSubscriptionQuotaOnlineIndexRestoresSessionOnFailure(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectQuery(`SELECT current_setting`).WillReturnRows(sqlmock.NewRows([]string{"lock", "statement"}).AddRow("2s", "30s"))
	mock.ExpectExec(`SELECT set_config`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`SELECT EXISTS`).WithArgs("usage_logs_subscription_term_time").WillReturnRows(sqlmock.NewRows([]string{"invalid"}).AddRow(false))
	mock.ExpectExec(`CREATE INDEX CONCURRENTLY`).WillReturnError(errors.New("cancelled by lock timeout"))
	mock.ExpectExec(`SELECT set_config`).WithArgs("2s", "30s").WillReturnResult(sqlmock.NewResult(0, 1))
	err = applySubscriptionQuotaOnlineIndexMigration(context.Background(), db, "247_subscription_quota_usage_index_notx.sql", "CREATE INDEX CONCURRENTLY IF NOT EXISTS usage_logs_subscription_term_time ON usage_logs(subscription_term_id)", "synthetic")
	require.ErrorContains(t, err, "lock timeout")
	require.NoError(t, mock.ExpectationsWereMet())
}
