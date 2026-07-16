package router

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestVideoContentProxyUsesDailyUsageLimitBeforeAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)

	oldDB := model.DB
	oldLogDB := model.LOG_DB
	oldRedisEnabled := common.RedisEnabled
	t.Cleanup(func() {
		model.DB = oldDB
		model.LOG_DB = oldLogDB
		common.RedisEnabled = oldRedisEnabled
	})

	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db
	common.RedisEnabled = false
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Log{}))
	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
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

	message := "daily usage route guard test"
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"daily_usage_setting.enabled":      "true",
		"daily_usage_setting.limit_tokens": "1",
		"daily_usage_setting.timezone":     "UTC",
		"daily_usage_setting.message":      message,
		"daily_usage_setting.model_limits": "[]",
	}))

	engine := gin.New()
	SetVideoRouter(engine)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/videos/task-1/content", nil)
	engine.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusTooManyRequests, recorder.Code)
	require.Contains(t, recorder.Body.String(), "system_daily_usage_exceeded")
	require.Contains(t, recorder.Body.String(), message)
}
