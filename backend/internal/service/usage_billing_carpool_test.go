package service

import (
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestUsageBillingFingerprint_NonCarpoolCompatibility(t *testing.T) {
	cmd := &UsageBillingCommand{
		UserID: 1, AccountID: 2, APIKeyID: 3, AccountType: "apikey", Model: "gpt-5",
		BalanceCost: 1.25, APIKeyQuotaCost: 1.25,
	}

	legacy := buildUsageBillingFingerprint(cmd)
	cmd.Normalize()

	require.Equal(t, legacy, cmd.RequestFingerprint)
	require.NotContains(t, cmd.RequestFingerprint, "v2")
}

func TestCarpoolIsNotOrdinaryBalanceBilling(t *testing.T) {
	require.True(t, isOrdinaryBalanceBilling(&postUsageBillingParams{}))
	require.False(t, isOrdinaryBalanceBilling(&postUsageBillingParams{IsSubscriptionBill: true}))
	require.False(t, isOrdinaryBalanceBilling(&postUsageBillingParams{IsCarpoolBill: true}))
}

func TestUsageBillingCommand_CarpoolSourceExclusivityAndFingerprint(t *testing.T) {
	admittedAt := time.Date(2026, 9, 5, 14, 0, 0, 123, time.UTC)
	snapshot := &domain.CarpoolBillingSnapshot{
		BillingRequestID: 10, RequestID: "local:req-1", UserID: 1, APIKeyID: 3,
		GroupID: 4, TermID: 5, CycleID: 6, AdmittedAt: admittedAt,
	}
	cmd := &UsageBillingCommand{
		RequestID: "local:req-1", UserID: 1, AccountID: 2, APIKeyID: 3,
		AccountType: "oauth", Model: "gpt-5", CarpoolSnapshot: snapshot,
		CarpoolCost: decimal.RequireFromString("0.123456789"),
	}

	cmd.Normalize()
	require.NoError(t, cmd.Validate())
	require.Equal(t, "0.12345679", cmd.CarpoolCost.StringFixed(8))
	require.NotEqual(t, buildUsageBillingFingerprint(&UsageBillingCommand{
		RequestID: cmd.RequestID, UserID: cmd.UserID, AccountID: cmd.AccountID,
		APIKeyID: cmd.APIKeyID, AccountType: cmd.AccountType, Model: cmd.Model,
		CarpoolSnapshot: &domain.CarpoolBillingSnapshot{
			BillingRequestID: 10, RequestID: "local:req-1", UserID: 1, APIKeyID: 3,
			GroupID: 4, TermID: 5, CycleID: 7, AdmittedAt: admittedAt,
		}, CarpoolCost: cmd.CarpoolCost,
	}), cmd.RequestFingerprint)

	cmd.BalanceCost = 1
	require.ErrorIs(t, cmd.Validate(), ErrUsageBillingSourceConflict)
}

func TestUsageBillingCommand_CarpoolSnapshotMustMatchCommand(t *testing.T) {
	cmd := &UsageBillingCommand{
		RequestID: "local:req-1", UserID: 1, APIKeyID: 3,
		CarpoolSnapshot: &domain.CarpoolBillingSnapshot{
			BillingRequestID: 10, RequestID: "local:req-1", UserID: 99, APIKeyID: 3,
			GroupID: 4, TermID: 5, CycleID: 6, AdmittedAt: time.Now().UTC(),
		},
	}
	cmd.Normalize()
	require.ErrorIs(t, cmd.Validate(), ErrUsageBillingCarpoolSnapshotInvalid)
}

func TestCarpoolUsageBillingReceiptRoundTripsFrozenFullCommand(t *testing.T) {
	admittedAt := time.Date(2026, 9, 6, 14, 15, 16, 987654000, time.UTC)
	cmd := &UsageBillingCommand{
		RequestID:           "carpool:receipt-roundtrip",
		RequestPayloadHash:  strings.Repeat("a", 64),
		UserID:              1,
		APIKeyID:            2,
		AccountID:           3,
		AccountType:         AccountTypeAPIKey,
		Model:               "gpt-5.4",
		ServiceTier:         "priority",
		ReasoningEffort:     "xhigh",
		BillingType:         BillingTypeBalance,
		InputTokens:         1000,
		OutputTokens:        100,
		CacheCreationTokens: 25,
		CacheReadTokens:     200,
		ImageCount:          2,
		MediaType:           "image",
		CarpoolCost:         decimal.RequireFromString("1.234567895"),
		CarpoolSnapshot: &domain.CarpoolBillingSnapshot{
			BillingRequestID: 91,
			RequestID:        "carpool:receipt-roundtrip",
			UserID:           1,
			APIKeyID:         2,
			GroupID:          4,
			TermID:           5,
			CycleID:          6,
			AdmittedAt:       admittedAt,
		},
		APIKeyQuotaCost:     1.234567895,
		APIKeyRateLimitCost: 1.234567895,
		AccountQuotaCost:    0.987654325,
	}
	cmd.Normalize()

	payload, err := MarshalCarpoolUsageBillingReceipt(cmd)
	require.NoError(t, err)
	require.Contains(t, string(payload), `"version":1`)
	require.NotContains(t, string(payload), "prompt")

	recovered, err := DecodeCarpoolUsageBillingReceipt(payload)
	require.NoError(t, err)
	require.Equal(t, cmd, recovered)
	require.Equal(t, admittedAt, recovered.CarpoolSnapshot.AdmittedAt)
	require.Equal(t, "1.23456790", recovered.CarpoolCost.StringFixed(8))
}

func TestDecodeCarpoolUsageBillingReceiptRejectsUnknownVersion(t *testing.T) {
	_, err := DecodeCarpoolUsageBillingReceipt([]byte(`{"version":2,"command":{}}`))
	require.ErrorIs(t, err, ErrUsageBillingCarpoolSnapshotInvalid)
}
