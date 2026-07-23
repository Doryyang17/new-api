package model

import (
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestGetAllLogsWithOptionsUsesCompactCursorPage(t *testing.T) {
	truncateTables(t)
	now := common.GetTimestamp()
	for i := 0; i < 2; i++ {
		require.NoError(t, LOG_DB.Create(&Log{
			CreatedAt: now - int64(i),
			Type:      LogTypeError,
			RequestId: "compact-cursor-" + string(rune('a'+i)),
			Other: common.MapToJsonStr(map[string]interface{}{
				"reject_reason": "prompt_filter",
				"full_text":     "large prompt that stays out of list pages",
			}),
		}).Error)
	}

	logs, total, err := GetAllLogsWithOptions(
		LogTypeError,
		0,
		0,
		"",
		"",
		"",
		0,
		1,
		0,
		"",
		"",
		"",
		LogListOptions{Compact: true, CursorMode: true},
	)
	require.NoError(t, err)
	assert.Zero(t, total)
	require.Len(t, logs, 1)
	assert.NotZero(t, logs[0].CursorId)
	assert.NotContains(t, logs[0].Other, "large prompt")

	secondPage, _, err := GetAllLogsWithOptions(
		LogTypeError,
		0,
		0,
		"",
		"",
		"",
		0,
		1,
		0,
		"",
		"",
		"",
		LogListOptions{
			Compact:         true,
			CursorMode:      true,
			CursorCreatedAt: logs[0].CreatedAt,
			CursorId:        logs[0].CursorId,
		},
	)
	require.NoError(t, err)
	require.Len(t, secondPage, 1)
	assert.NotEqual(t, logs[0].CursorId, secondPage[0].CursorId)
}

func TestGetAllLogsWithOptionsOmitsOversizedOtherUntilDetail(t *testing.T) {
	truncateTables(t)
	largeOther := common.MapToJsonStr(map[string]interface{}{
		"opaque_detail": strings.Repeat("x", compactLogOtherMaxLength+1),
	})
	log := &Log{
		CreatedAt: common.GetTimestamp(),
		Type:      LogTypeError,
		RequestId: "oversized-compact-detail",
		Other:     largeOther,
	}
	require.NoError(t, LOG_DB.Create(log).Error)

	logs, _, err := GetAllLogsWithOptions(
		LogTypeError,
		0,
		0,
		"",
		"",
		"",
		0,
		1,
		0,
		"",
		"",
		"",
		LogListOptions{Compact: true},
	)
	require.NoError(t, err)
	require.Len(t, logs, 1)
	assert.Empty(t, logs[0].Other)

	detail, err := GetAllLogDetail(log.Id, log.RequestId, log.CreatedAt)
	require.NoError(t, err)
	assert.Equal(t, largeOther, detail.Other)
}

func TestGetUserLogDetailEnforcesOwnerAndStripsAdminFields(t *testing.T) {
	truncateTables(t)
	users := []User{
		{Username: "detail-owner", AffCode: "detail-owner", Status: common.UserStatusEnabled},
		{Username: "detail-other", AffCode: "detail-other", Status: common.UserStatusEnabled},
	}
	require.NoError(t, DB.Create(&users).Error)
	log := &Log{
		UserId:    users[1].Id,
		CreatedAt: common.GetTimestamp(),
		Type:      LogTypeError,
		Content:   "private detail",
		Other: common.MapToJsonStr(map[string]interface{}{
			"reject_reason": "prompt_filter",
			"admin_info":    map[string]interface{}{"secret": "hidden"},
			"full_text":     "large private prompt",
		}),
	}
	require.NoError(t, LOG_DB.Create(log).Error)

	_, err := GetUserLogDetail(users[0].Id, log.Id, log.RequestId, log.CreatedAt)
	require.Error(t, err)

	detail, err := GetUserLogDetail(users[1].Id, log.Id, log.RequestId, log.CreatedAt)
	require.NoError(t, err)
	assert.NotContains(t, detail.Other, "secret")
	assert.NotContains(t, detail.Other, "large private prompt")
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

func TestRecordRequestGuardLogStoresLargeDetailsSeparately(t *testing.T) {
	truncateTables(t)
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Set(common.RequestIdKey, "request-risk-separated")
	c.Set("id", 88)
	c.Set("username", "risk-user")
	other := map[string]interface{}{
		"admin_info": map[string]interface{}{
			"text_preview":           "请求预览",
			"full_request_available": true,
		},
	}

	RecordRequestGuardLogWithDetail(
		c,
		"观察命中",
		"request_probe_guard",
		other,
		false,
		&RequestRiskLogDetail{
			ExtractedText: "完整评分文本",
			FullRequest:   `{"model":"gpt-test","messages":[{"role":"user","content":"完整请求内容"}]}`,
		},
	)

	var log Log
	require.NoError(t, LOG_DB.Where("request_id = ?", "request-risk-separated").First(&log).Error)
	assert.NotContains(t, log.Other, "request_risk_extracted_text")
	assert.NotContains(t, log.Other, "request_risk_full_request")
	assert.NotContains(t, log.Other, "完整请求内容")

	var detail RequestRiskLogDetail
	require.NoError(t, LOG_DB.Where("request_id = ?", "request-risk-separated").First(&detail).Error)
	assert.Equal(t, RequestRiskLogKindProbe, detail.Kind)
	assert.Equal(t, "完整评分文本", detail.ExtractedText)
	assert.Contains(t, detail.FullRequest, "完整请求内容")
}
