package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestUpdateOptionRejectsTruthyDailyUsageEnabledWithoutLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)

	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error {
		if strings.HasPrefix(key, "daily_usage_setting.") {
			saved[key] = value
		}
		return nil
	}))
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(saved))
	})
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"daily_usage_setting.enabled":      "false",
		"daily_usage_setting.limit_tokens": "0",
		"daily_usage_setting.timezone":     "UTC",
		"daily_usage_setting.message":      "daily limit exceeded",
		"daily_usage_setting.model_limits": "[]",
	}))

	body, err := common.Marshal(map[string]interface{}{
		"key":   "daily_usage_setting.enabled",
		"value": 1,
	})
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPut, "/api/option/", bytes.NewReader(body))

	UpdateOption(ctx)

	var response struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.False(t, response.Success)
	require.Equal(t, "daily usage token limit must be greater than 0 when enabled", response.Message)
}

func TestUpdateOptionRejectsDailyUsageEnabledWhenConsumeLogsDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)

	originalLogConsumeEnabled := common.LogConsumeEnabled
	common.LogConsumeEnabled = false
	t.Cleanup(func() {
		common.LogConsumeEnabled = originalLogConsumeEnabled
	})
	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error {
		if strings.HasPrefix(key, "daily_usage_setting.") {
			saved[key] = value
		}
		return nil
	}))
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(saved))
	})
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"daily_usage_setting.enabled":      "false",
		"daily_usage_setting.limit_tokens": "1000",
		"daily_usage_setting.timezone":     "UTC",
		"daily_usage_setting.message":      "daily limit exceeded",
		"daily_usage_setting.model_limits": "[]",
	}))

	body, err := common.Marshal(map[string]interface{}{
		"key":   "daily_usage_setting.enabled",
		"value": true,
	})
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPut, "/api/option/", bytes.NewReader(body))

	UpdateOption(ctx)

	var response struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.False(t, response.Success)
	require.Equal(t, system_setting.DailyUsageRequiresLogsMsg, response.Message)
}

func TestUpdateOptionRejectsDisablingConsumeLogsWhenDailyUsageEnabled(t *testing.T) {
	gin.SetMode(gin.TestMode)

	originalLogConsumeEnabled := common.LogConsumeEnabled
	common.LogConsumeEnabled = true
	t.Cleanup(func() {
		common.LogConsumeEnabled = originalLogConsumeEnabled
	})
	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error {
		if strings.HasPrefix(key, "daily_usage_setting.") {
			saved[key] = value
		}
		return nil
	}))
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(saved))
	})
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"daily_usage_setting.enabled":      "true",
		"daily_usage_setting.limit_tokens": "1000",
		"daily_usage_setting.timezone":     "UTC",
		"daily_usage_setting.message":      "daily limit exceeded",
		"daily_usage_setting.model_limits": "[]",
	}))

	body, err := common.Marshal(map[string]interface{}{
		"key":   "LogConsumeEnabled",
		"value": false,
	})
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPut, "/api/option/", bytes.NewReader(body))

	UpdateOption(ctx)

	var response struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.False(t, response.Success)
	require.Equal(t, system_setting.DailyUsageRequiresLogsMsg, response.Message)
}

func TestUpdateOptionRejectsModelLimitAtOrAboveGlobalLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)

	originalLogConsumeEnabled := common.LogConsumeEnabled
	common.LogConsumeEnabled = true
	t.Cleanup(func() {
		common.LogConsumeEnabled = originalLogConsumeEnabled
	})
	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error {
		if strings.HasPrefix(key, "daily_usage_setting.") {
			saved[key] = value
		}
		return nil
	}))
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(saved))
	})
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"daily_usage_setting.enabled":      "true",
		"daily_usage_setting.limit_tokens": "1000",
		"daily_usage_setting.timezone":     "UTC",
		"daily_usage_setting.message":      "daily limit exceeded",
		"daily_usage_setting.model_limits": "[]",
	}))

	body, err := common.Marshal(map[string]interface{}{
		"key":   "daily_usage_setting.model_limits",
		"value": `[{"model_name":"GLM-5.2","model_max_usage":1000,"enabled":true}]`,
	})
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPut, "/api/option/", bytes.NewReader(body))

	UpdateOption(ctx)

	var response struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.False(t, response.Success)
	require.Equal(t, system_setting.ModelLimitLessThanGlobalMsg, response.Message)
}
