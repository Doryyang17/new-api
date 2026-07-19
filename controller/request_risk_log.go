package controller

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func ListRequestRiskLogs(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", c.DefaultQuery("limit", "50")))
	apiKeyID, _ := strconv.Atoi(c.Query("api_key_id"))
	logs, total, err := model.ListRequestRiskLogsPage(model.RequestRiskLogQuery{
		Page:     page,
		PageSize: pageSize,
		Kind:     c.Query("kind"),
		Action:   c.Query("action"),
		Level:    c.Query("level"),
		Model:    c.Query("model"),
		APIKeyID: apiKeyID,
		Query:    c.Query("q"),
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"items":     logs,
			"total":     total,
			"page":      page,
			"page_size": pageSize,
		},
	})
}

func GetRequestRiskLogDetail(c *gin.Context) {
	createdAt, err := strconv.ParseInt(c.Query("created_at"), 10, 64)
	if err != nil || createdAt <= 0 {
		common.ApiErrorMsg(c, "风控日志时间参数不正确")
		return
	}
	log, err := model.GetRequestRiskLogDetail(
		c.Query("request_id"),
		c.Query("kind"),
		createdAt,
	)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, log)
}

func ClearRequestRiskLogs(c *gin.Context) {
	deleted, err := model.ClearRequestRiskLogs()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    gin.H{"deleted_count": deleted},
	})
}
