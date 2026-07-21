package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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
		UsageMode:     "used",
		UsagePeriod:   "7d",
	}, now)
	require.NoError(t, err)
	require.NotNil(t, filters.MaxQuota)
	assert.Equal(t, 1000, *filters.MaxQuota)
	assert.False(t, filters.MaxQuotaInclusive)
	assert.Equal(t, "used", filters.UsageMode)
	assert.Equal(t, now.Add(-7*24*time.Hour).Unix(), filters.UsageStartAt)
	assert.Contains(t, filterJson, `"balance_mode":"lt"`)
	assert.Equal(t, "已启用；普通用户；余额 < $10.00；近7天有模型消耗", summary)
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
