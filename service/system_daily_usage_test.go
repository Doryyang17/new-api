package service

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/config"
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
		status := buildSystemDailyUsageStatus(now, settings, signature, func(dayStart int64, timezone string, refreshedAt int64) (int64, error) {
			require.Equal(t, time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC).Unix(), dayStart)
			require.Equal(t, "UTC", timezone)
			require.Equal(t, now.Unix(), refreshedAt)
			return 900, nil
		})

		assert.True(t, status.Enabled)
		assert.False(t, status.ShouldBlock())
		assert.Equal(t, int64(900), status.UsedTokens)
		assert.Equal(t, int64(100), status.RemainingTokens)
		assert.Empty(t, status.EvaluationError)
	})

	t.Run("reached limit", func(t *testing.T) {
		status := buildSystemDailyUsageStatus(now, settings, signature, func(int64, string, int64) (int64, error) {
			return 1000, nil
		})

		assert.True(t, status.ShouldBlock())
		assert.Equal(t, int64(1000), status.UsedTokens)
		assert.Equal(t, int64(0), status.RemainingTokens)
		assert.Positive(t, status.RetryAfterSeconds)
	})

	t.Run("snapshot refresh failure fails closed when enabled", func(t *testing.T) {
		status := buildSystemDailyUsageStatus(now, settings, signature, func(int64, string, int64) (int64, error) {
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
	status := buildSystemDailyUsageStatus(now, settings, dailyUsageSettingsSignature(settings), func(int64, string, int64) (int64, error) {
		return 0, errors.New("query failed")
	})

	assert.False(t, status.ShouldBlock())
	assert.Equal(t, "query failed", status.EvaluationError)
}

func TestBuildSystemDailyUsageStatusRequiresConsumeLogsWhenEnabled(t *testing.T) {
	originalLogConsumeEnabled := common.LogConsumeEnabled
	common.LogConsumeEnabled = false
	t.Cleanup(func() {
		common.LogConsumeEnabled = originalLogConsumeEnabled
	})
	now := time.Date(2026, 6, 30, 10, 0, 0, 0, time.UTC)
	settings := system_setting.DailyUsageLimitSettings{
		Enabled:     true,
		LimitTokens: 1000,
		Timezone:    "UTC",
		Message:     system_setting.DefaultDailyUsageLimitMsg,
	}

	status := buildSystemDailyUsageStatus(now, settings, dailyUsageSettingsSignature(settings), func(int64, string, int64) (int64, error) {
		return 900, nil
	})

	assert.True(t, status.ShouldBlock())
	assert.Equal(t, system_setting.DailyUsageRequiresLogsMsg, status.EvaluationError)
}

func TestGetSystemDailyUsageStatusRefreshesExpiredSnapshotFromLogs(t *testing.T) {
	truncate(t)
	now := time.Now().UTC()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).Unix()
	require.NoError(t, model.DB.Create(&model.Log{
		CreatedAt:        dayStart + 60,
		Type:             model.LogTypeConsume,
		PromptTokens:     120,
		CompletionTokens: 30,
	}).Error)

	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error {
		if strings.HasPrefix(key, "daily_usage_setting.") {
			saved[key] = value
		}
		return nil
	}))
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"daily_usage_setting.enabled":      "true",
		"daily_usage_setting.limit_tokens": "1000",
		"daily_usage_setting.timezone":     "UTC",
		"daily_usage_setting.message":      system_setting.DefaultDailyUsageLimitMsg,
	}))

	settings := system_setting.GetDailyUsageLimitSettings()
	staleStatus := baseSystemDailyUsageStatus(now.Add(-10*time.Minute), settings, dailyUsageSettingsSignature(settings))
	staleStatus.UsedTokens = 0
	staleStatus.NextRefreshAt = now.Add(-5 * time.Minute).Unix()

	systemDailyUsageMu.Lock()
	oldStatus := systemDailyUsageStatus
	oldLoaded := systemDailyUsageLoaded
	systemDailyUsageStatus = staleStatus
	systemDailyUsageLoaded = true
	systemDailyUsageMu.Unlock()

	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(saved))
		systemDailyUsageMu.Lock()
		systemDailyUsageStatus = oldStatus
		systemDailyUsageLoaded = oldLoaded
		systemDailyUsageMu.Unlock()
	})

	status := GetSystemDailyUsageStatus()
	assert.Equal(t, int64(150), status.UsedTokens)
	assert.False(t, status.ShouldBlock())
	assert.Greater(t, status.NextRefreshAt, now.Unix())
}
