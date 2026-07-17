package model

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedCheckinBonus(t *testing.T, userId int, amount int, createdAt, expireAt time.Time) *CheckinBonus {
	t.Helper()
	checkin := &Checkin{
		UserId:       userId,
		CheckinDate:  createdAt.Format("2006-01-02"),
		QuotaAwarded: 1,
		CreatedAt:    createdAt.Unix(),
	}
	require.NoError(t, DB.Create(checkin).Error)
	bonus := &CheckinBonus{
		UserId:          userId,
		CheckinId:       checkin.Id,
		Amount:          amount,
		RemainingAmount: amount,
		CreatedAt:       createdAt.Unix(),
		ExpireAt:        expireAt.Unix(),
		Status:          CheckinBonusStatusActive,
	}
	require.NoError(t, DB.Create(bonus).Error)
	return bonus
}

func TestCheckinBonusReservationSettlementAndRefund(t *testing.T) {
	truncateTables(t)
	now := time.Date(2026, 7, 19, 19, 0, 0, 0, time.FixedZone("CST", 8*3600))
	bonus := seedCheckinBonus(t, 101, 100, now, nextLocalMidnight(now))

	reserved, err := ReserveCheckinBonus(101, "req-a", 60, now)
	require.NoError(t, err)
	assert.Equal(t, 60, reserved)
	require.NoError(t, SettleCheckinBonusUsage("req-a", 60, now))

	require.NoError(t, DB.First(bonus, bonus.Id).Error)
	assert.Equal(t, 40, bonus.RemainingAmount)
	assert.Equal(t, CheckinBonusStatusActive, bonus.Status)

	reserved, err = ReserveCheckinBonus(101, "req-b", 100, now)
	require.NoError(t, err)
	assert.Equal(t, 40, reserved)
	require.NoError(t, SettleCheckinBonusUsage("req-b", 20, now))

	require.NoError(t, DB.First(bonus, bonus.Id).Error)
	assert.Equal(t, 20, bonus.RemainingAmount)
	assert.Equal(t, CheckinBonusStatusActive, bonus.Status)

	refunded, err := RefundCheckinBonusUsage("req-a", now)
	require.NoError(t, err)
	assert.Equal(t, 60, refunded)
	require.NoError(t, DB.First(bonus, bonus.Id).Error)
	assert.Equal(t, 80, bonus.RemainingAmount)
}

func TestCheckinBonusExpiresAtNextLocalMidnight(t *testing.T) {
	truncateTables(t)
	location := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 7, 19, 23, 30, 0, 0, location)
	bonus := seedCheckinBonus(t, 102, 100, now, nextLocalMidnight(now))

	active, err := GetActiveCheckinBonus(102, now.Add(29*time.Minute))
	require.NoError(t, err)
	require.NotNil(t, active)
	assert.Equal(t, nextLocalMidnight(now).Unix(), active.ExpireAt)

	active, err = GetActiveCheckinBonus(102, nextLocalMidnight(now))
	require.NoError(t, err)
	assert.Nil(t, active)

	reserved, err := ReserveCheckinBonus(102, "expired-request", 50, nextLocalMidnight(now))
	require.NoError(t, err)
	assert.Zero(t, reserved)
	require.NoError(t, DB.First(bonus, bonus.Id).Error)
	assert.Equal(t, CheckinBonusStatusExpired, bonus.Status)
}

func TestExpireCheckinBonusesMarksOnlyDueActiveRows(t *testing.T) {
	truncateTables(t)
	now := time.Date(2026, 7, 20, 0, 0, 0, 0, time.FixedZone("CST", 8*3600))
	due := seedCheckinBonus(t, 104, 100, now.Add(-time.Hour), now)
	future := seedCheckinBonus(t, 105, 100, now, now.Add(24*time.Hour))

	expired, err := ExpireCheckinBonuses(now.Unix(), 500)
	require.NoError(t, err)
	assert.Equal(t, int64(1), expired)
	require.NoError(t, DB.First(due, due.Id).Error)
	require.NoError(t, DB.First(future, future.Id).Error)
	assert.Equal(t, CheckinBonusStatusExpired, due.Status)
	assert.Equal(t, CheckinBonusStatusActive, future.Status)
}

