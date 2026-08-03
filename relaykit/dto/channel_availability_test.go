package dto

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelAvailabilityWindowDaytimeBoundaries(t *testing.T) {
	schedule := ChannelAvailabilitySchedule{
		Enabled:  true,
		Start:    "08:00",
		End:      "12:00",
		Timezone: "Asia/Shanghai",
	}
	location, err := time.LoadLocation(schedule.Timezone)
	require.NoError(t, err)

	tests := []struct {
		name           string
		now            time.Time
		available      bool
		nextTransition time.Time
	}{
		{
			name:           "before start",
			now:            time.Date(2026, 7, 28, 7, 59, 59, 0, location),
			available:      false,
			nextTransition: time.Date(2026, 7, 28, 8, 0, 0, 0, location),
		},
		{
			name:           "start inclusive",
			now:            time.Date(2026, 7, 28, 8, 0, 0, 0, location),
			available:      true,
			nextTransition: time.Date(2026, 7, 28, 12, 0, 0, 0, location),
		},
		{
			name:           "end exclusive",
			now:            time.Date(2026, 7, 28, 12, 0, 0, 0, location),
			available:      false,
			nextTransition: time.Date(2026, 7, 29, 8, 0, 0, 0, location),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			window, err := schedule.WindowAt(test.now)
			require.NoError(t, err)
			assert.Equal(t, test.available, window.Available)
			assert.Equal(t, test.nextTransition, window.NextTransitionAt)
		})
	}
}

func TestChannelAvailabilityWindowCrossesMidnight(t *testing.T) {
	schedule := ChannelAvailabilitySchedule{
		Enabled:  true,
		Start:    "20:00",
		End:      "02:00",
		Timezone: "Asia/Shanghai",
	}
	location, err := time.LoadLocation(schedule.Timezone)
	require.NoError(t, err)

	evening, err := schedule.WindowAt(time.Date(2026, 7, 28, 21, 0, 0, 0, location))
	require.NoError(t, err)
	assert.True(t, evening.Available)
	assert.Equal(t, time.Date(2026, 7, 29, 2, 0, 0, 0, location), evening.EndAt)

	afterMidnight, err := schedule.WindowAt(time.Date(2026, 7, 29, 1, 0, 0, 0, location))
	require.NoError(t, err)
	assert.True(t, afterMidnight.Available)
	assert.Equal(t, time.Date(2026, 7, 28, 20, 0, 0, 0, location), afterMidnight.StartAt)

	closed, err := schedule.WindowAt(time.Date(2026, 7, 29, 2, 0, 0, 0, location))
	require.NoError(t, err)
	assert.False(t, closed.Available)
	assert.Equal(t, time.Date(2026, 7, 29, 20, 0, 0, 0, location), closed.NextTransitionAt)
}

func TestChannelAvailabilityScheduleNormalizationAndValidation(t *testing.T) {
	normalized, err := (ChannelAvailabilitySchedule{Enabled: true}).Normalize()
	require.NoError(t, err)
	assert.Equal(t, DefaultChannelAvailabilityStart, normalized.Start)
	assert.Equal(t, DefaultChannelAvailabilityEnd, normalized.End)
	assert.Equal(t, DefaultChannelAvailabilityTimezone, normalized.Timezone)

	_, err = (ChannelAvailabilitySchedule{Enabled: true, Start: "8:00", End: "12:00", Timezone: "Asia/Shanghai"}).Normalize()
	require.ErrorContains(t, err, "应为 HH:MM")
	_, err = (ChannelAvailabilitySchedule{Enabled: true, Start: "08:00", End: "08:00", Timezone: "Asia/Shanghai"}).Normalize()
	require.ErrorContains(t, err, "不能相同")
	_, err = (ChannelAvailabilitySchedule{Enabled: true, Start: "08:00", End: "12:00", Timezone: "Mars/Olympus"}).Normalize()
	require.ErrorContains(t, err, "渠道可用时区")
}

