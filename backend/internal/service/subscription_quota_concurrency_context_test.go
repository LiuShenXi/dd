//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSubscriptionQuotaConcurrencyV2CancellationContextBoundary(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		t.Run(map[bool]string{false: "json", true: "sse"}[streaming], func(t *testing.T) {
			parent, cancel := context.WithCancel(context.Background())
			defer cancel()
			termCtx, _, err := ContextWithSubscriptionQuotaForward(parent, termPricingKey(), &termForwardBillingStub{}, ChannelUsageFields{})
			require.NoError(t, err)
			upstream, release := detachStreamUpstreamContext(termCtx, streaming)
			defer release()
			billing, finishBilling := detachedBillingContext(termCtx)
			defer finishBilling()
			cancel()
			require.ErrorIs(t, upstream.Err(), context.Canceled)
			require.NoError(t, billing.Err(), "mandatory complete known persistence must survive forward cancellation")
			deadline, bounded := billing.Deadline()
			require.True(t, bounded)
			require.Greater(t, time.Until(deadline), time.Duration(0))
		})
	}
	t.Run("legacy_stream_keeps_existing_detachment", func(t *testing.T) {
		parent, cancel := context.WithCancel(context.Background())
		upstream, release := detachStreamUpstreamContext(parent, true)
		defer release()
		cancel()
		require.NoError(t, upstream.Err())
	})
}
