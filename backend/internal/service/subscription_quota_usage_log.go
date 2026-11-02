package service

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/shopspring/decimal"
)

// An explicit allowlist, rather than serializing UsageLog and its entity
// pointers, keeps credentials, prompts and arbitrary headers out of receipts.
type subscriptionQuotaUsageLogPayload struct {
	Version                   int       `json:"version"`
	RequestID                 string    `json:"request_id"`
	UserID                    int64     `json:"user_id"`
	APIKeyID                  int64     `json:"api_key_id"`
	AccountID                 int64     `json:"account_id"`
	GroupID                   int64     `json:"group_id"`
	SubscriptionID            int64     `json:"subscription_id"`
	TermID                    int64     `json:"term_id"`
	RequestRowID              int64     `json:"request_row_id"`
	Model                     string    `json:"model"`
	RequestedModel            string    `json:"requested_model"`
	UpstreamModel             *string   `json:"upstream_model"`
	UpstreamResponseModel     *string   `json:"upstream_response_model"`
	ChannelID                 *int64    `json:"channel_id"`
	ModelMappingChain         *string   `json:"model_mapping_chain"`
	BillingMode               *string   `json:"billing_mode"`
	ServiceTier               *string   `json:"service_tier"`
	ReasoningEffort           *string   `json:"reasoning_effort"`
	InboundEndpoint           *string   `json:"inbound_endpoint"`
	UpstreamEndpoint          *string   `json:"upstream_endpoint"`
	InputTokens               int       `json:"input_tokens"`
	OutputTokens              int       `json:"output_tokens"`
	CacheCreationTokens       int       `json:"cache_creation_tokens"`
	CacheReadTokens           int       `json:"cache_read_tokens"`
	CacheCreation5mTokens     int       `json:"cache_creation_5m_tokens"`
	CacheCreation1hTokens     int       `json:"cache_creation_1h_tokens"`
	InputCost                 string    `json:"input_cost"`
	OutputCost                string    `json:"output_cost"`
	CacheCreationCost         string    `json:"cache_creation_cost"`
	CacheReadCost             string    `json:"cache_read_cost"`
	TotalCost                 string    `json:"total_cost"`
	ActualCost                string    `json:"actual_cost"`
	RateMultiplier            float64   `json:"rate_multiplier"`
	AccountRateMultiplier     *float64  `json:"account_rate_multiplier"`
	AccountStatsCost          *string   `json:"account_stats_cost"`
	LongContextBillingApplied bool      `json:"long_context_billing_applied"`
	Stream                    bool      `json:"stream"`
	DurationMs                *int      `json:"duration_ms"`
	FirstTokenMs              *int      `json:"first_token_ms"`
	CreatedAt                 time.Time `json:"created_at"`
}

