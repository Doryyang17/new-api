package controller

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"golang.org/x/sync/errgroup"
)

func parseLogListOptions(c *gin.Context) model.LogListOptions {
	options := model.LogListOptions{IncludeTotal: true}
	if value := c.Query("with_count"); value != "" {
		if parsed, err := strconv.ParseBool(value); err == nil {
			options.IncludeTotal = parsed
		}
	}
	options.Compact = c.Query("compact") == "true"
	options.CursorMode = c.Query("cursor_mode") == "true"
	options.CursorCreatedAt, _ = strconv.ParseInt(c.Query("cursor_created_at"), 10, 64)
	options.CursorId, _ = strconv.Atoi(c.Query("cursor_id"))
	options.CursorRequestId = c.Query("cursor_request_id")
	options.CursorRowId = c.Query("cursor_row_id")
	return options
}

func GetAllLogs(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	logType, _ := strconv.Atoi(c.Query("type"))
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	username := c.Query("username")
	tokenName := c.Query("token_name")
	modelName := c.Query("model_name")
	channel, _ := strconv.Atoi(c.Query("channel"))
	group := c.Query("group")
	requestId := c.Query("request_id")
	upstreamRequestId := c.Query("upstream_request_id")
	logs, total, err := model.GetAllLogsWithOptions(logType, startTimestamp, endTimestamp, modelName, username, tokenName, pageInfo.GetStartIdx(), pageInfo.GetPageSize(), channel, group, requestId, upstreamRequestId, parseLogListOptions(c))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(logs)
	common.ApiSuccess(c, pageInfo)
	return
}

func GetUserLogs(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	userId := c.GetInt("id")
	logType, _ := strconv.Atoi(c.Query("type"))
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	tokenName := c.Query("token_name")
	modelName := c.Query("model_name")
	group := c.Query("group")
	requestId := c.Query("request_id")
	upstreamRequestId := c.Query("upstream_request_id")
	logs, total, err := model.GetUserLogsWithOptions(userId, logType, startTimestamp, endTimestamp, modelName, tokenName, pageInfo.GetStartIdx(), pageInfo.GetPageSize(), group, requestId, upstreamRequestId, parseLogListOptions(c))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(logs)
	common.ApiSuccess(c, pageInfo)
	return
}

func GetAllLogDetail(c *gin.Context) {
	log, err := model.GetAllLogDetailWithLocator(parseLogDetailLocator(c))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, log)
}

func GetUserLogDetail(c *gin.Context) {
	log, err := model.GetUserLogDetailWithLocator(c.GetInt("id"), parseLogDetailLocator(c))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, log)
}

func parseLogDetailLocator(c *gin.Context) model.LogDetailLocator {
	id, _ := strconv.Atoi(c.Query("log_id"))
	createdAt, _ := strconv.ParseInt(c.Query("created_at"), 10, 64)
	typeValue, _ := strconv.Atoi(c.Query("type"))
	channelId, _ := strconv.Atoi(c.Query("channel"))
	return model.LogDetailLocator{
		Id:                id,
		RowId:             c.Query("row_id"),
		RequestId:         c.Query("request_id"),
		CreatedAt:         createdAt,
		Type:              typeValue,
		ChannelId:         channelId,
		UpstreamRequestId: c.Query("upstream_request_id"),
	}
}

// Deprecated: SearchAllLogs 已废弃，前端未使用该接口。
func SearchAllLogs(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": false,
		"message": "该接口已废弃",
	})
}

// Deprecated: SearchUserLogs 已废弃，前端未使用该接口。
func SearchUserLogs(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": false,
		"message": "该接口已废弃",
	})
}

func GetLogByKey(c *gin.Context) {
	tokenId := c.GetInt("token_id")
	if tokenId == 0 {
		c.JSON(200, gin.H{
			"success": false,
			"message": "无效的令牌",
		})
		return
	}
	logs, err := model.GetLogByTokenId(tokenId)
	if err != nil {
		c.JSON(200, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	c.JSON(200, gin.H{
		"success": true,
		"message": "",
		"data":    logs,
	})
}

func GetLogsStat(c *gin.Context) {
	logType, _ := strconv.Atoi(c.Query("type"))
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	tokenName := c.Query("token_name")
	username := c.Query("username")
	modelName := c.Query("model_name")
	channel, _ := strconv.Atoi(c.Query("channel"))
	group := c.Query("group")
	requestId := c.Query("request_id")
	upstreamRequestId := c.Query("upstream_request_id")
	var total int64
	var stat model.Stat
	var queryGroup errgroup.Group
	queryGroup.Go(func() error {
		var err error
		total, err = model.CountAllLogs(logType, startTimestamp, endTimestamp, modelName, username, tokenName, channel, group, requestId, upstreamRequestId)
		return err
	})
	queryGroup.Go(func() error {
		var err error
		stat, err = model.SumUsedQuotaWithRequestIds(logType, startTimestamp, endTimestamp, modelName, username, tokenName, channel, group, requestId, upstreamRequestId)
		return err
	})
	if err := queryGroup.Wait(); err != nil {
		common.ApiError(c, err)
		return
	}
	//tokenNum := model.SumUsedToken(logType, startTimestamp, endTimestamp, modelName, username, "")
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"total": total,
			"quota": stat.Quota,
			"rpm":   stat.Rpm,
			"tpm":   stat.Tpm,
		},
	})
	return
}

func GetLogsSelfStat(c *gin.Context) {
	userId := c.GetInt("id")
	username := c.GetString("username")
	logType, _ := strconv.Atoi(c.Query("type"))
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	tokenName := c.Query("token_name")
	modelName := c.Query("model_name")
	channel, _ := strconv.Atoi(c.Query("channel"))
	group := c.Query("group")
	requestId := c.Query("request_id")
	upstreamRequestId := c.Query("upstream_request_id")
	var total int64
	var quotaNum model.Stat
	var queryGroup errgroup.Group
	queryGroup.Go(func() error {
		var err error
		total, err = model.CountUserLogs(userId, logType, startTimestamp, endTimestamp, modelName, tokenName, group, requestId, upstreamRequestId)
		return err
	})
	queryGroup.Go(func() error {
		var err error
		quotaNum, err = model.SumUsedQuotaWithRequestIds(logType, startTimestamp, endTimestamp, modelName, username, tokenName, channel, group, requestId, upstreamRequestId)
		return err
	})
	if err := queryGroup.Wait(); err != nil {
		common.ApiError(c, err)
		return
	}
	//tokenNum := model.SumUsedToken(logType, startTimestamp, endTimestamp, modelName, username, tokenName)
	c.JSON(200, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"total": total,
			"quota": quotaNum.Quota,
			"rpm":   quotaNum.Rpm,
			"tpm":   quotaNum.Tpm,
			//"token": tokenNum,
		},
	})
	return
}
