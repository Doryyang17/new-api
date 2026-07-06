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