func TestCheckinBonusConcurrentReservationsNeverOverspend(t *testing.T) {
	truncateTables(t)
	now := time.Now()
	bonus := seedCheckinBonus(t, 103, 100, now, now.Add(time.Hour))
	var totalReserved atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			requestId := fmt.Sprintf("concurrent-%d", index)
			reserved, err := ReserveCheckinBonus(103, requestId, 30, now)
			if err != nil {
				return
			}
			if err := SettleCheckinBonusUsage(requestId, reserved, now); err != nil {
				return
			}
			totalReserved.Add(int64(reserved))
		}(i)
	}
	wg.Wait()

	assert.Equal(t, int64(100), totalReserved.Load())
	require.NoError(t, DB.First(bonus, bonus.Id).Error)
	assert.Zero(t, bonus.RemainingAmount)
	assert.Equal(t, CheckinBonusStatusConsumed, bonus.Status)
}

func TestRecoverOrphanedCheckinBonusWalletUsageRestoresBothSources(t *testing.T) {
	truncateTables(t)
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.FixedZone("CST", 8*3600))
	user := &User{Id: 106, Username: "orphan-recovery", AffCode: "orphan-recovery", Quota: 100, Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(user).Error)
	token := &Token{Id: 106, UserId: user.Id, Key: "orphan-recovery", RemainQuota: 100, Status: common.TokenStatusEnabled}
	require.NoError(t, DB.Create(token).Error)
	bonus := seedCheckinBonus(t, user.Id, 30, now, nextLocalMidnight(now))

	require.NoError(t, DecreaseTokenQuota(token.Id, token.Key, 80))
	bonusReserved, walletReserved, tracked, err := ReserveCheckinBonusWallet(
		user.Id, "orphan-wallet", 80, now, "node-a", 1000, "process-old", token.Id, true,
	)
	require.NoError(t, err)
	assert.True(t, tracked)
	assert.Equal(t, 30, bonusReserved)
	assert.Equal(t, 50, walletReserved)
	require.NoError(t, DB.First(user, user.Id).Error)
	assert.Equal(t, 50, user.Quota)
	require.NoError(t, DB.First(bonus, bonus.Id).Error)
	assert.Zero(t, bonus.RemainingAmount)
	require.NoError(t, DB.First(token, token.Id).Error)
	assert.Equal(t, 20, token.RemainQuota)
	assert.Equal(t, 80, token.UsedQuota)

	require.NoError(t, UpsertCheckinBonusProcessLease("process-old", "node-a", 1000, now.Add(-time.Minute).Unix()))
	recovered, err := RecoverOrphanedCheckinBonusUsages("process-current", now.Add(time.Minute), 30*time.Second, 100)
	require.NoError(t, err)
	assert.Equal(t, 1, recovered)
	require.NoError(t, DB.First(user, user.Id).Error)
	assert.Equal(t, 100, user.Quota)
	require.NoError(t, DB.First(bonus, bonus.Id).Error)
	assert.Equal(t, 30, bonus.RemainingAmount)
	assert.Equal(t, CheckinBonusStatusActive, bonus.Status)
	require.NoError(t, DB.First(token, token.Id).Error)
	assert.Equal(t, 100, token.RemainQuota)
	assert.Zero(t, token.UsedQuota)

	var usage CheckinBonusUsage
	require.NoError(t, DB.Where("request_id = ?", "orphan-wallet").First(&usage).Error)
	assert.Equal(t, CheckinBonusUsageStatusRefunded, usage.Status)
	assert.Equal(t, 50, usage.OriginalReservedAmount)
	assert.Equal(t, token.Id, usage.TokenId)
	assert.Equal(t, 80, usage.TokenReservedAmount)

	recovered, err = RecoverOrphanedCheckinBonusUsages("process-current", now.Add(2*time.Minute), 30*time.Second, 100)
	require.NoError(t, err)
	assert.Zero(t, recovered, "recovery must be idempotent")
	require.NoError(t, DB.First(user, user.Id).Error)
	assert.Equal(t, 100, user.Quota)
	require.NoError(t, DB.First(token, token.Id).Error)
	assert.Equal(t, 100, token.RemainQuota)
	assert.Zero(t, token.UsedQuota)
}

