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

func TestBuildSystemDailyUsageStatusEnforcesGlobalAndModelLayers(t *testing.T) {
	originalLogConsumeEnabled := common.LogConsumeEnabled
	common.LogConsumeEnabled = true
	t.Cleanup(func() {
		common.LogConsumeEnabled = originalLogConsumeEnabled
	})
	now := time.Date(2026, 6, 30, 10, 0, 0, 0, time.UTC)
	settings := system_setting.DailyUsageLimitSettings{
		Enabled:     true,
		LimitTokens: 1000,
		Timezone:    "UTC",
		Message:     system_setting.DefaultDailyUsageLimitMsg,
		ModelLimits: []system_setting.DailyUsageModelLimit{{ModelName: "GLM-5.2", MaxUsage: 200, Enabled: true}},
	}
	signature := dailyUsageSettingsSignature(settings)

	t.Run("model limit blocks only matching model", func(t *testing.T) {
		status := buildSystemDailyUsageStatus(now, settings, signature, func(dayStart int64, timezone string, refreshedAt int64, models []string) (model.SystemDailyUsageSnapshot, error) {
			require.Equal(t, time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC).Unix(), dayStart)
			require.Equal(t, "UTC", timezone)
			require.Equal(t, now.Unix(), refreshedAt)
			require.Equal(t, []string{"GLM-5.2"}, models)
			return model.SystemDailyUsageSnapshot{
				UsedTokens:      500,
				ModelUsedTokens: map[string]int64{"GLM-5.2": 200},
			}, nil
		})

		assert.False(t, status.ShouldBlock())
		modelLimit, blocked := status.ShouldBlockModel("GLM-5.2")
		assert.True(t, blocked)
		assert.Equal(t, int64(200), modelLimit.CurrentUsage)
		_, blocked = status.ShouldBlockModel("GLM-5.1")
		assert.False(t, blocked)
		_, blocked = status.ShouldBlockModel("GPT-5")
		assert.False(t, blocked)
	})

	t.Run("global limit blocks all models", func(t *testing.T) {
		status := buildSystemDailyUsageStatus(now, settings, signature, func(int64, string, int64, []string) (model.SystemDailyUsageSnapshot, error) {
			return model.SystemDailyUsageSnapshot{
				UsedTokens:      1000,
				ModelUsedTokens: map[string]int64{"GLM-5.2": 100},
			}, nil
		})

		assert.True(t, status.ShouldBlock())
		assert.Equal(t, int64(0), status.RemainingTokens)
	})

	t.Run("snapshot refresh failure fails closed", func(t *testing.T) {
		status := buildSystemDailyUsageStatus(now, settings, signature, func(int64, string, int64, []string) (model.SystemDailyUsageSnapshot, error) {
			return model.SystemDailyUsageSnapshot{}, errors.New("query failed")
		})

		assert.True(t, status.ShouldBlock())
		assert.Equal(t, "query failed", status.EvaluationError)
	})
}

func TestBuildSystemDailyUsageStatusModelQueryFailureDoesNotBlockGlobalOrOtherModels(t *testing.T) {
	originalLogConsumeEnabled := common.LogConsumeEnabled
	common.LogConsumeEnabled = true
	t.Cleanup(func() {
		common.LogConsumeEnabled = originalLogConsumeEnabled
	})
	now := time.Date(2026, 6, 30, 10, 0, 0, 0, time.UTC)
	settings := system_setting.DailyUsageLimitSettings{
		Enabled:     true,
		LimitTokens: 1000,
		Timezone:    "UTC",
		Message:     system_setting.DefaultDailyUsageLimitMsg,
		ModelLimits: []system_setting.DailyUsageModelLimit{{ModelName: "GLM-5.2", MaxUsage: 200, Enabled: true}},
	}
	status := buildSystemDailyUsageStatus(now, settings, dailyUsageSettingsSignature(settings), func(int64, string, int64, []string) (model.SystemDailyUsageSnapshot, error) {
		return model.SystemDailyUsageSnapshot{
			UsedTokens:           100,
			ModelUsedTokens:      map[string]int64{},
			ModelEvaluationError: "model query failed",
		}, nil
	})

	assert.False(t, status.ShouldBlock())
	assert.Equal(t, int64(100), status.UsedTokens)
	assert.Empty(t, status.EvaluationError)
	_, blocked := status.ShouldBlockModel("GLM-5.2")
	assert.True(t, blocked)
	_, blocked = status.ShouldBlockModel("GLM-5.1")
	assert.False(t, blocked)
}

func TestBuildSystemDailyUsageStatusRequiresConsumeLogsForModelLimits(t *testing.T) {
	originalLogConsumeEnabled := common.LogConsumeEnabled
	common.LogConsumeEnabled = false
	t.Cleanup(func() {
		common.LogConsumeEnabled = originalLogConsumeEnabled
	})
	settings := system_setting.DailyUsageLimitSettings{
		Enabled:     false,
		LimitTokens: 1000,
		Timezone:    "UTC",
		Message:     system_setting.DefaultDailyUsageLimitMsg,
		ModelLimits: []system_setting.DailyUsageModelLimit{{ModelName: "GLM-5.2", MaxUsage: 200, Enabled: true}},
	}
	status := buildSystemDailyUsageStatus(time.Now(), settings, dailyUsageSettingsSignature(settings), func(int64, string, int64, []string) (model.SystemDailyUsageSnapshot, error) {
		return model.SystemDailyUsageSnapshot{}, nil
	})

	assert.False(t, status.ShouldBlock())
	_, blocked := status.ShouldBlockModel("GLM-5.2")
	assert.True(t, blocked)
	assert.Equal(t, system_setting.DailyUsageRequiresLogsMsg, status.EvaluationError)
}

func TestGetSystemDailyUsageStatusRefreshesExpiredSnapshotFromLogs(t *testing.T) {
	truncate(t)
	now := time.Now().UTC()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).Unix()
	require.NoError(t, model.DB.Create(&model.Log{
		CreatedAt:        dayStart + 60,
		Type:             model.LogTypeConsume,
		ModelName:        "GLM-5.2",
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
		"daily_usage_setting.model_limits": `[{"model_name":"GLM-5.2","model_max_usage":200,"enabled":true}]`,
	}))

	settings := system_setting.GetDailyUsageLimitSettings()
	staleStatus := baseSystemDailyUsageStatus(now.Add(-10*time.Minute), settings, dailyUsageSettingsSignature(settings))
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
	modelLimit, blocked := status.ShouldBlockModel("GLM-5.2")
	assert.False(t, blocked)
	assert.Equal(t, int64(150), modelLimit.CurrentUsage)
	assert.Greater(t, status.NextRefreshAt, now.Unix())
}
