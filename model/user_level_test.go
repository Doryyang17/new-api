package model

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/user_level_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func applyUserLevelConfigForModelTest(t *testing.T, value user_level_setting.UserLevelConfig) {
	t.Helper()
	data, err := common.Marshal(value)
	require.NoError(t, err)
	module := config.GlobalConfig.Get("user_level_setting")
	require.NotNil(t, module)
	require.NoError(t, config.UpdateConfigFromMap(module, map[string]string{
		"config": string(data),
	}))
}

func userLevelClaimConfig() user_level_setting.UserLevelConfig {
	return user_level_setting.UserLevelConfig{
		SchemaVersion: user_level_setting.SchemaVersion,
		Enabled:       true,
		Levels: []user_level_setting.Level{
			{ID: "base", Name: "普通用户", ThresholdQuota: 0, Ratio: 1, BadgeColor: "neutral"},
			{ID: "silver", Name: "白银会员", ThresholdQuota: 500_000, Ratio: 0.8, BadgeColor: "blue"},
			{ID: "gold", Name: "黄金会员", ThresholdQuota: 1_000_000, Ratio: 0.6, BadgeColor: "warning"},
		},
	}
}

func TestInitTaskOnlyTracksLevelProgressAfterFundingSettlement(t *testing.T) {
	relayInfo := &relaycommon.RelayInfo{
		UserId:      901,
		UsingGroup:  "default",
		ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 1},
	}

	unsettled := InitTask(constant.TaskPlatformSuno, relayInfo, false)
	assert.False(t, unsettled.LevelProgressEligible)
	assert.False(t, unsettled.LevelProgressPending)

	settled := InitTask(constant.TaskPlatformSuno, relayInfo, true)
	assert.True(t, settled.LevelProgressEligible)
	assert.True(t, settled.LevelProgressPending)
}

func TestClaimHighestEligibleUserLevelUsesDurableConsumedQuota(t *testing.T) {
	truncateTables(t)
	originalConfig := user_level_setting.GetConfig()
	t.Cleanup(func() { applyUserLevelConfigForModelTest(t, originalConfig) })
	applyUserLevelConfigForModelTest(t, userLevelClaimConfig())

	user := &User{
		Id:              901,
		Username:        "level-user",
		Status:          common.UserStatusEnabled,
		UsedQuota:       9_000_000,
		LevelUsageQuota: 1_100_000,
	}
	require.NoError(t, DB.Create(user).Error)

	result, err := ClaimHighestEligibleUserLevel(user.Id)
	require.NoError(t, err)
	require.True(t, result.Changed)
	assert.Equal(t, "base", result.PreviousLevel.ID)
	assert.Equal(t, "gold", result.CurrentLevel.ID)
	assert.EqualValues(t, 1_100_000, result.ConsumedQuota)

	var reloaded User
	require.NoError(t, DB.Select("level_key").First(&reloaded, user.Id).Error)
	assert.Equal(t, "gold", reloaded.LevelKey)
	var claims []UserLevelClaim
	require.NoError(t, DB.Where("user_id = ?", user.Id).Find(&claims).Error)
	require.Len(t, claims, 1)
	assert.Equal(t, "gold", claims[0].LevelId)
	assert.EqualValues(t, 1_100_000, claims[0].ConsumedQuota)

	result, err = ClaimHighestEligibleUserLevel(user.Id)
	require.NoError(t, err)
	assert.False(t, result.Changed)
	require.NoError(t, DB.Where("user_id = ?", user.Id).Find(&claims).Error)
	assert.Len(t, claims, 1)
}

func TestClaimHighestEligibleUserLevelIgnoresRechargeQuota(t *testing.T) {
	truncateTables(t)
	originalConfig := user_level_setting.GetConfig()
	t.Cleanup(func() { applyUserLevelConfigForModelTest(t, originalConfig) })
	applyUserLevelConfigForModelTest(t, userLevelClaimConfig())

	user := &User{Id: 902, Username: "level-topup-only", Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(user).Error)
	require.NoError(t, DB.Create(&TopUp{
		UserId:        user.Id,
		TradeNo:       "level-topup-does-not-qualify",
		Status:        common.TopUpStatusSuccess,
		CreditedQuota: 9_000_000,
	}).Error)

	result, err := ClaimHighestEligibleUserLevel(user.Id)
	require.NoError(t, err)
	assert.False(t, result.Changed)
	assert.Equal(t, "base", result.CurrentLevel.ID)
}

