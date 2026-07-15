package system_setting

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateRequestRiskOption(t *testing.T) {
	require.NoError(t, ValidateRequestRiskOption("request_risk_setting.enabled", "true"))
	require.NoError(t, ValidateRequestRiskOption("request_risk_setting.mode", RequestRiskModeObserve))
	require.NoError(t, ValidateRequestRiskOption("request_risk_setting.mode", RequestRiskModeEnforce))
	require.NoError(t, ValidateRequestRiskOption("request_risk_setting.token_block_seconds", "300"))
	require.NoError(t, ValidateRequestRiskOption("request_risk_setting.user_concurrency_limit", "8"))
	require.NoError(t, ValidateRequestRiskOption("request_risk_setting.token_concurrency_limit", "0"))
	require.NoError(t, ValidateRequestRiskOption("request_risk_setting.group_whitelist", `["trusted","vip"]`))

	assert.Error(t, ValidateRequestRiskOption("request_risk_setting.enabled", "maybe"))
	assert.Error(t, ValidateRequestRiskOption("request_risk_setting.mode", "block"))
	assert.Error(t, ValidateRequestRiskOption("request_risk_setting.token_block_seconds", "0"))
	assert.Error(t, ValidateRequestRiskOption("request_risk_setting.token_block_seconds", "86401"))
	assert.Error(t, ValidateRequestRiskOption("request_risk_setting.user_concurrency_limit", "-1"))
	assert.Error(t, ValidateRequestRiskOption("request_risk_setting.token_concurrency_limit", "1001"))
	assert.Error(t, ValidateRequestRiskOption("request_risk_setting.group_whitelist", `{"group":"trusted"}`))
	assert.Error(t, ValidateRequestRiskOption("request_risk_setting.unknown", "value"))
}

func TestRequestRiskGroupWhitelistedNormalizesValues(t *testing.T) {
	settings := RequestRiskSettings{GroupWhitelist: []string{" trusted ", "", "trusted", "vip"}}
	settings.GroupWhitelist = normalizeRequestRiskGroups(settings.GroupWhitelist)

	assert.True(t, RequestRiskGroupWhitelisted("trusted", settings))
	assert.True(t, RequestRiskGroupWhitelisted("vip", settings))
	assert.False(t, RequestRiskGroupWhitelisted("default", settings))
}

func TestNormalizeRequestRiskConcurrencyLimitPreservesDisabledZero(t *testing.T) {
	assert.Zero(t, normalizeRequestRiskConcurrencyLimit(0, DefaultRequestRiskUserConcurrencyLimit))
	assert.Equal(t, DefaultRequestRiskUserConcurrencyLimit, normalizeRequestRiskConcurrencyLimit(-1, DefaultRequestRiskUserConcurrencyLimit))
}

func TestGetRequestRiskSettingsReturnsIndependentWhitelist(t *testing.T) {
	common.OptionMapRWMutex.Lock()
	previous := requestRiskSettings
	previous.GroupWhitelist = append([]string(nil), requestRiskSettings.GroupWhitelist...)
	requestRiskSettings.GroupWhitelist = []string{"trusted"}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		requestRiskSettings = previous
		common.OptionMapRWMutex.Unlock()
	})

	settings := GetRequestRiskSettings()
	settings.GroupWhitelist[0] = "changed"

	assert.Equal(t, []string{"trusted"}, GetRequestRiskSettings().GroupWhitelist)
}
