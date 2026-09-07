//go:build integration

package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestCarpoolRollingOpeningTakeoverDeadlineClampsAtExpiry(t *testing.T) {
	ctx := context.Background()
	repo, userID, groupID, planID := newSyntheticCarpoolTermDependencies(t)
	carpoolService := service.NewCarpoolService(repo)
	now := carpoolDatabaseNow(t, ctx)
	startsAt := now.Add(-27 * 24 * time.Hour)
	requestedDeadline := now.Add(3 * 24 * time.Hour)
	takeover := &domain.CarpoolTakeoverInput{
		CurrentBaseBalanceUSD: decimal.RequireFromString("10.00000000"),
		NextNaturalResetAt:    &requestedDeadline,
	}
	invalidDeadline := now.Add(8 * 24 * time.Hour)
	invalidTakeover := &domain.CarpoolTakeoverInput{NextNaturalResetAt: &invalidDeadline}
	_, err := carpoolService.Preview(ctx, userID, planID, &startsAt, "takeover", invalidTakeover)
	require.ErrorIs(t, err, service.ErrCarpoolInvalidNaturalReset)
	_, err = carpoolService.Open(ctx, userID, userID, service.CarpoolOpenInput{
		PlanID: planID, GroupID: groupID, StartsAt: &startsAt, Mode: "takeover", Takeover: invalidTakeover,
	}, uuid.NewString())
	require.ErrorIs(t, err, service.ErrCarpoolInvalidNaturalReset)

	preview, err := carpoolService.Preview(ctx, userID, planID, &startsAt, "takeover", takeover)
	require.NoError(t, err)
	require.Equal(t, domain.CarpoolResetModeRolling, preview.Plan.ResetMode)
	require.Len(t, preview.Cycles, 4)
	require.Equal(t, preview.ExpiresAt, preview.Cycles[3].EndsAt)
	require.Nil(t, preview.NextNaturalResetAt)

	opened, err := carpoolService.Open(ctx, userID, userID, service.CarpoolOpenInput{
		PlanID: planID, GroupID: groupID, StartsAt: &startsAt, Mode: "takeover", Takeover: takeover,
	}, uuid.NewString())
	require.NoError(t, err)
	require.Len(t, opened.Cycles, 4)
	require.NotNil(t, opened.CurrentCycle)
	require.Equal(t, opened.ExpiresAt, opened.CurrentCycle.EndsAt)
	require.Nil(t, opened.NextNaturalResetAt)
	details, err := repo.GetUserDetails(ctx, userID)
	require.NoError(t, err)
	require.NotNil(t, details.Term)
	require.Equal(t, domain.CarpoolResetModeRolling, details.Term.ResetMode)
	require.Nil(t, details.Term.NextNaturalResetAt)
}

