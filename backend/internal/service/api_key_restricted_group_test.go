//go:build unit

package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type restrictedGroupCreateRepo struct {
	quotaBaseAPIKeyRepoStub
	created int
}

func (r *restrictedGroupCreateRepo) Create(_ context.Context, key *APIKey) error {
	r.created++
	key.ID = 91
	return nil
}

func TestAPIKeyCreate_UngroupedRestriction(t *testing.T) {
	for _, tc := range []struct {
		name       string
		role       string
		restricted bool
	}{
		{name: "restricted member", role: RoleUser, restricted: true},
		{name: "ordinary user", role: RoleUser},
		{name: "administrator", role: RoleAdmin},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &restrictedGroupCreateRepo{}
			svc := &APIKeyService{apiKeyRepo: repo, cfg: &config.Config{}, userRepo: &mockUserRepo{
				getByIDUser: &User{ID: 7, Role: tc.role, RestrictPublicGroups: tc.restricted},
			}}
			key, err := svc.Create(context.Background(), 7, CreateAPIKeyRequest{Name: "test"})
			if tc.restricted {
				require.ErrorIs(t, err, ErrGroupNotAllowed)
				require.Nil(t, key)
				require.Zero(t, repo.created)
			} else {
				require.NoError(t, err)
				require.Nil(t, key.GroupID)
				require.Equal(t, 1, repo.created)
			}
		})
	}
}

func TestAPIKeyUpdate_NullGroupPreservesAssignedCarpoolKey(t *testing.T) {
	groupID := int64(41)
	svc, repo := newUpdateFieldsAPIKeyService(&APIKey{
		ID: 1, UserID: 7, Key: "sk-test", Name: "before", Status: StatusActive, GroupID: &groupID,
	})
	var req UpdateAPIKeyRequest
	require.NoError(t, json.Unmarshal([]byte(`{"name":"renamed","group_id":null}`), &req))
	updated, err := svc.Update(context.Background(), 1, 7, req)
	require.NoError(t, err)
	require.Equal(t, groupID, *updated.GroupID)
	require.Equal(t, []APIKeyUpdateFields{{Name: true}}, repo.updateFields)
}
