package controller

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

const maxQuotaGrantReasonLength = 200

type quotaGrantRequest struct {
	RequestId string                   `json:"request_id"`
	UserIds   []int                    `json:"user_ids"`
	AmountUsd string                   `json:"amount_usd"`
	Reason    string                   `json:"reason"`
	Filters   *quotaGrantFilterRequest `json:"filters"`
}

type quotaGrantFilterRequest struct {
	Keyword       string `json:"keyword"`
	Roles         []int  `json:"roles"`
	Statuses      []int  `json:"statuses"`
	BalanceMode   string `json:"balance_mode"`
	BalanceAmount string `json:"balance_amount"`
	BalanceMax    string `json:"balance_max"`
	RechargeMode  string `json:"recharge_mode"`
	UsageMode     string `json:"usage_mode"`
	UsagePeriod   string `json:"usage_period"`
}

func ListQuotaGrantTargets(c *gin.Context) {
	now := time.Now()
	filters, _, _, err := quotaGrantFiltersFromQuery(c, now)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo := common.GetPageQuery(c)
	users, total, err := model.ListQuotaGrantTargets(
		filters,
		pageInfo.GetStartIdx(),
		pageInfo.GetPageSize(),
		now.Add(-7*24*time.Hour).Unix(),
		now.Add(-30*24*time.Hour).Unix(),
	)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(users)
	common.ApiSuccess(c, pageInfo)
}

func ListQuotaGrantTargetIds(c *gin.Context) {
	filters, _, _, err := quotaGrantFiltersFromQuery(c, time.Now())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	ids, err := model.ListQuotaGrantTargetIds(filters)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if len(ids) > model.MaxQuotaGrantTargets {
		common.ApiErrorMsg(c, fmt.Sprintf("当前筛选结果超过 %d 人，请缩小范围后再选择", model.MaxQuotaGrantTargets))
		return
	}
	common.ApiSuccess(c, gin.H{"ids": ids})
}

func GrantUserQuotaBatch(c *gin.Context) {
	var request quotaGrantRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorMsg(c, "请求参数格式不正确")
		return
	}

	request.RequestId = strings.TrimSpace(request.RequestId)
	if _, err := uuid.Parse(request.RequestId); err != nil {
		common.ApiErrorMsg(c, "请求幂等键格式不正确")
		return
	}
	if len(request.UserIds) == 0 || len(request.UserIds) > model.MaxQuotaGrantTargets {
		common.ApiErrorMsg(c, fmt.Sprintf("请选择 1 到 %d 位用户", model.MaxQuotaGrantTargets))
		return
	}

	reason := strings.TrimSpace(request.Reason)
	if reason == "" {
		common.ApiErrorMsg(c, "请填写发放原因")
		return
	}
	if utf8.RuneCountInString(reason) > maxQuotaGrantReasonLength {
		common.ApiErrorMsg(c, fmt.Sprintf("发放原因不能超过 %d 个字符", maxQuotaGrantReasonLength))
		return
	}

	quota, amountUsd, err := quotaGrantAmountFromUSD(request.AmountUsd)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	filters := model.QuotaGrantTargetFilters{
		Roles:    []int{common.RoleCommonUser, common.RoleAdminUser},
		Statuses: []int{common.UserStatusEnabled, common.UserStatusDisabled},
	}
	filterJson, filterSummary := "{}", "手动选择（兼容旧版请求）"
	if request.Filters != nil {
		filters, filterJson, filterSummary, err = normalizeQuotaGrantFilters(*request.Filters, time.Now())
		if err != nil {
			common.ApiError(c, err)
			return
		}
	}

	result, err := model.GrantUserQuotaBatch(model.QuotaGrantBatchParams{
		RequestId:      request.RequestId,
		OperatorUserId: c.GetInt("id"),
		OperatorName:   c.GetString("username"),
		TargetUserIds:  request.UserIds,
		Quota:          quota,
		AmountUsd:      amountUsd,
		Reason:         reason,
		FilterJson:     filterJson,
		FilterSummary:  filterSummary,
		Filters:        filters,
		Ip:             c.ClientIP(),
		AdminInfo:      auditOperatorInfo(c),
	})
	if err != nil {
		uniqueTargetIds := make(map[int]struct{}, len(request.UserIds))
		for _, userId := range request.UserIds {
			if userId > 0 {
				uniqueTargetIds[userId] = struct{}{}
			}
		}
		recordManageAudit(c, "user.quota_grant_batch", map[string]interface{}{
			"request_id":  request.RequestId,
			"amount_usd":  amountUsd,
			"count":       len(uniqueTargetIds),
			"reason":      reason,
			"filters":     filterSummary,
			"filter_json": filterJson,
			"result":      "failed",
			"error":       err.Error(),
		})
		common.ApiError(c, err)
		return
	}

	markAuditLogged(c)
	common.ApiSuccess(c, result)
}

