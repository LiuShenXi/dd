package handler

import (
	"context"
	"log/slog"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *GatewayHandler) admitProtectedGroupForward(c *gin.Context, original, current *service.APIKey, platform string) (*service.SubscriptionQuotaProtectedRequest, error) {
	if _, term := service.SubscriptionQuotaForwardStateFromContext(c.Request.Context()); term {
		return nil, nil
	}
	if original != nil && original.Group != nil && original.Group.HasTermQuotaMode() && (c.Request.URL.Path == "/v1/messages/count_tokens" || c.Request.URL.Path == "/messages/count_tokens") {
		return nil, nil
	}
	if h.billingCacheService != nil {
		return h.billingCacheService.AdmitSubscriptionQuotaProtectedGroup(c.Request.Context(), original, current, platform)
	}
	if h.cfg != nil {
		for _, id := range h.cfg.SubscriptionQuota.ProtectedGroupIDs {
			if (original != nil && original.GroupID != nil && *original.GroupID == id) || (current != nil && current.GroupID != nil && *current.GroupID == id) {
				return nil, service.ErrSubscriptionQuotaProtectedUnavailable
			}
		}
	}
	return nil, nil
}

func protectedGroupForwardEnded(protection *service.SubscriptionQuotaProtectedRequest) {
	if err := protection.ForwardEnded(); err != nil {
		slog.Error("protected group request remains held after forward", "error", err)
	}
}

func finishProtectedGroupUsage(protection *service.SubscriptionQuotaProtectedRequest, billingErr error) {
	if err := protection.FinishUsage(billingErr); err != nil {
		slog.Error("protected group request remains held after billing", "error", err)
	}
}

func (h *GatewayHandler) submitProtectedGroupUsageRecordTask(parent context.Context, protection *service.SubscriptionQuotaProtectedRequest, result *service.ForwardResult, accountID int64, pricingAt time.Time, task service.UsageRecordTask) {
	if state, term := service.SubscriptionQuotaForwardStateFromContext(parent); term {
		state.MarkUsageQueued()
		snapshot := state.Snapshot()
		h.submitMandatoryUsageRecordTask(parent, func(ctx context.Context) {
			defer state.StopHeartbeat()
			task(service.ContextWithSubscriptionQuotaBillingSnapshot(ctx, snapshot))
		})
		return
	}
	if protection == nil {
		h.submitUsageRecordTask(parent, task)
		return
	}
	if err := protection.PrepareUsage(result, accountID, pricingAt); err != nil {
		slog.Error("protected group usage facts could not be saved; request remains held", "error", err)
	}
	// A protected request must not lose its legacy settlement to pool sampling or
	// overflow. Completion is recorded inside the billing task, never on HTTP exit.
	h.submitMandatoryUsageRecordTask(parent, task)
}