func TestChannelAvailabilityWindowSpringForward(t *testing.T) {
	schedule := ChannelAvailabilitySchedule{
		Enabled:  true,
		Start:    "02:30",
		End:      "04:00",
		Timezone: "America/New_York",
	}

	before, err := schedule.WindowAt(time.Date(2026, 3, 8, 6, 59, 59, 0, time.UTC))
	require.NoError(t, err)
	assert.False(t, before.Available)
	assert.Equal(t, time.Date(2026, 3, 8, 7, 0, 0, 0, time.UTC).Unix(), before.NextTransitionAt.Unix())

	opened, err := schedule.WindowAt(time.Date(2026, 3, 8, 7, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	assert.True(t, opened.Available)
	assert.Equal(t, time.Date(2026, 3, 8, 7, 0, 0, 0, time.UTC).Unix(), opened.StartAt.Unix())
	assert.Equal(t, time.Date(2026, 3, 8, 8, 0, 0, 0, time.UTC).Unix(), opened.EndAt.Unix())
	assert.Equal(t, opened.EndAt, opened.NextTransitionAt)
}

func TestChannelAvailabilityWindowSpringForwardSkipsCollapsedWindow(t *testing.T) {
	schedule := ChannelAvailabilitySchedule{
		Enabled:  true,
		Start:    "02:10",
		End:      "02:50",
		Timezone: "America/New_York",
	}

	window, err := schedule.WindowAt(time.Date(2026, 3, 8, 6, 59, 59, 0, time.UTC))
	require.NoError(t, err)
	assert.False(t, window.Available)
	assert.Equal(t, time.Date(2026, 3, 9, 6, 10, 0, 0, time.UTC).Unix(), window.NextTransitionAt.Unix())
}

func TestChannelAvailabilityWindowFallBackUsesContinuousWindow(t *testing.T) {
	schedule := ChannelAvailabilitySchedule{
		Enabled:  true,
		Start:    "01:30",
		End:      "01:45",
		Timezone: "America/New_York",
	}

	firstOpening, err := schedule.WindowAt(time.Date(2026, 11, 1, 5, 30, 0, 0, time.UTC))
	require.NoError(t, err)
	assert.True(t, firstOpening.Available)
	assert.Equal(t, time.Date(2026, 11, 1, 5, 30, 0, 0, time.UTC).Unix(), firstOpening.StartAt.Unix())
	assert.Equal(t, time.Date(2026, 11, 1, 6, 45, 0, 0, time.UTC).Unix(), firstOpening.EndAt.Unix())

	secondHour, err := schedule.WindowAt(time.Date(2026, 11, 1, 6, 15, 0, 0, time.UTC))
	require.NoError(t, err)
	assert.True(t, secondHour.Available)
	assert.Equal(t, firstOpening.StartAt, secondHour.StartAt)
	assert.Equal(t, firstOpening.EndAt, secondHour.NextTransitionAt)

	closed, err := schedule.WindowAt(time.Date(2026, 11, 1, 6, 45, 0, 0, time.UTC))
	require.NoError(t, err)
	assert.False(t, closed.Available)
}

func TestChannelAvailabilityWindowCrossesSpringForward(t *testing.T) {
	schedule := ChannelAvailabilitySchedule{
		Enabled:  true,
		Start:    "22:00",
		End:      "02:30",
		Timezone: "America/New_York",
	}

	opened, err := schedule.WindowAt(time.Date(2026, 3, 8, 6, 59, 59, 0, time.UTC))
	require.NoError(t, err)
	assert.True(t, opened.Available)
	assert.Equal(t, time.Date(2026, 3, 8, 7, 0, 0, 0, time.UTC).Unix(), opened.EndAt.Unix())

	closed, err := schedule.WindowAt(time.Date(2026, 3, 8, 7, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	assert.False(t, closed.Available)
}
