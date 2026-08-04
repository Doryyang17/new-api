package controller

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/user_level_setting"

	"github.com/gin-gonic/gin"
)

func GetUserLevelStatus(c *gin.Context) {
	status, err := service.GetUserLevelStatus(c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    status,
	})
}

func ClaimUserLevel(c *gin.Context) {
	result, err := service.ClaimUserLevel(c.GetInt("id"))
	if err != nil {
		if errors.Is(err, model.ErrUserLevelDisabled) {
			common.ApiErrorMsg(c, err.Error())
			return
		}
		common.ApiError(c, err)
		return
	}
	message := "当前没有可领取的新等级"
	if result.Changed {
		message = "等级领取成功"
		model.RecordLog(c.GetInt("id"), model.LogTypeSystem, fmt.Sprintf(
			"领取用户等级：%s → %s，累计已结算消耗额度 %d，等级倍率 ×%.4g",
			result.PreviousLevel.Name,
			result.Status.CurrentLevel.Name,
			result.Status.TotalConsumedQuota,
			result.Status.CurrentLevel.Ratio,
		))
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": message,
		"data":    result,
	})
}

type userLevelConfigUpdateRequest struct {
	Config   user_level_setting.UserLevelConfig `json:"config"`
	Revision string                             `json:"revision"`
}

func GetUserLevelConfig(c *gin.Context) {
	current, revision, err := model.GetUserLevelConfigSnapshot()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	memberCounts, err := model.CountUsersByLevel()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"config":        current,
			"member_counts": memberCounts,
			"revision":      revision,
		},
	})
}

func UpdateUserLevelConfig(c *gin.Context) {
	var request userLevelConfigUpdateRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorMsg(c, "无效的用户等级配置")
		return
	}
	if request.Revision == "" {
		common.ApiErrorMsg(c, "缺少用户等级配置版本，请刷新后重试")
		return
	}
	next, err := user_level_setting.NormalizeAndValidate(request.Config)
	if err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	previous, saved, err := model.SaveUserLevelConfig(next, request.Revision)
	if err != nil {
		if errors.Is(err, model.ErrUserLevelConfigConflict) {
			common.ApiErrorMsg(c, err.Error())
			return
		}
		common.ApiError(c, err)
		return
	}
	revision, err := user_level_setting.ConfigRevision(saved)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	memberCounts, err := model.CountUsersByLevel()
	if err != nil {
		// The option is already durable at this point. Do not turn a successful
		// mutation into a retryable client error just because the informational
		// member-count refresh failed.
		common.SysError("用户等级设置已保存，但成员数量刷新失败: " + err.Error())
		memberCounts = map[string]int64{}
	}
	recordManageAudit(c, "user_level.config.update", map[string]interface{}{
		"enabled_before": previous.Enabled,
		"enabled_after":  saved.Enabled,
		"changed_levels": changedUserLevelIDs(previous, saved),
	})
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "用户等级设置已保存",
		"data": gin.H{
			"config":        saved,
			"member_counts": memberCounts,
			"revision":      revision,
		},
	})
}

func changedUserLevelIDs(previous, next user_level_setting.UserLevelConfig) []string {
	previousByID := make(map[string]user_level_setting.Level, len(previous.Levels))
	for _, level := range previous.Levels {
		previousByID[level.ID] = level
	}
	changed := make([]string, 0)
	for _, level := range next.Levels {
		old, exists := previousByID[level.ID]
		if !exists || old != level {
			changed = append(changed, level.ID)
		}
	}
	return changed
}

func decorateUsersWithLevels(users []*model.User) {
	if len(users) == 0 {
		return
	}
	currentConfig := user_level_setting.GetConfig()
	levelsByID := make(map[string]user_level_setting.Level, len(currentConfig.Levels))
	for _, level := range currentConfig.Levels {
		levelsByID[level.ID] = level
	}
	base := levelsByID[user_level_setting.BaseLevelID]
	for _, user := range users {
		if user == nil {
			continue
		}
		level := base
		if configured, exists := levelsByID[user.LevelKey]; exists {
			level = configured
		}
		user.LevelName = level.Name
		user.LevelRatio = level.Ratio
		user.LevelBadgeColor = level.BadgeColor
		user.LevelEnabled = currentConfig.Enabled
	}
}
