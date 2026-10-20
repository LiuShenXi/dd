package dto

import (
	"encoding/json"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func userSubscriptionQuotaMode(sub *service.UserSubscription) string {
	if sub.QuotaMode == "term" || sub.Group != nil && sub.Group.HasTermQuotaMode() {
		return "term"
	}
	return "windows"
}

type plainUserSubscription UserSubscription

func marshalSubscriptionProjection(value any, term bool) ([]byte, error) {
	data, err := json.Marshal(value)
	if err != nil || !term {
		return data, err
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return nil, err
	}
	for _, field := range []string{"daily_window_start", "weekly_window_start", "monthly_window_start", "daily_usage_usd", "weekly_usage_usd", "monthly_usage_usd"} {
		delete(object, field)
	}
	return json.Marshal(object)
}

func (s UserSubscription) MarshalJSON() ([]byte, error) {
	return marshalSubscriptionProjection(plainUserSubscription(s), s.QuotaMode == "term")
}

// An explicit admin marshaler preserves admin-only fields instead of inheriting
// the embedded public DTO's marshaler and accidentally dropping those fields.
func (s AdminUserSubscription) MarshalJSON() ([]byte, error) {
	value := struct {
		plainUserSubscription
		AssignedBy     *int64    `json:"assigned_by"`
		AssignedAt     time.Time `json:"assigned_at"`
		Notes          string    `json:"notes"`
		AssignedByUser *User     `json:"assigned_by_user,omitempty"`
	}{plainUserSubscription(s.UserSubscription), s.AssignedBy, s.AssignedAt, s.Notes, s.AssignedByUser}
	return marshalSubscriptionProjection(value, s.QuotaMode == "term")
}
