package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setTopUpQuotaPerUnitForTest(t *testing.T, quotaPerUnit float64) {
	t.Helper()
	originalQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = quotaPerUnit
	t.Cleanup(func() {
		common.QuotaPerUnit = originalQuotaPerUnit
	})
}

func getTopUpForQuotaTest(t *testing.T, tradeNo string) *TopUp {
	t.Helper()
	topUp := GetTopUpByTradeNo(tradeNo)
	require.NotNil(t, topUp)
	return topUp
}

func TestGetUserTopUpQuotaUsesFrozenCreditedQuota(t *testing.T) {
	truncateTables(t)
	setTopUpQuotaPerUnitForTest(t, 500_000)

	topUps := []*TopUp{
		{UserId: 101, TradeNo: "frozen-1", Status: common.TopUpStatusSuccess, CreditedQuota: 1_000_000},
		{UserId: 101, TradeNo: "frozen-2", Status: common.TopUpStatusSuccess, CreditedQuota: 750_000},
		{UserId: 101, TradeNo: "pending", Status: common.TopUpStatusPending, CreditedQuota: 5_000_000},
		{UserId: 202, TradeNo: "other-user", Status: common.TopUpStatusSuccess, CreditedQuota: 9_000_000},
	}
	require.NoError(t, DB.Create(&topUps).Error)

	totalQuota, err := GetUserTopUpQuota(101)
	require.NoError(t, err)
	assert.EqualValues(t, 1_750_000, totalQuota)

	common.QuotaPerUnit = 1_000_000
	totalQuota, err = GetUserTopUpQuota(101)
	require.NoError(t, err)
	assert.EqualValues(t, 1_750_000, totalQuota)
}

func TestPrepareTopUpCreditedQuotaRejectsOverflowBeforePayment(t *testing.T) {
	setTopUpQuotaPerUnitForTest(t, 500_000)

	testCases := []struct {
		name  string
		topUp *TopUp
	}{
		{
			name: "epay",
			topUp: &TopUp{
				Amount:          int64(common.MaxQuota/500_000 + 1),
				PaymentMethod:   "alipay",
				PaymentProvider: PaymentProviderEpay,
			},
		},
		{
			name: "stripe",
			topUp: &TopUp{
				Money:           float64(common.MaxQuota/500_000 + 1),
				PaymentMethod:   PaymentMethodStripe,
				PaymentProvider: PaymentProviderStripe,
			},
		},
		{
			name: "creem",
			topUp: &TopUp{
				Amount:          int64(common.MaxQuota) + 1,
				PaymentMethod:   PaymentMethodCreem,
				PaymentProvider: PaymentProviderCreem,
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			err := PrepareTopUpCreditedQuota(testCase.topUp)
			require.Error(t, err)
			var clamp *common.QuotaClamp
			require.ErrorAs(t, err, &clamp)
			assert.Zero(t, testCase.topUp.CreditedQuota)
		})
	}

	safeTopUp := &TopUp{
		Amount:          int64(common.MaxQuota / 500_000),
		PaymentMethod:   "alipay",
		PaymentProvider: PaymentProviderEpay,
	}
	require.NoError(t, PrepareTopUpCreditedQuota(safeTopUp))
	assert.EqualValues(t, 2_147_000_000, safeTopUp.CreditedQuota)
}

func TestPrepareTopUpCreditedQuotaPreservesDecimalTruncation(t *testing.T) {
	setTopUpQuotaPerUnitForTest(t, 500_000.72)

	topUp := &TopUp{
		Amount:          4_275,
		PaymentMethod:   "alipay",
		PaymentProvider: PaymentProviderEpay,
	}
	require.NoError(t, PrepareTopUpCreditedQuota(topUp))
	assert.EqualValues(t, 2_137_503_078, topUp.CreditedQuota)
}

func TestGetUserTopUpQuotaRepairsLateZeroSnapshot(t *testing.T) {
	truncateTables(t)
	setTopUpQuotaPerUnitForTest(t, 500_000)

	topUp := &TopUp{
		UserId: 101, Amount: 2, Money: 1.6, TradeNo: "late-legacy-settlement",
		PaymentMethod: "alipay", PaymentProvider: PaymentProviderEpay,
		Status: common.TopUpStatusSuccess,
	}
	require.NoError(t, topUp.Insert())

	totalQuota, err := GetUserTopUpQuota(101)
	require.NoError(t, err)
	assert.EqualValues(t, 1_000_000, totalQuota)
	assert.EqualValues(t, 1_000_000, getTopUpForQuotaTest(t, "late-legacy-settlement").CreditedQuota)
}

