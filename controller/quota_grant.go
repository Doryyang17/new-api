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

const maxQuotaGrantUsageModels = 100

const maxQuotaGrantFilterSummaryLength = 500

type quotaGrantRequest struct {
	RequestId string                   `json:"request_id"`
	UserIds   []int                    `json:"user_ids"`
	AmountUsd string                   `json:"amount_usd"`
	Reason    string                   `json:"reason"`
	Filters   *quotaGrantFilterRequest `json:"filters"`
}

type quotaGrantFilterRequest struct {
	Keyword       string   `json:"keyword"`
	Roles         []int    `json:"roles"`
	Statuses      []int    `json:"statuses"`
	BalanceMode   string   `json:"balance_mode"`
	BalanceAmount string   `json:"balance_amount"`
	BalanceMax    string   `json:"balance_max"`
	TimePeriod    string   `json:"time_period"`
	TimeStartDate string   `json:"time_start_date"`
	TimeEndDate   string   `json:"time_end_date"`
	TimeStartAt   int64    `json:"time_start_at"`
	TimeEndAt     int64    `json:"time_end_at"`
	RechargeMode  string   `json:"recharge_mode"`
	RechargeDate  string   `json:"recharge_date"`
	UsageMode     string   `json:"usage_mode"`
	UsagePeriod   string   `json:"usage_period"`
	UsageModel    string   `json:"usage_model"`
	UsageModels   []string `json:"usage_models"`
}

type quotaGrantTimeRange struct {
	StartAt int64
	EndAt   int64
	Summary string
	Active  bool
}

type quotaGrantTargetSearchRequest struct {
	Filters  quotaGrantFilterRequest `json:"filters"`
	Page     int                     `json:"page"`
	PageSize int                     `json:"page_size"`
}

func ListQuotaGrantTargets(c *gin.Context) {
	now := time.Now()
	filters, _, _, err := quotaGrantFiltersFromQuery(c, now)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	listQuotaGrantTargets(c, filters, common.GetPageQuery(c), now)
}

func SearchQuotaGrantTargets(c *gin.Context) {
	var request quotaGrantTargetSearchRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorMsg(c, "请求参数格式不正确")
		return
	}
	now := time.Now()
	filters, _, _, err := normalizeQuotaGrantFilters(request.Filters, now)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if request.Page < 1 {
		request.Page = 1
	}
	if request.PageSize <= 0 {
		request.PageSize = common.ItemsPerPage
	} else if request.PageSize > 100 {
		request.PageSize = 100
	}
	listQuotaGrantTargets(c, filters, &common.PageInfo{
		Page:     request.Page,
		PageSize: request.PageSize,
	}, now)
}

func listQuotaGrantTargets(c *gin.Context, filters model.QuotaGrantTargetFilters, pageInfo *common.PageInfo, now time.Time) {
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
	listQuotaGrantTargetIds(c, filters)
}

func SearchQuotaGrantTargetIds(c *gin.Context) {
	var request quotaGrantTargetSearchRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorMsg(c, "请求参数格式不正确")
		return
	}
	filters, _, _, err := normalizeQuotaGrantFilters(request.Filters, time.Now())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	listQuotaGrantTargetIds(c, filters)
}

