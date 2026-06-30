package service

import (
	"errors"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildSystemDailyUsageStatus(t *testing.T) {
	now := time.Date(2026, 6, 30, 10, 0, 0, 0, time.UTC)
	settings := system_setting.DailyUsageLimitSettings{
		Enabled:     true,
		LimitTokens: 1000,
		Timezone:    "UTC",
		Message:     system_setting.DefaultDailyUsageLimitMsg,
	}
	signature := dailyUsageSettingsSignature(settings)

	t.Run("below limit", func(t *testing.T) {
		status := buildSystemDailyUsageStatus(now, settings, signature, func(dayStart int64, timezone string) (int64, error) {
			require.Equal(t, time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC).Unix(), dayStart)
			require.Equal(t, "UTC", timezone)
			return 900, nil
		})

		assert.True(t, status.Enabled)
		assert.False(t, status.ShouldBlock())
		assert.Equal(t, int64(900), status.UsedTokens)
		assert.Equal(t, int64(100), status.RemainingTokens)
		assert.Empty(t, status.EvaluationError)
	})

	t.Run("reached limit", func(t *testing.T) {
		status := buildSystemDailyUsageStatus(now, settings, signature, func(int64, string) (int64, error) {
			return 1000, nil
		})

		assert.True(t, status.ShouldBlock())
		assert.Equal(t, int64(1000), status.UsedTokens)
		assert.Equal(t, int64(0), status.RemainingTokens)
		assert.Positive(t, status.RetryAfterSeconds)
	})

	t.Run("counter read failure fails closed when enabled", func(t *testing.T) {
		status := buildSystemDailyUsageStatus(now, settings, signature, func(int64, string) (int64, error) {
			return 0, errors.New("query failed")
		})

		assert.True(t, status.ShouldBlock())
		assert.Equal(t, "query failed", status.EvaluationError)
	})
}

func TestBuildSystemDailyUsageStatusDisabledDoesNotBlockOnCounterReadFailure(t *testing.T) {
	now := time.Date(2026, 6, 30, 10, 0, 0, 0, time.UTC)
	settings := system_setting.DailyUsageLimitSettings{
		Enabled:     false,
		LimitTokens: 0,
		Timezone:    "UTC",
		Message:     system_setting.DefaultDailyUsageLimitMsg,
	}
	status := buildSystemDailyUsageStatus(now, settings, dailyUsageSettingsSignature(settings), func(int64, string) (int64, error) {
		return 0, errors.New("query failed")
	})

	assert.False(t, status.ShouldBlock())
	assert.Equal(t, "query failed", status.EvaluationError)
}
