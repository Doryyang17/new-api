package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQuotaGrantAmountFromUSD(t *testing.T) {
	originalQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500000
	t.Cleanup(func() { common.QuotaPerUnit = originalQuotaPerUnit })

	quota, normalized, err := quotaGrantAmountFromUSD("10")
	require.NoError(t, err)
	assert.Equal(t, 5000000, quota)
	assert.Equal(t, "10.00", normalized)

	_, _, err = quotaGrantAmountFromUSD("0.001")
	assert.ErrorContains(t, err, "两位小数")

	_, _, err = quotaGrantAmountFromUSD("0")
	assert.ErrorContains(t, err, "必须大于")

	_, _, err = quotaGrantAmountFromUSD("999999999")
	assert.ErrorContains(t, err, "超出")
}

func TestNormalizeQuotaGrantFiltersBuildsIntersectionAndBalanceBoundaries(t *testing.T) {
	originalQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 100
	t.Cleanup(func() { common.QuotaPerUnit = originalQuotaPerUnit })
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.FixedZone("UTC+8", 8*60*60))

	filters, filterJson, summary, err := normalizeQuotaGrantFilters(quotaGrantFilterRequest{
		Roles:         []int{common.RoleCommonUser},
		Statuses:      []int{common.UserStatusEnabled},
		BalanceMode:   "lt",
		BalanceAmount: "10",
		RechargeMode:  "recharged",
		UsageMode:     "used",
		UsagePeriod:   "7d",
	}, now)
	require.NoError(t, err)
	require.NotNil(t, filters.MaxQuota)
	assert.Equal(t, 1000, *filters.MaxQuota)
	assert.False(t, filters.MaxQuotaInclusive)
	assert.Equal(t, "recharged", filters.RechargeMode)
	assert.Equal(t, "used", filters.UsageMode)
	assert.Equal(t, now.Add(-7*24*time.Hour).Unix(), filters.UsageStartAt)
	assert.Contains(t, filterJson, `"balance_mode":"lt"`)
	assert.Equal(t, "已启用；普通用户；余额 < $10.00；已充值；近7天有模型消耗", summary)
}

func TestNormalizeQuotaGrantFiltersRejectsInvalidRechargeMode(t *testing.T) {
	_, _, _, err := normalizeQuotaGrantFilters(quotaGrantFilterRequest{
		RechargeMode: "pending",
	}, time.Now())
	assert.ErrorContains(t, err, "不支持的充值情况筛选")
}

func TestNormalizeQuotaGrantFiltersBuildsRechargeDateWindows(t *testing.T) {
	now := time.Date(2026, 7, 22, 1, 0, 0, 0, time.UTC)
	filters, _, summary, err := normalizeQuotaGrantFilters(quotaGrantFilterRequest{
		RechargeMode: "yesterday",
	}, now)
	require.NoError(t, err)
	location, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 7, 21, 0, 0, 0, 0, location).Unix(), filters.RechargeStartAt)
	assert.Equal(t, time.Date(2026, 7, 22, 0, 0, 0, 0, location).Unix(), filters.RechargeEndAt)
	assert.Contains(t, summary, "昨日充值")

	filters, _, summary, err = normalizeQuotaGrantFilters(quotaGrantFilterRequest{
		RechargeMode: "date",
		RechargeDate: "2026-07-20",
	}, now)
	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 7, 20, 0, 0, 0, 0, location).Unix(), filters.RechargeStartAt)
	assert.Equal(t, time.Date(2026, 7, 21, 0, 0, 0, 0, location).Unix(), filters.RechargeEndAt)
	assert.Contains(t, summary, "2026-07-20 充值")
}

func TestNormalizeQuotaGrantFiltersRejectsMissingRechargeDate(t *testing.T) {
	_, _, _, err := normalizeQuotaGrantFilters(quotaGrantFilterRequest{RechargeMode: "date"}, time.Now())
	assert.ErrorContains(t, err, "指定充值日期格式不正确")
}