func TestClaimHighestEligibleUserLevelRejectsWhenDisabled(t *testing.T) {
	truncateTables(t)
	originalConfig := user_level_setting.GetConfig()
	t.Cleanup(func() { applyUserLevelConfigForModelTest(t, originalConfig) })
	disabled := userLevelClaimConfig()
	disabled.Enabled = false
	applyUserLevelConfigForModelTest(t, disabled)

	_, err := ClaimHighestEligibleUserLevel(903)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrUserLevelDisabled))
}

func TestCountUsersByLevelMapsLegacyEmptyKeyToBase(t *testing.T) {
	truncateTables(t)
	users := []User{
		{Id: 911, Username: "base-empty", AffCode: "level-911", Status: common.UserStatusEnabled},
		{Id: 912, Username: "base-explicit", AffCode: "level-912", Status: common.UserStatusEnabled, LevelKey: "base"},
		{Id: 913, Username: "silver", AffCode: "level-913", Status: common.UserStatusEnabled, LevelKey: "silver"},
	}
	require.NoError(t, DB.Create(&users).Error)

	counts, err := CountUsersByLevel()
	require.NoError(t, err)
	assert.EqualValues(t, 2, counts["base"])
	assert.EqualValues(t, 1, counts["silver"])
}

func TestUserLevelConsumedQuotaAdjustmentDoesNotGoBelowZero(t *testing.T) {
	truncateTables(t)
	user := User{Id: 914, Username: "level-refund-floor", AffCode: "level-914", Status: common.UserStatusEnabled, LevelUsageQuota: 100}
	require.NoError(t, DB.Create(&user).Error)

	task := Task{
		TaskID:                "level-refund-floor-task",
		UserId:                user.Id,
		Quota:                 200,
		Status:                TaskStatusFailure,
		LevelProgressEligible: true,
		LevelProgressQuota:    200,
	}
	require.NoError(t, DB.Create(&task).Error)
	changed, err := ReconcileTaskLevelConsumedQuota(task.ID)
	require.NoError(t, err)
	require.True(t, changed)

	_, consumedQuota, err := GetUserLevelProgressFromDB(user.Id)
	require.NoError(t, err)
	assert.Zero(t, consumedQuota)
}

func TestUserLevelConsumedQuotaStaysWithinExactClientRange(t *testing.T) {
	truncateTables(t)
	user := User{
		Id:              918,
		Username:        "level-progress-cap",
		AffCode:         "level-918",
		Status:          common.UserStatusEnabled,
		LevelUsageQuota: user_level_setting.MaxThresholdQuota - 5,
	}
	require.NoError(t, DB.Create(&user).Error)
	require.NoError(t, RecordUserSettledUsage(user.Id, 10))

	_, consumedQuota, err := GetUserLevelProgressFromDB(user.Id)
	require.NoError(t, err)
	assert.Equal(t, user_level_setting.MaxThresholdQuota, consumedQuota)
}

func TestRecordUserSettledUsageUpdatesAccountingCountersTogether(t *testing.T) {
	truncateTables(t)
	user := User{
		Id:              919,
		Username:        "level-settled-usage",
		AffCode:         "level-919",
		Status:          common.UserStatusEnabled,
		UsedQuota:       20,
		LevelUsageQuota: 30,
		RequestCount:    4,
	}
	require.NoError(t, DB.Create(&user).Error)

	require.NoError(t, RecordUserSettledUsage(user.Id, 50))
	var reloaded User
	require.NoError(t, DB.First(&reloaded, user.Id).Error)
	assert.Equal(t, 70, reloaded.UsedQuota)
	assert.EqualValues(t, 80, reloaded.LevelUsageQuota)
	assert.Equal(t, 5, reloaded.RequestCount)

	require.Error(t, RecordUserSettledUsage(user.Id, -1))
	require.NoError(t, DB.First(&reloaded, user.Id).Error)
	assert.Equal(t, 70, reloaded.UsedQuota)
	assert.EqualValues(t, 80, reloaded.LevelUsageQuota)
	assert.Equal(t, 5, reloaded.RequestCount)
}

func TestRecordUserSettledUsageUsesAtomicBatchSnapshot(t *testing.T) {
	truncateTables(t)
	previousBatchUpdateEnabled := common.BatchUpdateEnabled
	common.BatchUpdateEnabled = true
	t.Cleanup(func() {
		batchUpdate()
		common.BatchUpdateEnabled = previousBatchUpdateEnabled
	})

	user := User{
		Id:              920,
		Username:        "level-settled-batch",
		AffCode:         "level-920",
		Status:          common.UserStatusEnabled,
		UsedQuota:       20,
		LevelUsageQuota: 30,
		RequestCount:    4,
	}
	require.NoError(t, DB.Create(&user).Error)
	require.NoError(t, RecordUserSettledUsage(user.Id, 50))

	var reloaded User
	require.NoError(t, DB.First(&reloaded, user.Id).Error)
	assert.Equal(t, 20, reloaded.UsedQuota)
	assert.EqualValues(t, 30, reloaded.LevelUsageQuota)
	assert.Equal(t, 4, reloaded.RequestCount)

	batchUpdate()
	require.NoError(t, DB.First(&reloaded, user.Id).Error)
	assert.Equal(t, 70, reloaded.UsedQuota)
	assert.EqualValues(t, 80, reloaded.LevelUsageQuota)
	assert.Equal(t, 5, reloaded.RequestCount)
}