func MarshalSubscriptionQuotaUsageLogPayload(log *UsageLog) (json.RawMessage, error) {
	return marshalSubscriptionQuotaUsageLogPayload(log, nil)
}
func marshalSubscriptionQuotaUsageLogPayload(log *UsageLog, actual *decimal.Decimal) (json.RawMessage, error) {
	if log == nil || log.GroupID == nil || log.SubscriptionID == nil || log.SubscriptionTermID == nil || log.SubscriptionQuotaRequestID == nil {
		return nil, ErrSubscriptionQuotaReceiptConflict
	}
	for _, amount := range []float64{log.InputCost, log.OutputCost, log.CacheCreationCost, log.CacheReadCost, log.TotalCost, log.ActualCost, log.RateMultiplier} {
		if math.IsNaN(amount) || math.IsInf(amount, 0) || amount < 0 {
			return nil, ErrSubscriptionQuotaReceiptConflict
		}
	}
	money := func(v float64) string { return decimal.NewFromFloat(v).Round(8).StringFixed(8) }
	p := subscriptionQuotaUsageLogPayload{Version: 1, RequestID: log.RequestID, UserID: log.UserID, APIKeyID: log.APIKeyID, AccountID: log.AccountID, GroupID: *log.GroupID, SubscriptionID: *log.SubscriptionID, TermID: *log.SubscriptionTermID, RequestRowID: *log.SubscriptionQuotaRequestID, Model: log.Model, RequestedModel: log.RequestedModel, UpstreamModel: log.UpstreamModel, UpstreamResponseModel: log.UpstreamResponseModel, ChannelID: log.ChannelID, ModelMappingChain: log.ModelMappingChain, BillingMode: log.BillingMode, ServiceTier: log.ServiceTier, ReasoningEffort: log.ReasoningEffort, InboundEndpoint: log.InboundEndpoint, UpstreamEndpoint: log.UpstreamEndpoint, InputTokens: log.InputTokens, OutputTokens: log.OutputTokens, CacheCreationTokens: log.CacheCreationTokens, CacheReadTokens: log.CacheReadTokens, CacheCreation5mTokens: log.CacheCreation5mTokens, CacheCreation1hTokens: log.CacheCreation1hTokens, InputCost: money(log.InputCost), OutputCost: money(log.OutputCost), CacheCreationCost: money(log.CacheCreationCost), CacheReadCost: money(log.CacheReadCost), TotalCost: money(log.TotalCost), ActualCost: money(log.ActualCost), RateMultiplier: log.RateMultiplier, AccountRateMultiplier: log.AccountRateMultiplier, LongContextBillingApplied: log.LongContextBillingApplied, Stream: log.Stream, DurationMs: log.DurationMs, FirstTokenMs: log.FirstTokenMs, CreatedAt: log.CreatedAt}
	if actual != nil {
		p.ActualCost = actual.StringFixed(8)
	}
	if log.SubscriptionQuotaAccountStatsCost != nil {
		if log.SubscriptionQuotaAccountStatsCost.IsNegative() || !log.SubscriptionQuotaAccountStatsCost.Equal(log.SubscriptionQuotaAccountStatsCost.Round(8)) {
			return nil, ErrSubscriptionQuotaReceiptConflict
		}
		v := log.SubscriptionQuotaAccountStatsCost.StringFixed(8)
		p.AccountStatsCost = &v
	} else if log.AccountStatsCost != nil {
		if math.IsNaN(*log.AccountStatsCost) || math.IsInf(*log.AccountStatsCost, 0) || *log.AccountStatsCost < 0 {
			return nil, ErrSubscriptionQuotaReceiptConflict
		}
		v := money(*log.AccountStatsCost)
		p.AccountStatsCost = &v
	}
	data, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	if _, err = DecodeSubscriptionQuotaUsageLogPayload(data); err != nil {
		return nil, err
	}
	return data, nil
}
func ValidateSubscriptionQuotaUsageLogPayload(raw json.RawMessage, command *UsageBillingCommand) error {
	log, err := DecodeSubscriptionQuotaUsageLogPayload(raw)
	if err != nil {
		return err
	}
	if command == nil || command.SubscriptionTermSnapshot == nil {
		return ErrSubscriptionQuotaReceiptConflict
	}
	s := command.SubscriptionTermSnapshot
	var exact subscriptionQuotaUsageLogPayload
	if err = json.Unmarshal(raw, &exact); err != nil {
		return ErrSubscriptionQuotaReceiptConflict
	}
	actual, err := decimal.NewFromString(exact.ActualCost)
	if err != nil {
		return ErrSubscriptionQuotaReceiptConflict
	}
	if log.RequestID != s.RequestID || log.UserID != s.UserID || log.APIKeyID != s.APIKeyID || log.AccountID != s.AccountID || *log.GroupID != s.GroupID || *log.SubscriptionID != s.SubscriptionID || *log.SubscriptionTermID != s.TermID || *log.SubscriptionQuotaRequestID != s.RequestRowID || log.Model != command.Model || log.InputTokens != command.InputTokens || log.OutputTokens != command.OutputTokens || log.CacheCreationTokens != command.CacheCreationTokens || log.CacheReadTokens != command.CacheReadTokens || !actual.Equal(command.SubscriptionTermCost) {
		return ErrSubscriptionQuotaReceiptConflict
	}
	if log.ServiceTier != nil && *log.ServiceTier != command.ServiceTier || log.ReasoningEffort != nil && *log.ReasoningEffort != command.ReasoningEffort {
		return ErrSubscriptionQuotaReceiptConflict
	}
	if !log.CreatedAt.Equal(s.AdmittedAt) {
		return ErrSubscriptionQuotaReceiptConflict
	}
	// Production snapshots also pin display pricing identities. Repository-only
	// synthetic fixtures need not implement the gateway pricing DTO.
	var pricing struct {
		RequestedModel        string   `json:"requested_model"`
		UpstreamModel         string   `json:"upstream_model"`
		AccountRateMultiplier *float64 `json:"account_rate_multiplier"`
	}
	if json.Unmarshal(s.PricingSnapshot, &pricing) != nil {
		return ErrSubscriptionQuotaReceiptConflict
	}
	if pricing.RequestedModel != "" && log.RequestedModel != pricing.RequestedModel {
		return ErrSubscriptionQuotaReceiptConflict
	}
	if pricing.UpstreamModel != "" && (log.UpstreamModel == nil || *log.UpstreamModel != pricing.UpstreamModel) {
		return ErrSubscriptionQuotaReceiptConflict
	}
	if pricing.AccountRateMultiplier != nil && (log.AccountRateMultiplier == nil || *log.AccountRateMultiplier != *pricing.AccountRateMultiplier) {
		return ErrSubscriptionQuotaReceiptConflict
	}
	if (log.CacheCreation5mTokens > 0 || log.CacheCreation1hTokens > 0) && (int64(log.CacheCreation5mTokens)+int64(log.CacheCreation1hTokens) != int64(log.CacheCreationTokens)) {
		return ErrSubscriptionQuotaReceiptConflict
	}
	return nil
}

