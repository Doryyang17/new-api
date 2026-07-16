package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRelayTaskRemixChecksResolvedModelDailyUsageLimit(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.Log{}, &model.SystemDailyUsageCounter{}))

	originalLogConsumeEnabled := common.LogConsumeEnabled
	common.LogConsumeEnabled = true
	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error {
		if strings.HasPrefix(key, "daily_usage_setting.") {
			saved[key] = value
		}
		return nil
	}))
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(saved))
		common.LogConsumeEnabled = originalLogConsumeEnabled
		service.RefreshSystemDailyUsageStatus(time.Now())
	})

	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"daily_usage_setting.enabled":      "true",
		"daily_usage_setting.limit_tokens": "1000",
		"daily_usage_setting.timezone":     "UTC",
		"daily_usage_setting.message":      "global limit exceeded",
		"daily_usage_setting.model_limits": `[{"model_name":"sora-2","model_max_usage":10,"enabled":true}]`,
	}))

	channel := &model.Channel{
		Type:   constant.ChannelTypeSora,
		Key:    "sk-test",
		Status: common.ChannelStatusEnabled,
		Name:   "sora remix limit test",
	}
	require.NoError(t, db.Create(channel).Error)
	require.NoError(t, db.Create(&model.Task{
		TaskID:    "video-remix-source",
		UserId:    1,
		ChannelId: channel.Id,
		Platform:  constant.TaskPlatform("sora"),
		Status:    model.TaskStatusSuccess,
		Properties: model.Properties{
			OriginModelName: "sora-2",
		},
	}).Error)
	require.NoError(t, db.Create(&model.Log{
		CreatedAt:    time.Now().Unix(),
		Type:         model.LogTypeConsume,
		ModelName:    "sora-2",
		PromptTokens: 10,
	}).Error)
	service.RefreshSystemDailyUsageStatus(time.Now())

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "video_id", Value: "video-remix-source"}}
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/videos/video-remix-source/remix", nil)
	common.SetContextKey(ctx, constant.ContextKeyUserId, 1)

	RelayTask(ctx)

	require.Equal(t, http.StatusTooManyRequests, recorder.Code)
	require.Contains(t, recorder.Body.String(), "model_daily_usage_exceeded")
}
