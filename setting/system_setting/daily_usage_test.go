package system_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateDailyUsageLimitOption(t *testing.T) {
	require.NoError(t, ValidateDailyUsageLimitOption("daily_usage_setting.enabled", "true"))
	require.NoError(t, ValidateDailyUsageLimitOption("daily_usage_setting.limit_tokens", "1000"))
	require.NoError(t, ValidateDailyUsageLimitOption("daily_usage_setting.timezone", "Asia/Shanghai"))
	require.NoError(t, ValidateDailyUsageLimitOption("daily_usage_setting.message", DefaultDailyUsageLimitMsg))
	require.NoError(t, ValidateDailyUsageLimitOption("daily_usage_setting.model_limits", `[{"model_name":"gpt-4","model_max_usage":100,"enabled":true}]`))

	assert.Error(t, ValidateDailyUsageLimitOption("daily_usage_setting.enabled", "maybe"))
	assert.Error(t, ValidateDailyUsageLimitOption("daily_usage_setting.limit_tokens", "-1"))
	assert.Error(t, ValidateDailyUsageLimitOption("daily_usage_setting.timezone", ""))
	assert.Error(t, ValidateDailyUsageLimitOption("daily_usage_setting.timezone", "Invalid/Zone"))
	assert.Error(t, ValidateDailyUsageLimitOption("daily_usage_setting.message", ""))
	assert.Error(t, ValidateDailyUsageLimitOption("daily_usage_setting.model_limits", "gpt-4"))
}

func TestParseDailyUsageModelLimitsNormalizesModelNames(t *testing.T) {
	limits, err := ParseDailyUsageModelLimits(`[{"model_name":" gpt-4 ","model_max_usage":100,"enabled":true}]`)
	require.NoError(t, err)
	require.Len(t, limits, 1)
	assert.Equal(t, "gpt-4", limits[0].ModelName)
}

func TestValidateDailyUsageLimitSettings(t *testing.T) {
	settings := DailyUsageLimitSettings{
		Enabled:     true,
		LimitTokens: 1000,
		Timezone:    "UTC",
		Message:     DefaultDailyUsageLimitMsg,
		ModelLimits: []DailyUsageModelLimit{{ModelName: "GLM-5.2", MaxUsage: 200, Enabled: true}},
	}
	require.NoError(t, ValidateDailyUsageLimitSettings(settings))

	settings.ModelLimits[0].MaxUsage = 1000
	assert.EqualError(t, ValidateDailyUsageLimitSettings(settings), ModelLimitLessThanGlobalMsg)

	settings.ModelLimits = []DailyUsageModelLimit{
		{ModelName: "GLM-5.2", MaxUsage: 200, Enabled: true},
		{ModelName: "GLM-5.2", MaxUsage: 300, Enabled: false},
	}
	assert.EqualError(t, ValidateDailyUsageLimitSettings(settings), `duplicate daily usage model limit "GLM-5.2"`)
}
