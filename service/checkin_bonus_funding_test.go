package service

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedFundingBonus(t *testing.T, userId int, amount int, expireAt time.Time) *model.CheckinBonus {
	t.Helper()
	bonus := &model.CheckinBonus{
		UserId:          userId,
		CheckinId:       userId,
		Amount:          amount,
		RemainingAmount: amount,
		CreatedAt:       time.Now().Unix(),
		ExpireAt:        expireAt.Unix(),
		Status:          model.CheckinBonusStatusActive,
	}
	require.NoError(t, model.DB.Create(bonus).Error)
	return bonus
}

func walletQuota(t *testing.T, userId int) int {
	t.Helper()
	quota, err := model.GetUserQuota(userId, true)
	require.NoError(t, err)
	return quota
}

func TestCheckinBonusFundingUsesBonusBeforeWallet(t *testing.T) {
	tests := []struct {
		name            string
		bonus           int
		wallet          int
		preConsume      int
		actual          int
		wantBonusRemain int
		wantWallet      int
		wantBonusUsed   int
	}{
		{name: "bonus covers all", bonus: 100, wallet: 200, preConsume: 50, actual: 50, wantBonusRemain: 50, wantWallet: 200, wantBonusUsed: 50},
		{name: "bonus covers part", bonus: 30, wallet: 200, preConsume: 100, actual: 100, wantBonusRemain: 0, wantWallet: 130, wantBonusUsed: 30},
		{name: "settlement refunds wallet first", bonus: 30, wallet: 200, preConsume: 100, actual: 50, wantBonusRemain: 0, wantWallet: 180, wantBonusUsed: 30},
		{name: "settlement then refunds bonus", bonus: 30, wallet: 200, preConsume: 100, actual: 20, wantBonusRemain: 10, wantWallet: 200, wantBonusUsed: 20},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			truncate(t)
			userId := 500 + i
			seedUser(t, userId, tt.wallet)
			bonus := seedFundingBonus(t, userId, tt.bonus, time.Now().Add(time.Hour))
			funding := &CheckinBonusFunding{
				requestId: fmt.Sprintf("funding-%d", i),
				userId:    userId,
				base:      &WalletFunding{userId: userId},
			}

			require.NoError(t, funding.PreConsume(tt.preConsume))
			require.NoError(t, funding.Settle(tt.actual-tt.preConsume))

			require.NoError(t, model.DB.First(bonus, bonus.Id).Error)
			assert.Equal(t, tt.wantBonusRemain, bonus.RemainingAmount)
			assert.Equal(t, tt.wantWallet, walletQuota(t, userId))
			assert.Equal(t, tt.wantBonusUsed, funding.bonusConsumed)
			assert.Equal(t, tt.actual-tt.wantBonusUsed, funding.baseConsumed)
		})
	}
}

func TestCheckinBonusFundingRefundRestoresBothSources(t *testing.T) {
	truncate(t)
	seedUser(t, 510, 100)
	bonus := seedFundingBonus(t, 510, 30, time.Now().Add(time.Hour))
	funding := &CheckinBonusFunding{
		requestId: "refund-request",
		userId:    510,
		base:      &WalletFunding{userId: 510},
	}

	require.NoError(t, funding.PreConsume(80))
	assert.Equal(t, 50, walletQuota(t, 510))
	require.NoError(t, funding.Refund())
	assert.Equal(t, 100, walletQuota(t, 510))
	require.NoError(t, model.DB.First(bonus, bonus.Id).Error)
	assert.Equal(t, 30, bonus.RemainingAmount)
	assert.Equal(t, model.CheckinBonusStatusActive, bonus.Status)
}

