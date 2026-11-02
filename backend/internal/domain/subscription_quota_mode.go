package domain

const (
	SubscriptionQuotaModeWindows = "windows"
	SubscriptionQuotaModeTerm    = "term"
)

// NormalizeSubscriptionQuotaMode preserves the pre-mode representation of
// existing groups. Unknown nonempty values remain unknown and must fail closed.
func NormalizeSubscriptionQuotaMode(mode string) string {
	if mode == "" {
		return SubscriptionQuotaModeWindows
	}
	return mode
}
