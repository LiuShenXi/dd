//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

type quotaKeyEntitlementReader struct {
	current         bool
	ids             []int64
	err             error
	userID, groupID int64
	calls           int
}

func (r *quotaKeyEntitlementReader) HasCurrentTerm(_ context.Context, userID, groupID int64) (bool, error) {
	r.calls++
	r.userID, r.groupID = userID, groupID
	return r.current, r.err
}
func (r *quotaKeyEntitlementReader) ListCurrentTermGroupIDs(_ context.Context, userID int64) ([]int64, error) {
	r.calls++
	r.userID = userID
	return r.ids, r.err
}

type quotaKeyRepository struct {
	APIKeyRepository
	key              *APIKey
	creates, updates int
}

func (r *quotaKeyRepository) Create(_ context.Context, key *APIKey) error {
	r.creates++
	key.ID = 100
	return nil
}
func (r *quotaKeyRepository) GetByID(context.Context, int64) (*APIKey, error) {
	key := *r.key
	return &key, nil
}
func (r *quotaKeyRepository) Update(_ context.Context, key *APIKey, _ APIKeyUpdateFields) error {
	r.updates++
	r.key = key
	return nil
}

func quotaKeyGroup(id int64) *Group {
	return &Group{ID: id, Platform: PlatformAnthropic, SubscriptionType: SubscriptionTypeSubscription, SubscriptionQuotaMode: "term", Status: StatusActive, IsExclusive: true}
}

func TestSubscriptionQuotaKeyBindingUsesCurrentTerm(t *testing.T) {
	failure := errors.New("current-term database failure")
	for _, tc := range []struct {
		name    string
		current bool
		err     error
		absent  bool
	}{
		{name: "future-only"}, {name: "gap"}, {name: "current exhausted retains management", current: true}, {name: "current active", current: true}, {name: "database failure", err: failure}, {name: "unwired", absent: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			group := quotaKeyGroup(6)
			reader := &quotaKeyEntitlementReader{current: tc.current, err: tc.err}
			keys := &quotaKeyRepository{key: &APIKey{ID: 1, UserID: 7, Key: "test-key", Status: StatusActive}}
			svc := &APIKeyService{apiKeyRepo: keys, userRepo: &visibilityUserRepo{user: &User{ID: 7, AllowedGroups: []int64{6}}}, groupRepo: &visibilityGroupRepo{groups: []Group{*group}}, cfg: &config.Config{}}
			if !tc.absent {
				svc.SetSubscriptionQuotaEntitlementReader(reader)
			}
			_, err := svc.Create(context.Background(), 7, CreateAPIKeyRequest{Name: "one", GroupID: &group.ID})
			if tc.current {
				require.NoError(t, err)
				require.Equal(t, 1, keys.creates)
			} else {
				require.Error(t, err)
				require.Zero(t, keys.creates)
			}
			if tc.err != nil {
				require.ErrorIs(t, err, failure)
			}
			if tc.absent {
				require.ErrorIs(t, err, ErrSubscriptionQuotaBillingUnavailable)
			}
			_, err = svc.Update(context.Background(), 1, 7, UpdateAPIKeyRequest{GroupID: &group.ID})
			if tc.current {
				require.NoError(t, err)
				require.Equal(t, 1, keys.updates)
			} else {
				require.Error(t, err)
				require.Zero(t, keys.updates)
			}
			if !tc.absent {
				require.EqualValues(t, 7, reader.userID)
				require.EqualValues(t, 6, reader.groupID)
			}
		})
	}
}

func TestSubscriptionQuotaKeyRetainedIdentityNeedsNoCurrentEntitlement(t *testing.T) {
	groupID := int64(6)
	name := "renamed"
	keys := &quotaKeyRepository{key: &APIKey{ID: 1, UserID: 7, Key: "test-key", GroupID: &groupID, Status: StatusActive}}
	svc := &APIKeyService{apiKeyRepo: keys}
	key, err := svc.Update(context.Background(), 1, 7, UpdateAPIKeyRequest{GroupID: &groupID, Name: &name})
	require.NoError(t, err)
	require.Equal(t, name, key.Name)
	require.EqualValues(t, 6, *key.GroupID)
	_, err = svc.Update(context.Background(), 1, 8, UpdateAPIKeyRequest{GroupID: &groupID})
	require.ErrorIs(t, err, ErrInsufficientPerms)
}

func TestSubscriptionQuotaKeyAvailableAndVisibilityAgree(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	term := quotaKeyGroup(6)
	future := quotaKeyGroup(7)
	windows := Group{ID: 8, Platform: PlatformAnthropic, SubscriptionType: SubscriptionTypeSubscription, Status: StatusActive, IsExclusive: true}
	gpt := Group{ID: 9, Platform: PlatformOpenAI, SubscriptionType: SubscriptionTypeCarpool, Status: StatusActive}
	for _, current := range []bool{false, true} {
		t.Run(map[bool]string{false: "future_and_gap", true: "current_exhausted"}[current], func(t *testing.T) {
			reader := &quotaKeyEntitlementReader{}
			if current {
				reader.ids = []int64{6}
			}
			svc := &APIKeyService{userRepo: &visibilityUserRepo{user: &User{ID: 7, AllowedGroups: []int64{6, 7, 9}}}, groupRepo: &visibilityGroupRepo{groups: []Group{*term, *future, windows, gpt}}, userSubRepo: &visibilitySubRepo{subscriptions: []UserSubscription{
				{UserID: 7, GroupID: 6, Group: term, Status: SubscriptionStatusActive, ExpiresAt: now.Add(30 * 24 * time.Hour)},
				{UserID: 7, GroupID: 7, Group: future, Status: SubscriptionStatusActive, ExpiresAt: now.Add(30 * 24 * time.Hour)},
				{UserID: 7, GroupID: 8, Group: &windows, Status: SubscriptionStatusActive, ExpiresAt: now.Add(time.Hour)},
			}}}
			svc.SetSubscriptionQuotaEntitlementReader(reader)
			groups, err := svc.GetAvailableGroups(ctx, 7)
			require.NoError(t, err)
			ids := make([]int64, 0, len(groups))
			for _, group := range groups {
				ids = append(ids, group.ID)
			}
			if current {
				require.ElementsMatch(t, []int64{6, 8}, ids)
			} else {
				require.Equal(t, []int64{8}, ids)
			}
			visible, _, err := svc.GetUserGroupVisibility(ctx, 7)
			require.NoError(t, err)
			require.NotContains(t, visible, int64(7))
			require.Contains(t, visible, int64(8))
			require.Contains(t, visible, int64(9), "existing GPT display rights are preserved")
			_, hasCurrent := visible[6]
			require.Equal(t, current, hasCurrent)
			reader.err = errors.New("reader failed")
			_, err = svc.GetAvailableGroups(ctx, 7)
			require.ErrorIs(t, err, reader.err)
			_, _, err = svc.GetUserGroupVisibility(ctx, 7)
			require.ErrorIs(t, err, reader.err)
		})
	}
}

