package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *GatewayHandler) SetSubscriptionQuotaSummaryReader(reader service.SubscriptionQuotaSummaryReader) {
	h.subscriptionQuotaSummaryReader = reader
}

func (h *GatewayHandler) addUserConcurrencyUsage(ctx context.Context, resp gin.H, key *service.APIKey) {
	if key == nil || key.User == nil {
		return
	}
	var concurrency *service.ConcurrencyService
	if h.concurrencyHelper != nil {
		concurrency = h.concurrencyHelper.concurrencyService
	}
	view := concurrency.ClaudeConcurrencyView(ctx, key.User)
	resp["legacy_current_concurrency"] = view.LegacyCurrentConcurrency
	resp["legacy_concurrency_status"] = view.LegacyConcurrencyStatus
	resp["claude_subscription_concurrency"] = key.User.ClaudeSubscriptionConcurrency
	resp["default_claude_subscription_concurrency"] = view.DefaultClaudeSubscriptionConcurrency
	resp["effective_claude_subscription_concurrency"] = view.EffectiveClaudeSubscriptionConcurrency
	resp["claude_current_concurrency"] = view.ClaudeCurrentConcurrency
	resp["claude_waiting_count"] = view.ClaudeWaitingCount
	resp["claude_concurrency_status"] = view.ClaudeConcurrencyStatus
	resp["concurrency"] = key.User.Concurrency
	resp["current_concurrency"] = view.LegacyCurrentConcurrency
}

func (h *GatewayHandler) usageSubscriptionQuota(c *gin.Context, ctx context.Context, key *service.APIKey, userID int64, usage gin.H, daily, models any) {
	if key.UserID != userID || key.GroupID == nil || *key.GroupID != key.Group.ID {
		h.errorResponse(c, http.StatusServiceUnavailable, "TERM_BILLING_UNAVAILABLE", "Claude quota identity is unavailable")
		return
	}
	var summary *domain.SubscriptionQuotaSummary
	var err error
	if h.subscriptionQuotaSummaryReader == nil {
		err = errors.New("quota summary reader unavailable")
	} else {
		summary, err = h.subscriptionQuotaSummaryReader.GetSummary(ctx, userID, key.Group.ID)
	}
	if err != nil || summary == nil || summary.GroupID != key.Group.ID || summary.QuotaMode != "term" {
		h.errorResponse(c, http.StatusServiceUnavailable, "TERM_BILLING_UNAVAILABLE", "Claude quota summary is unavailable")
		return
	}
	resp := gin.H{"mode": "unrestricted"}
	if key.Quota > 0 || key.HasRateLimits() {
		resp = h.buildKeyLimitedUsage(ctx, key, usage, daily, models)
		if expiry, ok := resp["expires_at"]; ok {
			resp["key_expires_at"] = expiry
		}
		if days, ok := resp["days_until_expiry"]; ok {
			resp["key_days_until_expiry"] = days
			delete(resp, "days_until_expiry")
		}
	}
	status, remaining := "not_active", "0.00000000"
	var expiry any
	if current := summary.CurrentTerm; current != nil {
		status, remaining, expiry = current.Status, current.RemainingUSD, current.ExpiresAt
	} else if len(summary.NextTerms) > 0 {
		status = "scheduled"
	} else if summary.LastTerm != nil {
		status = summary.LastTerm.Status
	}
	resp["billing_scope"], resp["quota_mode"] = "claude_subscription", "term"
	h.addUserConcurrencyUsage(ctx, resp, key)
	resp["quota_summary"] = dto.SubscriptionQuotaSummaryFromDomain(summary)
	resp["remaining"], resp["unit"], resp["expires_at"] = remaining, "USD", expiry
	resp["status"], resp["key_status"], resp["planName"] = status, key.Status, key.Group.Name
	resp["isValid"] = status == "active" && key.Status == service.StatusAPIKeyActive &&
		(key.ExpiresAt == nil || summary.ServerTime.Before(*key.ExpiresAt)) && (key.Quota <= 0 || key.QuotaUsed < key.Quota)
	if usage != nil {
		resp["usage"] = usage
	}
	if daily != nil {
		resp["daily_usage"] = daily
	}
	if models != nil {
		resp["model_stats"] = models
	}
	c.JSON(http.StatusOK, resp)
}