func TestRecoverOrphanedCheckinBonusWalletUsageLeavesLiveAndOtherNodesUntouched(t *testing.T) {
	truncateTables(t)
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.FixedZone("CST", 8*3600))
	for _, fixture := range []struct {
		userId    int
		requestId string
		node      string
		startedAt int64
		instance  string
	}{
		{userId: 107, requestId: "current-process", node: "node-a", startedAt: 2000, instance: "process-current"},
		{userId: 108, requestId: "older-live-process", node: "node-a", startedAt: 1000, instance: "process-old-live"},
	} {
		user := &User{Id: fixture.userId, Username: fixture.requestId, AffCode: fixture.requestId, Quota: 100, Status: common.UserStatusEnabled}
		require.NoError(t, DB.Create(user).Error)
		seedCheckinBonus(t, user.Id, 30, now, nextLocalMidnight(now))
		_, _, tracked, err := ReserveCheckinBonusWallet(
			user.Id, fixture.requestId, 80, now, fixture.node, fixture.startedAt, fixture.instance, 0, true,
		)
		require.NoError(t, err)
		assert.True(t, tracked)
		require.NoError(t, UpsertCheckinBonusProcessLease(fixture.instance, fixture.node, fixture.startedAt, now.Add(time.Minute).Unix()))
	}

	recovered, err := RecoverOrphanedCheckinBonusUsages("process-current", now.Add(time.Minute), 30*time.Second, 100)
	require.NoError(t, err)
	assert.Zero(t, recovered)
	for _, userId := range []int{107, 108} {
		var user User
		require.NoError(t, DB.First(&user, userId).Error)
		assert.Equal(t, 50, user.Quota)
	}
}

func TestRecoverOrphanedCheckinBonusSubscriptionUsageRestoresBothSources(t *testing.T) {
	truncateTables(t)
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.FixedZone("CST", 8*3600))
	user := &User{Id: 109, Username: "orphan-subscription", AffCode: "orphan-subscription", Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(user).Error)
	plan := &SubscriptionPlan{Id: 109, Title: "orphan plan", DurationUnit: SubscriptionDurationMonth, DurationValue: 1, Enabled: true, TotalAmount: 100}
	require.NoError(t, DB.Create(plan).Error)
	subscription := &UserSubscription{
		Id: 109, UserId: user.Id, PlanId: plan.Id, AmountTotal: 100, Status: "active",
		StartTime: now.Add(-time.Hour).Unix(), EndTime: now.Add(time.Hour).Unix(),
	}
	require.NoError(t, DB.Create(subscription).Error)
	bonus := seedCheckinBonus(t, user.Id, 30, now, nextLocalMidnight(now))

	bonusReserved, subscriptionReserved, tracked, result, err := ReserveCheckinBonusSubscription(
		user.Id, "orphan-subscription", "test-model", 80, now,
		"node-a", 1000, "subscription-process-old", 0,
	)
	require.NoError(t, err)
	require.True(t, tracked)
	require.NotNil(t, result)
	assert.Equal(t, 30, bonusReserved)
	assert.Equal(t, 50, subscriptionReserved)
	require.NoError(t, DB.First(subscription, subscription.Id).Error)
	assert.Equal(t, int64(50), subscription.AmountUsed)

	require.NoError(t, UpsertCheckinBonusProcessLease("subscription-process-old", "node-a", 1000, now.Add(-time.Minute).Unix()))
	recovered, err := RecoverOrphanedCheckinBonusUsages("process-current", now.Add(time.Minute), 30*time.Second, 100)
	require.NoError(t, err)
	assert.Equal(t, 1, recovered)
	require.NoError(t, DB.First(subscription, subscription.Id).Error)
	assert.Zero(t, subscription.AmountUsed)
	require.NoError(t, DB.First(bonus, bonus.Id).Error)
	assert.Equal(t, 30, bonus.RemainingAmount)

	var record SubscriptionPreConsumeRecord
	require.NoError(t, DB.Where("request_id = ?", "orphan-subscription").First(&record).Error)
	assert.Equal(t, "refunded", record.Status)
}

