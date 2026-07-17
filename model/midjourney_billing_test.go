package model

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImmediateChargeAtomicallyPersistsMidjourneyBilling(t *testing.T) {
	truncateTables(t)
	user := &User{Id: 701, Username: "mj-charge", AffCode: "mj-charge", Quota: 100}
	token := &Token{Id: 91, UserId: user.Id, Key: "mj-charge", RemainQuota: 100}
	task := &Midjourney{
		UserId: user.Id, MjId: "atomic-upstream-id", Quota: 20,
		BillingStatus: MidjourneyBillingStatusPending,
	}
	require.NoError(t, DB.Create(user).Error)
	require.NoError(t, DB.Create(token).Error)
	require.NoError(t, DB.Create(task).Error)

	result, err := ConsumeCheckinBonusFunding(CheckinBonusImmediateChargeParams{
		UserId: user.Id, RequestId: "atomic-mj-charge", Amount: 20,
		OriginalFundingSource: CheckinBonusOriginalFundingWallet,
		TokenId:               token.Id, TokenKey: token.Key, DeductToken: true,
		MidjourneyTaskId: task.Id, Now: time.Now(),
	})
	require.NoError(t, err)
	assert.Zero(t, result.BonusConsumed)
	assert.Equal(t, 20, result.OriginalConsumed)

	require.NoError(t, DB.First(user, user.Id).Error)
	require.NoError(t, DB.First(token, token.Id).Error)
	require.NoError(t, DB.First(task, task.Id).Error)
	assert.Equal(t, 80, user.Quota)
	assert.Equal(t, 80, token.RemainQuota)
	assert.Equal(t, 20, token.UsedQuota)
	assert.Equal(t, "atomic-mj-charge", task.BillingRequestId)
	assert.Equal(t, MidjourneyBillingStatusCharged, task.BillingStatus)
	assert.Equal(t, token.Id, task.TokenId)

	var usage CheckinBonusUsage
	require.NoError(t, DB.Where("request_id = ?", "atomic-mj-charge").First(&usage).Error)
	assert.Equal(t, CheckinBonusUsageStatusSettled, usage.Status)
	assert.Zero(t, usage.BonusId)
}

func TestImmediateChargeRollsBackWhenMidjourneyMetadataCannotCommit(t *testing.T) {
	truncateTables(t)
	user := &User{Id: 702, Username: "mj-rollback", AffCode: "mj-rollback", Quota: 100}
	token := &Token{Id: 92, UserId: user.Id, Key: "mj-rollback", RemainQuota: 100}
	task := &Midjourney{
		UserId: user.Id, MjId: "not-pending", Quota: 20,
		BillingStatus: MidjourneyBillingStatusCharged,
	}
	require.NoError(t, DB.Create(user).Error)
	require.NoError(t, DB.Create(token).Error)
	require.NoError(t, DB.Create(task).Error)

	_, err := ConsumeCheckinBonusFunding(CheckinBonusImmediateChargeParams{
		UserId: user.Id, RequestId: "rolled-back-mj-charge", Amount: 20,
		OriginalFundingSource: CheckinBonusOriginalFundingWallet,
		TokenId:               token.Id, TokenKey: token.Key, DeductToken: true,
		MidjourneyTaskId: task.Id, Now: time.Now(),
	})
	require.Error(t, err)

	require.NoError(t, DB.First(user, user.Id).Error)
	require.NoError(t, DB.First(token, token.Id).Error)
	assert.Equal(t, 100, user.Quota)
	assert.Equal(t, 100, token.RemainQuota)
	assert.Zero(t, token.UsedQuota)
	var usageCount int64
	require.NoError(t, DB.Model(&CheckinBonusUsage{}).
		Where("request_id = ?", "rolled-back-mj-charge").Count(&usageCount).Error)
	assert.Zero(t, usageCount)
}