func TestCarpoolRollingNaturalCatchupAndAdminReadAdvanceOnDemand(t *testing.T) {
	ctx := context.Background()
	repo, userID, groupID, planID := newSyntheticCarpoolTermDependencies(t)
	term, cycles, err := repo.CreateTerm(ctx, domain.CreateCarpoolTermParams{
		UserID: userID, ScopeID: domain.CarpoolGlobalScopeID, GroupID: groupID, PlanID: planID,
		ActorID: userID, Mode: "new", Operation: carpoolTestOperation("open_term", userID),
	})
	require.NoError(t, err)
	require.Len(t, cycles, 1)

	now := carpoolDatabaseNow(t, ctx)
	startsAt := now.Add(-15 * 24 * time.Hour)
	require.NoError(t, moveSingleRollingLifecycleTerm(ctx, term, cycles[0], startsAt, domain.CarpoolTermPending))

	userFilter := userID
	terms, total, err := repo.ListAdminTerms(ctx, domain.CarpoolTermFilters{Page: 1, PageSize: 10, UserID: &userFilter, Status: domain.CarpoolTermActive})
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, terms, 1)
	require.Equal(t, domain.CarpoolTermActive, terms[0].Status)
	require.NotNil(t, terms[0].CurrentCycle)
	require.Equal(t, 3, terms[0].CurrentCycle.CycleNo)
	require.NotNil(t, terms[0].NextNaturalResetAt)
	require.Equal(t, startsAt.Add(21*24*time.Hour).UTC(), terms[0].NextNaturalResetAt.UTC())

	storedCycles, err := repo.ListTermCycles(ctx, term.ID)
	require.NoError(t, err)
	require.Len(t, storedCycles, 3)
	require.Equal(t, domain.CarpoolCycleClosing, storedCycles[0].State)
	require.Equal(t, domain.CarpoolCycleMissed, storedCycles[1].State)
	require.Equal(t, domain.CarpoolCycleActive, storedCycles[2].State)
	assertLifecycleInitialGrantCounts(t, ctx, term.ID, []int{1, 0, 1})

	terms, _, err = repo.ListAdminTerms(ctx, domain.CarpoolTermFilters{Page: 1, PageSize: 10, UserID: &userFilter})
	require.NoError(t, err)
	assertLifecycleInitialGrantCounts(t, ctx, term.ID, []int{1, 0, 1})
	pendingTerms, pendingTotal, err := repo.ListAdminTerms(ctx, domain.CarpoolTermFilters{Page: 1, PageSize: 10, UserID: &userFilter, Status: domain.CarpoolTermPending})
	require.NoError(t, err)
	require.Zero(t, pendingTotal)
	require.Empty(t, pendingTerms)
}

func TestCarpoolRollingLastShortNaturalPeriodGrantsFullQuotaAndExpiryWins(t *testing.T) {
	ctx := context.Background()
	repo, userID, groupID, planID := newSyntheticCarpoolTermDependencies(t)
	term, cycles, err := repo.CreateTerm(ctx, domain.CreateCarpoolTermParams{
		UserID: userID, ScopeID: domain.CarpoolGlobalScopeID, GroupID: groupID, PlanID: planID,
		ActorID: userID, Mode: "new", Operation: carpoolTestOperation("open_term", userID),
	})
	require.NoError(t, err)
	require.Len(t, cycles, 1)

	now := carpoolDatabaseNow(t, ctx)
	startsAt := now.Add(-27 * 24 * time.Hour)
	expiresAt := startsAt.Add(28 * 24 * time.Hour)
	boundary := now.Add(-time.Second)
	_, err = integrationDB.ExecContext(ctx, `UPDATE carpool_terms SET starts_at=$1,expires_at=$2,status='active' WHERE id=$3`, startsAt, expiresAt, term.ID)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `UPDATE carpool_cycles SET cycle_no=3,starts_at=$1,ends_at=$2,state='active' WHERE id=$3`, boundary.Add(-7*24*time.Hour), boundary, cycles[0].ID)
	require.NoError(t, err)

	current, err := repo.EnsureCurrentCycle(ctx, term.ID, now)
	require.NoError(t, err)
	require.Equal(t, 4, current.CycleNo)
	require.Equal(t, boundary, current.StartsAt)
	require.Equal(t, expiresAt, current.EndsAt)
	require.Less(t, current.EndsAt.Sub(current.StartsAt), 7*24*time.Hour)
	require.True(t, term.PlanSnapshot.WeeklyQuotaUSD.Equal(current.BaseQuotaUSD))
	require.True(t, term.PlanSnapshot.WeeklyQuotaUSD.Equal(current.BaseBalanceUSD))
	require.Nil(t, domain.CarpoolNextNaturalResetAt(domain.CarpoolTermActive, expiresAt, now, []domain.CarpoolCycle{*current}))

	_, err = repo.EnsureCurrentCycle(ctx, term.ID, expiresAt)
	require.True(t, errors.Is(err, service.ErrCarpoolUnavailable))
	var cycleCount, grantCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM carpool_cycles WHERE term_id=$1`, term.ID).Scan(&cycleCount))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM carpool_ledger WHERE term_id=$1 AND event_type='cycle_initial'`, term.ID).Scan(&grantCount))
	require.Equal(t, 2, cycleCount)
	require.Equal(t, 2, grantCount)
}