func quotaGrantAmountFromUSD(raw string) (int, string, error) {
	raw = strings.TrimSpace(raw)
	amount, err := decimal.NewFromString(raw)
	if err != nil {
		return 0, "", errors.New("发放金额格式不正确")
	}
	if amount.Exponent() < -2 {
		return 0, "", errors.New("发放金额最多保留两位小数")
	}
	if amount.LessThanOrEqual(decimal.Zero) {
		return 0, "", errors.New("发放金额必须大于 $0")
	}
	if common.QuotaPerUnit <= 0 || math.IsNaN(common.QuotaPerUnit) || math.IsInf(common.QuotaPerUnit, 0) {
		return 0, "", errors.New("系统美元额度换算比例无效")
	}

	quota, clamp := common.QuotaFromDecimalChecked(
		amount.Mul(decimal.NewFromFloat(common.QuotaPerUnit)),
	)
	if clamp != nil || quota <= 0 {
		return 0, "", errors.New("发放金额超出系统额度范围")
	}
	return quota, amount.StringFixed(2), nil
}

func quotaGrantFiltersFromQuery(c *gin.Context, now time.Time) (model.QuotaGrantTargetFilters, string, string, error) {
	roles, err := parseQuotaGrantFilter(
		c.Query("roles"),
		[]int{common.RoleCommonUser},
		map[int]struct{}{
			common.RoleCommonUser: {},
			common.RoleAdminUser:  {},
		},
		"用户角色",
	)
	if err != nil {
		return model.QuotaGrantTargetFilters{}, "", "", err
	}
	statuses, err := parseQuotaGrantFilter(
		c.Query("statuses"),
		[]int{common.UserStatusEnabled},
		map[int]struct{}{
			common.UserStatusEnabled:  {},
			common.UserStatusDisabled: {},
		},
		"用户状态",
	)
	if err != nil {
		return model.QuotaGrantTargetFilters{}, "", "", err
	}
	return normalizeQuotaGrantFilters(quotaGrantFilterRequest{
		Keyword:       c.Query("keyword"),
		Roles:         roles,
		Statuses:      statuses,
		BalanceMode:   c.Query("balance_mode"),
		BalanceAmount: c.Query("balance_amount"),
		BalanceMax:    c.Query("balance_max"),
		RechargeMode:  c.Query("recharge_mode"),
		UsageMode:     c.Query("usage_mode"),
		UsagePeriod:   c.Query("usage_period"),
	}, now)
}