func TestNormalizeQuotaGrantFiltersAppliesSharedBehaviorTimeRangeAndUsageModel(t *testing.T) {
	now := time.Date(2026, 7, 23, 1, 0, 0, 0, time.UTC)
	filters, filterJson, summary, err := normalizeQuotaGrantFilters(quotaGrantFilterRequest{
		TimePeriod:    "custom",
		TimeStartDate: "2026-07-20",
		TimeEndDate:   "2026-07-22",
		RechargeMode:  "recharged",
		UsageMode:     "used",
		UsageModel:    "gpt-4o-gizmo-abc123",
	}, now)
	require.NoError(t, err)
	location, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	startAt := time.Date(2026, 7, 20, 0, 0, 0, 0, location).Unix()
	endAt := time.Date(2026, 7, 23, 0, 0, 0, 0, location).Unix()
	assert.Equal(t, startAt, filters.RechargeStartAt)
	assert.Equal(t, endAt, filters.RechargeEndAt)
	assert.Equal(t, startAt, filters.UsageStartAt)
	assert.Equal(t, endAt, filters.UsageEndAt)
	assert.Equal(t, []string{"gpt-4o-gizmo-*"}, filters.UsageModels)
	assert.Contains(t, filterJson, `"time_period":"custom"`)
	assert.Contains(t, filterJson, `"usage_model":"gpt-4o-gizmo-abc123"`)
	assert.Equal(t, "已启用；普通用户；行为时间：2026-07-20 至 2026-07-22（北京时间）；范围内有充值；范围内使用过任一模型：gpt-4o-gizmo-*", summary)
}

func TestNormalizeQuotaGrantFiltersAppliesExactDateTimeRange(t *testing.T) {
	location, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	start := time.Date(2026, 7, 23, 0, 0, 0, 0, location).Unix()
	end := time.Date(2026, 7, 23, 12, 45, 0, 0, location).Unix()

	filters, filterJson, summary, err := normalizeQuotaGrantFilters(quotaGrantFilterRequest{
		TimeStartAt:  start,
		TimeEndAt:    end,
		RechargeMode: "recharged",
		UsageMode:    "used",
		UsageModels:  []string{"gpt-4o", "gpt-4"},
	}, time.Now())
	require.NoError(t, err)
	assert.Equal(t, start, filters.RechargeStartAt)
	assert.Equal(t, end+1, filters.RechargeEndAt)
	assert.Equal(t, start, filters.UsageStartAt)
	assert.Equal(t, end+1, filters.UsageEndAt)
	assert.Equal(t, []string{"gpt-4o", "gpt-4"}, filters.UsageModels)
	assert.Contains(t, filterJson, `"usage_models":["gpt-4o","gpt-4"]`)
	assert.Contains(t, summary, "行为时间：2026-07-23 00:00 至 2026-07-23 12:45（北京时间）")
	assert.Contains(t, summary, "范围内使用过任一模型：gpt-4o、gpt-4")
}

func TestNormalizeQuotaGrantFiltersRejectsTooManyRawUsageModels(t *testing.T) {
	models := make([]string, maxQuotaGrantUsageModels+1)
	for index := range models {
		models[index] = "duplicate-model"
	}

	_, _, _, err := normalizeQuotaGrantFilters(quotaGrantFilterRequest{
		TimeStartAt: time.Now().Add(-time.Hour).Unix(),
		TimeEndAt:   time.Now().Unix(),
		UsageMode:   "used",
		UsageModels: models,
	}, time.Now())
	assert.ErrorContains(t, err, "使用模型最多选择 100 个")
}

func TestNormalizeQuotaGrantFiltersRejectsOverlongRawUsageModels(t *testing.T) {
	now := time.Now()
	baseRequest := quotaGrantFilterRequest{
		TimeStartAt: now.Add(-time.Hour).Unix(),
		TimeEndAt:   now.Unix(),
		UsageMode:   "used",
	}

	request := baseRequest
	request.UsageModels = []string{strings.Repeat(" ", 101) + "gpt-4o"}
	_, _, _, err := normalizeQuotaGrantFilters(request, now)
	assert.ErrorContains(t, err, "单个使用模型名称不能超过 100 个字符")

	request = baseRequest
	request.UsageModel = strings.Repeat(" ", 101)
	_, _, _, err = normalizeQuotaGrantFilters(request, now)
	assert.ErrorContains(t, err, "单个使用模型名称不能超过 100 个字符")
}

