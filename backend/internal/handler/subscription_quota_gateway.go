package handler

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *GatewayHandler) prepareSubscriptionQuotaForward(c *gin.Context, original, current *service.APIKey, platform string, channel service.ChannelUsageFields) (*service.SubscriptionQuotaForwardState, error) {
	if original == nil || original.Group == nil || !original.Group.HasTermQuotaMode() {
		return nil, nil
	}
	if platform != service.PlatformAnthropic || original.GroupID == nil || current == nil || current.GroupID == nil || *original.GroupID != *current.GroupID {
		return nil, service.ErrSubscriptionQuotaEndpointUnsupported
	}
	// Only the exact generating routes have adapters. In particular, arbitrary
	// safe URL suffixes are not equivalent to an Anthropic Responses generation.
	if c.Request.Method != http.MethodPost {
		return nil, service.ErrSubscriptionQuotaEndpointUnsupported
	}
	switch c.Request.URL.Path {
	case "/v1/messages", "/v1/chat/completions", "/chat/completions", "/v1/responses", "/responses", "/backend-api/codex/responses":
	default:
		return nil, service.ErrSubscriptionQuotaEndpointUnsupported
	}
	ctx, state, err := service.ContextWithSubscriptionQuotaForward(c.Request.Context(), original, h.billingCacheService.SubscriptionQuotaGatewayBilling(), channel)
	if err == nil {
		c.Request = c.Request.WithContext(ctx)
	}
	return state, err
}

func (h *GatewayHandler) writeSubscriptionQuotaForwardError(c *gin.Context, err error, streamStarted bool) bool {
	reason := errors.Reason(err)
	if !strings.HasPrefix(reason, "TERM_") {
		return false
	}
	h.handleStreamingAwareErrorWithCode(c, errors.Code(err), "api_error", reason, errors.Message(err), streamStarted)
	return true
}

// A final trusted bill survives a later adapter/client error. Unknown usage is
// left to FinishForward rather than converted into a zero-valued usage job.
func (h *GatewayHandler) submitSubscriptionQuotaUsageAfterError(c *gin.Context, state *service.SubscriptionQuotaForwardState, key *service.APIKey, account *service.Account, subscription *service.UserSubscription, result *service.ForwardResult, pricingAt time.Time, channel service.ChannelUsageFields, payload []byte) {
	if state == nil || state.Snapshot() == nil || result == nil || !result.UsageObserved || !result.UsageFinal {
		return
	}
	input := &service.RecordUsageInput{Result: result, APIKey: key, User: key.User, Account: account, Subscription: subscription, PricingAt: pricingAt, ForceCacheBilling: service.IsForceCacheBilling(c.Request.Context()), InboundEndpoint: GetInboundEndpoint(c), UpstreamEndpoint: GetUpstreamEndpoint(c, account.Platform), UserAgent: c.GetHeader("User-Agent"), IPAddress: ip.GetClientIP(c), RequestPayloadHash: service.HashUsageRequestPayload(payload), APIKeyService: h.apiKeyService, SessionID: service.ExtractClientSessionID(c), QuotaPlatform: service.QuotaPlatform(c.Request.Context(), key), ChannelUsageFields: channel}
	h.submitProtectedGroupUsageRecordTask(c.Request.Context(), nil, result, account.ID, pricingAt, func(ctx context.Context) {
		input.SubscriptionQuotaSnapshot = service.SubscriptionQuotaForwardBillingSnapshot(ctx)
		if err := h.gatewayService.RecordUsage(ctx, input); err != nil {
			slog.Error("term final usage persistence failed; request remains held", "error", err)
		}
	})
}

func finishSubscriptionQuotaForward(state *service.SubscriptionQuotaForwardState) {
	if err := state.FinishForward(); err != nil {
		slog.Error("term forward facts remain unresolved", "error", err)
	}
}