func TestBillingSessionRefundClearsSplitBeforeNewCharge(t *testing.T) {
	truncate(t)
	seedUser(t, 512, 100)
	bonus := seedFundingBonus(t, 512, 30, time.Now().Add(time.Hour))
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		UserId:       512,
		RequestId:    "refund-then-charge",
		IsPlayground: true,
		UserSetting:  dto.UserSetting{BillingPreference: "wallet_only"},
	}

	session, apiErr := NewBillingSession(ctx, info, 80)
	require.Nil(t, apiErr)
	assert.Equal(t, 30, info.CheckinBonusConsumed)
	assert.Equal(t, 50, info.OriginalFundingConsumed)
	require.NoError(t, session.Refund(ctx))
	assert.Zero(t, info.CheckinBonusConsumed)
	assert.Zero(t, info.OriginalFundingConsumed)
	assert.Equal(t, 100, walletQuota(t, 512))

	require.NoError(t, PostConsumeQuota(info, 20, 0, false))
	assert.Equal(t, 20, info.CheckinBonusConsumed)
	assert.Zero(t, info.OriginalFundingConsumed)
	assert.Equal(t, 100, walletQuota(t, 512))
	require.NoError(t, model.DB.First(bonus, bonus.Id).Error)
	assert.Equal(t, 10, bonus.RemainingAmount)
}

func TestCheckinBonusFundingPreservesSubscriptionSettlement(t *testing.T) {
	truncate(t)
	seedUser(t, 515, 0)
	plan := &model.SubscriptionPlan{
		Id:               915,
		Title:            "bonus subscription",
		Currency:         "USD",
		DurationUnit:     "month",
		DurationValue:    1,
		Enabled:          true,
		TotalAmount:      1000,
		QuotaResetPeriod: model.SubscriptionResetNever,
	}
	require.NoError(t, model.DB.Create(plan).Error)
	subscription := &model.UserSubscription{
		Id:          916,
		UserId:      515,
		PlanId:      plan.Id,
		AmountTotal: 1000,
		AmountUsed:  0,
		Status:      "active",
		StartTime:   time.Now().Add(-time.Hour).Unix(),
		EndTime:     time.Now().Add(24 * time.Hour).Unix(),
	}
	require.NoError(t, model.DB.Create(subscription).Error)
	bonus := seedFundingBonus(t, 515, 30, time.Now().Add(time.Hour))
	funding := &CheckinBonusFunding{
		requestId: "subscription-bonus",
		userId:    515,
		base: &SubscriptionFunding{
			requestId: "subscription-bonus",
			userId:    515,
			modelName: "test-model",
		},
	}

	require.NoError(t, funding.PreConsume(100))
	require.NoError(t, funding.Settle(-50))

	require.NoError(t, model.DB.First(subscription, subscription.Id).Error)
	assert.Equal(t, int64(20), subscription.AmountUsed)
	require.NoError(t, model.DB.First(bonus, bonus.Id).Error)
	assert.Zero(t, bonus.RemainingAmount)
	assert.Equal(t, 30, funding.bonusConsumed)
	assert.Equal(t, 20, funding.baseConsumed)
}

func TestSubscriptionBonusSettlementKeepsRelayAuditInSync(t *testing.T) {
	truncate(t)
	seedUser(t, 516, 0)
	plan := &model.SubscriptionPlan{
		Id: 917, Title: "relay audit plan", Currency: "USD",
		DurationUnit: model.SubscriptionDurationMonth, DurationValue: 1,
		Enabled: true, TotalAmount: 1000, QuotaResetPeriod: model.SubscriptionResetNever,
	}
	require.NoError(t, model.DB.Create(plan).Error)
	subscription := &model.UserSubscription{
		Id: 918, UserId: 516, PlanId: plan.Id, AmountTotal: 1000, Status: "active",
		StartTime: time.Now().Add(-time.Hour).Unix(), EndTime: time.Now().Add(time.Hour).Unix(),
	}
	require.NoError(t, model.DB.Create(subscription).Error)
	seedFundingBonus(t, 516, 30, time.Now().Add(time.Hour))
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		UserId: 516, RequestId: "subscription-relay-audit", OriginModelName: "test-model",
		IsPlayground: true, UserSetting: dto.UserSetting{BillingPreference: "subscription_only"},
	}

	session, apiErr := NewBillingSession(ctx, info, 100)
	require.Nil(t, apiErr)
	require.NoError(t, session.Settle(50))
	require.NoError(t, model.DB.First(subscription, subscription.Id).Error)
	assert.Equal(t, int64(20), subscription.AmountUsed)
	assert.Equal(t, int64(70), info.SubscriptionPreConsumed)
	assert.Equal(t, int64(-50), info.SubscriptionPostDelta)
	assert.Equal(t, int64(70), info.SubscriptionAmountUsedAfterPreConsume)

	other := map[string]interface{}{}
	appendBillingInfo(info, other)
	assert.Equal(t, int64(20), other["subscription_consumed"])
	assert.Equal(t, int64(20), other["subscription_used"])
	assert.Equal(t, int64(980), other["subscription_remain"])
}

