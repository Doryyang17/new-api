package service

import (
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/setting/user_level_setting"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type committedSettlementStub struct{}

func (committedSettlementStub) Settle(int) error          { return errors.New("token quota adjustment failed") }
func (committedSettlementStub) Refund(*gin.Context) error { return nil }
func (committedSettlementStub) NeedsRefund() bool         { return false }
func (committedSettlementStub) GetPreConsumedQuota() int  { return 0 }
func (committedSettlementStub) Reserve(int) error         { return nil }
func (committedSettlementStub) FundingSettled() bool      { return true }

type failedSettlementStub struct{}

func (failedSettlementStub) Settle(int) error          { return errors.New("funding settlement failed") }
func (failedSettlementStub) Refund(*gin.Context) error { return nil }
func (failedSettlementStub) NeedsRefund() bool         { return false }
func (failedSettlementStub) GetPreConsumedQuota() int  { return 0 }
func (failedSettlementStub) Reserve(int) error         { return nil }
func (failedSettlementStub) FundingSettled() bool      { return false }

func applyUserLevelConfigForServiceTest(t *testing.T, value user_level_setting.UserLevelConfig) {
	t.Helper()
	data, err := common.Marshal(value)
	require.NoError(t, err)
	module := config.GlobalConfig.Get("user_level_setting")
	require.NotNil(t, module)
	require.NoError(t, config.UpdateConfigFromMap(module, map[string]string{
		"config": string(data),
	}))
}

func userLevelBillingConfig(ratio float64, enabled bool) user_level_setting.UserLevelConfig {
	return user_level_setting.UserLevelConfig{
		SchemaVersion: user_level_setting.SchemaVersion,
		Enabled:       enabled,
		Levels: []user_level_setting.Level{
			{ID: "base", Name: "普通用户", ThresholdQuota: 0, Ratio: 1, BadgeColor: "neutral"},
			{ID: "gold", Name: "黄金会员", ThresholdQuota: 1_000_000, Ratio: ratio, BadgeColor: "warning"},
		},
	}
}

func configureUserLevelBillingTest(t *testing.T) {
	t.Helper()
	originalLevelConfig := user_level_setting.GetConfig()
	originalGroupRatios := ratio_setting.GroupRatio2JSONString()
	originalSpecialRatios := ratio_setting.GroupGroupRatio2JSONString()
	t.Cleanup(func() {
		applyUserLevelConfigForServiceTest(t, originalLevelConfig)
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatios))
		require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(originalSpecialRatios))
	})

	applyUserLevelConfigForServiceTest(t, userLevelBillingConfig(0.8, true))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"premium":1.5,"default":1}`))
	require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(`{"vip":{"premium":1.25}}`))
}

func TestResolveUserGroupRatioMultipliesLevelAfterSpecialGroupRatio(t *testing.T) {
	configureUserLevelBillingTest(t)

	standard := ResolveUserGroupRatio("default", "premium", "gold")
	assert.Equal(t, 1.5, standard.BaseGroupRatio)
	assert.Equal(t, 0.8, standard.UserLevelRatio)
	assert.InDelta(t, 1.2, standard.GroupRatio, 1e-12)
	assert.True(t, standard.HasUserLevel)
	assert.False(t, standard.HasSpecialRatio)

	special := ResolveUserGroupRatio("vip", "premium", "gold")
	assert.Equal(t, 1.25, special.BaseGroupRatio)
	assert.Equal(t, 0.8, special.UserLevelRatio)
	assert.Equal(t, 1.0, special.GroupRatio)
	assert.True(t, special.HasSpecialRatio)
	assert.Equal(t, 1.0, special.GroupSpecialRatio)
}

func TestResolveUserGroupRatioBypassesLevelWhenFeatureDisabled(t *testing.T) {
	configureUserLevelBillingTest(t)
	applyUserLevelConfigForServiceTest(t, userLevelBillingConfig(0.8, false))

	resolved := ResolveUserGroupRatio("default", "premium", "gold")
	assert.Equal(t, 1.5, resolved.BaseGroupRatio)
	assert.Equal(t, 1.0, resolved.UserLevelRatio)
	assert.Equal(t, 1.5, resolved.GroupRatio)
	assert.False(t, resolved.HasUserLevel)
}

func TestResolveGroupRatioFreezesLevelSnapshotForRequest(t *testing.T) {
	configureUserLevelBillingTest(t)
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	relayInfo := &relaycommon.RelayInfo{
		UserGroup:    "default",
		UsingGroup:   "premium",
		UserLevelKey: "gold",
	}

	first := ResolveGroupRatio(ctx, relayInfo)
	assert.InDelta(t, 1.2, first.GroupRatio, 1e-12)
	applyUserLevelConfigForServiceTest(t, userLevelBillingConfig(0.5, true))
	second := ResolveGroupRatio(ctx, relayInfo)

	assert.Equal(t, 0.8, second.UserLevelRatio)
	assert.InDelta(t, 1.2, second.GroupRatio, 1e-12)
}

func TestGenerateMjOtherInfoUsesPerCallUserLevelSnapshot(t *testing.T) {
	relayInfo := &relaycommon.RelayInfo{}
	priceData := hosttypes.PriceData{
		ModelPrice: 0.02,
		GroupRatioInfo: hosttypes.GroupRatioInfo{
			GroupRatio:     1.2,
			BaseGroupRatio: 1.5,
			UserLevelID:    "gold",
			UserLevelName:  "黄金会员",
			UserLevelRatio: 0.8,
			UserLevelColor: "warning",
			HasUserLevel:   true,
		},
	}

	other := GenerateMjOtherInfo(relayInfo, priceData)

	assert.Equal(t, 1.5, other["base_group_ratio"])
	assert.Equal(t, "gold", other["user_level_id"])
	assert.Equal(t, "黄金会员", other["user_level_name"])
	assert.Equal(t, 0.8, other["user_level_ratio"])
	assert.Equal(t, "warning", other["user_level_color"])
}

func TestGetUserLevelStatusBuildsClaimAndProgressFromConsumedQuota(t *testing.T) {
	truncate(t)
	originalConfig := user_level_setting.GetConfig()
	t.Cleanup(func() { applyUserLevelConfigForServiceTest(t, originalConfig) })
	config := user_level_setting.UserLevelConfig{
		SchemaVersion: user_level_setting.SchemaVersion,
		Enabled:       true,
		Levels: []user_level_setting.Level{
			{ID: "base", Name: "普通用户", ThresholdQuota: 0, Ratio: 1, BadgeColor: "neutral"},
			{ID: "silver", Name: "白银会员", ThresholdQuota: 500_000, Ratio: 0.8, BadgeColor: "blue"},
			{ID: "gold", Name: "黄金会员", ThresholdQuota: 1_000_000, Ratio: 0.6, BadgeColor: "warning"},
		},
	}
	applyUserLevelConfigForServiceTest(t, config)
	user := &model.User{
		Id:              951,
		Username:        "level-status",
		AffCode:         "level-status-951",
		Status:          common.UserStatusEnabled,
		LevelKey:        "silver",
		LevelUsageQuota: 750_000,
	}
	require.NoError(t, model.DB.Create(user).Error)

	status, err := GetUserLevelStatus(user.Id)
	require.NoError(t, err)
	assert.Equal(t, "silver", status.CurrentLevel.ID)
	require.NotNil(t, status.NextLevel)
	assert.Equal(t, "gold", status.NextLevel.ID)
	assert.Nil(t, status.ClaimableLevel)
	assert.EqualValues(t, 250_000, status.Progress.Remaining)
	assert.Equal(t, 50.0, status.Progress.Percent)

	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", user.Id).Update("level_consumed_quota", 1_050_000).Error)
	status, err = GetUserLevelStatus(user.Id)
	require.NoError(t, err)
	require.NotNil(t, status.ClaimableLevel)
	assert.Equal(t, "gold", status.ClaimableLevel.ID)
	assert.EqualValues(t, 1_050_000, status.TotalConsumedQuota)
	assert.Zero(t, status.Progress.Remaining)
}

func TestCommittedSettlementStillRecordsUserLevelUsageWhenTokenAdjustmentFails(t *testing.T) {
	truncate(t)
	const userID = 952
	const channelID = 952
	seedUser(t, userID, 1_000)
	seedChannel(t, channelID)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	relayInfo := &relaycommon.RelayInfo{
		UserId:      userID,
		ChannelMeta: &relaycommon.ChannelMeta{ChannelId: channelID},
		Billing:     committedSettlementStub{},
	}

	settleBillingAndRecordUsage(ctx, relayInfo, 75, true)

	var user model.User
	require.NoError(t, model.DB.First(&user, userID).Error)
	assert.Equal(t, 75, user.UsedQuota)
	assert.EqualValues(t, 75, user.LevelUsageQuota)
	assert.Equal(t, 1, user.RequestCount)
}

func TestFailedSettlementPreservesUsageStatsWithoutAdvancingUserLevel(t *testing.T) {
	truncate(t)
	const userID = 953
	const channelID = 953
	seedUser(t, userID, 1_000)
	seedChannel(t, channelID)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	relayInfo := &relaycommon.RelayInfo{
		UserId:      userID,
		ChannelMeta: &relaycommon.ChannelMeta{ChannelId: channelID},
		Billing:     failedSettlementStub{},
	}

	settleBillingAndRecordUsage(ctx, relayInfo, 75, true)

	var user model.User
	require.NoError(t, model.DB.First(&user, userID).Error)
	assert.Equal(t, 75, user.UsedQuota)
	assert.Zero(t, user.LevelUsageQuota)
	assert.Equal(t, 1, user.RequestCount)

	var channel model.Channel
	require.NoError(t, model.DB.First(&channel, channelID).Error)
	assert.EqualValues(t, 75, channel.UsedQuota)
}
