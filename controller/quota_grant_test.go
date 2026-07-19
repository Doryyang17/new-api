package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/common"

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
