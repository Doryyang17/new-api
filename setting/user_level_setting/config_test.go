package user_level_setting

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func levelConfigForTest(enabled bool) UserLevelConfig {
	return UserLevelConfig{
		SchemaVersion: SchemaVersion,
		Enabled:       enabled,
		Levels: []Level{
			{
				ID:             BaseLevelID,
				Name:           "普通用户",
				ThresholdQuota: 0,
				Ratio:          1,
				BadgeColor:     "neutral",
			},
			{
				ID:             "silver",
				Name:           "白银会员",
				ThresholdQuota: 500_000,
				Ratio:          0.8,
				BadgeColor:     "blue",
			},
			{
				ID:             "gold",
				Name:           "黄金会员",
				ThresholdQuota: 1_000_000,
				Ratio:          0.6,
				BadgeColor:     "warning",
			},
		},
	}
}

func TestNormalizeAndValidateUserLevelConfig(t *testing.T) {
	config := levelConfigForTest(true)
	config.Levels[1], config.Levels[2] = config.Levels[2], config.Levels[1]

	normalized, err := NormalizeAndValidate(config)
	require.NoError(t, err)
	require.Len(t, normalized.Levels, 3)
	assert.Equal(t, []string{BaseLevelID, "silver", "gold"}, []string{
		normalized.Levels[0].ID,
		normalized.Levels[1].ID,
		normalized.Levels[2].ID,
	})

	invalid := levelConfigForTest(true)
	invalid.Levels[2].ThresholdQuota = invalid.Levels[1].ThresholdQuota
	_, err = NormalizeAndValidate(invalid)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "严格递增")

	invalid = levelConfigForTest(true)
	invalid.Levels[2].Ratio = 0.9
	_, err = NormalizeAndValidate(invalid)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "不能高于前一等级")

	invalid = levelConfigForTest(true)
	invalid.Levels[2].ThresholdQuota = MaxThresholdQuota + 1
	_, err = NormalizeAndValidate(invalid)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "不能超过")
}

func TestConfigRevisionUsesNormalizedConfig(t *testing.T) {
	ordered := levelConfigForTest(true)
	reordered := levelConfigForTest(true)
	reordered.Levels[1], reordered.Levels[2] = reordered.Levels[2], reordered.Levels[1]

	orderedRevision, err := ConfigRevision(ordered)
	require.NoError(t, err)
	reorderedRevision, err := ConfigRevision(reordered)
	require.NoError(t, err)
	assert.Equal(t, orderedRevision, reorderedRevision)

	changed := levelConfigForTest(true)
	changed.Levels[1].Ratio = 0.75
	changedRevision, err := ConfigRevision(changed)
	require.NoError(t, err)
	assert.NotEqual(t, orderedRevision, changedRevision)
}

func TestResolveBillingLevelHonorsFeatureSwitch(t *testing.T) {
	original := GetConfig()
	t.Cleanup(func() { currentSettings.Config.Store(original) })

	currentSettings.Config.Store(levelConfigForTest(true))
	level := ResolveBillingLevel("silver")
	assert.True(t, level.Enabled)
	assert.Equal(t, "silver", level.ID)
	assert.Equal(t, 0.8, level.Ratio)

	currentSettings.Config.Store(levelConfigForTest(false))
	level = ResolveBillingLevel("silver")
	assert.False(t, level.Enabled)
	assert.Equal(t, "silver", level.ID)
	assert.Equal(t, 1.0, level.Ratio)

	level = ResolveBillingLevel("missing")
	assert.Equal(t, BaseLevelID, level.ID)
	assert.Equal(t, 1.0, level.Ratio)
}

func TestHighestClaimableLevelSkipsArchivedLevels(t *testing.T) {
	original := GetConfig()
	t.Cleanup(func() { currentSettings.Config.Store(original) })
	config := levelConfigForTest(true)
	config.Levels[1].Archived = true
	currentSettings.Config.Store(config)

	level, ok := HighestClaimableLevel(1_000_000, BaseLevelID)
	require.True(t, ok)
	assert.Equal(t, "gold", level.ID)
}

func TestValidateConfigJSONPreservesSavedLevelIdentity(t *testing.T) {
	original := GetConfig()
	t.Cleanup(func() { currentSettings.Config.Store(original) })
	currentSettings.Config.Store(levelConfigForTest(true))

	next := levelConfigForTest(true)
	next.Levels = next.Levels[:2]
	data, err := common.Marshal(next)
	require.NoError(t, err)

	err = ValidateConfigJSON(string(data))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "不能删除")
}
