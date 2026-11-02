package service

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

const SubscriptionQuotaUsageBillingReceiptVersion = 1

type subscriptionQuotaUsageReceipt struct {
	Version             int                                     `json:"version"`
	Snapshot            domain.SubscriptionQuotaBillingSnapshot `json:"snapshot"`
	RequestFingerprint  string                                  `json:"request_fingerprint"`
	RequestPayloadHash  string                                  `json:"request_payload_hash"`
	AccountType         string                                  `json:"account_type"`
	Model               string                                  `json:"model"`
	ServiceTier         string                                  `json:"service_tier"`
	ReasoningEffort     string                                  `json:"reasoning_effort"`
	InputTokens         int                                     `json:"input_tokens"`
	OutputTokens        int                                     `json:"output_tokens"`
	CacheCreationTokens int                                     `json:"cache_creation_tokens"`
	CacheReadTokens     int                                     `json:"cache_read_tokens"`
	TermCost            string                                  `json:"term_cost"`
	APIKeyQuotaCost     string                                  `json:"api_key_quota_cost"`
	APIKeyRateLimitCost string                                  `json:"api_key_rate_limit_cost"`
	AccountQuotaCost    string                                  `json:"account_quota_cost"`
}

func validateSubscriptionQuotaUsageCommand(c *UsageBillingCommand) error {
	s := c.SubscriptionTermSnapshot
	if s == nil || c.SubscriptionTermCounters == nil {
		return ErrSubscriptionQuotaReceiptConflict
	}
	// Check raw alternative sources before any normalization: negative, NaN and
	// zero-valued populated source identities must never disappear via rounding.
	if c.BalanceCost != 0 || c.SubscriptionCost != 0 || c.CarpoolSnapshot != nil || !c.CarpoolCost.IsZero() || c.SubscriptionID != nil {
		return ErrUsageBillingSourceConflict
	}
	if c.APIKeyQuotaCost != 0 || c.APIKeyRateLimitCost != 0 || c.AccountQuotaCost != 0 {
		return ErrUsageBillingSourceConflict
	}
	if s.SchemaVersion != 1 || s.RequestRowID <= 0 || s.UserID <= 0 || s.APIKeyID <= 0 || s.GroupID <= 0 || s.SubscriptionID <= 0 || s.TermID <= 0 || s.AccountID <= 0 || s.AdmittedAt.IsZero() || s.PricingAt.IsZero() || !s.AdmittedAt.Equal(s.PricingAt) || s.UserID != c.UserID || s.APIKeyID != c.APIKeyID || s.AccountID != c.AccountID || s.RequestID != c.RequestID || s.AccountType != c.AccountType || c.BillingType != BillingTypeSubscription {
		return ErrSubscriptionQuotaReceiptConflict
	}
	id, err := uuid.Parse(strings.TrimPrefix(c.RequestID, "subquota:"))
	if err != nil || "subquota:"+id.String() != c.RequestID {
		return ErrSubscriptionQuotaReceiptConflict
	}
	_, hash, err := domain.MarshalSubscriptionQuotaPricingEnvelope(*s)
	if err != nil || hash != s.PricingSnapshotHash {
		return ErrSubscriptionQuotaReceiptConflict
	}
	if c.SubscriptionTermCost.IsNegative() || !c.SubscriptionTermCost.Equal(c.SubscriptionTermCost.Round(UsageBillingMonetaryScale)) {
		return ErrSubscriptionQuotaReceiptConflict
	}
	if _, err = domain.NormalizeSubscriptionQuotaCost(c.SubscriptionTermCost.StringFixed(8)); err != nil {
		return ErrSubscriptionQuotaReceiptConflict
	}
	for _, v := range []decimal.Decimal{c.SubscriptionTermCounters.APIKeyQuotaCost, c.SubscriptionTermCounters.APIKeyRateLimitCost, c.SubscriptionTermCounters.AccountQuotaCost} {
		if v.IsNegative() || !v.Equal(v.Round(8)) {
			return ErrSubscriptionQuotaReceiptConflict
		}
		if _, err = domain.NormalizeSubscriptionQuotaCost(v.StringFixed(8)); err != nil {
			return ErrSubscriptionQuotaReceiptConflict
		}
	}
	if !s.KeyQuotaTracked && !c.SubscriptionTermCounters.APIKeyQuotaCost.IsZero() || !s.KeyRateLimitTracked && !c.SubscriptionTermCounters.APIKeyRateLimitCost.IsZero() || !s.AccountQuotaTracked && !c.SubscriptionTermCounters.AccountQuotaCost.IsZero() {
		return ErrSubscriptionQuotaReceiptConflict
	}
	if s.KeyQuotaTracked && !c.SubscriptionTermCounters.APIKeyQuotaCost.Equal(c.SubscriptionTermCost) || s.KeyRateLimitTracked && !c.SubscriptionTermCounters.APIKeyRateLimitCost.Equal(c.SubscriptionTermCost) {
		return ErrSubscriptionQuotaReceiptConflict
	}
	if strings.TrimSpace(c.Model) == "" || c.InputTokens < 0 || c.OutputTokens < 0 || c.CacheCreationTokens < 0 || c.CacheReadTokens < 0 || c.ImageCount != 0 || c.MediaType != "" {
		return ErrSubscriptionQuotaReceiptConflict
	}
	if c.RequestFingerprint != "" && c.RequestFingerprint != buildSubscriptionQuotaUsageFingerprint(c) {
		return ErrUsageBillingRequestConflict
	}
	return nil
}