func TestBackfillTopUpCreditedQuotaHandlesLegacyProvidersAndFreezesValue(t *testing.T) {
	truncateTables(t)
	setTopUpQuotaPerUnitForTest(t, 500_000)

	topUps := []*TopUp{
		{
			UserId: 101, Amount: 250_000, Money: 9.9, TradeNo: "legacy-creem",
			PaymentMethod: PaymentMethodCreem, Status: common.TopUpStatusSuccess,
		},
		{
			UserId: 101, Amount: 99, Money: 1.5, TradeNo: "legacy-stripe",
			PaymentMethod: PaymentMethodStripe, Status: common.TopUpStatusSuccess,
		},
		{
			UserId: 101, Amount: 2, Money: 1.6, TradeNo: "legacy-epay",
			PaymentMethod: "alipay", Status: common.TopUpStatusSuccess,
		},
		{
			UserId: 101, Amount: 100_000, Money: 4.9, TradeNo: "modern-creem",
			PaymentMethod: PaymentMethodCreem, PaymentProvider: PaymentProviderCreem,
			Status: common.TopUpStatusSuccess,
		},
		{
			UserId: 101, Amount: 0, Money: 12.5, TradeNo: "subscription-record",
			PaymentMethod: PaymentMethodStripe, Status: common.TopUpStatusSuccess,
		},
		{
			UserId: 101, Amount: 4, Money: 3.2, TradeNo: "pending-order",
			PaymentMethod: "alipay", Status: common.TopUpStatusPending,
		},
	}
	require.NoError(t, DB.Create(&topUps).Error)
	require.NoError(t, BackfillTopUpCreditedQuota())

	assert.EqualValues(t, 250_000, getTopUpForQuotaTest(t, "legacy-creem").CreditedQuota)
	assert.EqualValues(t, 750_000, getTopUpForQuotaTest(t, "legacy-stripe").CreditedQuota)
	assert.EqualValues(t, 1_000_000, getTopUpForQuotaTest(t, "legacy-epay").CreditedQuota)
	assert.EqualValues(t, 100_000, getTopUpForQuotaTest(t, "modern-creem").CreditedQuota)
	assert.Zero(t, getTopUpForQuotaTest(t, "subscription-record").CreditedQuota)
	assert.Zero(t, getTopUpForQuotaTest(t, "pending-order").CreditedQuota)

	common.QuotaPerUnit = 1_000_000
	require.NoError(t, BackfillTopUpCreditedQuota())
	assert.EqualValues(t, 1_000_000, getTopUpForQuotaTest(t, "legacy-epay").CreditedQuota)

	totalQuota, err := GetUserTopUpQuota(101)
	require.NoError(t, err)
	assert.EqualValues(t, 2_100_000, totalQuota)
}

func TestCompleteEpayTopUpIsAtomicAndIdempotent(t *testing.T) {
	truncateTables(t)
	setTopUpQuotaPerUnitForTest(t, 500_000)

	user := &User{Id: 301, Username: "epay_quota_user", Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(user).Error)
	topUp := &TopUp{
		UserId: 301, Amount: 2, Money: 1.6, TradeNo: "epay-atomic",
		PaymentMethod: "wxpay", PaymentProvider: PaymentProviderEpay,
		Status: common.TopUpStatusPending,
	}
	require.NoError(t, topUp.Insert())

	completedTopUp, quotaToAdd, completed, err := CompleteEpayTopUp("epay-atomic", "alipay")
	require.NoError(t, err)
	require.True(t, completed)
	assert.Equal(t, 1_000_000, quotaToAdd)
	assert.Equal(t, "alipay", completedTopUp.PaymentMethod)
	assert.EqualValues(t, 1_000_000, completedTopUp.CreditedQuota)
	assert.Equal(t, common.TopUpStatusSuccess, completedTopUp.Status)
	assert.Equal(t, 1_000_000, getUserQuotaForPaymentGuardTest(t, 301))

	_, _, completed, err = CompleteEpayTopUp("epay-atomic", "alipay")
	require.NoError(t, err)
	assert.False(t, completed)
	assert.Equal(t, 1_000_000, getUserQuotaForPaymentGuardTest(t, 301))
}

