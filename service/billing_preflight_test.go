package service

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidatePreConsumeBillingDoesNotMutateWalletOrToken(t *testing.T) {
	truncate(t)
	seedUser(t, 601, 100)
	seedToken(t, 601, 601, "preflight-wallet", 80)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("token_quota", 80)
	info := &relaycommon.RelayInfo{
		UserId:      601,
		TokenId:     601,
		TokenKey:    "preflight-wallet",
		UserSetting: dto.UserSetting{BillingPreference: "wallet_only"},
	}

	apiErr := ValidatePreConsumeBilling(ctx, 50, info)

	require.Nil(t, apiErr)
	assert.Equal(t, 100, walletQuota(t, 601))
	token, err := model.GetTokenById(601)
	require.NoError(t, err)
	assert.Equal(t, 80, token.RemainQuota)
	assert.Nil(t, info.Billing)
}

func TestValidatePreConsumeBillingReturnsNativeTokenQuotaErrorWithoutMutation(t *testing.T) {
	truncate(t)
	seedUser(t, 602, 100)
	seedToken(t, 602, 602, "preflight-token-low", 100)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("token_quota", 10)
	info := &relaycommon.RelayInfo{
		UserId:      602,
		TokenId:     602,
		TokenKey:    "preflight-token-low",
		UserSetting: dto.UserSetting{BillingPreference: "wallet_only"},
	}

	apiErr := ValidatePreConsumeBilling(ctx, 50, info)

	require.NotNil(t, apiErr)
	assert.Equal(t, types.ErrorCodePreConsumeTokenQuotaFailed, apiErr.GetErrorCode())
	assert.Equal(t, http.StatusForbidden, apiErr.StatusCode)
	assert.Equal(t, 100, walletQuota(t, 602))
	token, err := model.GetTokenById(602)
	require.NoError(t, err)
	assert.Equal(t, 100, token.RemainQuota)
}

func TestValidatePreConsumeBillingAccountsForBonusBeforeSubscription(t *testing.T) {
	truncate(t)
	seedUser(t, 603, 0)
	seedToken(t, 603, 603, "preflight-subscription", 100)
	plan := &model.SubscriptionPlan{
		Id:               603,
		Title:            "preflight plan",
		DurationUnit:     model.SubscriptionDurationMonth,
		DurationValue:    1,
		TotalAmount:      100,
		QuotaResetPeriod: model.SubscriptionResetNever,
	}
	require.NoError(t, model.DB.Create(plan).Error)
	subscription := &model.UserSubscription{
		Id:          603,
		UserId:      603,
		PlanId:      plan.Id,
		AmountTotal: 100,
		AmountUsed:  80,
		Status:      "active",
		StartTime:   time.Now().Add(-time.Hour).Unix(),
		EndTime:     time.Now().Add(time.Hour).Unix(),
	}
	require.NoError(t, model.DB.Create(subscription).Error)
	bonus := seedFundingBonus(t, 603, 30, time.Now().Add(time.Hour))
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("token_quota", 100)
	info := &relaycommon.RelayInfo{
		UserId:          603,
		TokenId:         603,
		TokenKey:        "preflight-subscription",
		OriginModelName: "test-model",
		UserSetting:     dto.UserSetting{BillingPreference: "subscription_only"},
	}

	apiErr := ValidatePreConsumeBilling(ctx, 50, info)

	require.Nil(t, apiErr)
	require.NoError(t, model.DB.First(subscription, subscription.Id).Error)
	assert.Equal(t, int64(80), subscription.AmountUsed)
	require.NoError(t, model.DB.First(bonus, bonus.Id).Error)
	assert.Equal(t, 30, bonus.RemainingAmount)
	token, err := model.GetTokenById(603)
	require.NoError(t, err)
	assert.Equal(t, 100, token.RemainQuota)
}

func TestValidatePreConsumeBillingReturnsSubscriptionErrorWithoutReservation(t *testing.T) {
	truncate(t)
	seedUser(t, 605, 0)
	seedToken(t, 605, 605, "preflight-subscription-low", 100)
	plan := &model.SubscriptionPlan{
		Id:               605,
		Title:            "limited preflight plan",
		DurationUnit:     model.SubscriptionDurationMonth,
		DurationValue:    1,
		TotalAmount:      100,
		QuotaResetPeriod: model.SubscriptionResetNever,
	}
	require.NoError(t, model.DB.Create(plan).Error)
	subscription := &model.UserSubscription{
		Id:          605,
		UserId:      605,
		PlanId:      plan.Id,
		AmountTotal: 100,
		AmountUsed:  90,
		Status:      "active",
		StartTime:   time.Now().Add(-time.Hour).Unix(),
		EndTime:     time.Now().Add(time.Hour).Unix(),
	}
	require.NoError(t, model.DB.Create(subscription).Error)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("token_quota", 100)
	info := &relaycommon.RelayInfo{
		UserId:          605,
		TokenId:         605,
		TokenKey:        "preflight-subscription-low",
		OriginModelName: "test-model",
		UserSetting:     dto.UserSetting{BillingPreference: "subscription_only"},
	}

	apiErr := ValidatePreConsumeBilling(ctx, 50, info)

	require.NotNil(t, apiErr)
	assert.Equal(t, types.ErrorCodeInsufficientUserQuota, apiErr.GetErrorCode())
	assert.Equal(t, http.StatusForbidden, apiErr.StatusCode)
	require.NoError(t, model.DB.First(subscription, subscription.Id).Error)
	assert.Equal(t, int64(90), subscription.AmountUsed)
	var recordCount int64
	require.NoError(t, model.DB.Model(&model.SubscriptionPreConsumeRecord{}).Count(&recordCount).Error)
	assert.Zero(t, recordCount)
}

func TestValidateImmediateWalletChargeChecksTokenBeforeProtection(t *testing.T) {
	truncate(t)
	seedUser(t, 604, 100)
	seedToken(t, 604, 604, "midjourney-token-low", 100)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("token_quota", 10)
	info := &relaycommon.RelayInfo{
		UserId:   604,
		TokenId:  604,
		TokenKey: "midjourney-token-low",
	}

	apiErr := ValidateImmediateWalletCharge(ctx, 50, info)

	require.NotNil(t, apiErr)
	assert.Equal(t, types.ErrorCodePreConsumeTokenQuotaFailed, apiErr.GetErrorCode())
	assert.Equal(t, http.StatusForbidden, apiErr.StatusCode)
	assert.Equal(t, 100, walletQuota(t, 604))
	token, err := model.GetTokenById(604)
	require.NoError(t, err)
	assert.Equal(t, 100, token.RemainQuota)
}