func TestSubscriptionQuotaKeyAdminBindingUsesOwnerCurrentTerm(t *testing.T) {
	failure := errors.New("current term lookup failed")
	for _, tc := range []struct {
		name    string
		current bool
		err     error
		absent  bool
	}{
		{name: "future-only"}, {name: "gap"}, {name: "current exhausted", current: true}, {name: "error", err: failure}, {name: "unwired", absent: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			group := quotaKeyGroup(6)
			reader := &quotaKeyEntitlementReader{current: tc.current, err: tc.err}
			keys := &quotaKeyRepository{key: &APIKey{ID: 1, UserID: 7, Key: "test-key", Status: StatusActive}}
			users := &userRepoStubForGroupUpdate{}
			svc := &adminServiceImpl{apiKeyRepo: keys, userRepo: users, groupRepo: &visibilityGroupRepo{groups: []Group{*group}}}
			if !tc.absent {
				SetAdminSubscriptionQuotaEntitlementReader(svc, reader)
			}
			result, err := svc.AdminUpdateAPIKeyGroupID(context.Background(), 1, &group.ID)
			if tc.current {
				require.NoError(t, err)
				require.False(t, result.AutoGrantedGroupAccess)
				require.Equal(t, 1, keys.updates)
			} else {
				require.Error(t, err)
				require.Zero(t, keys.updates)
			}
			if tc.err != nil {
				require.ErrorIs(t, err, failure)
			} else if tc.absent {
				require.ErrorIs(t, err, ErrSubscriptionQuotaBillingUnavailable)
			} else if !tc.current {
				require.Equal(t, "SUBSCRIPTION_REQUIRED", infraerrors.Reason(err))
			}
			require.False(t, users.addGroupCalled, "term rights must never be converted to a permanent allowed_groups grant")
			if !tc.absent {
				require.EqualValues(t, 7, reader.userID)
				require.EqualValues(t, 6, reader.groupID)
			}
		})
	}
}

func TestSubscriptionQuotaKeyMissingReaderFailsClosedAndWindowsRemain(t *testing.T) {
	term := quotaKeyGroup(6)
	now := time.Now()
	svc := &APIKeyService{userRepo: &visibilityUserRepo{user: &User{ID: 7, AllowedGroups: []int64{6}}}, groupRepo: &visibilityGroupRepo{groups: []Group{*term}}, userSubRepo: &visibilitySubRepo{subscriptions: []UserSubscription{{UserID: 7, GroupID: 6, Group: term, Status: SubscriptionStatusActive, ExpiresAt: now.Add(time.Hour)}}}}
	_, err := svc.GetAvailableGroups(context.Background(), 7)
	require.ErrorIs(t, err, ErrSubscriptionQuotaBillingUnavailable)
	_, _, err = svc.GetUserGroupVisibility(context.Background(), 7)
	require.ErrorIs(t, err, ErrSubscriptionQuotaBillingUnavailable)
	allowed, err := svc.canUserBindGroup(context.Background(), &User{ID: 7}, &Group{ID: 9, SubscriptionType: SubscriptionTypeCarpool, Platform: PlatformOpenAI})
	require.NoError(t, err)
	require.False(t, allowed)
}

func TestSubscriptionQuotaKeyGPTVisibilityNeverQueriesTermRepository(t *testing.T) {
	gpt := Group{ID: 2, Platform: PlatformOpenAI, Status: StatusActive, SubscriptionType: SubscriptionTypeCarpool}
	term := quotaKeyGroup(6)
	reader := &quotaKeyEntitlementReader{err: errors.New("new quota repository unavailable")}
	svc := &APIKeyService{userRepo: &visibilityUserRepo{user: &User{ID: 7, AllowedGroups: []int64{2}}}, groupRepo: &visibilityGroupRepo{groups: []Group{gpt, *term}}, userSubRepo: &visibilitySubRepo{subscriptions: []UserSubscription{{UserID: 7, GroupID: 2, Group: &gpt, Status: SubscriptionStatusActive, ExpiresAt: time.Now().Add(time.Hour)}}}}
	svc.SetSubscriptionQuotaEntitlementReader(reader)
	groups, err := svc.GetAvailableGroups(context.Background(), 7)
	require.NoError(t, err)
	require.Empty(t, groups)
	visible, _, err := svc.GetUserGroupVisibility(context.Background(), 7)
	require.NoError(t, err)
	require.Contains(t, visible, int64(2))
	require.Zero(t, reader.calls)
}