func listQuotaGrantTargetIds(c *gin.Context, filters model.QuotaGrantTargetFilters) {
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
	timeStartAt, err := parseQuotaGrantTimestamp(c.Query("time_start_at"), "行为时间开始")
	if err != nil {
		return model.QuotaGrantTargetFilters{}, "", "", err
	}
	timeEndAt, err := parseQuotaGrantTimestamp(c.Query("time_end_at"), "行为时间结束")
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
		TimePeriod:    c.Query("time_period"),
		TimeStartDate: c.Query("time_start_date"),
		TimeEndDate:   c.Query("time_end_date"),
		TimeStartAt:   timeStartAt,
		TimeEndAt:     timeEndAt,
		RechargeMode:  c.Query("recharge_mode"),
		RechargeDate:  c.Query("recharge_date"),
		UsageMode:     c.Query("usage_mode"),
		UsagePeriod:   c.Query("usage_period"),
		UsageModel:    c.Query("usage_model"),
		UsageModels:   splitQuotaGrantUsageModels(c.Query("usage_models")),
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

	timeRange, err := normalizeQuotaGrantTimeRange(request, now)
	if err != nil {
		return model.QuotaGrantTargetFilters{}, "", "", err
	}

	rechargeSummary, err := applyQuotaGrantRechargeFilter(&filters, request, now, timeRange)
	if err != nil {
		return model.QuotaGrantTargetFilters{}, "", "", err
	}

	usageSummary, err := applyQuotaGrantUsageFilter(&filters, request, now, timeRange)
	if err != nil {
		return model.QuotaGrantTargetFilters{}, "", "", err
	}
	rechargeMode := strings.TrimSpace(request.RechargeMode)
	usageMode := strings.TrimSpace(request.UsageMode)
	usesSharedTimeRange := timeRange.Active && ((rechargeMode == "recharged" || rechargeMode == "unrecharged") ||
		(usageMode == "used" || usageMode == "unused"))
	if usesSharedTimeRange {
		summary = append(summary, timeRange.Summary)
	}
	if rechargeSummary != "" {
		summary = append(summary, rechargeSummary)
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
	return filters, string(filterBytes), truncateQuotaGrantFilterSummary(strings.Join(summary, "；")), nil
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

func normalizeQuotaGrantTimeRange(request quotaGrantFilterRequest, now time.Time) (quotaGrantTimeRange, error) {
	if request.TimeStartAt != 0 || request.TimeEndAt != 0 {
		if request.TimeStartAt <= 0 || request.TimeEndAt <= 0 {
			return quotaGrantTimeRange{}, errors.New("请选择完整的行为时间范围")
		}
		if request.TimeEndAt < request.TimeStartAt {
			return quotaGrantTimeRange{}, errors.New("行为时间范围结束时间不能早于开始时间")
		}
		if request.TimeEndAt == 1<<63-1 {
			return quotaGrantTimeRange{}, errors.New("行为时间范围超出系统支持范围")
		}
		location, err := time.LoadLocation("Asia/Shanghai")
		if err != nil {
			return quotaGrantTimeRange{}, err
		}
		start := time.Unix(request.TimeStartAt, 0).In(location)
		end := time.Unix(request.TimeEndAt, 0).In(location)
		return quotaGrantTimeRange{
			StartAt: request.TimeStartAt,
			// The usage-log picker treats its end timestamp as inclusive. Keep
			// the same contract while the model query remains half-open.
			EndAt:   request.TimeEndAt + 1,
			Summary: fmt.Sprintf("行为时间：%s 至 %s（北京时间）", start.Format("2006-01-02 15:04"), end.Format("2006-01-02 15:04")),
			Active:  true,
		}, nil
	}
	period := strings.TrimSpace(request.TimePeriod)
	if period == "" {
		return quotaGrantTimeRange{}, nil
	}

	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return quotaGrantTimeRange{}, err
	}
	localNow := now.In(location)
	today := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, location)
	tomorrow := today.AddDate(0, 0, 1)
	rangeValue := quotaGrantTimeRange{EndAt: tomorrow.Unix(), Active: true}
	switch period {
	case "today":
		rangeValue.StartAt = today.Unix()
		rangeValue.Summary = "行为时间：今日（北京时间）"
	case "yesterday":
		rangeValue.StartAt = today.AddDate(0, 0, -1).Unix()
		rangeValue.EndAt = today.Unix()
		rangeValue.Summary = "行为时间：昨日（北京时间）"
	case "3d", "7d", "30d":
		days, parseErr := strconv.Atoi(strings.TrimSuffix(period, "d"))
		if parseErr != nil {
			return quotaGrantTimeRange{}, errors.New("不支持的行为时间范围")
		}
		rangeValue.StartAt = today.AddDate(0, 0, -(days - 1)).Unix()
		rangeValue.Summary = fmt.Sprintf("行为时间：近%d个自然日（北京时间）", days)
	case "custom":
		startDate := strings.TrimSpace(request.TimeStartDate)
		endDate := strings.TrimSpace(request.TimeEndDate)
		start, startErr := time.ParseInLocation("2006-01-02", startDate, location)
		end, endErr := time.ParseInLocation("2006-01-02", endDate, location)
		if startErr != nil || endErr != nil || start.Format("2006-01-02") != startDate || end.Format("2006-01-02") != endDate {
			return quotaGrantTimeRange{}, errors.New("自定义行为时间范围格式不正确")
		}
		if end.Before(start) {
			return quotaGrantTimeRange{}, errors.New("行为时间范围结束日期不能早于开始日期")
		}
		rangeValue.StartAt = start.Unix()
		rangeValue.EndAt = end.AddDate(0, 0, 1).Unix()
		rangeValue.Summary = fmt.Sprintf("行为时间：%s 至 %s（北京时间）", startDate, endDate)
	default:
		return quotaGrantTimeRange{}, errors.New("不支持的行为时间范围")
	}
	return rangeValue, nil
}