func TestUnsettledUsageStatisticsDoNotAdvanceLevelProgress(t *testing.T) {
	truncateTables(t)
	user := User{
		Id:              921,
		Username:        "level-exempt-usage",
		AffCode:         "level-921",
		Status:          common.UserStatusEnabled,
		LevelUsageQuota: 30,
	}
	require.NoError(t, DB.Create(&user).Error)

	// Async pre-charges and violation fees use this statistics-only path.
	UpdateUserUsedQuotaAndRequestCount(user.Id, 50)
	var reloaded User
	require.NoError(t, DB.First(&reloaded, user.Id).Error)
	assert.Equal(t, 50, reloaded.UsedQuota)
	assert.EqualValues(t, 30, reloaded.LevelUsageQuota)
	assert.Equal(t, 1, reloaded.RequestCount)
}

func TestPendingTaskCannotUnlockUserLevelBeforeTerminalSuccess(t *testing.T) {
	truncateTables(t)
	originalConfig := user_level_setting.GetConfig()
	t.Cleanup(func() { applyUserLevelConfigForModelTest(t, originalConfig) })
	applyUserLevelConfigForModelTest(t, userLevelClaimConfig())

	user := User{
		Id:        915,
		Username:  "level-pending-task",
		AffCode:   "level-915",
		Status:    common.UserStatusEnabled,
		UsedQuota: 1_100_000,
	}
	require.NoError(t, DB.Create(&user).Error)
	task := Task{
		TaskID:                "level-pending-task",
		UserId:                user.Id,
		Quota:                 1_100_000,
		Status:                TaskStatusInProgress,
		Progress:              "50%",
		LevelProgressEligible: true,
		LevelProgressPending:  true,
	}
	require.NoError(t, DB.Create(&task).Error)

	claim, err := ClaimHighestEligibleUserLevel(user.Id)
	require.NoError(t, err)
	assert.False(t, claim.Changed)
	assert.Zero(t, claim.ConsumedQuota)

	require.NoError(t, DB.Model(&Task{}).Where("id = ?", task.ID).Updates(map[string]interface{}{
		"status":   TaskStatusSuccess,
		"progress": "100%",
	}).Error)
	assert.True(t, HasPendingTaskLevelProgress())
	reconciled, err := ReconcilePendingTaskLevelProgress(10)
	require.NoError(t, err)
	assert.Equal(t, 1, reconciled)
	assert.False(t, HasPendingTaskLevelProgress())
	changed, err := ReconcileTaskLevelConsumedQuota(task.ID)
	require.NoError(t, err)
	assert.False(t, changed, "terminal reconciliation must be idempotent")

	claim, err = ClaimHighestEligibleUserLevel(user.Id)
	require.NoError(t, err)
	require.True(t, claim.Changed)
	assert.Equal(t, "gold", claim.CurrentLevel.ID)
}

func TestMidjourneyLevelProgressFollowsTerminalStateIdempotently(t *testing.T) {
	truncateTables(t)
	user := User{Id: 917, Username: "level-midjourney", AffCode: "level-917", Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(&user).Error)
	task := Midjourney{
		UserId:                user.Id,
		MjId:                  "level-midjourney-task",
		Quota:                 600,
		Status:                "SUBMITTED",
		Progress:              "20%",
		BillingStatus:         MidjourneyBillingStatusCharged,
		LevelProgressEligible: true,
		LevelProgressPending:  true,
	}
	require.NoError(t, DB.Create(&task).Error)

	changed, err := ReconcileMidjourneyLevelConsumedQuota(task.Id)
	require.NoError(t, err)
	assert.False(t, changed)

	require.NoError(t, DB.Model(&Midjourney{}).Where("id = ?", task.Id).Updates(map[string]interface{}{
		"status":   string(TaskStatusSuccess),
		"progress": "100%",
	}).Error)
	assert.True(t, HasPendingMidjourneyLevelProgress())
	reconciled, err := ReconcilePendingMidjourneyLevelProgress(10)
	require.NoError(t, err)
	assert.Equal(t, 1, reconciled)
	assert.False(t, HasPendingMidjourneyLevelProgress())
	changed, err = ReconcileMidjourneyLevelConsumedQuota(task.Id)
	require.NoError(t, err)
	assert.False(t, changed)

	require.NoError(t, DB.First(&task, task.Id).Error)
	task.Status = string(TaskStatusFailure)
	task.BillingStatus = MidjourneyBillingStatusRefunded
	won, err := task.UpdateWithStatus(string(TaskStatusSuccess))
	require.NoError(t, err)
	require.True(t, won)
	assert.True(t, HasPendingMidjourneyLevelProgress())
	reconciled, err = ReconcilePendingMidjourneyLevelProgress(10)
	require.NoError(t, err)
	require.Equal(t, 1, reconciled)

	var reloaded User
	require.NoError(t, DB.First(&reloaded, user.Id).Error)
	assert.Zero(t, reloaded.LevelUsageQuota)
}

