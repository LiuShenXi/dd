package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
)

type openingAuthRepo struct {
	CarpoolRepositoryAPI
	calls []domain.CreateCarpoolTermParams
	err   error
}

func (r *openingAuthRepo) CreateTerm(_ context.Context, p domain.CreateCarpoolTermParams) (*domain.CarpoolTerm, []domain.CarpoolCycle, error) {
	r.calls = append(r.calls, p)
	if r.err != nil {
		return nil, nil, r.err
	}
	return &domain.CarpoolTerm{ID: 88, UserID: 42, GroupID: 2, StartsAt: time.Now(), ExpiresAt: time.Now().Add(28 * 24 * time.Hour)}, nil, nil
}

type openingAuthInvalidator struct {
	userIDs  [][]int64
	groupIDs [][]int64
	err      error
}

func (i *openingAuthInvalidator) InvalidateReleaseAuthCache(_ context.Context, users, groups []int64) error {
	i.userIDs = append(i.userIDs, users)
	i.groupIDs = append(i.groupIDs, groups)
	return i.err
}

func TestCarpoolOpeningAuthRefreshRetriesCommittedOperation(t *testing.T) {
	for _, renew := range []bool{false, true} {
		name := "open"
		if renew {
			name = "renew"
		}
		t.Run(name, func(t *testing.T) {
			repo := &openingAuthRepo{}
			unavailable := errors.New("cache unavailable")
			invalidator := &openingAuthInvalidator{err: unavailable}
			svc := NewCarpoolService(repo)
			svc.SetAuthInvalidator(invalidator)
			call := func() (*domain.CarpoolAdminTerm, error) {
				if renew {
					return svc.Renew(context.Background(), 81, 1, CarpoolRenewInput{}, "same-idempotency-key")
				}
				return svc.Open(context.Background(), 42, 1, CarpoolOpenInput{GroupID: 2, PlanID: 3}, "same-idempotency-key")
			}
			result, err := call()
			require.ErrorIs(t, err, unavailable)
			require.Nil(t, result)
			invalidator.err = nil
			result, err = call()
			require.NoError(t, err)
			require.EqualValues(t, 88, result.ID)
			require.Len(t, repo.calls, 2)
			require.Equal(t, repo.calls[0].Operation, repo.calls[1].Operation, "same operation reaches repository replay and cache repair")
			require.Equal(t, [][]int64{{42}, {42}}, invalidator.userIDs, "refresh only the target member, never other users of the shared group")
			require.Equal(t, [][]int64{nil, nil}, invalidator.groupIDs)
		})
	}
}

func TestCarpoolOpeningFailureDoesNotInvalidateOtherUsers(t *testing.T) {
	repo := &openingAuthRepo{err: ErrCarpoolOpeningBalance}
	invalidator := &openingAuthInvalidator{}
	svc := NewCarpoolService(repo)
	svc.SetAuthInvalidator(invalidator)
	_, err := svc.Open(context.Background(), 42, 1, CarpoolOpenInput{GroupID: 2, PlanID: 3}, "opening-key")
	require.ErrorIs(t, err, ErrCarpoolOpeningBalance)
	require.Empty(t, invalidator.userIDs)
}
