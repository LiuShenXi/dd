package repository

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCarpoolBillingDiagnosticCategoryCanonicalizesKnownInputsIdempotently(t *testing.T) {
	tests := map[string][]string{
		"receipt_missing_timeout": {
			"usage receipt missing after recovery timeout",
			"receipt_missing_timeout",
		},
		"automatic_settlement_retry_pending": {
			"automatic settlement retry pending",
			"automatic_settlement_retry_pending",
		},
		"automatic_settlement_retry_failed": {
			"automatic settlement retry failed",
			"automatic_settlement_retry_failed",
		},
		"automatic_settlement_permanent_failure": {
			"automatic settlement permanent failure",
			"automatic_settlement_permanent_failure",
		},
		"automatic_settlement_retry_exhausted": {
			"automatic settlement retry exhausted",
			"automatic_settlement_retry_exhausted",
		},
		"invalid_durable_billing_command": {
			"invalid durable carpool billing command",
			"invalid_durable_billing_command",
		},
		"usage_receipt_missing": {
			"alpha search request ended without durable known usage",
			"chat completions request ended without durable known usage",
			"embeddings request ended without durable known usage",
			"responses request ended without durable known usage",
			"messages request ended without durable known usage",
			"images request ended without durable known usage",
			"websocket turn ended without durable known usage",
			"usage_receipt_missing",
		},
		"usage_persistence_or_settlement_failed": {
			"alpha search usage persistence or settlement failed",
			"chat completions usage persistence or settlement failed",
			"embeddings usage persistence or settlement failed",
			"responses usage persistence or settlement failed",
			"messages usage persistence or settlement failed",
			"images usage persistence or settlement failed",
			"carpool cyber usage persistence or settlement failed",
			"websocket cyber usage persistence or settlement failed",
			"websocket known usage persistence or settlement failed",
			"usage_persistence_or_settlement_failed",
		},
		"usage_reconciliation_required": {
			"usage_reconciliation_required",
			"unknown diagnostic that must not be exposed",
		},
	}

	for expected, inputs := range tests {
		for _, input := range inputs {
			t.Run(expected+"/"+input, func(t *testing.T) {
				require.Equal(t, expected, carpoolBillingDiagnosticCategory(input))
				projected := sanitizeBillingExceptionDiagnostic(input)
				require.NotNil(t, projected)
				require.Equal(t, expected, *projected)
			})
		}
	}

	require.Nil(t, sanitizeBillingExceptionDiagnostic("  "))
}
