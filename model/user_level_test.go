package model

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/common"
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
	require.NoError(t, config.UpdateConfigFromMap(module, map[string]string{"config": string(data)}))
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

func TestClaimHighestEligibleUserLevelUsesNativeUsedQuota(t *testing.T) {
	truncateTables(t)
	originalConfig := user_level_setting.GetConfig()
	t.Cleanup(func() { applyUserLevelConfigForModelTest(t, originalConfig) })
	applyUserLevelConfigForModelTest(t, userLevelClaimConfig())

	user := &User{Id: 901, Username: "level-user", Status: common.UserStatusEnabled, UsedQuota: 1_100_000}
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

func TestAsyncTaskStateDoesNotChangeNativeUserLevelProgress(t *testing.T) {
	truncateTables(t)
	originalConfig := user_level_setting.GetConfig()
	t.Cleanup(func() { applyUserLevelConfigForModelTest(t, originalConfig) })
	applyUserLevelConfigForModelTest(t, userLevelClaimConfig())

	user := User{Id: 904, Username: "level-pending", AffCode: "level-904", Status: common.UserStatusEnabled, UsedQuota: 1_300_000}
	require.NoError(t, DB.Create(&user).Error)
	task := Task{TaskID: "level-pending-task", UserId: user.Id, Quota: 200_000, Status: TaskStatusInProgress}
	require.NoError(t, DB.Create(&task).Error)
	midjourney := Midjourney{
		UserId: user.Id, MjId: "level-pending-mj", Quota: 200_000,
		Status: string(TaskStatusInProgress), Progress: "50%", BillingStatus: MidjourneyBillingStatusCharged,
	}
	require.NoError(t, DB.Create(&midjourney).Error)

	result, err := ClaimHighestEligibleUserLevel(user.Id)
	require.NoError(t, err)
	require.True(t, result.Changed)
	assert.Equal(t, "gold", result.CurrentLevel.ID)
	assert.EqualValues(t, 1_300_000, result.ConsumedQuota)

	require.NoError(t, DB.Model(&Task{}).Where("id = ?", task.ID).Update("status", TaskStatusSuccess).Error)
	require.NoError(t, DB.Model(&Midjourney{}).Where("id = ?", midjourney.Id).Updates(map[string]interface{}{
		"status":   string(TaskStatusSuccess),
		"progress": "100%",
	}).Error)
	result, err = ClaimHighestEligibleUserLevel(user.Id)
	require.NoError(t, err)
	assert.False(t, result.Changed)
	assert.Equal(t, "gold", result.CurrentLevel.ID)
	assert.EqualValues(t, 1_300_000, result.ConsumedQuota)
}

func TestClaimHighestEligibleUserLevelIgnoresRechargeQuota(t *testing.T) {
	truncateTables(t)
	originalConfig := user_level_setting.GetConfig()
	t.Cleanup(func() { applyUserLevelConfigForModelTest(t, originalConfig) })
	applyUserLevelConfigForModelTest(t, userLevelClaimConfig())

	user := &User{Id: 902, Username: "level-topup-only", Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(user).Error)
	require.NoError(t, DB.Create(&TopUp{UserId: user.Id, TradeNo: "level-topup-does-not-qualify", Status: common.TopUpStatusSuccess, CreditedQuota: 9_000_000}).Error)

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

func TestGetUserLevelProgressFollowsNativeUsedQuota(t *testing.T) {
	truncateTables(t)
	user := User{Id: 916, Username: "level-historical", AffCode: "level-916", Status: common.UserStatusEnabled, UsedQuota: 1_000}
	require.NoError(t, DB.Create(&user).Error)

	_, consumedQuota, err := GetUserLevelProgressFromDB(user.Id)
	require.NoError(t, err)
	assert.EqualValues(t, 1_000, consumedQuota)

	require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).Update("used_quota", 2_000).Error)
	_, consumedQuota, err = GetUserLevelProgressFromDB(user.Id)
	require.NoError(t, err)
	assert.EqualValues(t, 2_000, consumedQuota)
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
	assert.Equal(t, []string{"base", "silver", "gold"}, []string{parsed.Levels[0].ID, parsed.Levels[1].ID, parsed.Levels[2].ID})
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
