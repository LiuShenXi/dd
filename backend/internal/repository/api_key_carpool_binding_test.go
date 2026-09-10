package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyCarpoolBindingProjectionIsPrivateAndFailsClosed(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	repo := &apiKeyRepository{sql: db}
	shared := &service.Group{ID: 2, Platform: service.PlatformOpenAI, SubscriptionType: service.SubscriptionTypeStandard, RateMultiplier: 0.8}
	wrongGroupID := int64(3)
	query := `SELECT group_id FROM carpool_billing_bindings WHERE user_id=\$1`
	for _, tc := range []struct {
		name        string
		groupID     *int64
		bound       bool
		lookupError error
	}{
		{name: "bound member", groupID: &shared.ID, bound: true},
		{name: "unbound administrator", groupID: &shared.ID},
		{name: "bound again", groupID: &shared.ID, bound: true},
		{name: "nil group", bound: true},
		{name: "wrong group", groupID: &wrongGroupID, bound: true},
		{name: "database unavailable", groupID: &shared.ID, lookupError: errors.New("database unavailable")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			expect := mock.ExpectQuery(query).WithArgs(int64(7))
			if tc.lookupError != nil {
				expect.WillReturnError(tc.lookupError)
			} else {
				rows := sqlmock.NewRows([]string{"group_id"})
				if tc.bound {
					rows.AddRow(2)
				}
				expect.WillReturnRows(rows)
			}
			key := &service.APIKey{UserID: 7, GroupID: tc.groupID, Group: shared}
			got, err := repo.projectCarpoolBilling(context.Background(), key)
			if tc.lookupError != nil || tc.bound && (tc.groupID == nil || *tc.groupID != shared.ID) {
				require.Error(t, err)
				require.Nil(t, got)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.bound, got.Group.IsCarpoolType())
				require.Equal(t, 0.8, got.Group.RateMultiplier)
				if tc.bound {
					require.NotSame(t, shared, got.Group)
				}
			}
			require.Equal(t, service.SubscriptionTypeStandard, shared.SubscriptionType)
		})
	}
	require.NoError(t, mock.ExpectationsWereMet())
}