func TestNewBillingSessionKeepsOriginalInsufficientQuotaRule(t *testing.T) {
	truncate(t)
	seedUser(t, 520, 5)
	seedFundingBonus(t, 520, 3, time.Now().Add(time.Hour))
	ctx, _ := gin.CreateTestContext(nil)
	info := &relaycommon.RelayInfo{
		UserId:      520,
		RequestId:   "insufficient-request",
		UserSetting: dto.UserSetting{BillingPreference: "wallet_only"},
	}

	session, apiErr := NewBillingSession(ctx, info, 10)
	assert.Nil(t, session)
	require.NotNil(t, apiErr)
	assert.Equal(t, 5, walletQuota(t, 520))
	active, err := model.GetActiveCheckinBonus(520, time.Now())
	require.NoError(t, err)
	require.NotNil(t, active)
	assert.Equal(t, 3, active.RemainingAmount)
}

func TestCheckinBonusFundingPreservesPostSettlementNegativeBalanceRule(t *testing.T) {
	truncate(t)
	seedUser(t, 525, 5)
	seedFundingBonus(t, 525, 3, time.Now().Add(time.Hour))
	ctx, _ := gin.CreateTestContext(nil)
	info := &relaycommon.RelayInfo{
		UserId:       525,
		RequestId:    "negative-settlement",
		IsPlayground: true,
		UserSetting:  dto.UserSetting{BillingPreference: "wallet_only"},
	}

	session, apiErr := NewBillingSession(ctx, info, 8)
	require.Nil(t, apiErr)
	require.NoError(t, session.Settle(10))
	assert.Equal(t, -2, walletQuota(t, 525), "post-settlement delta keeps the original negative-balance behavior")

	nextInfo := &relaycommon.RelayInfo{
		UserId:       525,
		RequestId:    "negative-next-request",
		IsPlayground: true,
		UserSetting:  dto.UserSetting{BillingPreference: "wallet_only"},
	}
	nextSession, nextErr := NewBillingSession(ctx, nextInfo, 1)
	assert.Nil(t, nextSession)
	require.NotNil(t, nextErr)
	assert.Equal(t, types.ErrorCodeInsufficientUserQuota, nextErr.GetErrorCode())
}

func TestExpiredCheckinBonusDoesNotParticipateInFunding(t *testing.T) {
	truncate(t)
	seedUser(t, 530, 100)
	bonus := seedFundingBonus(t, 530, 30, time.Now().Add(-time.Minute))
	funding := &CheckinBonusFunding{
		requestId: "expired-funding",
		userId:    530,
		base:      &WalletFunding{userId: 530},
	}

	require.NoError(t, funding.PreConsume(20))
	require.NoError(t, funding.Settle(0))
	assert.Equal(t, 80, walletQuota(t, 530))
	require.NoError(t, model.DB.First(bonus, bonus.Id).Error)
	assert.Equal(t, 30, bonus.RemainingAmount)
	assert.Equal(t, model.CheckinBonusStatusExpired, bonus.Status)
}

