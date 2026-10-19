package domain

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/shopspring/decimal"
)

// FreezePricing is a pre-resolved, pure factory: no I/O or current configuration
// reads may occur under the repository's final admission locks.
type SubscriptionQuotaAdmissionParams struct {
	UserID              int64
	APIKeyID            int64
	GroupID             int64
	AccountID           int64
	ExpectedAccountType string
	RequestID           string
	FreezePricing       func(time.Time) (json.RawMessage, error)
}

type SubscriptionQuotaBillingSnapshot struct {
	SchemaVersion       int             `json:"schema_version"`
	RequestRowID        int64           `json:"request_row_id"`
	RequestID           string          `json:"request_id"`
	UserID              int64           `json:"user_id"`
	APIKeyID            int64           `json:"api_key_id"`
	GroupID             int64           `json:"group_id"`
	SubscriptionID      int64           `json:"subscription_id"`
	TermID              int64           `json:"term_id"`
	AccountID           int64           `json:"account_id"`
	AccountType         string          `json:"account_type"`
	KeyQuotaTracked     bool            `json:"key_quota_tracked"`
	KeyRateLimitTracked bool            `json:"key_rate_limit_tracked"`
	AccountQuotaTracked bool            `json:"account_quota_tracked"`
	AdmittedAt          time.Time       `json:"admitted_at"`
	PricingAt           time.Time       `json:"pricing_at"`
	PricingSnapshotHash string          `json:"pricing_snapshot_hash"`
	PricingSnapshot     json.RawMessage `json:"pricing_snapshot"`
}

func (s SubscriptionQuotaBillingSnapshot) Clone() SubscriptionQuotaBillingSnapshot {
	s.PricingSnapshot = append(json.RawMessage(nil), s.PricingSnapshot...)
	return s
}

func SubscriptionQuotaSnapshotsEqual(a, b SubscriptionQuotaBillingSnapshot) bool {
	if a.SchemaVersion != b.SchemaVersion || a.RequestRowID != b.RequestRowID || a.RequestID != b.RequestID || a.UserID != b.UserID || a.APIKeyID != b.APIKeyID || a.GroupID != b.GroupID || a.SubscriptionID != b.SubscriptionID || a.TermID != b.TermID || a.AccountID != b.AccountID || a.AccountType != b.AccountType || a.KeyQuotaTracked != b.KeyQuotaTracked || a.KeyRateLimitTracked != b.KeyRateLimitTracked || a.AccountQuotaTracked != b.AccountQuotaTracked || !a.AdmittedAt.Equal(b.AdmittedAt) || !a.PricingAt.Equal(b.PricingAt) || a.PricingSnapshotHash != b.PricingSnapshotHash {
		return false
	}
	_, aHash, aErr := MarshalSubscriptionQuotaPricingEnvelope(a)
	_, bHash, bErr := MarshalSubscriptionQuotaPricingEnvelope(b)
	return aErr == nil && bErr == nil && aHash == bHash && aHash == a.PricingSnapshotHash
}

type SubscriptionQuotaKnownUsage struct {
	Snapshot           SubscriptionQuotaBillingSnapshot
	ActualCostUSD      string
	ReceiptVersion     int
	RequestFingerprint string
	BillingPayload     json.RawMessage
	UsageLogPayload    json.RawMessage
	RequestRevision    int64
	LeaseOwner         string
	RecoveryAttempts   int
}

// Term counters are exact end to end. The legacy float counter fields remain
// dedicated to the existing billing sources and their original fingerprints.
type SubscriptionQuotaCounterCosts struct {
	APIKeyQuotaCost     decimal.Decimal
	APIKeyRateLimitCost decimal.Decimal
	AccountQuotaCost    decimal.Decimal
}
type SubscriptionQuotaNoChargeEvidence struct {
	Kind      string `json:"kind"`
	Reference string `json:"reference"`
}
type SubscriptionQuotaRecoveryClaim struct {
	Owner string
	Limit int
	Lease time.Duration
}

type SubscriptionQuotaPricingEnvelope struct {
	SchemaVersion       int             `json:"schema_version"`
	AccountID           int64           `json:"account_id"`
	AccountType         string          `json:"account_type"`
	KeyQuotaTracked     bool            `json:"key_quota_tracked"`
	KeyRateLimitTracked bool            `json:"key_rate_limit_tracked"`
	AccountQuotaTracked bool            `json:"account_quota_tracked"`
	Pricing             json.RawMessage `json:"pricing"`
}

func CanonicalSubscriptionQuotaJSON(raw json.RawMessage) (json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value map[string]any
	if err := decoder.Decode(&value); err != nil || value == nil {
		return nil, fmt.Errorf("invalid quota JSON object")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("invalid trailing quota JSON")
	}
	canonical, err := canonicalSubscriptionQuotaNumbers(value)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(canonical)
	return data, err
}
func canonicalSubscriptionQuotaNumbers(value any) (any, error) {
	switch typed := value.(type) {
	case json.Number:
		if len(typed) > 1024 {
			return nil, fmt.Errorf("quota JSON number is too large")
		}
		amount, err := decimal.NewFromString(string(typed))
		if err != nil || amount.Exponent() > 1024 || amount.Exponent() < -1024 {
			return nil, fmt.Errorf("invalid quota JSON number")
		}
		return json.Number(amount.String()), nil
	case map[string]any:
		for key, item := range typed {
			normalized, err := canonicalSubscriptionQuotaNumbers(item)
			if err != nil {
				return nil, err
			}
			typed[key] = normalized
		}
		return typed, nil
	case []any:
		for index, item := range typed {
			normalized, err := canonicalSubscriptionQuotaNumbers(item)
			if err != nil {
				return nil, err
			}
			typed[index] = normalized
		}
		return typed, nil
	default:
		return value, nil
	}
}
func MarshalSubscriptionQuotaPricingEnvelope(snapshot SubscriptionQuotaBillingSnapshot) (json.RawMessage, string, error) {
	pricing, err := CanonicalSubscriptionQuotaJSON(snapshot.PricingSnapshot)
	if err != nil {
		return nil, "", err
	}
	data, err := json.Marshal(SubscriptionQuotaPricingEnvelope{SchemaVersion: 1, AccountID: snapshot.AccountID, AccountType: snapshot.AccountType, KeyQuotaTracked: snapshot.KeyQuotaTracked, KeyRateLimitTracked: snapshot.KeyRateLimitTracked, AccountQuotaTracked: snapshot.AccountQuotaTracked, Pricing: pricing})
	if err != nil {
		return nil, "", err
	}
	data, err = CanonicalSubscriptionQuotaJSON(data)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(data)
	return data, hex.EncodeToString(sum[:]), nil
}
func NormalizeSubscriptionQuotaCost(value string) (string, error) {
	if !subscriptionQuotaMoneyPattern.MatchString(value) {
		return "", fmt.Errorf("invalid quota cost")
	}
	amount, err := decimal.NewFromString(value)
	if err != nil || amount.IsNegative() {
		return "", fmt.Errorf("invalid quota cost")
	}
	return amount.StringFixed(8), nil
}
