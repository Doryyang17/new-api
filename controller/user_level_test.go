package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/user_level_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func userLevelTransitionConfig(ids ...string) user_level_setting.UserLevelConfig {
	levels := make([]user_level_setting.Level, 0, len(ids))
	for index, id := range ids {
		ratio := 1 - float64(index)*0.1
		levels = append(levels, user_level_setting.Level{
			ID:             id,
			Name:           id,
			ThresholdQuota: int64(index) * 100,
			Ratio:          ratio,
			BadgeColor:     "neutral",
		})
	}
	return user_level_setting.UserLevelConfig{
		SchemaVersion: user_level_setting.SchemaVersion,
		Enabled:       true,
		Levels:        levels,
	}
}

func TestValidateUserLevelConfigTransitionPreservesDurableIdentities(t *testing.T) {
	previous := userLevelTransitionConfig("base", "silver", "gold")

	err := user_level_setting.ValidateTransition(previous, userLevelTransitionConfig("base", "silver"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "不能删除")

	err = user_level_setting.ValidateTransition(previous, userLevelTransitionConfig("base", "gold", "silver"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "相对顺序不能改变")

	next := userLevelTransitionConfig("base", "bronze", "silver", "gold", "platinum")
	require.NoError(t, user_level_setting.ValidateTransition(previous, next))
}

func TestUpdateUserLevelConfigRequiresRevision(t *testing.T) {
	body, err := common.Marshal(userLevelTransitionConfig("base", "silver"))
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPut, "/api/option/user-level", bytes.NewReader(body))

	UpdateUserLevelConfig(context)

	var response struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.False(t, response.Success)
	assert.Contains(t, response.Message, "配置版本")
}