func TestImmediateChargeRejectsWalletRemainderAfterBonusChanged(t *testing.T) {
	truncateTables(t)
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.FixedZone("CST", 8*3600))
	user := &User{Id: 707, Username: "mj-insufficient-wallet", AffCode: "mj-insufficient-wallet", Quota: 0}
	token := &Token{Id: 97, UserId: user.Id, Key: "mj-insufficient-wallet", RemainQuota: 40}
	require.NoError(t, DB.Create(user).Error)
	require.NoError(t, DB.Create(token).Error)
	bonus := seedCheckinBonus(t, user.Id, 20, now, now.Add(time.Hour))

	_, err := ConsumeCheckinBonusFunding(CheckinBonusImmediateChargeParams{
		UserId: user.Id, RequestId: "mj-first-bonus-charge", Amount: 20,
		OriginalFundingSource: CheckinBonusOriginalFundingWallet,
		TokenId:               token.Id, TokenKey: token.Key, DeductToken: true, Now: now,
	})
	require.NoError(t, err)

	_, err = ConsumeCheckinBonusFunding(CheckinBonusImmediateChargeParams{
		UserId: user.Id, RequestId: "mj-insufficient-wallet", Amount: 20,
		OriginalFundingSource: CheckinBonusOriginalFundingWallet,
		TokenId:               token.Id, TokenKey: token.Key, DeductToken: true, Now: now,
	})
	require.ErrorContains(t, err, "insufficient user quota for immediate charge")

	require.NoError(t, DB.First(user, user.Id).Error)
	require.NoError(t, DB.First(token, token.Id).Error)
	require.NoError(t, DB.First(bonus, bonus.Id).Error)
	assert.Zero(t, user.Quota)
	assert.Zero(t, bonus.RemainingAmount)
	assert.Equal(t, 20, token.RemainQuota)
	assert.Equal(t, 20, token.UsedQuota)
	var usageCount int64
	require.NoError(t, DB.Model(&CheckinBonusUsage{}).
		Where("request_id = ?", "mj-insufficient-wallet").Count(&usageCount).Error)
	assert.Zero(t, usageCount)
}

func TestRefundImmediateChargeRestoresTokenExactlyOnce(t *testing.T) {
	truncateTables(t)
	now := time.Now()
	user := &User{Id: 703, Username: "mj-refund", AffCode: "mj-refund", Quota: 100}
	token := &Token{Id: 93, UserId: user.Id, Key: "mj-refund", RemainQuota: 100}
	require.NoError(t, DB.Create(user).Error)
	require.NoError(t, DB.Create(token).Error)

	_, err := ConsumeCheckinBonusFunding(CheckinBonusImmediateChargeParams{
		UserId: user.Id, RequestId: "atomic-mj-refund", Amount: 20,
		OriginalFundingSource: CheckinBonusOriginalFundingWallet,
		TokenId:               token.Id, TokenKey: token.Key, DeductToken: true, Now: now,
	})
	require.NoError(t, err)

	bonusRefunded, originalRefunded, tokenRefunded, tokenHandled, handled, err := RefundCheckinBonusFundingUsage("atomic-mj-refund", now)
	require.NoError(t, err)
	assert.True(t, handled)
	assert.True(t, tokenHandled)
	assert.Zero(t, bonusRefunded)
	assert.Equal(t, 20, originalRefunded)
	assert.Equal(t, 20, tokenRefunded)
	require.NoError(t, DB.First(user, user.Id).Error)
	require.NoError(t, DB.First(token, token.Id).Error)
	assert.Equal(t, 100, user.Quota)
	assert.Equal(t, 100, token.RemainQuota)
	assert.Zero(t, token.UsedQuota)

	_, _, tokenRefunded, tokenHandled, handled, err = RefundCheckinBonusFundingUsage("atomic-mj-refund", now)
	require.NoError(t, err)
	assert.True(t, handled)
	assert.True(t, tokenHandled)
	assert.Zero(t, tokenRefunded)
	require.NoError(t, DB.First(token, token.Id).Error)
	assert.Equal(t, 100, token.RemainQuota)
	assert.Zero(t, token.UsedQuota)
}

func TestRefundImmediateChargeStillRestoresFundingAfterTokenDeletion(t *testing.T) {
	truncateTables(t)
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.FixedZone("CST", 8*3600))
	user := &User{Id: 705, Username: "mj-deleted-token", AffCode: "mj-deleted-token", Quota: 100}
	token := &Token{Id: 95, UserId: user.Id, Key: "mj-deleted-token", RemainQuota: 100}
	require.NoError(t, DB.Create(user).Error)
	require.NoError(t, DB.Create(token).Error)
	bonus := seedCheckinBonus(t, user.Id, 30, now, now.Add(time.Hour))

	_, err := ConsumeCheckinBonusFunding(CheckinBonusImmediateChargeParams{
		UserId: user.Id, RequestId: "deleted-token-refund", Amount: 50,
		OriginalFundingSource: CheckinBonusOriginalFundingWallet,
		TokenId:               token.Id, TokenKey: token.Key, DeductToken: true, Now: now,
	})
	require.NoError(t, err)
	require.NoError(t, DB.Delete(&Token{}, token.Id).Error)

	bonusRefunded, originalRefunded, tokenRefunded, tokenHandled, handled, err := RefundCheckinBonusFundingUsage("deleted-token-refund", now)
	require.NoError(t, err)
	assert.True(t, handled)
	assert.True(t, tokenHandled)
	assert.Equal(t, 30, bonusRefunded)
	assert.Equal(t, 20, originalRefunded)
	assert.Zero(t, tokenRefunded)
	require.NoError(t, DB.First(user, user.Id).Error)
	assert.Equal(t, 100, user.Quota)
	require.NoError(t, DB.First(bonus, bonus.Id).Error)
	assert.Equal(t, 30, bonus.RemainingAmount)
	assert.Equal(t, CheckinBonusStatusActive, bonus.Status)

	var usage CheckinBonusUsage
	require.NoError(t, DB.Where("request_id = ?", "deleted-token-refund").First(&usage).Error)
	assert.Equal(t, CheckinBonusUsageStatusRefunded, usage.Status)
}