func TestAdjustSettledBonusOnlySubscriptionInitializesSubscriptionForOverage(t *testing.T) {
	truncateTables(t)
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.FixedZone("CST", 8*3600))
	user := &User{Id: 111, Username: "bonus-only-subscription", AffCode: "bonus-only-subscription", Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(user).Error)
	plan := &SubscriptionPlan{
		Id: 111, Title: "bonus-only plan", DurationUnit: SubscriptionDurationMonth,
		DurationValue: 1, Enabled: true, TotalAmount: 100, QuotaResetPeriod: SubscriptionResetNever,
	}
	require.NoError(t, DB.Create(plan).Error)
	subscription := &UserSubscription{
		Id: 111, UserId: user.Id, PlanId: plan.Id, AmountTotal: 100, Status: "active",
		StartTime: now.Add(-time.Hour).Unix(), EndTime: now.Add(time.Hour).Unix(),
	}
	require.NoError(t, DB.Create(subscription).Error)
	seedCheckinBonus(t, user.Id, 30, now, nextLocalMidnight(now))

	bonusReserved, subscriptionReserved, tracked, result, err := ReserveCheckinBonusSubscription(
		user.Id, "bonus-only-subscription-overage", "test-model", 30, now,
		"node-a", 1000, "process-a", 0,
	)
	require.NoError(t, err)
	assert.True(t, tracked)
	assert.Equal(t, 30, bonusReserved)
	assert.Zero(t, subscriptionReserved)
	assert.Nil(t, result)
	handled, err := SettleCheckinBonusFundingUsage("bonus-only-subscription-overage", 30, 0, now)
	require.NoError(t, err)
	assert.True(t, handled)

	bonusDelta, originalDelta, handled, err := AdjustSettledCheckinBonusFundingUsage(
		"bonus-only-subscription-overage", 20, now,
	)
	require.NoError(t, err)
	assert.True(t, handled)
	assert.Zero(t, bonusDelta)
	assert.Equal(t, 20, originalDelta)

	require.NoError(t, DB.First(subscription, subscription.Id).Error)
	assert.Equal(t, int64(20), subscription.AmountUsed)
	var usage CheckinBonusUsage
	require.NoError(t, DB.Where("request_id = ?", "bonus-only-subscription-overage").First(&usage).Error)
	assert.Equal(t, subscription.Id, usage.OriginalFundingId)
	assert.Equal(t, 20, usage.OriginalConsumedAmount)
	var record SubscriptionPreConsumeRecord
	require.NoError(t, DB.Where("request_id = ?", "bonus-only-subscription-overage").First(&record).Error)
	assert.Equal(t, subscription.Id, record.UserSubscriptionId)
	assert.Equal(t, int64(20), record.PreConsumed)
	assert.Equal(t, "settled", record.Status)
}

func TestUserCheckinTransactionRollsBackCheckinAndQuotaWhenBonusInsertFails(t *testing.T) {
	truncateTables(t)
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.FixedZone("CST", 8*3600))
	user := &User{Id: 110, Username: "atomic-checkin", AffCode: "atomic-checkin", Quota: 100, Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(user).Error)
	require.NoError(t, DB.Create(&CheckinBonus{
		UserId: 999, CheckinId: 9001, Amount: 1, RemainingAmount: 1,
		CreatedAt: now.Unix(), ExpireAt: now.Add(time.Hour).Unix(), Status: CheckinBonusStatusActive,
	}).Error)

	checkin := &Checkin{Id: 9001, UserId: user.Id, CheckinDate: "2026-07-17", QuotaAwarded: 7, CreatedAt: now.Unix()}
	bonus := newCheckinBonus(user.Id, 0, 10, now)
	_, _, err := userCheckinWithTransaction(checkin, bonus, user.Id, checkin.QuotaAwarded)
	require.Error(t, err)

	var checkinCount int64
	require.NoError(t, DB.Model(&Checkin{}).Where("id = ?", checkin.Id).Count(&checkinCount).Error)
	assert.Zero(t, checkinCount)
	require.NoError(t, DB.First(user, user.Id).Error)
	assert.Equal(t, 100, user.Quota)
}

