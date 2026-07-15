package model

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecordErrorLogResolvesUsernameFromUserId(t *testing.T) {
	truncateTables(t)
	gin.SetMode(gin.TestMode)

	user := User{
		Username: "curfew-user",
		Status:   common.UserStatusEnabled,
	}
	require.NoError(t, DB.Create(&user).Error)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	RecordErrorLog(c, user.Id, 0, "", "", "system curfew", 0, 0, false, "", nil)

	var log Log
	require.NoError(t, LOG_DB.First(&log).Error)
	assert.Equal(t, user.Id, log.UserId)
	assert.Equal(t, user.Username, log.Username)
	assert.Equal(t, LogTypeError, log.Type)
	assert.Equal(t, "system curfew", log.Content)
}

func TestGetAllLogsHydratesMissingUsername(t *testing.T) {
	truncateTables(t)

	user := User{
		Username: "historical-curfew-user",
		Status:   common.UserStatusEnabled,
	}
	require.NoError(t, DB.Create(&user).Error)
	require.NoError(t, LOG_DB.Create(&Log{
		UserId:    user.Id,
		CreatedAt: common.GetTimestamp(),
		Type:      LogTypeError,
		Content:   "system curfew",
	}).Error)

	logs, total, err := GetAllLogs(LogTypeError, 0, 0, "", "", "", 0, 10, 0, "", "", "")
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, logs, 1)
	assert.Equal(t, user.Username, logs[0].Username)
}

func TestGetAllLogsUsernameFilterFindsLogByUserIdWhenUsernameMissing(t *testing.T) {
	truncateTables(t)

	user := User{
		Username: "filterable-curfew-user",
		Status:   common.UserStatusEnabled,
	}
	require.NoError(t, DB.Create(&user).Error)
	require.NoError(t, LOG_DB.Create(&Log{
		UserId:    user.Id,
		CreatedAt: common.GetTimestamp(),
		Type:      LogTypeError,
		Content:   "system curfew",
	}).Error)

	logs, total, err := GetAllLogs(LogTypeError, 0, 0, "", user.Username, "", 0, 10, 0, "", "", "")
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, logs, 1)
	assert.Equal(t, user.Username, logs[0].Username)
}

func TestRecordRequestGuardLogKeepsObservationAdminOnly(t *testing.T) {
	truncateTables(t)
	gin.SetMode(gin.TestMode)

	user := User{
		Username: "request-guard-user",
		Status:   common.UserStatusEnabled,
	}
	require.NoError(t, DB.Create(&user).Error)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Set("id", user.Id)
	c.Set("username", user.Username)
	c.Set("token_id", 99123)
	c.Set("token_name", "probe-token")
	c.Set("group", "default")

	RecordRequestGuardLog(c, "观察命中", "request_probe_guard", nil, false)
	RecordRequestGuardLog(c, "已限制请求", "request_probe_guard", nil, true)

	var observed Log
	require.NoError(t, LOG_DB.Where("content = ?", "观察命中").First(&observed).Error)
	assert.Zero(t, observed.UserId)
	assert.Zero(t, observed.TokenId)
	assert.Empty(t, observed.TokenName)
	observedOther, err := common.StrToMap(observed.Other)
	require.NoError(t, err)
	adminInfo, ok := observedOther["admin_info"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, common.Interface2String(user.Id), common.Interface2String(adminInfo["user_id"]))
	assert.Equal(t, "99123", common.Interface2String(adminInfo["token_id"]))
	assert.Equal(t, "probe-token", adminInfo["token_name"])

	userLogs, total, err := GetUserLogs(user.Id, LogTypeError, 0, 0, "", "", 0, 10, "", "", "")
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	require.Len(t, userLogs, 1)
	assert.Equal(t, "已限制请求", userLogs[0].Content)

	tokenLogs, err := GetLogByTokenId(99123)
	require.NoError(t, err)
	require.Len(t, tokenLogs, 1)
	assert.Equal(t, "已限制请求", tokenLogs[0].Content)
}
