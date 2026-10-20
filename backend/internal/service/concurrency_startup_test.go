package service

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestProvideConcurrencyPreservesOtherProcessesOnlyWhenExplicit(t *testing.T) {
	for _, mode := range []string{"", "exclusive", "preserve"} {
		t.Run(mode, func(t *testing.T) {
			cache := &startupConcurrencyCache{}
			cfg := &config.Config{}
			cfg.Gateway.ConcurrencyStartupCleanupMode = mode
			svc := ProvideConcurrencyService(cache, nil, nil, cfg)
			require.NotNil(t, svc)
			if mode == "preserve" {
				require.Empty(t, cache.cleanupPrefix)
			} else {
				require.Equal(t, RequestIDPrefix(), cache.cleanupPrefix)
			}
		})
	}
}

type startupConcurrencyCache struct {
	ConcurrencyCache
	cleanupPrefix string
}

func (c *startupConcurrencyCache) CleanupStaleProcessSlots(_ context.Context, prefix string) error {
	c.cleanupPrefix = prefix
	return nil
}