func TestUserCheckinCreatesIndependentConfiguredBonus(t *testing.T) {
	truncateTables(t)
	checkinSetting := operation_setting.GetCheckinSetting()
	bonusSetting := operation_setting.GetCheckinBonusSetting()
	originalCheckin := *checkinSetting
	originalBonus := *bonusSetting
	t.Cleanup(func() {
		*checkinSetting = originalCheckin
		*bonusSetting = originalBonus
	})
	checkinSetting.Enabled = true
	checkinSetting.MinQuota = 7
	checkinSetting.MaxQuota = 7
	bonusSetting.Enabled = true
	bonusSetting.MinAmount = 10
	bonusSetting.MaxAmount = 20

	for i := 0; i < 8; i++ {
		userId := 200 + i
		user := &User{Id: userId, Username: fmt.Sprintf("checkin-%d", i), AffCode: fmt.Sprintf("ci-%d", i), Quota: 100, Status: common.UserStatusEnabled}
		require.NoError(t, DB.Create(user).Error)

		checkin, bonus, err := UserCheckin(userId)
		require.NoError(t, err)
		require.NotNil(t, checkin)
		require.NotNil(t, bonus)
		assert.Zero(t, checkin.QuotaAwarded)
		assert.GreaterOrEqual(t, bonus.Amount, 10)
		assert.LessOrEqual(t, bonus.Amount, 20)
		assert.Equal(t, bonus.Amount, bonus.RemainingAmount)

		var stored User
		require.NoError(t, DB.First(&stored, userId).Error)
		assert.Equal(t, 100, stored.Quota, "bonus mode must not also award account balance")
		assert.Zero(t, stored.UsedQuota, "awarding a bonus must not count as API usage")
		assert.Zero(t, stored.RequestCount, "awarding a bonus must not count as a request")

		stats, err := GetUserCheckinStats(userId, time.Now().Format("2006-01"))
		require.NoError(t, err)
		assert.Equal(t, int64(0), stats["total_quota"])
		assert.Equal(t, int64(bonus.Amount), stats["total_bonus"])
		assert.Equal(t, int64(bonus.Amount), stats["total_reward"])
		assert.Equal(t, int64(bonus.Amount), stats["monthly_reward"])
		records := stats["records"].([]CheckinRecord)
		require.Len(t, records, 1)
		assert.Zero(t, records[0].QuotaAwarded)
		assert.Equal(t, bonus.Amount, records[0].BonusAwarded)
	}

	var topUpCount int64
	var quotaDataCount int64
	require.NoError(t, DB.Model(&TopUp{}).Count(&topUpCount).Error)
	require.NoError(t, DB.Model(&QuotaData{}).Count(&quotaDataCount).Error)
	assert.Zero(t, topUpCount, "bonus awards must not create recharge records")
	assert.Zero(t, quotaDataCount, "granting the balance itself is not an API usage event")
}

func TestUserCheckinKeepsLegacyBalanceRewardWhenBonusDisabled(t *testing.T) {
	truncateTables(t)
	checkinSetting := operation_setting.GetCheckinSetting()
	bonusSetting := operation_setting.GetCheckinBonusSetting()
	originalCheckin := *checkinSetting
	originalBonus := *bonusSetting
	t.Cleanup(func() {
		*checkinSetting = originalCheckin
		*bonusSetting = originalBonus
	})
	checkinSetting.Enabled = true
	checkinSetting.MinQuota = 7
	checkinSetting.MaxQuota = 7
	bonusSetting.Enabled = false

	user := &User{Id: 300, Username: "legacy-checkin", AffCode: "legacy-ci", Quota: 100, Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(user).Error)
	checkin, bonus, err := UserCheckin(user.Id)
	require.NoError(t, err)
	require.NotNil(t, checkin)
	assert.Nil(t, bonus)
	assert.Equal(t, 7, checkin.QuotaAwarded)

	var stored User
	require.NoError(t, DB.First(&stored, user.Id).Error)
	assert.Equal(t, 107, stored.Quota)
	var bonusCount int64
	require.NoError(t, DB.Model(&CheckinBonus{}).Where("user_id = ?", user.Id).Count(&bonusCount).Error)
	assert.Zero(t, bonusCount)
}
