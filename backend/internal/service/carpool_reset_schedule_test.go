package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func shanghaiTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.ParseInLocation("2006-01-02 15:04:05", value, carpoolShanghaiLocation)
	require.NoError(t, err)
	return parsed
}

func TestNextCarpoolResetScheduleQualificationBoundary(t *testing.T) {
	tests := []struct {
		name string
		now  string
		want string
	}{
		{name: "before 22 schedules tonight", now: "2026-09-05 21:59:00", want: "2026-09-05 22:00:00"},
		{name: "at 22 schedules tomorrow", now: "2026-09-05 22:00:00", want: "2026-09-06 22:00:00"},
		{name: "after 22 schedules tomorrow", now: "2026-09-05 22:00:20", want: "2026-09-06 22:00:00"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			slot, scheduled := NextCarpoolResetSchedule(shanghaiTime(t, tt.now), nil)
			require.Equal(t, shanghaiTime(t, tt.want), slot.In(carpoolShanghaiLocation))
			require.Equal(t, slot, scheduled)
		})
	}
}

func TestNextCarpoolResetSchedulePreservesCooldownSeconds(t *testing.T) {
	last := shanghaiTime(t, "2026-09-05 22:00:20")
	slot, scheduled := NextCarpoolResetSchedule(shanghaiTime(t, "2026-09-07 21:00:00"), &last)
	require.Equal(t, shanghaiTime(t, "2026-09-07 22:00:00"), slot.In(carpoolShanghaiLocation))
	require.Equal(t, shanghaiTime(t, "2026-09-07 22:00:20"), scheduled.In(carpoolShanghaiLocation))
	require.Equal(t, 48*time.Hour, scheduled.Sub(last))
}

func TestNextCarpoolResetScheduleSkipsCooldownOutsideMinute(t *testing.T) {
	last := shanghaiTime(t, "2026-09-05 22:01:00")
	slot, scheduled := NextCarpoolResetSchedule(shanghaiTime(t, "2026-09-06 10:00:00"), &last)
	require.Equal(t, shanghaiTime(t, "2026-09-08 22:00:00"), slot.In(carpoolShanghaiLocation))
	require.Equal(t, slot, scheduled)
}

func TestCarpoolResetExecutionState(t *testing.T) {
	slot := shanghaiTime(t, "2026-09-07 22:00:00")
	scheduled := shanghaiTime(t, "2026-09-07 22:00:20")
	last := shanghaiTime(t, "2026-09-05 22:00:20")
	require.Equal(t, "not_due", CarpoolResetExecutionState(shanghaiTime(t, "2026-09-07 22:00:19"), slot, scheduled, &last))
	require.Equal(t, "due", CarpoolResetExecutionState(shanghaiTime(t, "2026-09-07 22:00:20"), slot, scheduled, &last))
	require.Equal(t, "missed", CarpoolResetExecutionState(shanghaiTime(t, "2026-09-07 22:01:00"), slot, scheduled, &last))
}

func TestValidCarpoolResetSchedule(t *testing.T) {
	last := shanghaiTime(t, "2026-09-05 22:00:20")
	slot := shanghaiTime(t, "2026-09-07 22:00:00")
	require.True(t, ValidCarpoolResetSchedule(slot, shanghaiTime(t, "2026-09-07 22:00:20"), &last))
	require.False(t, ValidCarpoolResetSchedule(slot, shanghaiTime(t, "2026-09-07 22:00:19"), &last))
	require.False(t, ValidCarpoolResetSchedule(slot, shanghaiTime(t, "2026-09-07 22:01:00"), &last))
	require.False(t, ValidCarpoolResetSchedule(shanghaiTime(t, "2026-09-07 21:59:59"), slot, nil))
	require.Equal(t, "invalid", CarpoolResetExecutionState(shanghaiTime(t, "2026-09-07 22:01:00"), slot, shanghaiTime(t, "2026-09-07 23:00:00"), nil))
	require.Equal(t, "cooldown", CarpoolResetExecutionState(shanghaiTime(t, "2026-09-07 22:00:10"), slot, slot, &last))
}