func TestBillingSessionReserveUsesBonusBeforeWallet(t *testing.T) {
	truncate(t)
	seedUser(t, 540, 100)
	bonus := seedFundingBonus(t, 540, 20, time.Now().Add(time.Hour))
	ctx, _ := gin.CreateTestContext(nil)
	info := &relaycommon.RelayInfo{
		UserId:       540,
		RequestId:    "stream-reserve",
		IsPlayground: true,
		UserSetting:  dto.UserSetting{BillingPreference: "wallet_only"},
	}

	session, apiErr := NewBillingSession(ctx, info, 5)
	require.Nil(t, apiErr)
	require.NoError(t, session.Reserve(25))
	require.NoError(t, session.Settle(25))

	require.NoError(t, model.DB.First(bonus, bonus.Id).Error)
	assert.Zero(t, bonus.RemainingAmount)
	assert.Equal(t, 95, walletQuota(t, 540))
	assert.Equal(t, 20, info.CheckinBonusConsumed)
	assert.Equal(t, 5, info.OriginalFundingConsumed)
}

func TestCheckinBonusFundingDoesNotSwitchToLateBonusAfterBasePreConsume(t *testing.T) {
	t.Run("wallet reserve keeps the original funding mode", func(t *testing.T) {
		truncate(t)
		const userId = 542
		seedUser(t, userId, 1000)
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		info := &relaycommon.RelayInfo{
			UserId:       userId,
			RequestId:    "late-wallet-bonus",
			IsPlayground: true,
			UserSetting:  dto.UserSetting{BillingPreference: "wallet_only"},
		}

		session, apiErr := NewBillingSession(ctx, info, 100)
		require.Nil(t, apiErr)
		bonus := seedFundingBonus(t, userId, 20, time.Now().Add(time.Hour))
		require.NoError(t, session.Reserve(150))
		require.NoError(t, session.Settle(150))

		assert.Equal(t, 850, walletQuota(t, userId))
		require.NoError(t, model.DB.First(bonus, bonus.Id).Error)
		assert.Equal(t, 20, bonus.RemainingAmount)
		assert.Zero(t, info.CheckinBonusConsumed)
		assert.Equal(t, 150, info.OriginalFundingConsumed)
		var usageCount int64
		require.NoError(t, model.DB.Model(&model.CheckinBonusUsage{}).
			Where("request_id = ?", info.RequestId).Count(&usageCount).Error)
		assert.Zero(t, usageCount)
	})

	t.Run("late bonus cannot bypass the locked wallet balance", func(t *testing.T) {
		truncate(t)
		const userId = 546
		seedUser(t, userId, 100)
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		info := &relaycommon.RelayInfo{
			UserId:       userId,
			RequestId:    "late-wallet-insufficient",
			IsPlayground: true,
			UserSetting:  dto.UserSetting{BillingPreference: "wallet_only"},
		}

		session, apiErr := NewBillingSession(ctx, info, 80)
		require.Nil(t, apiErr)
		bonus := seedFundingBonus(t, userId, 100, time.Now().Add(time.Hour))
		require.Error(t, session.Reserve(150))

		assert.Equal(t, 20, walletQuota(t, userId))
		require.NoError(t, model.DB.First(bonus, bonus.Id).Error)
		assert.Equal(t, 100, bonus.RemainingAmount)
	})

	t.Run("subscription reserve keeps the original funding mode", func(t *testing.T) {
		truncate(t)
		const userId = 543
		seedUser(t, userId, 0)
		plan := &model.SubscriptionPlan{
			Id: 943, Title: "late bonus plan", Currency: "USD",
			DurationUnit: model.SubscriptionDurationMonth, DurationValue: 1,
			Enabled: true, TotalAmount: 1000, QuotaResetPeriod: model.SubscriptionResetNever,
		}
		require.NoError(t, model.DB.Create(plan).Error)
		subscription := &model.UserSubscription{
			Id: 944, UserId: userId, PlanId: plan.Id, AmountTotal: 1000, Status: "active",
			StartTime: time.Now().Add(-time.Hour).Unix(), EndTime: time.Now().Add(time.Hour).Unix(),
		}
		require.NoError(t, model.DB.Create(subscription).Error)
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		info := &relaycommon.RelayInfo{
			UserId: userId, RequestId: "late-subscription-bonus", OriginModelName: "test-model",
			IsPlayground: true, UserSetting: dto.UserSetting{BillingPreference: "subscription_only"},
		}

		session, apiErr := NewBillingSession(ctx, info, 100)
		require.Nil(t, apiErr)
		bonus := seedFundingBonus(t, userId, 20, time.Now().Add(time.Hour))
		require.NoError(t, session.Reserve(150))
		require.NoError(t, session.Settle(150))

		require.NoError(t, model.DB.First(subscription, subscription.Id).Error)
		assert.Equal(t, int64(150), subscription.AmountUsed)
		require.NoError(t, model.DB.First(bonus, bonus.Id).Error)
		assert.Equal(t, 20, bonus.RemainingAmount)
		assert.Zero(t, info.CheckinBonusConsumed)
		assert.Equal(t, 150, info.OriginalFundingConsumed)
	})

	t.Run("settlement overage does not create an ownerless bonus reservation", func(t *testing.T) {
		truncate(t)
		const userId = 544
		seedUser(t, userId, 1000)
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		info := &relaycommon.RelayInfo{
			UserId:       userId,
			RequestId:    "late-settlement-bonus",
			IsPlayground: true,
			UserSetting:  dto.UserSetting{BillingPreference: "wallet_only"},
		}

		session, apiErr := NewBillingSession(ctx, info, 100)
		require.Nil(t, apiErr)
		bonus := seedFundingBonus(t, userId, 20, time.Now().Add(time.Hour))
		require.NoError(t, session.Settle(150))

		assert.Equal(t, 850, walletQuota(t, userId))
		require.NoError(t, model.DB.First(bonus, bonus.Id).Error)
		assert.Equal(t, 20, bonus.RemainingAmount)
		assert.Zero(t, info.CheckinBonusConsumed)
		assert.Equal(t, 150, info.OriginalFundingConsumed)
		var usageCount int64
		require.NoError(t, model.DB.Model(&model.CheckinBonusUsage{}).
			Where("request_id = ?", info.RequestId).Count(&usageCount).Error)
		assert.Zero(t, usageCount)
	})
}

