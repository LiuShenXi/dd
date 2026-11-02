package service

// SetSubscriptionQuotaEntitlementReader wires the authoritative current-card
// reader before the service is exposed to requests.
func (s *APIKeyService) SetSubscriptionQuotaEntitlementReader(reader SubscriptionQuotaEntitlementReader) {
	s.subscriptionQuotaEntitlements = reader
}

// SetAdminSubscriptionQuotaEntitlementReader preserves the AdminService public
// interface while requiring startup wiring to use its concrete implementation.
func SetAdminSubscriptionQuotaEntitlementReader(s AdminService, reader SubscriptionQuotaEntitlementReader) {
	if impl, ok := s.(*adminServiceImpl); ok {
		impl.subscriptionQuotaEntitlements = reader
	}
}
