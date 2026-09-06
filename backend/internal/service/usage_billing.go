package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/shopspring/decimal"
)

var ErrUsageBillingRequestIDRequired = errors.New("usage billing request_id is required")
var ErrUsageBillingRequestConflict = errors.New("usage billing request fingerprint conflict")
var ErrUsageBillingSourceConflict = errors.New("usage billing sources must be mutually exclusive")
var ErrUsageBillingCarpoolSnapshotInvalid = errors.New("usage billing carpool snapshot is invalid")

// UsageBillingCommand describes one billable request that must be applied at most once.
type UsageBillingCommand struct {
	RequestID          string
	APIKeyID           int64
	RequestFingerprint string
	RequestPayloadHash string

	UserID              int64
	AccountID           int64
	SubscriptionID      *int64
	AccountType         string
	Model               string
	ServiceTier         string
	ReasoningEffort     string
	BillingType         int8
	InputTokens         int
	OutputTokens        int
	CacheCreationTokens int
	CacheReadTokens     int
	ImageCount          int
	MediaType           string

	BalanceCost         float64
	SubscriptionCost    float64
	CarpoolCost         decimal.Decimal
	CarpoolSnapshot     *domain.CarpoolBillingSnapshot
	APIKeyQuotaCost     float64
	APIKeyRateLimitCost float64
	AccountQuotaCost    float64
}

const CarpoolUsageBillingReceiptVersion = 1

// carpoolUsageBillingReceiptV1 is the durable, prompt-free settlement input.
// Recovery must replay these frozen values and must not reprice against current
// group, account, or API-key state.
type carpoolUsageBillingReceiptV1 struct {
	Version int `json:"version"`
	Command struct {
		RequestID          string `json:"request_id"`
		APIKeyID           int64  `json:"api_key_id"`
		RequestFingerprint string `json:"request_fingerprint"`
		RequestPayloadHash string `json:"request_payload_hash"`

		UserID              int64  `json:"user_id"`
		AccountID           int64  `json:"account_id"`
		SubscriptionID      *int64 `json:"subscription_id"`
		AccountType         string `json:"account_type"`
		Model               string `json:"model"`
		ServiceTier         string `json:"service_tier"`
		ReasoningEffort     string `json:"reasoning_effort"`
		BillingType         int8   `json:"billing_type"`
		InputTokens         int    `json:"input_tokens"`
		OutputTokens        int    `json:"output_tokens"`
		CacheCreationTokens int    `json:"cache_creation_tokens"`
		CacheReadTokens     int    `json:"cache_read_tokens"`
		ImageCount          int    `json:"image_count"`
		MediaType           string `json:"media_type"`

		BalanceCost         float64                        `json:"balance_cost"`
		SubscriptionCost    float64                        `json:"subscription_cost"`
		CarpoolCost         string                         `json:"carpool_cost"`
		CarpoolSnapshot     *domain.CarpoolBillingSnapshot `json:"carpool_snapshot"`
		APIKeyQuotaCost     float64                        `json:"api_key_quota_cost"`
		APIKeyRateLimitCost float64                        `json:"api_key_rate_limit_cost"`
		AccountQuotaCost    float64                        `json:"account_quota_cost"`
	} `json:"command"`
}

