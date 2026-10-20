package config

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestBlueGreenConfigurationDefaultsAndEnvironment(t *testing.T) {
	resetViperWithJWTSecret(t)
	cfg, err := Load()
	require.NoError(t, err)
	require.Equal(t, "exclusive", cfg.Gateway.ConcurrencyStartupCleanupMode)
	require.Equal(t, "legacy", cfg.Gateway.BackgroundJobMode)
	require.True(t, cfg.Gateway.BackgroundJobsStartHeld)
	t.Setenv("GATEWAY_CONCURRENCY_STARTUP_CLEANUP_MODE", "preserve")
	t.Setenv("GATEWAY_BACKGROUND_JOB_MODE", "coordinated")
	t.Setenv("GATEWAY_BACKGROUND_JOBS_START_HELD", "false")
	cfg, err = Load()
	require.NoError(t, err)
	require.Equal(t, "preserve", cfg.Gateway.ConcurrencyStartupCleanupMode)
	require.Equal(t, "coordinated", cfg.Gateway.BackgroundJobMode)
	require.False(t, cfg.Gateway.BackgroundJobsStartHeld)
}

func TestBlueGreenConfigurationRejectsMisspellings(t *testing.T) {
	for _, key := range []string{"GATEWAY_CONCURRENCY_STARTUP_CLEANUP_MODE", "GATEWAY_BACKGROUND_JOB_MODE"} {
		t.Run(key, func(t *testing.T) {
			resetViperWithJWTSecret(t)
			t.Setenv(key, "presrve")
			_, err := Load()
			require.Error(t, err)
		})
	}
}
