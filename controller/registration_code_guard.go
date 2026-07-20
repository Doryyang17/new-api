package controller

import (
	"fmt"
	"math"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

const (
	registrationCodeInvalidMessage       = "注册码无效或已被使用"
	registrationRiskBlockedMessage       = "注册尝试过多，请稍后再试"
	oauthRegistrationCodeRequiredMessage = "请填写注册码完成账号创建"
)

func respondRegistrationRiskBlocked(c *gin.Context, retryAfter time.Duration) {
	retrySeconds := int(math.Ceil(retryAfter.Seconds()))
	if retrySeconds < 1 {
		retrySeconds = 1
	}
	c.Header("Retry-After", fmt.Sprintf("%d", retrySeconds))
	c.JSON(http.StatusOK, gin.H{
		"success": false,
		"message": registrationRiskBlockedMessage,
	})
}

func validateRegistrationCodeForNewUser(c *gin.Context, rawCode string) (string, []string, bool) {
	riskKeys := service.BuildRegistrationRiskKeys(c.ClientIP(), c.Request.Header)
	blocked, retryAfter := service.IsRegistrationRiskBlocked(riskKeys)
	if blocked {
		respondRegistrationRiskBlocked(c, retryAfter)
		return "", riskKeys, false
	}

	code := model.NormalizeRegistrationCode(rawCode)
	if !model.IsRegistrationCodeFormatValid(code) {
		service.RecordRegistrationCodeFailure(riskKeys)
		common.ApiErrorMsg(c, registrationCodeInvalidMessage)
		return "", riskKeys, false
	}

	available, err := model.IsRegistrationCodeAvailable(code)
	if err != nil {
		common.SysLog(fmt.Sprintf("Check registration code error: %v", err))
		common.ApiErrorI18n(c, i18n.MsgDatabaseError)
		return "", riskKeys, false
	}
	if !available {
		service.RecordRegistrationCodeFailure(riskKeys)
		common.ApiErrorMsg(c, registrationCodeInvalidMessage)
		return "", riskKeys, false
	}

	return code, riskKeys, true
}
