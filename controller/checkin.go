package controller

import (
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

func checkinBonusResponse(bonus *model.CheckinBonus) interface{} {
	if bonus == nil {
		return nil
	}
	return gin.H{
		"amount":           bonus.Amount,
		"remaining_amount": bonus.RemainingAmount,
		"created_at":       bonus.CreatedAt,
		"expire_at":        bonus.ExpireAt,
		"status":           bonus.Status,
	}
}

// GetCheckinStatus 获取用户签到状态和历史记录
func GetCheckinStatus(c *gin.Context) {
	setting := operation_setting.GetCheckinSetting()
	userId := c.GetInt("id")
	// 获取月份参数，默认为当前月份
	month := c.DefaultQuery("month", time.Now().Format("2006-01"))

	stats, err := model.GetUserCheckinStats(userId, month)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	activeBonus, err := model.GetActiveCheckinBonus(userId, time.Now())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	latestBonus, err := model.GetLatestCheckinBonus(userId, time.Now())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	bonusSetting := operation_setting.GetCheckinBonusSetting()

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"enabled":   setting.Enabled,
			"min_quota": setting.MinQuota,
			"max_quota": setting.MaxQuota,
			"bonus_setting": gin.H{
				"enabled":    bonusSetting.Enabled,
				"min_amount": bonusSetting.MinAmount,
				"max_amount": bonusSetting.MaxAmount,
			},
			"active_bonus": checkinBonusResponse(activeBonus),
			"latest_bonus": checkinBonusResponse(latestBonus),
			"stats":        stats,
		},
	})
}

// DoCheckin 执行用户签到
func DoCheckin(c *gin.Context) {
	setting := operation_setting.GetCheckinSetting()
	if !setting.Enabled {
		common.ApiErrorT(c, "Check-in feature is not enabled")
		return
	}

	userId := c.GetInt("id")

	checkin, bonus, err := model.UserCheckin(userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	logContent := common.NewMessage("Daily check-in, received {{quota}}", map[string]any{"quota": logger.FormatQuota(checkin.QuotaAwarded)})
	if bonus != nil {
		logContent = common.NewMessage("用户签到，获得签到赠金 {{quota}}（当日有效）", map[string]any{"quota": logger.FormatQuota(bonus.Amount)})
	}
	model.RecordLog(userId, model.LogTypeSystem, logContent)
	common.ApiSuccessT(c, "Check-in successful", gin.H{
		"quota_awarded": checkin.QuotaAwarded,
		"checkin_date":  checkin.CheckinDate,
		"bonus":         checkinBonusResponse(bonus),
	})
}