func TestCompleteEpayTopUpUsesOrderCreationSnapshot(t *testing.T) {
	truncateTables(t)
	setTopUpQuotaPerUnitForTest(t, 500_000)

	user := &User{Id: 302, Username: "epay_snapshot_user", Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(user).Error)
	topUp := &TopUp{
		UserId: 302, Amount: 2, Money: 1.6, TradeNo: "epay-frozen-at-creation",
		PaymentMethod: "alipay", PaymentProvider: PaymentProviderEpay,
		Status: common.TopUpStatusPending,
	}
	require.NoError(t, PrepareTopUpCreditedQuota(topUp))
	require.NoError(t, topUp.Insert())

	common.QuotaPerUnit = 1_000_000
	completedTopUp, quotaToAdd, completed, err := CompleteEpayTopUp("epay-frozen-at-creation", "alipay")
	require.NoError(t, err)
	require.True(t, completed)
	assert.Equal(t, 1_000_000, quotaToAdd)
	assert.EqualValues(t, 1_000_000, completedTopUp.CreditedQuota)
	assert.Equal(t, 1_000_000, getUserQuotaForPaymentGuardTest(t, 302))
}

func TestCompleteEpayTopUpRollsBackWhenUserDoesNotExist(t *testing.T) {
	truncateTables(t)
	setTopUpQuotaPerUnitForTest(t, 500_000)

	topUp := &TopUp{
		UserId: 999, Amount: 2, Money: 1.6, TradeNo: "epay-missing-user",
		PaymentMethod: "alipay", PaymentProvider: PaymentProviderEpay,
		Status: common.TopUpStatusPending,
	}
	require.NoError(t, topUp.Insert())

	_, _, completed, err := CompleteEpayTopUp("epay-missing-user", "alipay")
	require.Error(t, err)
	assert.False(t, completed)

	reloaded := getTopUpForQuotaTest(t, "epay-missing-user")
	assert.Equal(t, common.TopUpStatusPending, reloaded.Status)
	assert.Zero(t, reloaded.CreditedQuota)
}

func TestSuccessfulTopUpPathsPersistCreditedQuota(t *testing.T) {
	testCases := []struct {
		name       string
		provider   string
		method     string
		amount     int64
		money      float64
		expected   int
		completeFn func(string) error
	}{
		{
			name: "stripe", provider: PaymentProviderStripe, method: PaymentMethodStripe,
			amount: 99, money: 1.5, expected: 750_000,
			completeFn: func(tradeNo string) error { return Recharge(tradeNo, "", "127.0.0.1") },
		},
		{
			name: "creem", provider: PaymentProviderCreem, method: PaymentMethodCreem,
			amount: 250_000, money: 9.9, expected: 250_000,
			completeFn: func(tradeNo string) error { return RechargeCreem(tradeNo, "", "", "127.0.0.1") },
		},
		{
			name: "waffo", provider: PaymentProviderWaffo, method: PaymentMethodWaffo,
			amount: 2, money: 1.6, expected: 1_000_000,
			completeFn: func(tradeNo string) error { return RechargeWaffo(tradeNo, "127.0.0.1") },
		},
		{
			name: "waffo pancake", provider: PaymentProviderWaffoPancake, method: PaymentMethodWaffoPancake,
			amount: 3, money: 2.4, expected: 1_500_000,
			completeFn: func(tradeNo string) error { return RechargeWaffoPancake(tradeNo) },
		},
	}

	for index, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			truncateTables(t)
			setTopUpQuotaPerUnitForTest(t, 500_000)

			userId := 400 + index
			user := &User{Id: userId, Username: "topup_path_" + testCase.name, Status: common.UserStatusEnabled}
			require.NoError(t, DB.Create(user).Error)
			tradeNo := "topup-path-" + testCase.name
			topUp := &TopUp{
				UserId: userId, Amount: testCase.amount, Money: testCase.money, TradeNo: tradeNo,
				PaymentMethod: testCase.method, PaymentProvider: testCase.provider,
				Status: common.TopUpStatusPending,
			}
			require.NoError(t, topUp.Insert())
			require.NoError(t, testCase.completeFn(tradeNo))

			reloaded := getTopUpForQuotaTest(t, tradeNo)
			assert.Equal(t, common.TopUpStatusSuccess, reloaded.Status)
			assert.EqualValues(t, testCase.expected, reloaded.CreditedQuota)
			assert.Equal(t, testCase.expected, getUserQuotaForPaymentGuardTest(t, userId))
		})
	}
}