func MarshalSubscriptionQuotaUsageLogPayloadForCommand(log *UsageLog, command *UsageBillingCommand) (json.RawMessage, error) {
	if command == nil {
		return nil, ErrSubscriptionQuotaReceiptConflict
	}
	data, err := marshalSubscriptionQuotaUsageLogPayload(log, &command.SubscriptionTermCost)
	if err != nil {
		return nil, err
	}
	var p subscriptionQuotaUsageLogPayload
	if err = json.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	p.ActualCost = command.SubscriptionTermCost.StringFixed(8)
	data, err = json.Marshal(p)
	if err != nil {
		return nil, err
	}
	if err = ValidateSubscriptionQuotaUsageLogPayload(data, command); err != nil {
		return nil, err
	}
	return data, nil
}

// Exact monetary columns for statistics publication. Analytics may expose
// floats, but storage and receipt identity never round-trip through them.
func SubscriptionQuotaUsageLogExactAmounts(raw json.RawMessage) (map[string]string, error) {
	if _, err := DecodeSubscriptionQuotaUsageLogPayload(raw); err != nil {
		return nil, err
	}
	var p subscriptionQuotaUsageLogPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	amounts := map[string]string{"input_cost": p.InputCost, "output_cost": p.OutputCost, "cache_creation_cost": p.CacheCreationCost, "cache_read_cost": p.CacheReadCost, "total_cost": p.TotalCost, "actual_cost": p.ActualCost}
	if p.AccountStatsCost != nil {
		amounts["account_stats_cost"] = *p.AccountStatsCost
	}
	return amounts, nil
}

