package controller

import (
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

const maxRegistrationCodeBatchCount = 500

type CreateRegistrationCodesRequest struct {
	Count int    `json:"count"`
	Note  string `json:"note"`
}

func registrationCodeStatusFromQuery(value string) (int, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "unused", "enabled":
		return common.RegistrationCodeStatusEnabled, true
	case "used":
		return common.RegistrationCodeStatusUsed, true
	default:
		return 0, false
	}
}

func GetRegistrationCodes(c *gin.Context) {
	status, ok := registrationCodeStatusFromQuery(c.Query("status"))
	if !ok {
		common.ApiErrorMsg(c, "无效的注册码状态")
		return
	}
	pageInfo := common.GetPageQuery(c)
	codes, total, err := model.GetRegistrationCodes(status, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(codes)
	common.ApiSuccess(c, pageInfo)
}

func CreateRegistrationCodes(c *gin.Context) {
	var request CreateRegistrationCodesRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorMsg(c, "无效的参数")
		return
	}
	if request.Count <= 0 {
		common.ApiErrorMsg(c, "生成数量必须大于 0")
		return
	}
	if request.Count > maxRegistrationCodeBatchCount {
		common.ApiErrorMsg(c, "单次最多生成 500 个注册码")
		return
	}

	codes, err := model.GenerateRegistrationCodes(request.Count, c.GetInt("id"), request.Note)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "registration_code.create", map[string]interface{}{
		"count": request.Count,
	})
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    codes,
	})
}
