//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type protectedHandlerRepo struct {
	mu     sync.Mutex
	states map[string]string
}

func (r *protectedHandlerRepo) AdmitProtectedGroup(ctx context.Context, in service.SubscriptionQuotaProtectedAdmission) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.states[in.RequestID] = "admitted"
	return ctx.Err()
}
func (r *protectedHandlerRepo) RecordProtectedGroupUsage(ctx context.Context, id string, p json.RawMessage) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.states[id] = "usage_pending"
	return ctx.Err()
}
func (r *protectedHandlerRepo) FinishProtectedGroup(ctx context.Context, id, state, reason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.states[id] = state
	return ctx.Err()
}
func (r *protectedHandlerRepo) CountProtectedGroupUnfinished(ctx context.Context, groupID int64) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var n int64
	for _, state := range r.states {
		if state != "completed" && state != "no_charge" {
			n++
		}
	}
	return n, ctx.Err()
}

func TestSubscriptionQuotaProtectedHTTPReturnDoesNotFinishAsyncUsage(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(map[bool]string{false: "settlement success", true: "settlement failure"}[failed], func(t *testing.T) {
			repo := &protectedHandlerRepo{states: map[string]string{}}
			gate := service.NewSubscriptionQuotaProtectedGroupService(repo, []int64{6})
			billing := &service.BillingCacheService{}
			billing.SetSubscriptionQuotaProtectedGroups(gate)
			pool := service.NewUsageRecordWorkerPoolWithOptions(service.UsageRecordWorkerPoolOptions{WorkerCount: 1, QueueSize: 2, TaskTimeout: 5 * time.Second, OverflowPolicy: "drop"})
			defer pool.Stop()
			h := &GatewayHandler{billingCacheService: billing, usageRecordWorkerPool: pool}
			groupID := int64(6)
			key := &service.APIKey{ID: 10, UserID: 20, GroupID: &groupID, Group: &service.Group{ID: 6, Platform: service.PlatformAnthropic, SubscriptionType: service.SubscriptionTypeStandard}}
			started, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
			t.Cleanup(func() {
				select {
				case <-release:
				default:
					close(release)
				}
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			router := gin.New()
			router.POST("/v1/messages", func(c *gin.Context) {
				protection, err := h.admitProtectedGroupForward(c, key, key, service.PlatformAnthropic)
				if err != nil {
					c.Status(http.StatusServiceUnavailable)
					return
				}
				defer protectedGroupForwardEnded(protection)
				h.submitProtectedGroupUsageRecordTask(c.Request.Context(), protection, &service.ForwardResult{Usage: service.ClaudeUsage{InputTokens: 3}}, 30, time.Now(), func(taskCtx context.Context) {
					close(started)
					<-release
					var billingErr error
					if failed {
						billingErr = errors.New("storage failed")
					}
					if taskCtx.Err() != nil {
						billingErr = taskCtx.Err()
					}
					finishProtectedGroupUsage(protection, billingErr)
					close(done)
				})
				c.Status(http.StatusOK)
			})
			request := httptest.NewRequest(http.MethodPost, "/v1/messages", nil).WithContext(ctx)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			require.Equal(t, http.StatusOK, response.Code)
			select {
			case <-started:
			case <-time.After(3 * time.Second):
				t.Fatal("worker did not start")
			}
			cancel() // HTTP lifecycle has ended, but the billing lifecycle has not.
			count, err := gate.CountUnfinished(context.Background(), 6)
			require.NoError(t, err)
			require.EqualValues(t, 1, count)
			close(release)
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("worker did not complete")
			}
			count, err = gate.CountUnfinished(context.Background(), 6)
			require.NoError(t, err)
			if failed {
				require.EqualValues(t, 1, count)
			} else {
				require.Zero(t, count)
			}
		})
	}
}
