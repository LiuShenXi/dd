package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCarpoolReleaseStagingDisablesWorkersAndQuotaObservationWrites(t *testing.T) {
	t.Setenv("CARPOOL_BACKGROUND_ENABLED", "false")
	carpool := ProvideCarpoolService(nil, nil)
	quota := &OpenAIQuotaService{}
	announcements := &AnnouncementService{}
	reset := ProvideCarpoolResetService(nil, nil, quota, announcements, carpool)
	require.Nil(t, carpool.cancel)
	require.Nil(t, reset.cancel)
	require.Nil(t, quota.observationHook)
	require.Same(t, reset, carpool.resetReader)
	require.Same(t, reset, announcements.carpoolAudience)
}
