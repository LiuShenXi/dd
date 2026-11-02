package domain

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// SubscriptionQuotaOrderSnapshot is the purchased entitlement, separate from
// the provider/price snapshot. It is never reconstructed from a mutable plan.
type SubscriptionQuotaOrderSnapshot struct {
	SchemaVersion   int    `json:"schema_version"`
	Mode            string `json:"mode"`
	GroupID         int64  `json:"group_id"`
	Platform        string `json:"platform"`
	DurationSeconds int64  `json:"duration_seconds"`
	QuotaUSD        string `json:"quota_usd"`
	SourcePlanID    int64  `json:"source_plan_id"`
	PlanName        string `json:"plan_name"`
	ProductName     string `json:"product_name"`
	GroupName       string `json:"group_name"`
}

func (s SubscriptionQuotaOrderSnapshot) Validate() error {
	if s.SchemaVersion != 1 || s.Mode != SubscriptionQuotaModeTerm || s.GroupID <= 0 || s.Platform != "anthropic" || s.DurationSeconds != int64(SubscriptionQuotaDuration/time.Second) || s.SourcePlanID <= 0 || strings.TrimSpace(s.PlanName) == "" || strings.TrimSpace(s.GroupName) == "" {
		return fmt.Errorf("invalid subscription quota order snapshot")
	}
	quota, err := NormalizeSubscriptionQuotaMoney(s.QuotaUSD)
	if err != nil || quota != s.QuotaUSD {
		return fmt.Errorf("invalid canonical order quota")
	}
	return nil
}

// A nil object means a legacy order. Any non-nil malformed or future-version
// object fails closed; it must never enter native subscription fulfillment.
func ParseSubscriptionQuotaOrderSnapshot(value map[string]any) (*SubscriptionQuotaOrderSnapshot, error) {
	if value == nil {
		return nil, nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var snapshot SubscriptionQuotaOrderSnapshot
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&snapshot); err != nil {
		return nil, err
	}
	if err = snapshot.Validate(); err != nil {
		return nil, err
	}
	return &snapshot, nil
}

func (s SubscriptionQuotaOrderSnapshot) Map() map[string]any {
	// Preserve exact IDs rather than round-tripping them through float64.
	return map[string]any{"schema_version": s.SchemaVersion, "mode": s.Mode, "group_id": s.GroupID, "platform": s.Platform, "duration_seconds": s.DurationSeconds, "quota_usd": s.QuotaUSD, "source_plan_id": s.SourcePlanID, "plan_name": s.PlanName, "product_name": s.ProductName, "group_name": s.GroupName}
}

type SubscriptionQuotaPaidTermParams struct {
	OrderID      int64
	LeaseVersion time.Time
}