func TestSearchQuotaGrantTargetsReadsModelArrayFromRequestBody(t *testing.T) {
	db := setupManageUserTestDB(t)
	now := time.Now().Unix()
	users := []*model.User{
		{Username: "quota-search-model-a", Password: "password123", AffCode: "quota-search-model-a", Role: common.RoleCommonUser, Status: common.UserStatusEnabled},
		{Username: "quota-search-model-b", Password: "password123", AffCode: "quota-search-model-b", Role: common.RoleCommonUser, Status: common.UserStatusEnabled},
		{Username: "quota-search-model-c", Password: "password123", AffCode: "quota-search-model-c", Role: common.RoleCommonUser, Status: common.UserStatusEnabled},
	}
	require.NoError(t, db.Create(users).Error)
	require.NoError(t, db.Create([]*model.Log{
		{UserId: users[0].Id, Username: users[0].Username, CreatedAt: now - 60, Type: model.LogTypeConsume, ModelName: "model-a", Quota: 10},
		{UserId: users[1].Id, Username: users[1].Username, CreatedAt: now - 60, Type: model.LogTypeConsume, ModelName: "model-b", Quota: 20},
		{UserId: users[2].Id, Username: users[2].Username, CreatedAt: now - 60, Type: model.LogTypeConsume, ModelName: "model-c", Quota: 30},
	}).Error)

	body, err := common.Marshal(quotaGrantTargetSearchRequest{
		Filters: quotaGrantFilterRequest{
			Roles:       []int{common.RoleCommonUser},
			Statuses:    []int{common.UserStatusEnabled},
			TimeStartAt: now - 120,
			TimeEndAt:   now,
			UsageMode:   "used",
			UsageModels: []string{"model-a", "model-b"},
		},
		Page:     1,
		PageSize: 20,
	})
	require.NoError(t, err)

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPost, "/api/user/quota-grants/targets/search", strings.NewReader(string(body)))
	context.Request.Header.Set("Content-Type", "application/json")
	SearchQuotaGrantTargets(context)
	assert.Contains(t, recorder.Body.String(), `"last_used_at":`)
	assert.Contains(t, recorder.Body.String(), `"last_used_at_in_scope":`)
	assert.Contains(t, recorder.Body.String(), `"used_quota_7d":`)
	assert.Contains(t, recorder.Body.String(), `"used_quota_in_scope":`)

	var response struct {
		Success bool `json:"success"`
		Data    struct {
			Total int                       `json:"total"`
			Items []*model.QuotaGrantTarget `json:"items"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.True(t, response.Success)
	assert.Equal(t, 2, response.Data.Total)
	require.Len(t, response.Data.Items, 2)
	assert.Equal(t, users[1].Id, response.Data.Items[0].Id)
	assert.Equal(t, users[0].Id, response.Data.Items[1].Id)
}

func TestNormalizeQuotaGrantFiltersCapsPersistedSummaryLength(t *testing.T) {
	originalQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 100
	t.Cleanup(func() { common.QuotaPerUnit = originalQuotaPerUnit })

	end := time.Now().Unix()
	filters, _, summary, err := normalizeQuotaGrantFilters(quotaGrantFilterRequest{
		Keyword:       strings.Repeat("关", 100),
		BalanceMode:   "between",
		BalanceAmount: "1000000.00",
		BalanceMax:    "2000000.00",
		TimeStartAt:   end - 3600,
		TimeEndAt:     end,
		RechargeMode:  "recharged",
		UsageMode:     "used",
		UsageModels: []string{
			strings.Repeat("甲", 100),
			strings.Repeat("乙", 100),
			strings.Repeat("丙", 100),
		},
	}, time.Now())
	require.NoError(t, err)
	assert.Len(t, filters.UsageModels, 3)
	assert.Equal(t, maxQuotaGrantFilterSummaryLength, utf8.RuneCountInString(summary))
	assert.True(t, strings.HasSuffix(summary, "…"))
}

func TestParseQuotaGrantTimestampRejectsInvalidValues(t *testing.T) {
	value, err := parseQuotaGrantTimestamp("", "行为时间开始")
	require.NoError(t, err)
	assert.Zero(t, value)

	_, err = parseQuotaGrantTimestamp("not-a-timestamp", "行为时间开始")
	assert.ErrorContains(t, err, "行为时间开始时间戳格式不正确")
	_, err = parseQuotaGrantTimestamp("0", "行为时间结束")
	assert.ErrorContains(t, err, "行为时间结束时间戳格式不正确")
}

func TestNormalizeQuotaGrantFiltersBuildsBeijingNaturalDayRange(t *testing.T) {
	now := time.Date(2026, 7, 23, 1, 0, 0, 0, time.UTC)
	filters, _, summary, err := normalizeQuotaGrantFilters(quotaGrantFilterRequest{
		TimePeriod: "7d",
		UsageMode:  "unused",
	}, now)
	require.NoError(t, err)
	location, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 7, 17, 0, 0, 0, 0, location).Unix(), filters.UsageStartAt)
	assert.Equal(t, time.Date(2026, 7, 24, 0, 0, 0, 0, location).Unix(), filters.UsageEndAt)
	assert.Contains(t, summary, "近7个自然日")
}

func TestNormalizeQuotaGrantFiltersRejectsInvalidSharedBehaviorFilters(t *testing.T) {
	_, _, _, err := normalizeQuotaGrantFilters(quotaGrantFilterRequest{
		TimePeriod:    "custom",
		TimeStartDate: "2026-07-22",
		TimeEndDate:   "2026-07-20",
		UsageMode:     "used",
	}, time.Now())
	assert.ErrorContains(t, err, "结束日期不能早于开始日期")

	_, _, _, err = normalizeQuotaGrantFilters(quotaGrantFilterRequest{
		TimePeriod: "7d",
		UsageModel: "gpt-4o",
	}, time.Now())
	assert.ErrorContains(t, err, "请先选择使用情况")
}

func TestNormalizeQuotaGrantFiltersUsesBeijingYesterdayWindow(t *testing.T) {
	now := time.Date(2026, 7, 21, 20, 0, 0, 0, time.UTC)
	filters, _, summary, err := normalizeQuotaGrantFilters(quotaGrantFilterRequest{
		UsageMode:   "unused",
		UsagePeriod: "yesterday",
	}, now)
	require.NoError(t, err)
	location, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 7, 21, 0, 0, 0, 0, location).Unix(), filters.UsageStartAt)
	assert.Equal(t, time.Date(2026, 7, 22, 0, 0, 0, 0, location).Unix(), filters.UsageEndAt)
	assert.Contains(t, summary, "昨日无模型消耗")
}

func TestGrantUserQuotaBatchRecordsStructuredFailureAudit(t *testing.T) {
	db := setupManageUserTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.QuotaGrantBatch{}))
	target := &model.User{
		Username: "quota-audit-target", Password: "password123", AffCode: "quota-audit-target",
		Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Quota: common.MaxQuota,
	}
	require.NoError(t, db.Create(target).Error)

	requestId := uuid.NewString()
	body := fmt.Sprintf(`{
		"request_id":%q,
		"user_ids":[%d],
		"amount_usd":"1.00",
		"reason":"失败审计测试",
		"filters":{"roles":[1],"statuses":[1],"balance_mode":"any","usage_mode":"any"}
	}`, requestId, target.Id)
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPost, "/api/user/quota-grants", strings.NewReader(body))
	context.Request.Header.Set("Content-Type", "application/json")
	context.Set("id", 9999)
	context.Set("role", common.RoleRootUser)
	context.Set("username", "root-operator")

	GrantUserQuotaBatch(context)
	assert.Contains(t, recorder.Body.String(), `"success":false`)

	var auditLog model.Log
	require.NoError(t, db.Where("user_id = ? AND type = ?", 9999, model.LogTypeManage).First(&auditLog).Error)
	other, err := common.StrToMap(auditLog.Other)
	require.NoError(t, err)
	op, ok := other["op"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "user.quota_grant_batch", op["action"])
	params, ok := op["params"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, requestId, params["request_id"])
	assert.Equal(t, "1.00", params["amount_usd"])
	assert.Equal(t, "failed", params["result"])
	assert.EqualValues(t, 1, params["count"])
	assert.Contains(t, params["filters"], "已启用")
	assert.Contains(t, params["filter_json"], `"statuses":[1]`)
	assert.NotEmpty(t, params["error"])
}

func TestParseQuotaGrantFilter(t *testing.T) {
	allowed := map[int]struct{}{1: {}, 10: {}}

	values, err := parseQuotaGrantFilter("10,1,10", []int{1}, allowed, "用户角色")
	require.NoError(t, err)
	assert.Equal(t, []int{10, 1}, values)

	values, err = parseQuotaGrantFilter("", []int{1}, allowed, "用户角色")
	require.NoError(t, err)
	assert.Equal(t, []int{1}, values)

	_, err = parseQuotaGrantFilter("100", []int{1}, allowed, "用户角色")
	assert.ErrorContains(t, err, "不支持")
}