func normalizeQuotaGrantFilters(request quotaGrantFilterRequest, now time.Time) (model.QuotaGrantTargetFilters, string, string, error) {
	roles, err := validateQuotaGrantFilterValues(request.Roles, []int{common.RoleCommonUser}, map[int]struct{}{
		common.RoleCommonUser: {}, common.RoleAdminUser: {},
	}, "用户角色")
	if err != nil {
		return model.QuotaGrantTargetFilters{}, "", "", err
	}
	statuses, err := validateQuotaGrantFilterValues(request.Statuses, []int{common.UserStatusEnabled}, map[int]struct{}{
		common.UserStatusEnabled: {}, common.UserStatusDisabled: {},
	}, "用户状态")
	if err != nil {
		return model.QuotaGrantTargetFilters{}, "", "", err
	}

	request.Keyword = strings.TrimSpace(request.Keyword)
	if utf8.RuneCountInString(request.Keyword) > 100 {
		return model.QuotaGrantTargetFilters{}, "", "", errors.New("搜索关键词不能超过 100 个字符")
	}
	request.Roles = roles
	request.Statuses = statuses
	filters := model.QuotaGrantTargetFilters{Keyword: request.Keyword, Roles: roles, Statuses: statuses}
	summary := make([]string, 0, 6)
	if len(statuses) == 2 {
		summary = append(summary, "全部状态")
	} else if statuses[0] == common.UserStatusEnabled {
		summary = append(summary, "已启用")
	} else {
		summary = append(summary, "已禁用")
	}
	if len(roles) == 2 {
		summary = append(summary, "普通用户和管理员")
	} else if roles[0] == common.RoleAdminUser {
		summary = append(summary, "管理员")
	} else {
		summary = append(summary, "普通用户")
	}

	balanceSummary, err := applyQuotaGrantBalanceFilter(&filters, request)
	if err != nil {
		return model.QuotaGrantTargetFilters{}, "", "", err
	}
	if balanceSummary != "" {
		summary = append(summary, balanceSummary)
	}

	rechargeSummary, err := applyQuotaGrantRechargeFilter(&filters, request)
	if err != nil {
		return model.QuotaGrantTargetFilters{}, "", "", err
	}
	if rechargeSummary != "" {
		summary = append(summary, rechargeSummary)
	}

	usageSummary, err := applyQuotaGrantUsageFilter(&filters, request, now)
	if err != nil {
		return model.QuotaGrantTargetFilters{}, "", "", err
	}
	if usageSummary != "" {
		summary = append(summary, usageSummary)
	}
	if request.Keyword != "" {
		summary = append(summary, "关键词："+request.Keyword)
	}

	filterBytes, err := common.Marshal(request)
	if err != nil {
		return model.QuotaGrantTargetFilters{}, "", "", err
	}
	return filters, string(filterBytes), strings.Join(summary, "；"), nil
}

func applyQuotaGrantBalanceFilter(filters *model.QuotaGrantTargetFilters, request quotaGrantFilterRequest) (string, error) {
	mode := strings.TrimSpace(request.BalanceMode)
	if mode == "" || mode == "any" {
		return "", nil
	}
	zero := 0
	switch mode {
	case "low":
		limit, _, err := quotaGrantAmountFromUSD("10")
		if err != nil {
			return "", err
		}
		filters.MaxQuota = &limit
		return "余额 < $10", nil
	case "negative":
		filters.MaxQuota = &zero
		return "负余额", nil
	case "zero":
		filters.MinQuota, filters.MaxQuota = &zero, &zero
		filters.MinQuotaInclusive, filters.MaxQuotaInclusive = true, true
		return "零余额", nil
	case "positive":
		filters.MinQuota = &zero
		return "余额 > $0", nil
	}
	amount, amountUsd, err := quotaGrantBalanceFromUSD(request.BalanceAmount)
	if err != nil {
		return "", err
	}
	switch mode {
	case "lt":
		filters.MaxQuota = &amount
		return "余额 < $" + amountUsd, nil
	case "lte":
		filters.MaxQuota, filters.MaxQuotaInclusive = &amount, true
		return "余额 ≤ $" + amountUsd, nil
	case "eq":
		filters.MinQuota, filters.MaxQuota = &amount, &amount
		filters.MinQuotaInclusive, filters.MaxQuotaInclusive = true, true
		return "余额 = $" + amountUsd, nil
	case "gte":
		filters.MinQuota, filters.MinQuotaInclusive = &amount, true
		return "余额 ≥ $" + amountUsd, nil
	case "gt":
		filters.MinQuota = &amount
		return "余额 > $" + amountUsd, nil
	case "between":
		maximum, maximumUsd, maximumErr := quotaGrantBalanceFromUSD(request.BalanceMax)
		if maximumErr != nil {
			return "", maximumErr
		}
		if maximum < amount {
			return "", errors.New("余额区间上限不能小于下限")
		}
		filters.MinQuota, filters.MaxQuota = &amount, &maximum
		filters.MinQuotaInclusive, filters.MaxQuotaInclusive = true, true
		return "余额 $" + amountUsd + "–$" + maximumUsd, nil
	default:
		return "", errors.New("不支持的余额筛选条件")
	}
}

