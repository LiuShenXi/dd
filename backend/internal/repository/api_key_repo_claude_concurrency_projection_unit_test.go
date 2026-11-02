package repository

import (
	"context"
	"fmt"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestClaudeSubscriptionConcurrencyNativeAPIKeyUserProjections(t *testing.T) {
	repo, client := newAPIKeyRepoSQLite(t)
	ctx := context.Background()
	for _, hasOverride := range []bool{true, false} {
		t.Run(fmt.Sprintf("override_%t", hasOverride), func(t *testing.T) {
			builder := client.User.Create().SetEmail(fmt.Sprintf("claude-auth-projection-%t@example.test", hasOverride)).
				SetPasswordHash("synthetic-hash").SetRole(service.RoleUser).SetStatus(service.StatusActive).SetConcurrency(5)
			if hasOverride {
				builder.SetClaudeSubscriptionConcurrency(2)
			}
			user, err := builder.Save(ctx)
			require.NoError(t, err)
			key := &service.APIKey{UserID: user.ID, Key: fmt.Sprintf("synthetic-auth-projection-%t", hasOverride), Name: "projection", Status: service.StatusActive}
			require.NoError(t, repo.Create(ctx, key))
			for name, fetch := range map[string]func() (*service.APIKey, error){
				"id_full":  func() (*service.APIKey, error) { return repo.GetByID(ctx, key.ID) },
				"key_full": func() (*service.APIKey, error) { return repo.GetByKey(ctx, key.Key) },
				"key_auth": func() (*service.APIKey, error) { return repo.GetByKeyForAuth(ctx, key.Key) },
			} {
				t.Run(name, func(t *testing.T) {
					loaded, err := fetch()
					require.NoError(t, err)
					require.NotNil(t, loaded.User)
					require.Equal(t, user.ID, loaded.User.ID)
					require.Equal(t, 5, loaded.User.Concurrency)
					if hasOverride {
						require.NotNil(t, loaded.User.ClaudeSubscriptionConcurrency)
						require.Equal(t, 2, *loaded.User.ClaudeSubscriptionConcurrency)
					} else {
						require.Nil(t, loaded.User.ClaudeSubscriptionConcurrency)
					}
				})
			}
		})
	}
}
