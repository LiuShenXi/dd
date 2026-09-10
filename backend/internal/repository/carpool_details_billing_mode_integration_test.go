//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestCarpoolUserDetailsBillingModeSurvivesExpiryAndMissingTerm(t *testing.T) {
	ctx := context.Background()
	repo, userID, groupID, planID := newSyntheticCarpoolTermDependencies(t)
	ordinary, err := repo.GetUserDetails(ctx, userID)
	require.NoError(t, err)
	require.Equal(t, "standard", ordinary.BillingMode)
	require.Nil(t, ordinary.Term)
	_, err = integrationDB.ExecContext(ctx, `UPDATE groups SET subscription_type='standard',platform='openai' WHERE id=$1`, groupID)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `UPDATE users SET restrict_public_groups=TRUE WHERE id=$1`, userID)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `INSERT INTO user_allowed_groups(user_id,group_id) VALUES($1,$2)`, userID, groupID)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `INSERT INTO carpool_billing_bindings(user_id,group_id) VALUES($1,$2)`, userID, groupID)
	require.NoError(t, err)
	bound, err := repo.GetUserDetails(ctx, userID)
	require.NoError(t, err)
	require.Equal(t, "carpool", bound.BillingMode)
	require.Nil(t, bound.Term)
	require.Nil(t, bound.Quota)
	start := carpoolDatabaseNow(t, ctx).Add(-29 * 24 * time.Hour)
	_, _, err = repo.CreateTerm(ctx, domain.CreateCarpoolTermParams{UserID: userID, ScopeID: 1, GroupID: groupID, PlanID: planID, ActorID: userID, StartsAt: &start, Mode: "new", Operation: carpoolTestOperation("open_term", userID)})
	require.NoError(t, err)
	expired, err := repo.GetUserDetails(ctx, userID)
	require.NoError(t, err)
	require.Equal(t, "carpool", expired.BillingMode)
	require.Equal(t, domain.CarpoolTermExpired, expired.Term.Status)
	require.Nil(t, expired.Quota)
}