func TestFailPendingMidjourneyBillingDoesNotOverwriteChargedTask(t *testing.T) {
	truncateTables(t)
	pending := &Midjourney{UserId: 704, MjId: "pending", Quota: 20, Status: "SUBMITTED", Progress: "0%", BillingStatus: MidjourneyBillingStatusPending, BillingPendingAt: time.Now().UnixMilli()}
	charged := &Midjourney{UserId: 704, MjId: "charged", Quota: 20, Status: "SUBMITTED", Progress: "0%", BillingStatus: MidjourneyBillingStatusCharged}
	require.NoError(t, DB.Create(pending).Error)
	require.NoError(t, DB.Create(charged).Error)

	updated, err := FailPendingMidjourneyBilling(pending.Id, "billing failed")
	require.NoError(t, err)
	assert.True(t, updated)
	require.NoError(t, DB.First(pending, pending.Id).Error)
	assert.Equal(t, MidjourneyBillingStatusFailed, pending.BillingStatus)
	assert.Equal(t, "FAILURE", pending.Status)
	assert.Equal(t, "100%", pending.Progress)
	assert.Zero(t, pending.Quota)
	assert.Zero(t, pending.BillingPendingAt)

	updated, err = FailPendingMidjourneyBilling(charged.Id, "must not overwrite")
	require.NoError(t, err)
	assert.False(t, updated)
	require.NoError(t, DB.First(charged, charged.Id).Error)
	assert.Equal(t, MidjourneyBillingStatusCharged, charged.BillingStatus)
	assert.Equal(t, "SUBMITTED", charged.Status)
	assert.Equal(t, 20, charged.Quota)
}

func TestMidjourneyPollingIncludesOnlyStalePendingBilling(t *testing.T) {
	truncateTables(t)
	now := time.Now().UnixMilli()
	cutoff := now - int64((2*time.Minute)/time.Millisecond)
	freshPending := &Midjourney{UserId: 706, MjId: "fresh-pending", Progress: "0%", BillingStatus: MidjourneyBillingStatusPending, BillingPendingAt: now}
	stalePending := &Midjourney{UserId: 706, MjId: "stale-pending", Progress: "100%", BillingStatus: MidjourneyBillingStatusPending, BillingPendingAt: cutoff - 1}
	legacyPending := &Midjourney{UserId: 706, MjId: "legacy-pending", Progress: "100%", BillingStatus: MidjourneyBillingStatusPending}
	unfinishedCharged := &Midjourney{UserId: 706, MjId: "unfinished-charged", Progress: "50%", BillingStatus: MidjourneyBillingStatusCharged}
	finishedCharged := &Midjourney{UserId: 706, MjId: "finished-charged", Progress: "100%", BillingStatus: MidjourneyBillingStatusCharged}
	for _, task := range []*Midjourney{freshPending, stalePending, legacyPending, unfinishedCharged, finishedCharged} {
		require.NoError(t, DB.Create(task).Error)
	}

	tasks := GetAllUnFinishTasks(cutoff)
	ids := make([]int, 0, len(tasks))
	for _, task := range tasks {
		ids = append(ids, task.Id)
	}
	assert.ElementsMatch(t, []int{stalePending.Id, legacyPending.Id, unfinishedCharged.Id}, ids)
	assert.True(t, HasUnfinishedMidjourneyTasks(cutoff))

	require.NoError(t, DB.Delete(stalePending).Error)
	require.NoError(t, DB.Delete(legacyPending).Error)
	require.NoError(t, DB.Delete(unfinishedCharged).Error)
	assert.False(t, HasUnfinishedMidjourneyTasks(cutoff), "fresh pending billing must remain owned by the live request")
}
