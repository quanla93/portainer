package backup

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCalculateS3BackupScheduleDisabled(t *testing.T) {
	schedule := calculateS3BackupSchedule("", time.Now(), time.Local)

	require.False(t, schedule.Enabled)
	require.Equal(t, "disabled", schedule.State)
	require.Empty(t, schedule.NextRunUTC)
	require.Empty(t, schedule.Timezone)
}

func TestCalculateS3BackupScheduleInvalid(t *testing.T) {
	schedule := calculateS3BackupSchedule("not a cron rule", time.Now(), time.Local)

	require.True(t, schedule.Enabled)
	require.Equal(t, "invalid", schedule.State)
	require.Empty(t, schedule.NextRunUTC)
	require.Empty(t, schedule.Timezone)
}

func TestCalculateS3BackupScheduleReturnsNextUTCAndIANAZone(t *testing.T) {
	location, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	now := time.Date(2026, 10, 12, 10, 30, 0, 0, location)

	schedule := calculateS3BackupSchedule("0 2 * * *", now, location)

	require.True(t, schedule.Enabled)
	require.Equal(t, "enabled", schedule.State)
	require.Equal(t, "2026-10-13T06:00:00Z", schedule.NextRunUTC)
	require.Equal(t, "available", schedule.TimezoneState)
	require.Equal(t, "America/New_York", schedule.Timezone)
}

func TestCalculateS3BackupScheduleRetainsUTCWhenTimezoneIsUnresolvable(t *testing.T) {
	location := time.FixedZone("custom-fixed-zone", -4*60*60)
	now := time.Date(2026, 10, 12, 10, 30, 0, 0, location)

	schedule := calculateS3BackupSchedule("0 2 * * *", now, location)

	require.True(t, schedule.Enabled)
	require.Equal(t, "enabled", schedule.State)
	require.Equal(t, "2026-10-13T06:00:00Z", schedule.NextRunUTC)
	require.Equal(t, "unavailable", schedule.TimezoneState)
	require.Empty(t, schedule.Timezone)
}

func TestCalculateS3BackupScheduleReportsResolvableTimeLocal(t *testing.T) {
	local := time.Local
	location, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	time.Local = location
	t.Cleanup(func() { time.Local = local })

	now := time.Date(2026, 10, 12, 10, 30, 0, 0, time.Local)
	schedule := calculateS3BackupSchedule("0 2 * * *", now, time.Local)

	require.Equal(t, "2026-10-13T06:00:00Z", schedule.NextRunUTC)
	require.Equal(t, "available", schedule.TimezoneState)
	require.Equal(t, "America/New_York", schedule.Timezone)
}