func MarshalCarpoolUsageBillingReceipt(cmd *UsageBillingCommand) (json.RawMessage, error) {
	if cmd == nil || cmd.CarpoolSnapshot == nil {
		return nil, ErrUsageBillingCarpoolSnapshotInvalid
	}
	copyCmd := *cmd
	copySnapshot := *cmd.CarpoolSnapshot
	copyCmd.CarpoolSnapshot = &copySnapshot
	copyCmd.Normalize()
	if err := copyCmd.Validate(); err != nil {
		return nil, err
	}

	receipt := carpoolUsageBillingReceiptV1{Version: CarpoolUsageBillingReceiptVersion}
	receipt.Command.RequestID = copyCmd.RequestID
	receipt.Command.APIKeyID = copyCmd.APIKeyID
	receipt.Command.RequestFingerprint = copyCmd.RequestFingerprint
	receipt.Command.RequestPayloadHash = copyCmd.RequestPayloadHash
	receipt.Command.UserID = copyCmd.UserID
	receipt.Command.AccountID = copyCmd.AccountID
	receipt.Command.SubscriptionID = copyCmd.SubscriptionID
	receipt.Command.AccountType = copyCmd.AccountType
	receipt.Command.Model = copyCmd.Model
	receipt.Command.ServiceTier = copyCmd.ServiceTier
	receipt.Command.ReasoningEffort = copyCmd.ReasoningEffort
	receipt.Command.BillingType = copyCmd.BillingType
	receipt.Command.InputTokens = copyCmd.InputTokens
	receipt.Command.OutputTokens = copyCmd.OutputTokens
	receipt.Command.CacheCreationTokens = copyCmd.CacheCreationTokens
	receipt.Command.CacheReadTokens = copyCmd.CacheReadTokens
	receipt.Command.ImageCount = copyCmd.ImageCount
	receipt.Command.MediaType = copyCmd.MediaType
	receipt.Command.BalanceCost = copyCmd.BalanceCost
	receipt.Command.SubscriptionCost = copyCmd.SubscriptionCost
	receipt.Command.CarpoolCost = copyCmd.CarpoolCost.StringFixed(UsageBillingMonetaryScale)
	receipt.Command.CarpoolSnapshot = copyCmd.CarpoolSnapshot
	receipt.Command.APIKeyQuotaCost = copyCmd.APIKeyQuotaCost
	receipt.Command.APIKeyRateLimitCost = copyCmd.APIKeyRateLimitCost
	receipt.Command.AccountQuotaCost = copyCmd.AccountQuotaCost

	payload, err := json.Marshal(receipt)
	return json.RawMessage(payload), err
}

