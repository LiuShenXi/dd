//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type protectedConcurrencyV2Repository struct {
	admissions   []service.SubscriptionQuotaProtectedAdmission
	finishStatus []string
	admitError   error
}

func (r *protectedConcurrencyV2Repository) AdmitProtectedGroup(_ context.Context, in service.SubscriptionQuotaProtectedAdmission) error {
	r.admissions = append(r.admissions, in)
	return r.admitError
}
func (*protectedConcurrencyV2Repository) RecordProtectedGroupUsage(context.Context, string, json.RawMessage) error {
	return nil
}
func (r *protectedConcurrencyV2Repository) FinishProtectedGroup(_ context.Context, _ string, status, _ string) error {
	r.finishStatus = append(r.finishStatus, status)
	return nil
}
func (*protectedConcurrencyV2Repository) CountProtectedGroupUnfinished(context.Context, int64) (int64, error) {
	return 0, nil
}

type protectedConcurrencyV2Cache struct {
	service.ConcurrencyCache
	acquires, releases int
	beforeAcquire      func()
}

func (c *protectedConcurrencyV2Cache) AcquireUserSlot(context.Context, int64, int, string) (bool, error) {
	c.acquires++
	if c.beforeAcquire != nil {
		c.beforeAcquire()
	}
	return true, nil
}
func (c *protectedConcurrencyV2Cache) ReleaseUserSlot(context.Context, int64, string) error {
	c.releases++
	return nil
}

func TestSubscriptionQuotaConcurrencyV2ProtectedPreUserBarrier(t *testing.T) {
	for _, scenario := range []string{"converted_mode_rejected", "not_forwarded_is_no_charge", "final_recheck_reuses_marker", "final_recheck_rejected", "unprotected_legacy_has_no_new_repo_dependency"} {
		t.Run(scenario, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			key := termHandlerKey()
			key.Group.SubscriptionQuotaMode = "windows"
			repo := &protectedConcurrencyV2Repository{}
			protectedID := key.Group.ID
			if scenario == "unprotected_legacy_has_no_new_repo_dependency" {
				protectedID++
			}
			billing := &service.BillingCacheService{}
			billing.SetSubscriptionQuotaProtectedGroups(service.NewSubscriptionQuotaProtectedGroupService(repo, []int64{protectedID}))
			cache := &protectedConcurrencyV2Cache{beforeAcquire: func() {
				if scenario == "unprotected_legacy_has_no_new_repo_dependency" {
					require.Empty(t, repo.admissions)
				} else {
					require.Len(t, repo.admissions, 1, "durable preflight must precede the legacy slot")
				}
			}}
			h := &GatewayHandler{billingCacheService: billing, concurrencyHelper: NewConcurrencyHelper(service.NewConcurrencyService(cache), SSEPingFormatNone, 0)}
			if scenario == "converted_mode_rejected" {
				repo.admitError = errors.New("database mode is now term")
			}
			started := false
			release, err := h.acquireGatewayUserSlotWithWait(c, key, key.UserID, 5, false, &started)
			if scenario == "converted_mode_rejected" {
				require.ErrorIs(t, err, service.ErrSubscriptionQuotaProtectedUnavailable)
				require.Nil(t, release)
				require.Zero(t, cache.acquires, "stale standard mode must not take the GPT pool")
				require.Empty(t, repo.finishStatus)
				return
			}
			require.NoError(t, err)
			require.Equal(t, 1, cache.acquires)
			if scenario == "final_recheck_reuses_marker" || scenario == "final_recheck_rejected" {
				if scenario == "final_recheck_rejected" {
					repo.admitError = errors.New("final database gate rejected")
				}
				request, finalErr := billing.AdmitSubscriptionQuotaProtectedGroup(c.Request.Context(), key, key, service.PlatformAnthropic)
				if scenario == "final_recheck_rejected" {
					require.ErrorIs(t, finalErr, service.ErrSubscriptionQuotaProtectedUnavailable)
					require.Nil(t, request)
				} else {
					require.NoError(t, finalErr)
					require.NotNil(t, request)
				}
				require.Len(t, repo.admissions, 2)
				require.Equal(t, repo.admissions[0].RequestID, repo.admissions[1].RequestID, "final DB recheck must retain the queued durable marker")
			}
			release()
			release()
			require.Equal(t, 1, cache.releases)
			switch scenario {
			case "final_recheck_reuses_marker", "unprotected_legacy_has_no_new_repo_dependency":
				require.Empty(t, repo.finishStatus, "an actual final gate cannot be cleared by the pre-user cleanup")
			default:
				require.Equal(t, []string{"no_charge"}, repo.finishStatus)
			}
		})
	}
	t.Run("missing_billing_dependency", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
		key := termHandlerKey()
		key.Group.SubscriptionQuotaMode = "windows"
		cfg := &config.Config{}
		cfg.SubscriptionQuota.ProtectedGroupIDs = []int64{key.Group.ID}
		cache := &protectedConcurrencyV2Cache{}
		h := &GatewayHandler{cfg: cfg, concurrencyHelper: NewConcurrencyHelper(service.NewConcurrencyService(cache), SSEPingFormatNone, 0)}
		started := false
		_, err := h.acquireGatewayUserSlotWithWait(c, key, key.UserID, 5, false, &started)
		require.ErrorIs(t, err, service.ErrSubscriptionQuotaProtectedUnavailable)
		require.Zero(t, cache.acquires)
	})
}
