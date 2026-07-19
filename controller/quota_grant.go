package controller

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

const maxQuotaGrantReasonLength = 200

type quotaGrantRequest struct {
	RequestId string `json:"request_id"`
	UserIds   []int  `json:"user_ids"`
	AmountUsd string `json:"amount_usd"`
	Reason    string `json:"reason"`
}

func ListQuotaGrantTargets(c *gin.Context) {
	roles, statuses, err := quotaGrantFilters(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo := common.GetPageQuery(c)
	users, total, err := model.ListQuotaGrantTargets(
		c.Query("keyword"),
		roles,
		statuses,
		pageInfo.GetStartIdx(),
		pageInfo.GetPageSize(),
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
	roles, statuses, err := quotaGrantFilters(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	ids, err := model.ListQuotaGrantTargetIds(c.Query("keyword"), roles, statuses)
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

	result, err := model.GrantUserQuotaBatch(model.QuotaGrantBatchParams{
		RequestId:      request.RequestId,
		OperatorUserId: c.GetInt("id"),
		OperatorName:   c.GetString("username"),
		TargetUserIds:  request.UserIds,
		Quota:          quota,
		AmountUsd:      amountUsd,
		Reason:         reason,
		Ip:             c.ClientIP(),
		AdminInfo:      auditOperatorInfo(c),
	})
	if err != nil {
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

func quotaGrantFilters(c *gin.Context) ([]int, []int, error) {
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
		return nil, nil, err
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
	return roles, statuses, err
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