func subscriptionQuotaReceiptFromCommand(c *UsageBillingCommand) subscriptionQuotaUsageReceipt {
	return subscriptionQuotaUsageReceipt{Version: SubscriptionQuotaUsageBillingReceiptVersion, Snapshot: c.SubscriptionTermSnapshot.Clone(), RequestFingerprint: c.RequestFingerprint, RequestPayloadHash: c.RequestPayloadHash, AccountType: c.AccountType, Model: c.Model, ServiceTier: c.ServiceTier, ReasoningEffort: c.ReasoningEffort, InputTokens: c.InputTokens, OutputTokens: c.OutputTokens, CacheCreationTokens: c.CacheCreationTokens, CacheReadTokens: c.CacheReadTokens, TermCost: c.SubscriptionTermCost.StringFixed(8), APIKeyQuotaCost: c.SubscriptionTermCounters.APIKeyQuotaCost.StringFixed(8), APIKeyRateLimitCost: c.SubscriptionTermCounters.APIKeyRateLimitCost.StringFixed(8), AccountQuotaCost: c.SubscriptionTermCounters.AccountQuotaCost.StringFixed(8)}
}
func buildSubscriptionQuotaUsageFingerprint(c *UsageBillingCommand) string {
	if c == nil || c.SubscriptionTermSnapshot == nil || c.SubscriptionTermCounters == nil {
		return ""
	}
	// Called only for validated finite inputs. Keep legacy fingerprint ordering
	// completely separate, including its historical pre-quantization behavior.
	for _, v := range []float64{c.APIKeyQuotaCost, c.APIKeyRateLimitCost, c.AccountQuotaCost} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return ""
		}
	}
	receipt := subscriptionQuotaReceiptFromCommand(c)
	receipt.RequestFingerprint = ""
	data, err := json.Marshal(receipt)
	if err != nil {
		return ""
	}
	data, err = domain.CanonicalSubscriptionQuotaJSON(data)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func MarshalSubscriptionQuotaUsageBillingReceipt(c *UsageBillingCommand) (json.RawMessage, error) {
	if c == nil {
		return nil, ErrSubscriptionQuotaReceiptConflict
	}
	if err := validateSubscriptionQuotaUsageCommand(c); err != nil {
		return nil, err
	}
	copy := *c
	snapshot := c.SubscriptionTermSnapshot.Clone()
	copy.SubscriptionTermSnapshot = &snapshot
	copy.Normalize()
	receipt := subscriptionQuotaReceiptFromCommand(&copy)
	data, err := json.Marshal(receipt)
	return data, err
}
func DecodeSubscriptionQuotaUsageBillingReceipt(data json.RawMessage) (*UsageBillingCommand, error) {
	var receipt subscriptionQuotaUsageReceipt
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&receipt); err != nil {
		return nil, ErrSubscriptionQuotaReceiptConflict
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF || receipt.Version != 1 {
		return nil, ErrSubscriptionQuotaReceiptConflict
	}
	cost, err := domain.NormalizeSubscriptionQuotaCost(receipt.TermCost)
	if err != nil {
		return nil, ErrSubscriptionQuotaReceiptConflict
	}
	termCost, _ := decimal.NewFromString(cost)
	values := []decimal.Decimal{}
	for _, text := range []string{receipt.APIKeyQuotaCost, receipt.APIKeyRateLimitCost, receipt.AccountQuotaCost} {
		normalized, err := domain.NormalizeSubscriptionQuotaCost(text)
		if err != nil {
			return nil, ErrSubscriptionQuotaReceiptConflict
		}
		value, _ := decimal.NewFromString(normalized)
		values = append(values, value)
	}
	s := receipt.Snapshot.Clone()
	c := &UsageBillingCommand{RequestID: s.RequestID, UserID: s.UserID, APIKeyID: s.APIKeyID, AccountID: s.AccountID, AccountType: receipt.AccountType, Model: receipt.Model, ServiceTier: receipt.ServiceTier, ReasoningEffort: receipt.ReasoningEffort, BillingType: BillingTypeSubscription, InputTokens: receipt.InputTokens, OutputTokens: receipt.OutputTokens, CacheCreationTokens: receipt.CacheCreationTokens, CacheReadTokens: receipt.CacheReadTokens, SubscriptionTermSnapshot: &s, SubscriptionTermCost: termCost, SubscriptionTermCounters: &domain.SubscriptionQuotaCounterCosts{APIKeyQuotaCost: values[0], APIKeyRateLimitCost: values[1], AccountQuotaCost: values[2]}, RequestFingerprint: receipt.RequestFingerprint, RequestPayloadHash: receipt.RequestPayloadHash}
	if err = validateSubscriptionQuotaUsageCommand(c); err != nil {
		return nil, err
	}
	if c.RequestFingerprint == "" {
		return nil, fmt.Errorf("%w: missing fingerprint", ErrSubscriptionQuotaReceiptConflict)
	}
	return c, nil
}
