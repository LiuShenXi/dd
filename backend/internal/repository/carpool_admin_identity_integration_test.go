//go:build integration

package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestCarpoolAdminListsIncludeCurrentUserIdentity(t *testing.T) {
	ctx := context.Background()
	repo, userID, groupID, planID := newSyntheticCarpoolTermDependencies(t)
	term, _, err := repo.CreateTerm(ctx, domain.CreateCarpoolTermParams{
		UserID: userID, ScopeID: domain.CarpoolGlobalScopeID, GroupID: groupID, PlanID: planID,
		ActorID: userID, Mode: "new", Operation: carpoolTestOperation("open_term", userID),
	})
	require.NoError(t, err)
	var email string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT email FROM users WHERE id=$1`, userID).Scan(&email))

	terms, total, err := repo.ListAdminTerms(ctx, domain.CarpoolTermFilters{Page: 1, PageSize: 10, UserID: &userID})
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, terms, 1)
	require.Equal(t, email, terms[0].UserEmail)
	require.NotNil(t, terms[0].CurrentCycle)
	require.Equal(t, email, terms[0].CurrentCycle.UserEmail)
	cycles, cycleCount, err := repo.ListAdminCycles(ctx, domain.CarpoolCycleFilters{Page: 1, PageSize: 10, TermID: &term.ID})
	require.NoError(t, err)
	require.Positive(t, cycleCount)
	for _, cycle := range cycles {
		require.Equal(t, email, cycle.UserEmail)
	}
	ledger, count, err := repo.ListAdminLedger(ctx, domain.CarpoolLedgerFilters{Page: 1, PageSize: 10, UserID: &userID})
	require.NoError(t, err)
	require.Positive(t, count)
	for _, entry := range ledger {
		require.Equal(t, email, entry.UserEmail)
	}

	fixture := newCarpoolBillingReconciliationFixture(t, nil, nil)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT email FROM users WHERE id=$1`, fixture.userID).Scan(&email))
	exceptions, count, err := fixture.repo.ListBillingExceptions(ctx, domain.CarpoolBillingExceptionFilters{Page: 1, PageSize: 10, UserID: &fixture.userID})
	require.NoError(t, err)
	require.Positive(t, count)
	for _, entry := range exceptions {
		require.Equal(t, email, entry.UserEmail)
	}
}
