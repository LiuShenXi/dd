//go:build integration

package repository

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestCarpoolBillingBindingPreservesGroupAndAdminThroughAuth(t *testing.T) {
	ctx := context.Background()
	carpoolRepo, memberID, groupID, planID := newSyntheticCarpoolTermDependencies(t)
	_, err := integrationDB.ExecContext(ctx, `UPDATE groups SET subscription_type='standard',platform='openai',rate_multiplier=0.8 WHERE id=$1`, groupID)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `UPDATE users SET balance=550,restrict_public_groups=TRUE WHERE id=$1`, memberID)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `INSERT INTO user_allowed_groups(user_id,group_id) VALUES($1,$2)`, memberID, groupID)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `INSERT INTO carpool_billing_bindings(user_id,group_id) VALUES($1,$2)`, memberID, groupID)
	require.NoError(t, err)
	client := testEntClient(t)
	admin := mustCreateUser(t, client, &service.User{Role: service.RoleAdmin, Balance: 99})
	memberKey := mustCreateApiKey(t, client, &service.APIKey{UserID: memberID, GroupID: &groupID, Key: "sk-binding-member-" + uuid.NewString()})
	adminKey := mustCreateApiKey(t, client, &service.APIKey{UserID: admin.ID, GroupID: &groupID, Key: "sk-binding-admin-" + uuid.NewString()})
	keyRepo := NewAPIKeyRepository(client, integrationDB)
	for _, lookup := range []func(context.Context, string) (*service.APIKey, error){keyRepo.GetByKey, keyRepo.GetByKeyForAuth} {
		for _, original := range []*service.APIKey{memberKey, adminKey, memberKey, adminKey} {
			got, err := lookup(ctx, original.Key)
			require.NoError(t, err)
			require.Equal(t, original.ID, got.ID)
			require.Equal(t, groupID, *got.GroupID)
			require.Equal(t, original.UserID == memberID, got.Group.IsCarpoolType())
			require.Equal(t, 0.8, got.Group.RateMultiplier)
		}
	}
	term, _, err := carpoolRepo.CreateTerm(ctx, domain.CreateCarpoolTermParams{UserID: memberID, GroupID: groupID, ScopeID: domain.CarpoolGlobalScopeID, PlanID: planID, ActorID: admin.ID, Mode: "new", Operation: carpoolTestOperation("open_term", admin.ID)})
	require.NoError(t, err)
	_, _, err = carpoolRepo.CreateTerm(ctx, domain.CreateCarpoolTermParams{UserID: admin.ID, GroupID: groupID, ScopeID: domain.CarpoolGlobalScopeID, PlanID: planID, ActorID: admin.ID, Mode: "new", Operation: carpoolTestOperation("open_term", admin.ID)})
	require.ErrorIs(t, err, service.ErrCarpoolOpeningBalance, "another user's opening does not enroll the administrator or import its balance")

	cfg := &config.Config{RunMode: config.RunModeStandard}
	cfg.APIKeyAuth.L1Size = 100
	cfg.APIKeyAuth.L1TTLSeconds = 60
	keyService := service.NewAPIKeyService(keyRepo, nil, nil, nil, nil, nil, cfg)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(gin.HandlerFunc(middleware.NewAPIKeyAuthMiddleware(keyService, nil, cfg)))
	router.POST("/test-admission", func(c *gin.Context) {
		key, ok := middleware.GetAPIKeyFromContext(c)
		if !ok {
			c.Status(http.StatusUnauthorized)
			return
		}
		if key.Group.IsCarpoolType() {
			_, admitErr := carpoolRepo.Admit(c.Request.Context(), key.UserID, key.ID, key.Group.ID, uuid.NewString(), time.Now())
			if admitErr != nil {
				if errors.Is(admitErr, service.ErrCarpoolUnavailable) {
					c.Status(http.StatusForbidden)
				} else {
					c.String(http.StatusInternalServerError, admitErr.Error())
				}
				return
			}
		}
		c.String(http.StatusOK, key.Group.SubscriptionType)
	})
	call := func(key *service.APIKey, status int, billing string) {
		request := httptest.NewRequest(http.MethodPost, "/test-admission", nil)
		request.Header.Set("Authorization", "Bearer "+key.Key)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		require.Equal(t, status, response.Code, response.Body.String())
		if billing != "" {
			require.Equal(t, billing, response.Body.String())
		}
	}
	call(memberKey, http.StatusOK, service.SubscriptionTypeCarpool)
	call(adminKey, http.StatusOK, service.SubscriptionTypeStandard)
	_, err = integrationDB.ExecContext(ctx, `UPDATE carpool_terms SET status='expired',starts_at=NOW()-INTERVAL '29 days',expires_at=NOW()-INTERVAL '1 day' WHERE id=$1`, term.ID)
	require.NoError(t, err)
	call(memberKey, http.StatusForbidden, "")
	call(adminKey, http.StatusOK, service.SubscriptionTypeStandard)
	got, err := keyRepo.GetByKeyForAuth(ctx, memberKey.Key)
	require.NoError(t, err)
	require.True(t, got.Group.IsCarpoolType(), "expiry never re-enables preserved ordinary balance")
	var groupType, memberBalance, adminBalance string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT subscription_type FROM groups WHERE id=$1`, groupID).Scan(&groupType))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT balance::text FROM users WHERE id=$1`, memberID).Scan(&memberBalance))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT balance::text FROM users WHERE id=$1`, admin.ID).Scan(&adminBalance))
	require.Equal(t, service.SubscriptionTypeStandard, groupType)
	require.Equal(t, "550.00000000", memberBalance)
	require.Equal(t, "99.00000000", adminBalance)
	for _, grouped := range []bool{false, true} {
		badKey := &service.APIKey{UserID: memberID, Name: "wrong-binding-fixture", Key: "sk-binding-wrong-" + uuid.NewString(), Status: service.StatusActive}
		if grouped {
			other := mustCreateGroup(t, client, &service.Group{Name: "binding-other-" + uuid.NewString(), Platform: service.PlatformOpenAI})
			badKey.GroupID = &other.ID
		}
		require.NoError(t, keyRepo.Create(ctx, badKey))
		for _, lookup := range []func(context.Context, string) (*service.APIKey, error){keyRepo.GetByKey, keyRepo.GetByKeyForAuth} {
			_, err := lookup(ctx, badKey.Key)
			require.ErrorIs(t, err, service.ErrCarpoolInvalidRelationship)
		}
		if grouped {
			_, err := carpoolRepo.Admit(ctx, memberID, badKey.ID, *badKey.GroupID, uuid.NewString(), time.Now())
			require.ErrorIs(t, err, service.ErrCarpoolInvalidRelationship)
		}
	}
}
