package controller

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func withRequestRiskOptionTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	gin.SetMode(gin.TestMode)

	previousRequestRiskOptions := make(map[string]string)
	common.OptionMapRWMutex.Lock()
	previousOptionMapWasNil := common.OptionMap == nil
	if previousOptionMapWasNil {
		common.OptionMap = make(map[string]string)
	}
	for key, value := range common.OptionMap {
		if strings.HasPrefix(key, "request_risk_setting.") {
			previousRequestRiskOptions[key] = value
		}
	}
	common.OptionMapRWMutex.Unlock()
	previousSettings := system_setting.GetRequestRiskSettings()
	previousDB := model.DB
	previousLogDB := model.LOG_DB
	previousRedisEnabled := common.RedisEnabled
	previousMainDatabaseType := common.MainDatabaseType()
	previousLogDatabaseType := common.LogDatabaseType()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Option{}, &model.User{}, &model.Log{}))
	model.DB = db
	model.LOG_DB = db
	common.RedisEnabled = false
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)

	t.Cleanup(func() {
		model.DB = previousDB
		model.LOG_DB = previousLogDB
		common.RedisEnabled = previousRedisEnabled
		common.SetDatabaseTypes(previousMainDatabaseType, previousLogDatabaseType)
		common.OptionMapRWMutex.Lock()
		for key := range common.OptionMap {
			if strings.HasPrefix(key, "request_risk_setting.") {
				delete(common.OptionMap, key)
			}
		}
		for key, value := range previousRequestRiskOptions {
			common.OptionMap[key] = value
		}
		if previousOptionMapWasNil {
			common.OptionMap = nil
		}
		common.OptionMapRWMutex.Unlock()
		restoreRequestRiskSettings(t, previousSettings)
		sqlDB, err := db.DB()
		if err == nil {
			require.NoError(t, sqlDB.Close())
		}
	})
	return db
}

func restoreRequestRiskSettings(t *testing.T, settings system_setting.RequestRiskSettings) {
	t.Helper()
	groupWhitelist, err := common.Marshal(settings.GroupWhitelist)
	require.NoError(t, err)
	require.NoError(t, system_setting.ValidateRequestRiskOption("request_risk_setting.group_whitelist", string(groupWhitelist)))
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"request_risk_setting.enabled":                 fmt.Sprintf("%t", settings.Enabled),
		"request_risk_setting.mode":                    settings.Mode,
		"request_risk_setting.log_matches":             fmt.Sprintf("%t", settings.LogMatches),
		"request_risk_setting.medium_cooldown_seconds": fmt.Sprintf("%d", settings.MediumCooldownSeconds),
		"request_risk_setting.token_block_seconds":     fmt.Sprintf("%d", settings.TokenBlockSeconds),
		"request_risk_setting.user_block_seconds":      fmt.Sprintf("%d", settings.UserBlockSeconds),
		"request_risk_setting.ip_block_seconds":        fmt.Sprintf("%d", settings.IPBlockSeconds),
		"request_risk_setting.user_concurrency_limit":  fmt.Sprintf("%d", settings.UserConcurrencyLimit),
		"request_risk_setting.token_concurrency_limit": fmt.Sprintf("%d", settings.TokenConcurrencyLimit),
		"request_risk_setting.group_whitelist":         string(groupWhitelist),
	}))
}

func TestUpdateRequestRiskOptionsPersistsCompleteSettings(t *testing.T) {
	db := withRequestRiskOptionTestDB(t)
	body, err := common.Marshal(requestRiskOptionsUpdateRequest{
		Updates: []requestRiskOptionUpdate{
			{Key: "request_risk_setting.enabled", Value: "true"},
			{Key: "request_risk_setting.mode", Value: system_setting.RequestRiskModeEnforce},
			{Key: "request_risk_setting.log_matches", Value: "false"},
			{Key: "request_risk_setting.medium_cooldown_seconds", Value: "12"},
			{Key: "request_risk_setting.token_block_seconds", Value: "360"},
			{Key: "request_risk_setting.user_block_seconds", Value: "180"},
			{Key: "request_risk_setting.ip_block_seconds", Value: "90"},
			{Key: "request_risk_setting.user_concurrency_limit", Value: "10"},
			{Key: "request_risk_setting.token_concurrency_limit", Value: "5"},
			{Key: "request_risk_setting.group_whitelist", Value: `["trusted","vip"]`},
		},
	})
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPut, "/api/option/request-risk", bytes.NewReader(body))
	UpdateRequestRiskOptions(context)

	var response struct {
		Success bool `json:"success"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)

	var options []model.Option
	require.NoError(t, db.Where("key LIKE ?", "request_risk_setting.%").Find(&options).Error)
	assert.Len(t, options, 10)
	settings := system_setting.GetRequestRiskSettings()
	assert.True(t, settings.Enabled)
	assert.Equal(t, system_setting.RequestRiskModeEnforce, settings.Mode)
	assert.False(t, settings.LogMatches)
	assert.Equal(t, 12, settings.MediumCooldownSeconds)
	assert.Equal(t, 360, settings.TokenBlockSeconds)
	assert.Equal(t, 180, settings.UserBlockSeconds)
	assert.Equal(t, 90, settings.IPBlockSeconds)
	assert.Equal(t, 10, settings.UserConcurrencyLimit)
	assert.Equal(t, 5, settings.TokenConcurrencyLimit)
	assert.Equal(t, []string{"trusted", "vip"}, settings.GroupWhitelist)
}

func TestUpdateRequestRiskOptionsValidatesBeforeWriting(t *testing.T) {
	db := withRequestRiskOptionTestDB(t)
	body, err := common.Marshal(requestRiskOptionsUpdateRequest{
		Updates: []requestRiskOptionUpdate{
			{Key: "request_risk_setting.medium_cooldown_seconds", Value: "0"},
		},
	})
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPut, "/api/option/request-risk", bytes.NewReader(body))
	UpdateRequestRiskOptions(context)

	var response struct {
		Success bool `json:"success"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.False(t, response.Success)
	var count int64
	require.NoError(t, db.Model(&model.Option{}).Where("key LIKE ?", "request_risk_setting.%").Count(&count).Error)
	assert.Zero(t, count)
}

func TestUpdateRequestRiskOptionsPreservesUnsubmittedSettings(t *testing.T) {
	db := withRequestRiskOptionTestDB(t)
	require.NoError(t, model.UpdateOption("request_risk_setting.mode", system_setting.RequestRiskModeEnforce))
	body, err := common.Marshal(requestRiskOptionsUpdateRequest{
		Updates: []requestRiskOptionUpdate{
			{Key: "request_risk_setting.medium_cooldown_seconds", Value: "15"},
		},
	})
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPut, "/api/option/request-risk", bytes.NewReader(body))
	UpdateRequestRiskOptions(context)

	var response struct {
		Success bool `json:"success"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	var mode model.Option
	require.NoError(t, db.First(&mode, "key = ?", "request_risk_setting.mode").Error)
	assert.Equal(t, system_setting.RequestRiskModeEnforce, mode.Value)
	settings := system_setting.GetRequestRiskSettings()
	assert.Equal(t, system_setting.RequestRiskModeEnforce, settings.Mode)
	assert.Equal(t, 15, settings.MediumCooldownSeconds)
}