func TestTerminalTaskPendingMarkerClearsAndRearmsOnQuotaChange(t *testing.T) {
	truncateTables(t)
	user := User{Id: 922, Username: "level-pending-marker", AffCode: "level-922", Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(&user).Error)
	task := Task{
		TaskID:                "level-pending-marker-task",
		UserId:                user.Id,
		Quota:                 0,
		Status:                TaskStatusSuccess,
		Progress:              "100%",
		LevelProgressEligible: true,
		LevelProgressPending:  true,
	}
	require.NoError(t, DB.Create(&task).Error)

	assert.True(t, HasPendingTaskLevelProgress())
	changed, err := ReconcileTaskLevelConsumedQuota(task.ID)
	require.NoError(t, err)
	require.True(t, changed, "zero-quota terminal tasks must leave the pending index")
	require.NoError(t, DB.First(&task, task.ID).Error)
	assert.False(t, task.LevelProgressPending)
	assert.False(t, HasPendingTaskLevelProgress())

	task.Quota = 150
	require.NoError(t, task.UpdateQuota())
	assert.True(t, HasPendingTaskLevelProgress())
	reconciled, err := ReconcilePendingTaskLevelProgress(10)
	require.NoError(t, err)
	require.Equal(t, 1, reconciled)
	require.NoError(t, DB.First(&task, task.ID).Error)
	assert.False(t, task.LevelProgressPending)

	var reloaded User
	require.NoError(t, DB.First(&reloaded, user.Id).Error)
	assert.EqualValues(t, 150, reloaded.LevelUsageQuota)
}

func TestTerminalStateUpdatesPreserveReconciledProgressSnapshot(t *testing.T) {
	truncateTables(t)
	user := User{Id: 923, Username: "level-stale-state", AffCode: "level-923", Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(&user).Error)

	task := Task{
		TaskID:                "level-stale-state-task",
		UserId:                user.Id,
		Quota:                 150,
		Status:                TaskStatusSuccess,
		Progress:              "100%",
		LevelProgressEligible: true,
		LevelProgressPending:  true,
	}
	require.NoError(t, DB.Create(&task).Error)
	_, err := ReconcileTaskLevelConsumedQuota(task.ID)
	require.NoError(t, err)

	// A polling retry may hold a terminal object loaded before reconciliation.
	staleTask := task
	staleTask.LevelProgressQuota = 0
	staleTask.LevelProgressPending = true
	won, err := staleTask.UpdateWithStatus(TaskStatusSuccess)
	require.NoError(t, err)
	require.True(t, won)
	_, err = ReconcileTaskLevelConsumedQuota(task.ID)
	require.NoError(t, err)

	var reloaded User
	require.NoError(t, DB.First(&reloaded, user.Id).Error)
	assert.EqualValues(t, 150, reloaded.LevelUsageQuota)
	require.NoError(t, DB.First(&task, task.ID).Error)
	assert.EqualValues(t, 150, task.LevelProgressQuota)

	midjourney := Midjourney{
		UserId:                user.Id,
		MjId:                  "level-stale-state-mj",
		Quota:                 175,
		Status:                string(TaskStatusSuccess),
		Progress:              "100%",
		BillingStatus:         MidjourneyBillingStatusCharged,
		LevelProgressEligible: true,
		LevelProgressPending:  true,
	}
	require.NoError(t, DB.Create(&midjourney).Error)
	_, err = ReconcileMidjourneyLevelConsumedQuota(midjourney.Id)
	require.NoError(t, err)

	staleMidjourney := midjourney
	staleMidjourney.LevelProgressQuota = 0
	staleMidjourney.LevelProgressPending = true
	won, err = staleMidjourney.UpdateWithStatus(string(TaskStatusSuccess))
	require.NoError(t, err)
	require.True(t, won)
	_, err = ReconcileMidjourneyLevelConsumedQuota(midjourney.Id)
	require.NoError(t, err)

	require.NoError(t, DB.First(&reloaded, user.Id).Error)
	assert.EqualValues(t, 325, reloaded.LevelUsageQuota)
	require.NoError(t, DB.First(&midjourney, midjourney.Id).Error)
	assert.EqualValues(t, 175, midjourney.LevelProgressQuota)
}

