package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestSystemModelDailyUsageLimitIsScopedToRequestedModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldDB := model.DB
	oldLogDB := model.LOG_DB
	oldLogConsumeEnabled := common.LogConsumeEnabled
	oldRedisEnabled := common.RedisEnabled
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db
	common.LogConsumeEnabled = true
	common.RedisEnabled = false
	require.NoError(t, db.AutoMigrate(&model.Log{}, &model.SystemDailyUsageCounter{}))

	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error {
		if strings.HasPrefix(key, "daily_usage_setting.") {
			saved[key] = value
		}
		return nil
	}))
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(saved))
		service.RefreshSystemDailyUsageStatus(time.Now())
		model.DB = oldDB
		model.LOG_DB = oldLogDB
		common.LogConsumeEnabled = oldLogConsumeEnabled
		common.RedisEnabled = oldRedisEnabled
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			_ = sqlDB.Close()
		}
	})

	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"daily_usage_setting.enabled":      "true",
		"daily_usage_setting.limit_tokens": "1000",
		"daily_usage_setting.timezone":     "UTC",
		"daily_usage_setting.message":      "global limit exceeded",
		"daily_usage_setting.model_limits": `[{"model_name":"GLM-5.2","model_max_usage":20,"enabled":true}]`,
	}))
	now := time.Now().Unix()
	require.NoError(t, db.Create(&model.Log{
		CreatedAt:        now,
		Type:             model.LogTypeConsume,
		ModelName:        "GLM-5.2",
		PromptTokens:     20,
		CompletionTokens: 0,
	}).Error)
	require.NoError(t, db.Create(&model.Log{
		CreatedAt:        now,
		Type:             model.LogTypeConsume,
		ModelName:        "GLM-5.1",
		PromptTokens:     10,
		CompletionTokens: 0,
	}).Error)
	service.RefreshSystemDailyUsageStatus(time.Now())

	blockedRecorder := httptest.NewRecorder()
	blockedContext, _ := gin.CreateTestContext(blockedRecorder)
	blockedContext.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	require.True(t, CheckSystemModelDailyUsageLimit(blockedContext, "GLM-5.2"))
	require.Equal(t, http.StatusTooManyRequests, blockedRecorder.Code)
	require.Contains(t, blockedRecorder.Body.String(), "model_daily_usage_exceeded")

	allowedRecorder := httptest.NewRecorder()
	allowedContext, _ := gin.CreateTestContext(allowedRecorder)
	allowedContext.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	require.False(t, CheckSystemModelDailyUsageLimit(allowedContext, "GLM-5.1"))

	require.NoError(t, db.Create(&model.Log{
		CreatedAt:        now,
		Type:             model.LogTypeConsume,
		ModelName:        "GPT-5",
		PromptTokens:     970,
		CompletionTokens: 0,
	}).Error)
	service.RefreshSystemDailyUsageStatus(time.Now())

	globalRecorder := httptest.NewRecorder()
	globalContext, _ := gin.CreateTestContext(globalRecorder)
	globalContext.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	require.True(t, CheckSystemModelDailyUsageLimit(globalContext, "GLM-5.1"))
	require.Contains(t, globalRecorder.Body.String(), "system_daily_usage_exceeded")
}