func DecodeSubscriptionQuotaUsageLogPayload(raw json.RawMessage) (*UsageLog, error) {
	var p subscriptionQuotaUsageLogPayload
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&p); err != nil {
		return nil, ErrSubscriptionQuotaReceiptConflict
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, ErrSubscriptionQuotaReceiptConflict
	}
	if p.Version != 1 || p.RequestID == "" || p.UserID <= 0 || p.APIKeyID <= 0 || p.AccountID <= 0 || p.GroupID <= 0 || p.SubscriptionID <= 0 || p.TermID <= 0 || p.RequestRowID <= 0 || p.Model == "" || p.CreatedAt.IsZero() || p.InputTokens < 0 || p.OutputTokens < 0 || p.CacheCreationTokens < 0 || p.CacheReadTokens < 0 || p.CacheCreation5mTokens < 0 || p.CacheCreation1hTokens < 0 || math.IsNaN(p.RateMultiplier) || math.IsInf(p.RateMultiplier, 0) || p.RateMultiplier < 0 {
		return nil, ErrSubscriptionQuotaReceiptConflict
	}
	values := []float64{}
	for _, text := range []string{p.InputCost, p.OutputCost, p.CacheCreationCost, p.CacheReadCost, p.TotalCost, p.ActualCost} {
		normalized, err := domain.NormalizeSubscriptionQuotaCost(text)
		if err != nil {
			return nil, ErrSubscriptionQuotaReceiptConflict
		}
		cost, _ := decimal.NewFromString(normalized)
		value, _ := cost.Float64()
		values = append(values, value)
	}
	for _, value := range []*float64{p.AccountRateMultiplier} {
		if value != nil && (math.IsNaN(*value) || math.IsInf(*value, 0) || *value < 0) {
			return nil, ErrSubscriptionQuotaReceiptConflict
		}
	}
	var statsFloat *float64
	var statsExact *decimal.Decimal
	if p.AccountStatsCost != nil {
		normalized, err := domain.NormalizeSubscriptionQuotaCost(*p.AccountStatsCost)
		if err != nil {
			return nil, ErrSubscriptionQuotaReceiptConflict
		}
		value, _ := decimal.NewFromString(normalized)
		f, _ := value.Float64()
		statsFloat = &f
		statsExact = &value
	}
	return &UsageLog{SubscriptionQuotaAccountStatsCost: statsExact, RequestID: p.RequestID, UserID: p.UserID, APIKeyID: p.APIKeyID, AccountID: p.AccountID, GroupID: &p.GroupID, SubscriptionID: &p.SubscriptionID, SubscriptionTermID: &p.TermID, SubscriptionQuotaRequestID: &p.RequestRowID, Model: p.Model, RequestedModel: p.RequestedModel, UpstreamModel: p.UpstreamModel, UpstreamResponseModel: p.UpstreamResponseModel, ChannelID: p.ChannelID, ModelMappingChain: p.ModelMappingChain, BillingMode: p.BillingMode, ServiceTier: p.ServiceTier, ReasoningEffort: p.ReasoningEffort, InboundEndpoint: p.InboundEndpoint, UpstreamEndpoint: p.UpstreamEndpoint, InputTokens: p.InputTokens, OutputTokens: p.OutputTokens, CacheCreationTokens: p.CacheCreationTokens, CacheReadTokens: p.CacheReadTokens, CacheCreation5mTokens: p.CacheCreation5mTokens, CacheCreation1hTokens: p.CacheCreation1hTokens, InputCost: values[0], OutputCost: values[1], CacheCreationCost: values[2], CacheReadCost: values[3], TotalCost: values[4], ActualCost: values[5], RateMultiplier: p.RateMultiplier, AccountRateMultiplier: p.AccountRateMultiplier, AccountStatsCost: statsFloat, LongContextBillingApplied: p.LongContextBillingApplied, BillingType: BillingTypeSubscription, Stream: p.Stream, DurationMs: p.DurationMs, FirstTokenMs: p.FirstTokenMs, CreatedAt: p.CreatedAt}, nil
}
