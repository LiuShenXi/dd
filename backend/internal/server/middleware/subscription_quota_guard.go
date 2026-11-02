package middleware

import (
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func subscriptionQuotaProtectedGroup(cfg *config.Config, groupID int64) bool {
	if cfg == nil {
		return false
	}
	for _, protectedID := range cfg.SubscriptionQuota.ProtectedGroupIDs {
		if groupID == protectedID {
			return true
		}
	}
	return false
}

// Metadata uses the authenticated identity and canonical term projection without
// issuing a bill or granting generation rights.
func subscriptionQuotaDiscoveryRequest(c *gin.Context) bool {
	if c.Request.Method == http.MethodPost && (c.Request.URL.Path == "/v1/messages/count_tokens" || c.Request.URL.Path == "/messages/count_tokens") {
		return true
	}
	if c.Request.Method != http.MethodGet {
		return false
	}
	path := c.Request.URL.Path
	return path == "/v1/usage" || path == "/models" || strings.HasPrefix(path, "/models/") ||
		path == "/v1/models" || strings.HasPrefix(path, "/v1/models/") ||
		path == "/backend-api/codex/models" || path == "/v1/sub2api/billing"
}

// Check before simple mode, subscription loading and every balance fallback.
func abortIfSubscriptionQuotaUnsupported(c *gin.Context, key *service.APIKey, cfg *config.Config, google bool, billing service.SubscriptionQuotaGatewayBilling) bool {
	if key == nil {
		return false
	}
	if key.Group == nil {
		if key.GroupID != nil && subscriptionQuotaProtectedGroup(cfg, *key.GroupID) {
			AbortWithError(c, 503, "TERM_BILLING_UNAVAILABLE", "Protected Claude group metadata is unavailable")
			return true
		}
		return false
	}
	group := key.Group
	mode := group.EffectiveSubscriptionQuotaMode()
	term := mode == domain.SubscriptionQuotaModeTerm
	protected := subscriptionQuotaProtectedGroup(cfg, group.ID)
	if key.GroupID != nil && subscriptionQuotaProtectedGroup(cfg, *key.GroupID) {
		protected = true
	}
	status, reason, message := 0, "", ""
	if mode != domain.SubscriptionQuotaModeWindows && (!term || service.ValidateSubscriptionQuotaMode(group) != nil) {
		status, reason, message = 503, "TERM_BILLING_UNAVAILABLE", "Claude quota group configuration is unavailable"
	} else if protected && (key.GroupID == nil || *key.GroupID != group.ID || group.Platform != service.PlatformAnthropic || (group.SubscriptionType != service.SubscriptionTypeStandard && group.SubscriptionType != service.SubscriptionTypeSubscription)) {
		status, reason, message = 503, "TERM_BILLING_UNAVAILABLE", "Protected Claude group metadata requires refresh"
	} else if (term || protected) && (google || HasForcePlatform(c)) {
		status, reason, message = 403, "TERM_PLATFORM_MISMATCH", "Claude quota keys cannot use a forced provider route"
	} else if protected && cfg.RunMode == config.RunModeSimple {
		status, reason, message = 503, "TERM_BILLING_UNAVAILABLE", "Protected Claude groups require durable billing"
	} else if term && !subscriptionQuotaDiscoveryRequest(c) {
		if !service.SubscriptionQuotaAdmissionEnabled(cfg, group.ID) || billing == nil {
			status, reason, message = 503, "TERM_BILLING_UNAVAILABLE", "Claude quota billing is unavailable"
		} else if !service.SubscriptionQuotaGenerationRouteAllowed(c.Request.Method, c.Request.URL.Path) {
			status, reason, message = 403, "TERM_ENDPOINT_UNSUPPORTED", "This endpoint is not covered by Claude quota billing"
		}
	} else if protected && !service.SubscriptionQuotaProtectedRouteAllowed(c.Request.Method, c.Request.URL.Path) {
		status, reason, message = 403, "TERM_ENDPOINT_UNSUPPORTED", "This endpoint is not covered by the protected Claude group gateway"
	}
	if status == 0 {
		return false
	}
	if google {
		abortWithGoogleError(c, status, message)
	} else {
		AbortWithError(c, status, reason, message)
	}
	return true
}