func applyQuotaGrantRechargeFilter(filters *model.QuotaGrantTargetFilters, request quotaGrantFilterRequest, now time.Time, timeRange quotaGrantTimeRange) (string, error) {
	mode := strings.TrimSpace(request.RechargeMode)
	if mode == "" || mode == "any" {
		return "", nil
	}
	if mode != "recharged" && mode != "unrecharged" && mode != "yesterday" && mode != "date" {
		return "", errors.New("不支持的充值情况筛选")
	}
	filters.RechargeMode = mode
	if mode == "recharged" {
		if timeRange.Active {
			filters.RechargeStartAt, filters.RechargeEndAt = timeRange.StartAt, timeRange.EndAt
			return "范围内有充值", nil
		}
		return "已充值", nil
	}
	if mode == "unrecharged" {
		if timeRange.Active {
			filters.RechargeStartAt, filters.RechargeEndAt = timeRange.StartAt, timeRange.EndAt
			return "范围内无充值", nil
		}
		return "未充值", nil
	}
	startAt, endAt, dateLabel, err := quotaGrantRechargeDateWindow(mode, request.RechargeDate, now)
	if err != nil {
		return "", err
	}
	filters.RechargeStartAt = startAt
	filters.RechargeEndAt = endAt
	if mode == "yesterday" {
		return "昨日充值", nil
	}
	return dateLabel + " 充值", nil
}

func quotaGrantRechargeDateWindow(mode string, rawDate string, now time.Time) (int64, int64, string, error) {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return 0, 0, "", err
	}
	localNow := now.In(location)
	if mode == "yesterday" {
		today := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, location)
		start := today.AddDate(0, 0, -1)
		return start.Unix(), today.Unix(), start.Format("2006-01-02"), nil
	}

	dateValue := strings.TrimSpace(rawDate)
	parsed, err := time.ParseInLocation("2006-01-02", dateValue, location)
	if err != nil || parsed.Format("2006-01-02") != dateValue {
		return 0, 0, "", errors.New("指定充值日期格式不正确")
	}
	return parsed.Unix(), parsed.AddDate(0, 0, 1).Unix(), dateValue, nil
}

