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

	assert.Error(t, ValidateDailyUsageLimitOption("daily_usage_setting.enabled", "maybe"))
	assert.Error(t, ValidateDailyUsageLimitOption("daily_usage_setting.limit_tokens", "-1"))
	assert.Error(t, ValidateDailyUsageLimitOption("daily_usage_setting.timezone", ""))
	assert.Error(t, ValidateDailyUsageLimitOption("daily_usage_setting.timezone", "Invalid/Zone"))
	assert.Error(t, ValidateDailyUsageLimitOption("daily_usage_setting.message", ""))
}