func TestTrustedBillingSessionSettlesBonusAndWalletInTrackedLedger(t *testing.T) {
	truncate(t)
	const userId = 545
	trustQuota := common.GetTrustQuota()
	seedUser(t, userId, trustQuota+100)
	bonus := seedFundingBonus(t, userId, 30, time.Now().Add(time.Hour))
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		UserId:         userId,
		RequestId:      "trusted-bonus-settlement",
		IsPlayground:   true,
		TokenUnlimited: true,
		UserSetting:    dto.UserSetting{BillingPreference: "wallet_only"},
	}

	session, apiErr := NewBillingSession(ctx, info, 50)
	require.Nil(t, apiErr)
	assert.Zero(t, session.GetPreConsumedQuota(), "trusted requests must skip pre-consumption")
	require.NoError(t, session.Settle(80))

	require.NoError(t, model.DB.First(bonus, bonus.Id).Error)
	assert.Zero(t, bonus.RemainingAmount)
	assert.Equal(t, trustQuota+50, walletQuota(t, userId))
	assert.Equal(t, 30, info.CheckinBonusConsumed)
	assert.Equal(t, 50, info.OriginalFundingConsumed)
	var usage model.CheckinBonusUsage
	require.NoError(t, model.DB.Where("request_id = ?", info.RequestId).First(&usage).Error)
	assert.Equal(t, model.CheckinBonusUsageStatusSettled, usage.Status)
	assert.Equal(t, model.CheckinBonusOriginalFundingWallet, usage.OriginalFundingSource)
	assert.Equal(t, 30, usage.ConsumedAmount)
	assert.Equal(t, 50, usage.OriginalConsumedAmount)
	assert.NotEmpty(t, usage.OwnerInstanceId)
}