func applyQuotaGrantUsageFilter(filters *model.QuotaGrantTargetFilters, request quotaGrantFilterRequest, now time.Time, timeRange quotaGrantTimeRange) (string, error) {
	mode := strings.TrimSpace(request.UsageMode)
	usageModels, err := normalizeQuotaGrantUsageModels(request)
	if err != nil {
		return "", err
	}
	if mode == "" || mode == "any" {
		if len(usageModels) > 0 {
			return "", errors.New("按模型筛选前请先选择使用情况")
		}
		return "", nil
	}
	if mode != "used" && mode != "unused" {
		return "", errors.New("不支持的使用情况筛选")
	}
	var startAt, endAt int64
	var label string
	if timeRange.Active {
		startAt, endAt, label = timeRange.StartAt, timeRange.EndAt, "范围内"
	} else if period := strings.TrimSpace(request.UsagePeriod); period == "yesterday" {
		location, err := time.LoadLocation("Asia/Shanghai")
		if err != nil {
			return "", err
		}
		localNow := now.In(location)
		today := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, location)
		startAt, endAt, label = today.AddDate(0, 0, -1).Unix(), today.Unix(), "昨日"
	} else {
		period := strings.TrimSpace(request.UsagePeriod)
		days, err := strconv.Atoi(strings.TrimSuffix(period, "d"))
		if err != nil || (days != 3 && days != 7 && days != 30) {
			return "", errors.New("不支持的使用时间范围")
		}
		startAt, label = now.Add(-time.Duration(days)*24*time.Hour).Unix(), fmt.Sprintf("近%d天", days)
	}
	filters.UsageMode, filters.UsageStartAt, filters.UsageEndAt = mode, startAt, endAt
	filters.UsageModels = usageModels
	if len(filters.UsageModels) > 0 {
		modelLabel := quotaGrantUsageModelsLabel(filters.UsageModels)
		if mode == "unused" {
			return label + "未使用任一模型：" + modelLabel, nil
		}
		return label + "使用过任一模型：" + modelLabel, nil
	}
	if mode == "unused" {
		return label + "无模型消耗", nil
	}
	return label + "有模型消耗", nil
}

func normalizeQuotaGrantUsageModels(request quotaGrantFilterRequest) ([]string, error) {
	rawModelCount := len(request.UsageModels)
	if utf8.RuneCountInString(request.UsageModel) > 100 {
		return nil, errors.New("单个使用模型名称不能超过 100 个字符")
	}
	legacyModel := strings.TrimSpace(request.UsageModel)
	if legacyModel != "" {
		rawModelCount++
	}
	if rawModelCount > maxQuotaGrantUsageModels {
		return nil, fmt.Errorf("使用模型最多选择 %d 个", maxQuotaGrantUsageModels)
	}

	rawModels := make([]string, 0, rawModelCount)
	rawModels = append(rawModels, request.UsageModels...)
	if legacyModel != "" {
		rawModels = append(rawModels, legacyModel)
	}
	models := make([]string, 0, len(rawModels))
	seen := make(map[string]struct{}, len(rawModels))
	for _, rawModel := range rawModels {
		if utf8.RuneCountInString(rawModel) > 100 {
			return nil, errors.New("单个使用模型名称不能超过 100 个字符")
		}
		modelName := strings.TrimSpace(rawModel)
		if modelName == "" {
			continue
		}
		modelName = model.NormalizeUsageModelName(modelName)
		if _, exists := seen[modelName]; exists {
			continue
		}
		seen[modelName] = struct{}{}
		models = append(models, modelName)
	}
	return models, nil
}

func truncateQuotaGrantFilterSummary(summary string) string {
	if utf8.RuneCountInString(summary) <= maxQuotaGrantFilterSummaryLength {
		return summary
	}
	runes := []rune(summary)
	return string(runes[:maxQuotaGrantFilterSummaryLength-1]) + "…"
}

func quotaGrantUsageModelsLabel(models []string) string {
	visibleModels := models
	if len(visibleModels) > 3 {
		visibleModels = visibleModels[:3]
	}
	label := strings.Join(visibleModels, "、")
	if len(models) <= len(visibleModels) {
		return label
	}
	return fmt.Sprintf("%s 等 %d 个模型", label, len(models))
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

func parseQuotaGrantTimestamp(raw string, label string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s时间戳格式不正确", label)
	}
	return value, nil
}

func splitQuotaGrantUsageModels(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	return strings.Split(raw, ",")
}