func applyQuotaGrantRechargeFilter(filters *model.QuotaGrantTargetFilters, request quotaGrantFilterRequest) (string, error) {
	mode := strings.TrimSpace(request.RechargeMode)
	if mode == "" || mode == "any" {
		return "", nil
	}
	if mode != "recharged" && mode != "unrecharged" {
		return "", errors.New("不支持的充值情况筛选")
	}
	filters.RechargeMode = mode
	if mode == "recharged" {
		return "已充值", nil
	}
	return "未充值", nil
}

func applyQuotaGrantUsageFilter(filters *model.QuotaGrantTargetFilters, request quotaGrantFilterRequest, now time.Time) (string, error) {
	mode := strings.TrimSpace(request.UsageMode)
	if mode == "" || mode == "any" {
		return "", nil
	}
	if mode != "used" && mode != "unused" {
		return "", errors.New("不支持的使用情况筛选")
	}
	period := strings.TrimSpace(request.UsagePeriod)
	var startAt, endAt int64
	var label string
	if period == "yesterday" {
		location, err := time.LoadLocation("Asia/Shanghai")
		if err != nil {
			return "", err
		}
		localNow := now.In(location)
		today := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, location)
		startAt, endAt, label = today.AddDate(0, 0, -1).Unix(), today.Unix(), "昨日"
	} else {
		days, err := strconv.Atoi(strings.TrimSuffix(period, "d"))
		if err != nil || (days != 3 && days != 7 && days != 30) {
			return "", errors.New("不支持的使用时间范围")
		}
		startAt, label = now.Add(-time.Duration(days)*24*time.Hour).Unix(), fmt.Sprintf("近%d天", days)
	}
	filters.UsageMode, filters.UsageStartAt, filters.UsageEndAt = mode, startAt, endAt
	if mode == "unused" {
		return label + "无模型消耗", nil
	}
	return label + "有模型消耗", nil
}

func quotaGrantBalanceFromUSD(raw string) (int, string, error) {
	raw = strings.TrimSpace(raw)
	amount, err := decimal.NewFromString(raw)
	if err != nil || amount.Exponent() < -2 || amount.IsNegative() {
		return 0, "", errors.New("余额金额格式不正确，最多保留两位小数且不能为负数")
	}
	if common.QuotaPerUnit <= 0 || math.IsNaN(common.QuotaPerUnit) || math.IsInf(common.QuotaPerUnit, 0) {
		return 0, "", errors.New("系统美元额度换算比例无效")
	}
	quota, clamp := common.QuotaFromDecimalChecked(amount.Mul(decimal.NewFromFloat(common.QuotaPerUnit)))
	if clamp != nil {
		return 0, "", errors.New("余额金额超出系统额度范围")
	}
	return quota, amount.StringFixed(2), nil
}

func validateQuotaGrantFilterValues(values []int, defaults []int, allowed map[int]struct{}, label string) ([]int, error) {
	if len(values) == 0 {
		return append([]int(nil), defaults...), nil
	}
	result := make([]int, 0, len(values))
	seen := make(map[int]struct{}, len(values))
	for _, value := range values {
		if _, ok := allowed[value]; !ok {
			return nil, fmt.Errorf("不支持的%s筛选值", label)
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result, nil
}

func parseQuotaGrantFilter(raw string, defaults []int, allowed map[int]struct{}, label string) ([]int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return append([]int(nil), defaults...), nil
	}

	values := make([]int, 0, len(allowed))
	seen := make(map[int]struct{}, len(allowed))
	for _, part := range strings.Split(raw, ",") {
		value, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			return nil, fmt.Errorf("%s筛选值格式不正确", label)
		}
		if _, ok := allowed[value]; !ok {
			return nil, fmt.Errorf("不支持的%s筛选值", label)
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}
	if len(values) == 0 {
		return nil, fmt.Errorf("请至少选择一个%s", label)
	}
	return values, nil
}