func DecodeCarpoolUsageBillingReceipt(payload json.RawMessage) (*UsageBillingCommand, error) {
	var receipt carpoolUsageBillingReceiptV1
	if len(payload) == 0 || json.Unmarshal(payload, &receipt) != nil || receipt.Version != CarpoolUsageBillingReceiptVersion {
		return nil, ErrUsageBillingCarpoolSnapshotInvalid
	}
	cost, err := decimal.NewFromString(receipt.Command.CarpoolCost)
	if err != nil {
		return nil, ErrUsageBillingCarpoolSnapshotInvalid
	}
	cmd := &UsageBillingCommand{
		RequestID:           receipt.Command.RequestID,
		APIKeyID:            receipt.Command.APIKeyID,
		RequestFingerprint:  receipt.Command.RequestFingerprint,
		RequestPayloadHash:  receipt.Command.RequestPayloadHash,
		UserID:              receipt.Command.UserID,
		AccountID:           receipt.Command.AccountID,
		SubscriptionID:      receipt.Command.SubscriptionID,
		AccountType:         receipt.Command.AccountType,
		Model:               receipt.Command.Model,
		ServiceTier:         receipt.Command.ServiceTier,
		ReasoningEffort:     receipt.Command.ReasoningEffort,
		BillingType:         receipt.Command.BillingType,
		InputTokens:         receipt.Command.InputTokens,
		OutputTokens:        receipt.Command.OutputTokens,
		CacheCreationTokens: receipt.Command.CacheCreationTokens,
		CacheReadTokens:     receipt.Command.CacheReadTokens,
		ImageCount:          receipt.Command.ImageCount,
		MediaType:           receipt.Command.MediaType,
		BalanceCost:         receipt.Command.BalanceCost,
		SubscriptionCost:    receipt.Command.SubscriptionCost,
		CarpoolCost:         cost,
		CarpoolSnapshot:     receipt.Command.CarpoolSnapshot,
		APIKeyQuotaCost:     receipt.Command.APIKeyQuotaCost,
		APIKeyRateLimitCost: receipt.Command.APIKeyRateLimitCost,
		AccountQuotaCost:    receipt.Command.AccountQuotaCost,
	}
	cmd.Normalize()
	if err := cmd.Validate(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(cmd.RequestFingerprint) == "" {
		return nil, ErrUsageBillingCarpoolSnapshotInvalid
	}
	return cmd, nil
}

func (c *UsageBillingCommand) Normalize() {
	if c == nil {
		return
	}
	c.RequestID = strings.TrimSpace(c.RequestID)
	if strings.TrimSpace(c.RequestFingerprint) == "" {
		c.RequestFingerprint = buildUsageBillingFingerprint(c)
	}
	// 量化必须在指纹计算之后：指纹是请求幂等键，保持由原始金额派生可以避免
	// 升级前后同一 request_id 的重试算出不同指纹而被判为 fingerprint conflict。
	c.quantizeMonetaryFields()
}

// UsageBillingMonetaryScale 是所有计费金额的规范小数位数，
// 对齐 users.balance / api_keys.quota_used 的 NUMERIC(20,8)。
const UsageBillingMonetaryScale = 8

// quantizeMonetaryFields 把命令中的金额统一量化到 NUMERIC(20,8)。
//
// 不量化时，同一笔 ActualCost 会在两条方向相反的 SQL 上被 PostgreSQL 分别舍入：
//
//	balance    = balance - $1      // 存剩余额度，舍入的是「减法结果」
//	quota_used = quota_used + $1   // 存累计用量，舍入的是「加法结果」
//
// PostgreSQL 对 NUMERIC 采用 half-away-from-zero。当金额在第 9 位出现 half 边界
// （例：10 输入 token × 0.00000125 + 5 输出 token × 0.00001000，再乘分组倍率
// 1.25 = 0.000078125）时：
//
//	balance:    10000 - 0.000078125 = 9999.999921875 → 9999.99992188（delta 0.00007812）
//	quota_used:     0 + 0.000078125 =     0.000078125 →     0.00007813（delta 0.00007813）
//
// 两个 delta 相差 1e-8，且方向相反——余额少扣、Key 配额多记，随请求量线性累积，
// 使余额、API Key 配额与用量记录无法精确对账（需要 epsilon 比较才能勉强吻合）。
//
// 在参数进入 SQL 之前量化一次，两条语句就都拿到已经落在 8 位刻度上的同一个金额，
// 存储阶段不再发生任何舍入，delta 精确相等。
func (c *UsageBillingCommand) quantizeMonetaryFields() {
	c.BalanceCost = QuantizeUsageBillingAmount(c.BalanceCost)
	c.SubscriptionCost = QuantizeUsageBillingAmount(c.SubscriptionCost)
	c.CarpoolCost = c.CarpoolCost.Round(UsageBillingMonetaryScale)
	c.APIKeyQuotaCost = QuantizeUsageBillingAmount(c.APIKeyQuotaCost)
	c.APIKeyRateLimitCost = QuantizeUsageBillingAmount(c.APIKeyRateLimitCost)
	c.AccountQuotaCost = QuantizeUsageBillingAmount(c.AccountQuotaCost)
}

func (c *UsageBillingCommand) Validate() error {
	if c == nil {
		return nil
	}
	sources := 0
	if c.BalanceCost > 0 {
		sources++
	}
	if c.SubscriptionCost > 0 {
		sources++
	}
	if c.CarpoolSnapshot != nil {
		sources++
	}
	if sources > 1 {
		return ErrUsageBillingSourceConflict
	}
	if c.CarpoolSnapshot == nil {
		if !c.CarpoolCost.IsZero() {
			return ErrUsageBillingCarpoolSnapshotInvalid
		}
		return nil
	}
	snapshot := c.CarpoolSnapshot
	if snapshot.BillingRequestID <= 0 || snapshot.TermID <= 0 || snapshot.CycleID <= 0 ||
		snapshot.UserID != c.UserID || snapshot.APIKeyID != c.APIKeyID || snapshot.RequestID != c.RequestID ||
		snapshot.AdmittedAt.IsZero() || c.CarpoolCost.IsNegative() {
		return ErrUsageBillingCarpoolSnapshotInvalid
	}
	return nil
}

// QuantizeUsageBillingAmount 把金额舍入到 UsageBillingMonetaryScale 位小数，
// 采用与 PostgreSQL NUMERIC 一致的 half-away-from-zero 规则。
//
// 走 decimal 而不是 math.Round(v*1e8)/1e8：后者在乘除过程中会引入额外的二进制
// 误差，边界值可能被推到错误的一侧。decimal.NewFromFloat 取 float64 的最短十进制
// 表示，正是 PostgreSQL 把 float8 参数转成 numeric 时所用的表示。
func QuantizeUsageBillingAmount(v float64) float64 {
	if v == 0 || math.IsNaN(v) || math.IsInf(v, 0) {
		return v
	}
	quantized, _ := decimal.NewFromFloat(v).Round(UsageBillingMonetaryScale).Float64()
	return quantized
}

func buildUsageBillingFingerprint(c *UsageBillingCommand) string {
	if c == nil {
		return ""
	}
	raw := fmt.Sprintf(
		"%d|%d|%d|%s|%s|%s|%s|%d|%d|%d|%d|%d|%d|%s|%d|%0.10f|%0.10f|%0.10f|%0.10f|%0.10f",
		c.UserID,
		c.AccountID,
		c.APIKeyID,
		strings.TrimSpace(c.AccountType),
		strings.TrimSpace(c.Model),
		strings.TrimSpace(c.ServiceTier),
		strings.TrimSpace(c.ReasoningEffort),
		c.BillingType,
		c.InputTokens,
		c.OutputTokens,
		c.CacheCreationTokens,
		c.CacheReadTokens,
		c.ImageCount,
		strings.TrimSpace(c.MediaType),
		valueOrZero(c.SubscriptionID),
		c.BalanceCost,
		c.SubscriptionCost,
		c.APIKeyQuotaCost,
		c.APIKeyRateLimitCost,
		c.AccountQuotaCost,
	)
	if payloadHash := strings.TrimSpace(c.RequestPayloadHash); payloadHash != "" {
		raw += "|" + payloadHash
	}
	if c.CarpoolSnapshot != nil {
		snapshot := c.CarpoolSnapshot
		raw = fmt.Sprintf(
			"v2|%s|%d|%d|%d|%d|%d|%s|%s",
			raw,
			snapshot.BillingRequestID,
			snapshot.GroupID,
			snapshot.TermID,
			snapshot.CycleID,
			snapshot.AdmittedAt.UTC().UnixNano(),
			snapshot.RequestID,
			c.CarpoolCost.Round(UsageBillingMonetaryScale).StringFixed(UsageBillingMonetaryScale),
		)
	}
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func HashUsageRequestPayload(payload []byte) string {
	if len(payload) == 0 {
		return ""
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func valueOrZero(v *int64) int64 {
	if v == nil {
		return 0
	}
	return *v
}

// AccountQuotaState holds the post-increment quota state returned by the DB transaction.
// All values are post-update (i.e., already include the increment).
type AccountQuotaState struct {
	TotalUsed   float64
	TotalLimit  float64
	DailyUsed   float64
	DailyLimit  float64
	WeeklyUsed  float64
	WeeklyLimit float64
}

type UsageBillingApplyResult struct {
	Applied              bool
	APIKeyQuotaExhausted bool
	NewBalance           *float64           // post-deduction balance (nil = no balance deduction)
	BalanceOverdrafted   bool               // true when the sufficient-balance guard missed and debt was still recorded
	QuotaState           *AccountQuotaState // post-increment quota state (nil = no quota increment)
}

// BatchImageBalanceHoldCommand describes an idempotent balance hold operation.
type BatchImageBalanceHoldCommand struct {
	RequestID          string
	APIKeyID           int64
	RequestFingerprint string
	RequestPayloadHash string
	UserID             int64
	BatchID            string
	HoldAmount         float64
	ActualAmount       float64
}

func (c *BatchImageBalanceHoldCommand) Normalize() {
	if c == nil {
		return
	}
	c.RequestID = strings.TrimSpace(c.RequestID)
	c.BatchID = strings.TrimSpace(c.BatchID)
	if strings.TrimSpace(c.RequestFingerprint) == "" {
		c.RequestFingerprint = buildBatchImageBalanceHoldFingerprint(c)
	}
}

func buildBatchImageBalanceHoldFingerprint(c *BatchImageBalanceHoldCommand) string {
	if c == nil {
		return ""
	}
	raw := fmt.Sprintf(
		"%d|%d|%s|%0.10f|%0.10f",
		c.UserID,
		c.APIKeyID,
		strings.TrimSpace(c.BatchID),
		c.HoldAmount,
		c.ActualAmount,
	)
	if payloadHash := strings.TrimSpace(c.RequestPayloadHash); payloadHash != "" {
		raw += "|" + payloadHash
	}
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

type BatchImageBalanceHoldResult struct {
	Applied       bool
	NewBalance    *float64
	FrozenBalance *float64
}

type UsageBillingRepository interface {
	Apply(ctx context.Context, cmd *UsageBillingCommand) (*UsageBillingApplyResult, error)
	ReserveBatchImageBalance(ctx context.Context, cmd *BatchImageBalanceHoldCommand) (*BatchImageBalanceHoldResult, error)
	CaptureBatchImageBalance(ctx context.Context, cmd *BatchImageBalanceHoldCommand) (*BatchImageBalanceHoldResult, error)
	ReleaseBatchImageBalance(ctx context.Context, cmd *BatchImageBalanceHoldCommand) (*BatchImageBalanceHoldResult, error)
}