func TestPostConsumeQuotaUsesBonusAndExposesAuditSplit(t *testing.T) {
	truncate(t)
	seedUser(t, 550, 100)
	bonus := seedFundingBonus(t, 550, 30, time.Now().Add(time.Hour))
	info := &relaycommon.RelayInfo{
		UserId:       550,
		IsPlayground: true,
	}

	require.NoError(t, PostConsumeQuota(info, 50, 0, false))
	require.NoError(t, model.DB.First(bonus, bonus.Id).Error)
	assert.Zero(t, bonus.RemainingAmount)
	assert.Equal(t, 80, walletQuota(t, 550))
	assert.NotEmpty(t, info.CheckinBonusRequestId)
	assert.Equal(t, 30, info.CheckinBonusConsumed)
	assert.Equal(t, 20, info.OriginalFundingConsumed)

	other := map[string]interface{}{}
	appendBillingInfo(info, other)
	assert.Equal(t, 50, other["consume_total"])
	assert.Equal(t, 30, other["checkin_bonus_deducted"])
	assert.Equal(t, 20, other["original_funding_deducted"])
	assert.Equal(t, 20, other["wallet_quota_deducted"])
}

func TestBonusFundedRequestStillCountsFullUsageAndRankingStats(t *testing.T) {
	truncate(t)
	const userID, channelID = 560, 560
	seedUser(t, userID, 100)
	seedChannel(t, channelID)
	seedFundingBonus(t, userID, 30, time.Now().Add(time.Hour))
	info := &relaycommon.RelayInfo{UserId: userID, IsPlayground: true}
	require.NoError(t, PostConsumeQuota(info, 50, 0, false))

	model.UpdateUserUsedQuotaAndRequestCount(userID, 50)
	model.UpdateChannelUsedQuota(channelID, 50)
	other := map[string]interface{}{}
	appendBillingInfo(info, other)

	originalDataExportEnabled := common.DataExportEnabled
	common.DataExportEnabled = true
	model.CacheQuotaDataLock.Lock()
	model.CacheQuotaData = make(map[string]*model.QuotaData)
	model.CacheQuotaDataLock.Unlock()
	t.Cleanup(func() {
		common.DataExportEnabled = originalDataExportEnabled
		model.CacheQuotaDataLock.Lock()
		model.CacheQuotaData = make(map[string]*model.QuotaData)
		model.CacheQuotaDataLock.Unlock()
	})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	model.RecordConsumeLog(ctx, userID, model.RecordConsumeLogParams{
		ChannelId:        channelID,
		PromptTokens:     6,
		CompletionTokens: 4,
		ModelName:        "test-model",
		Quota:            50,
		Group:            "default",
		Other:            other,
	})
	model.SaveQuotaDataCache()

	var user model.User
	var channel model.Channel
	var quotaData model.QuotaData
	require.NoError(t, model.DB.First(&user, userID).Error)
	require.NoError(t, model.DB.First(&channel, channelID).Error)
	require.NoError(t, model.DB.Where("user_id = ? AND model_name = ?", userID, "test-model").First(&quotaData).Error)
	assert.Equal(t, 50, user.UsedQuota)
	assert.Equal(t, 1, user.RequestCount)
	assert.Equal(t, int64(50), channel.UsedQuota)
	assert.Equal(t, 50, quotaData.Quota)
	assert.Equal(t, 10, quotaData.TokenUsed)

	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, 50, log.Quota, "financial and consumption logs keep the full request amount")
	rankings, err := model.GetRankingQuotaTotals(0, 0)
	require.NoError(t, err)
	require.Len(t, rankings, 1)
	assert.Equal(t, int64(10), rankings[0].TotalTokens)
}
