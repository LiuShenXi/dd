package service

import (
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"
)

func ProvideSubscriptionQuotaEntitlementReader(repo SubscriptionQuotaRepository) SubscriptionQuotaEntitlementReader {
	return repo
}

// Preserve NewAdminService's public constructor for existing callers. Startup
// always injects the authoritative current-term reader through this provider.
func ProvideAdminService(
	cfg *config.Config,
	userRepo UserRepository,
	groupRepo AdminGroupRepository,
	accountRepo AdminAccountRepository,
	proxyRepo ProxyRepository,
	apiKeyRepo APIKeyRepository,
	redeemCodeRepo RedeemCodeRepository,
	userGroupRateRepo UserGroupRateRepository,
	userRPMCache UserRPMCache,
	billingCacheService *BillingCacheService,
	proxyProber ProxyExitInfoProber,
	proxyLatencyCache ProxyLatencyCache,
	authCacheInvalidator APIKeyAuthCacheInvalidator,
	entClient *dbent.Client,
	settingService *SettingService,
	defaultSubAssigner DefaultSubscriptionAssigner,
	userSubRepo UserSubscriptionRepository,
	privacyClientFactory PrivacyClientFactory,
	runtimeBlocker AccountRuntimeBlocker,
	affiliateService *AffiliateService,
	compositeRouteRepo CompositeModelRouteRepository,
	compositeResolver *CompositeRouteResolver,
	channelCacheInvalidator ChannelCacheInvalidator,
	quotaEntitlements SubscriptionQuotaEntitlementReader,
) AdminService {
	svc := NewAdminService(cfg, userRepo, groupRepo, accountRepo, proxyRepo, apiKeyRepo, redeemCodeRepo, userGroupRateRepo, userRPMCache, billingCacheService, proxyProber, proxyLatencyCache, authCacheInvalidator, entClient, settingService, defaultSubAssigner, userSubRepo, privacyClientFactory, runtimeBlocker, affiliateService, compositeRouteRepo, compositeResolver, channelCacheInvalidator)
	SetAdminSubscriptionQuotaEntitlementReader(svc, quotaEntitlements)
	return svc
}
