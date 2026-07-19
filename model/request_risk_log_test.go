package model

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListRequestRiskLogsReturnsAdminDiagnostics(t *testing.T) {
	truncateTables(t)

	observedOther := common.MapToJsonStr(map[string]interface{}{
		"risk_reason": "request_probe_guard",
		"admin_info": map[string]interface{}{
			"user_id":                101,
			"token_id":               202,
			"token_name":             "probe-key",
			"endpoint":               "/v1/chat/completions",
			"request_risk_mode":      "observe",
			"risk_level":             "medium",
			"risk_score":             3,
			"risk_factors":           []string{"meaningless_exact_match", "model_sweep"},
			"matched_keywords":       []string{"你好"},
			"text_preview":           "你好",
			"full_request_available": true,
			"extracted_chars":        2,
			"request_count_10s":      4,
			"distinct_models_60s":    4,
		},
	})
	blockedOther := common.MapToJsonStr(map[string]interface{}{
		"risk_reason":   "request_concurrency_guard",
		"reject_reason": "request_concurrency_guard",
		"admin_info": map[string]interface{}{
			"endpoint":                        "/v1/responses",
			"request_risk_mode":               "enforce",
			"risk_factors":                    []string{"token_concurrency_limit"},
			"full_request_available":          false,
			"full_request_unavailable_reason": "并发保护在读取请求体前命中",
			"token_in_flight":                 5,
			"token_limit":                     4,
		},
	})
	require.NoError(t, LOG_DB.Create(&Log{
		CreatedAt: 100,
		Type:      LogTypeError,
		Username:  "observer",
		ModelName: "gpt-test",
		RequestId: "req-observed",
		Other:     observedOther,
	}).Error)
	require.NoError(t, LOG_DB.Create(&RequestRiskLogDetail{
		RequestId:     "req-observed",
		CreatedAt:     100,
		Kind:          RequestRiskLogKindProbe,
		ExtractedText: "你好",
		FullRequest:   `{"model":"gpt-test","messages":[{"role":"user","content":"你好"}]}`,
	}).Error)
	require.NoError(t, LOG_DB.Create(&Log{
		UserId:    303,
		TokenId:   404,
		CreatedAt: 101,
		Type:      LogTypeError,
		Username:  "blocked-user",
		RequestId: "req-blocked",
		Other:     blockedOther,
	}).Error)
	require.NoError(t, LOG_DB.Create(&Log{
		CreatedAt: 102,
		Type:      LogTypeError,
		Content:   "unrelated",
		Other:     common.MapToJsonStr(map[string]interface{}{"reject_reason": "other"}),
	}).Error)

	items, total, err := ListRequestRiskLogsPage(RequestRiskLogQuery{Page: 1, PageSize: 50, Kind: RequestRiskLogKindProbe})
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, items, 1)
	item := items[0]
	assert.Equal(t, RequestRiskLogKindProbe, item.Kind)
	assert.False(t, item.Blocked)
	assert.Equal(t, 101, item.UserId)
	assert.Equal(t, 202, item.TokenId)
	assert.Equal(t, "probe-key", item.TokenName)
	assert.Equal(t, "medium", item.RiskLevel)
	assert.Equal(t, 3, item.Score)
	assert.Equal(t, []string{"meaningless_exact_match", "model_sweep"}, item.Factors)
	assert.Equal(t, []string{"你好"}, item.MatchedKeywords)
	assert.Empty(t, item.ExtractedText)
	assert.Empty(t, item.FullRequest)
	assert.True(t, item.FullRequestAvailable)

	prefixItems, prefixTotal, err := ListRequestRiskLogsPage(RequestRiskLogQuery{Page: 1, PageSize: 50, APIKeyID: 2})
	require.NoError(t, err)
	assert.Zero(t, prefixTotal)
	assert.Empty(t, prefixItems)

	exactItems, exactTotal, err := ListRequestRiskLogsPage(RequestRiskLogQuery{Page: 1, PageSize: 50, APIKeyID: 202})
	require.NoError(t, err)
	require.EqualValues(t, 1, exactTotal)
	require.Len(t, exactItems, 1)
	assert.Equal(t, 202, exactItems[0].TokenId)

	detail, err := GetRequestRiskLogDetail("req-observed", RequestRiskLogKindProbe, 100)
	require.NoError(t, err)
	assert.Equal(t, "你好", detail.ExtractedText)
	assert.JSONEq(t, `{"model":"gpt-test","messages":[{"role":"user","content":"你好"}]}`, detail.FullRequest)
	assert.True(t, detail.FullRequestAvailable)

	blockedItems, blockedTotal, err := ListRequestRiskLogsPage(RequestRiskLogQuery{Page: 1, PageSize: 50, Action: "blocked"})
	require.NoError(t, err)
	require.EqualValues(t, 1, blockedTotal)
	require.Len(t, blockedItems, 1)
	assert.Equal(t, RequestRiskLogKindConcurrency, blockedItems[0].Kind)
	assert.False(t, blockedItems[0].FullRequestAvailable)
	assert.Equal(t, "并发保护在读取请求体前命中", blockedItems[0].FullRequestUnavailableReason)
}

func TestClearRequestRiskLogsKeepsOtherErrorLogs(t *testing.T) {
	truncateTables(t)
	require.NoError(t, LOG_DB.Create(&Log{
		CreatedAt: 1,
		Type:      LogTypeError,
		Other:     common.MapToJsonStr(map[string]interface{}{"risk_reason": "request_probe_guard"}),
	}).Error)
	require.NoError(t, LOG_DB.Create(&Log{
		CreatedAt: 2,
		Type:      LogTypeError,
		Other:     common.MapToJsonStr(map[string]interface{}{"reject_reason": "other"}),
	}).Error)
	require.NoError(t, LOG_DB.Create(&RequestRiskLogDetail{
		RequestId:     "req-clear",
		CreatedAt:     1,
		Kind:          RequestRiskLogKindProbe,
		ExtractedText: "待清理",
	}).Error)

	deleted, err := ClearRequestRiskLogs()
	require.NoError(t, err)
	assert.EqualValues(t, 1, deleted)

	var remaining int64
	require.NoError(t, LOG_DB.Model(&Log{}).Count(&remaining).Error)
	assert.EqualValues(t, 1, remaining)
	require.NoError(t, LOG_DB.Model(&RequestRiskLogDetail{}).Count(&remaining).Error)
	assert.Zero(t, remaining)
}

func TestDeleteOldLogAlsoDeletesRequestRiskDetails(t *testing.T) {
	truncateTables(t)
	require.NoError(t, LOG_DB.Create(&Log{CreatedAt: 1, Type: LogTypeError}).Error)
	require.NoError(t, LOG_DB.Create(&RequestRiskLogDetail{
		RequestId: "old-detail",
		CreatedAt: 1,
		Kind:      RequestRiskLogKindProbe,
	}).Error)
	require.NoError(t, LOG_DB.Create(&RequestRiskLogDetail{
		RequestId: "new-detail",
		CreatedAt: 20,
		Kind:      RequestRiskLogKindProbe,
	}).Error)

	deleted, err := DeleteOldLog(context.Background(), 10, 100)
	require.NoError(t, err)
	assert.EqualValues(t, 1, deleted)

	var details []RequestRiskLogDetail
	require.NoError(t, LOG_DB.Order("created_at asc").Find(&details).Error)
	require.Len(t, details, 1)
	assert.Equal(t, "new-detail", details[0].RequestId)
}