func TestSaveUserLevelConfigRejectsStaleRevisionAgainstDatabaseCurrentConfig(t *testing.T) {
	truncateTables(t)
	originalConfig := user_level_setting.GetConfig()
	t.Cleanup(func() { applyUserLevelConfigForModelTest(t, originalConfig) })

	databaseConfig := userLevelClaimConfig()
	data, err := common.Marshal(databaseConfig)
	require.NoError(t, err)
	require.NoError(t, DB.Create(&Option{Key: user_level_setting.OptionKey, Value: string(data)}).Error)
	applyUserLevelConfigForModelTest(t, user_level_setting.DefaultConfig())
	staleRevision, err := user_level_setting.ConfigRevision(user_level_setting.DefaultConfig())
	require.NoError(t, err)

	_, _, err = SaveUserLevelConfig(user_level_setting.DefaultConfig(), staleRevision)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUserLevelConfigConflict)

	var persisted Option
	require.NoError(t, DB.Where("key = ?", user_level_setting.OptionKey).First(&persisted).Error)
	parsed, err := user_level_setting.ParseConfigJSON([]byte(persisted.Value))
	require.NoError(t, err)
	assert.Equal(t, []string{"base", "silver", "gold"}, []string{
		parsed.Levels[0].ID,
		parsed.Levels[1].ID,
		parsed.Levels[2].ID,
	})
}

func TestSaveUserLevelConfigPreventsStaleNonStructuralOverwrite(t *testing.T) {
	truncateTables(t)
	originalConfig := user_level_setting.GetConfig()
	t.Cleanup(func() { applyUserLevelConfigForModelTest(t, originalConfig) })
	common.OptionMapRWMutex.Lock()
	originalOptionMap := common.OptionMap
	common.OptionMap = make(map[string]string)
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = originalOptionMap
		common.OptionMapRWMutex.Unlock()
	})

	staleConfig := userLevelClaimConfig()
	staleRevision, err := user_level_setting.ConfigRevision(staleConfig)
	require.NoError(t, err)
	databaseConfig := userLevelClaimConfig()
	databaseConfig.Levels[1].Ratio = 0.75
	data, err := common.Marshal(databaseConfig)
	require.NoError(t, err)
	require.NoError(t, DB.Create(&Option{Key: user_level_setting.OptionKey, Value: string(data)}).Error)

	desired := staleConfig
	desired.Levels[2].Name = "黄金等级"
	_, _, err = SaveUserLevelConfig(desired, staleRevision)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUserLevelConfigConflict)

	current, currentRevision, err := GetUserLevelConfigSnapshot()
	require.NoError(t, err)
	assert.Equal(t, 0.75, current.Levels[1].Ratio)
	assert.Equal(t, databaseConfig.Levels[2].Name, current.Levels[2].Name)

	desired = current
	desired.Levels[2].Name = "黄金等级"
	_, saved, err := SaveUserLevelConfig(desired, currentRevision)
	require.NoError(t, err)
	assert.Equal(t, "黄金等级", saved.Levels[2].Name)
	assert.Equal(t, 0.75, saved.Levels[1].Ratio)
}

func TestLegacyUsageDoesNotBecomeUserLevelProgress(t *testing.T) {
	truncateTables(t)
	user := User{Id: 916, Username: "level-legacy", AffCode: "level-916", Status: common.UserStatusEnabled, UsedQuota: 1_000}
	require.NoError(t, DB.Create(&user).Error)
	legacyTask := Task{
		TaskID:   "level-legacy-success",
		UserId:   user.Id,
		Quota:    400,
		Status:   TaskStatusSuccess,
		Progress: "100%",
	}
	require.NoError(t, DB.Create(&legacyTask).Error)

	changed, err := ReconcileTaskLevelConsumedQuota(legacyTask.ID)
	require.NoError(t, err)
	assert.False(t, changed)
	_, consumedQuota, err := GetUserLevelProgressFromDB(user.Id)
	require.NoError(t, err)
	assert.Zero(t, consumedQuota)
}
