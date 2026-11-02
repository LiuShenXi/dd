package service

import (
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestSubscriptionQuotaOperationalMetricsHaveControlledResultsAndExactOverage(t *testing.T) {
	before := GetSubscriptionQuotaMetricsSnapshot()
	for _, err := range []error{nil, ErrSubscriptionQuotaExpired, ErrSubscriptionQuotaExhausted, ErrSubscriptionQuotaCancelled, ErrSubscriptionQuotaNotActive, ErrSubscriptionQuotaReconciliation, errors.New("synthetic dependency failure")} {
		ObserveSubscriptionQuotaAdmission(err)
	}
	ObserveSubscriptionQuotaSettlement(true, nil)
	ObserveSubscriptionQuotaSettlement(false, nil)
	ObserveSubscriptionQuotaSettlement(false, ErrSubscriptionQuotaReceiptConflict)
	ObserveSubscriptionQuotaSettlement(false, errors.New("synthetic storage failure"))
	ObserveSubscriptionQuotaScopeWait(1500 * time.Microsecond)
	ObserveSubscriptionQuotaLedgerMismatch()
	ObserveSubscriptionQuotaOverage(decimal.RequireFromString("0.00000001"))
	observeSubscriptionQuotaPublicationFailure()
	observeSubscriptionQuotaRecoveryRetry()
	after := GetSubscriptionQuotaMetricsSnapshot()
	for _, key := range []string{"admitted", "expired", "exhausted", "cancelled", "inactive", "reconcile", "unavailable"} {
		require.Equal(t, before.Admission[key]+1, after.Admission[key])
	}
	for _, key := range []string{"applied", "replay", "conflict", "failed"} {
		require.Equal(t, before.Settlement[key]+1, after.Settlement[key])
	}
	require.Equal(t, before.LedgerMismatch+1, after.LedgerMismatch)
	require.Equal(t, before.UsageLogMissing+1, after.UsageLogMissing)
	require.Equal(t, before.RecoveryRetries+1, after.RecoveryRetries)
	require.Equal(t, decimal.RequireFromString(before.OverageUSD).Add(decimal.RequireFromString("0.00000001")).StringFixed(8), after.OverageUSD)
	require.Equal(t, before.ScopeLockWaitCount+1, after.ScopeLockWaitCount)
	require.Greater(t, after.ScopeLockWaitSeconds, before.ScopeLockWaitSeconds)
	// Returned maps are detached; a monitoring client cannot mutate counters.
	after.Admission["admitted"] = 0
	require.Equal(t, before.Admission["admitted"]+1, GetSubscriptionQuotaMetricsSnapshot().Admission["admitted"])
}
