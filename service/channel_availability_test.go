package service

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupChannelAvailabilityServiceTest(t *testing.T) {
	t.Helper()
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	originalCachePending := channelAvailabilityCachePending.Load()
	common.MemoryCacheEnabled = false
	channelAvailabilityCachePending.Store(false)
	require.NoError(t, model.DB.AutoMigrate(&model.Ability{}))
	require.NoError(t, model.DB.Exec("DELETE FROM abilities").Error)
	require.NoError(t, model.DB.Exec("DELETE FROM channels").Error)
	t.Cleanup(func() {
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
		channelAvailabilityCachePending.Store(originalCachePending)
		require.NoError(t, model.DB.Exec("DELETE FROM abilities").Error)
		require.NoError(t, model.DB.Exec("DELETE FROM channels").Error)
	})
}

func TestUpdateChannelAvailabilityScheduleAppliesCurrentWindow(t *testing.T) {
	setupChannelAvailabilityServiceTest(t)
	location, err := time.LoadLocation(dto.DefaultChannelAvailabilityTimezone)
	require.NoError(t, err)
	statusInfo, err := common.Marshal(map[string]any{
		"status_reason": "manual operation",
		"status_time":   time.Date(2026, 7, 28, 7, 0, 0, 0, location).Unix(),
	})
	require.NoError(t, err)
	channel := &model.Channel{
		Name:      "scheduled channel",
		Key:       "sk-test",
		Status:    common.ChannelStatusManuallyDisabled,
		Models:    "gpt-test",
		Group:     "default",
		OtherInfo: string(statusInfo),
	}
	require.NoError(t, model.DB.Create(channel).Error)
	require.NoError(t, model.DB.Create(&model.Ability{
		Group:     "default",
		Model:     "gpt-test",
		ChannelId: channel.Id,
		Enabled:   false,
	}).Error)

	now := time.Date(2026, 7, 28, 9, 0, 0, 0, location)
	result, err := UpdateChannelAvailabilitySchedule(channel.Id, dto.ChannelAvailabilitySchedule{Enabled: true}, now)
	require.NoError(t, err)
	assert.True(t, result.InWindow)
	assert.True(t, result.StatusChanged)
	assert.Equal(t, common.ChannelStatusEnabled, result.Status)
	assert.Equal(t, time.Date(2026, 7, 28, 12, 0, 0, 0, location).Unix(), result.NextTransitionAt)

	persisted, err := model.GetChannelById(channel.Id, true)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusEnabled, persisted.Status)
	settings := persisted.GetOtherSettings()
	require.NotNil(t, settings.AvailabilitySchedule)
	assert.Equal(t, dto.DefaultChannelAvailabilityStart, settings.AvailabilitySchedule.Start)
	assert.Equal(t, dto.DefaultChannelAvailabilityEnd, settings.AvailabilitySchedule.End)
	var ability model.Ability
	require.NoError(t, model.DB.Where("channel_id = ?", channel.Id).First(&ability).Error)
	assert.True(t, ability.Enabled)

	disabled, err := UpdateChannelAvailabilitySchedule(channel.Id, dto.ChannelAvailabilitySchedule{Enabled: false}, time.Date(2026, 7, 28, 13, 0, 0, 0, location))
	require.NoError(t, err)
	assert.False(t, disabled.InWindow)
	assert.False(t, disabled.StatusChanged)
	assert.Zero(t, disabled.NextTransitionAt)
	assert.Equal(t, common.ChannelStatusEnabled, disabled.Status)
}

func TestUpdateChannelAvailabilitySchedulePreservesCurrentWindowDisable(t *testing.T) {
	setupChannelAvailabilityServiceTest(t)
	location, err := time.LoadLocation(dto.DefaultChannelAvailabilityTimezone)
	require.NoError(t, err)
	schedule := dto.ChannelAvailabilitySchedule{
		Enabled:  true,
		Start:    "08:00",
		End:      "12:00",
		Timezone: dto.DefaultChannelAvailabilityTimezone,
	}
	statusInfo, err := common.Marshal(map[string]any{
		"status_reason": "manual operation",
		"status_time":   time.Date(2026, 7, 28, 9, 30, 0, 0, location).Unix(),
	})
	require.NoError(t, err)
	channel := &model.Channel{
		Name:      "manually disabled in window",
		Key:       "sk-test",
		Status:    common.ChannelStatusManuallyDisabled,
		Models:    "gpt-test",
		Group:     "default",
		OtherInfo: string(statusInfo),
	}
	channel.SetOtherSettings(dto.ChannelOtherSettings{AvailabilitySchedule: &schedule})
	require.NoError(t, model.DB.Create(channel).Error)

	updatedSchedule := schedule
	updatedSchedule.End = "13:00"
	result, err := UpdateChannelAvailabilitySchedule(
		channel.Id,
		updatedSchedule,
		time.Date(2026, 7, 28, 10, 0, 0, 0, location),
	)
	require.NoError(t, err)
	assert.False(t, result.StatusChanged)
	assert.Equal(t, common.ChannelStatusManuallyDisabled, result.Status)

	persisted, err := model.GetChannelById(channel.Id, true)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusManuallyDisabled, persisted.Status)
	assert.Equal(t, "manual operation", persisted.GetOtherInfo()["status_reason"])
}

func TestUpdateChannelAvailabilityScheduleReopensSchedulerDisabledChannel(t *testing.T) {
	location, err := time.LoadLocation(dto.DefaultChannelAvailabilityTimezone)
	require.NoError(t, err)

	for _, statusReason := range []string{channelAvailabilityClosedReason, channelAvailabilityInvalidReason} {
		t.Run(statusReason, func(t *testing.T) {
			setupChannelAvailabilityServiceTest(t)
			schedule := dto.ChannelAvailabilitySchedule{
				Enabled:  true,
				Start:    "08:00",
				End:      "12:00",
				Timezone: dto.DefaultChannelAvailabilityTimezone,
			}
			statusInfo, err := common.Marshal(map[string]any{
				"status_reason": statusReason,
				"status_time":   time.Date(2026, 7, 28, 12, 0, 0, 0, location).Unix(),
			})
			require.NoError(t, err)
			channel := &model.Channel{
				Name:      "scheduler-disabled channel",
				Key:       "sk-test",
				Status:    common.ChannelStatusManuallyDisabled,
				Models:    "gpt-test",
				Group:     "default",
				OtherInfo: string(statusInfo),
			}
			channel.SetOtherSettings(dto.ChannelOtherSettings{AvailabilitySchedule: &schedule})
			require.NoError(t, model.DB.Create(channel).Error)
			require.NoError(t, model.DB.Create(&model.Ability{
				Group:     "default",
				Model:     "gpt-test",
				ChannelId: channel.Id,
				Enabled:   false,
			}).Error)

			updatedSchedule := schedule
			updatedSchedule.End = "14:00"
			result, err := UpdateChannelAvailabilitySchedule(
				channel.Id,
				updatedSchedule,
				time.Date(2026, 7, 28, 12, 30, 0, 0, location),
			)
			require.NoError(t, err)
			assert.True(t, result.InWindow)
			assert.True(t, result.StatusChanged)
			assert.Equal(t, common.ChannelStatusEnabled, result.Status)

			persisted, err := model.GetChannelById(channel.Id, true)
			require.NoError(t, err)
			assert.Equal(t, common.ChannelStatusEnabled, persisted.Status)
			var ability model.Ability
			require.NoError(t, model.DB.Where("channel_id = ?", channel.Id).First(&ability).Error)
			assert.True(t, ability.Enabled)
		})
	}
}

func TestApplyChannelAvailabilityStatusRejectsStaleSnapshot(t *testing.T) {
	location, err := time.LoadLocation(dto.DefaultChannelAvailabilityTimezone)
	require.NoError(t, err)
	schedule := dto.ChannelAvailabilitySchedule{
		Enabled:  true,
		Start:    "08:00",
		End:      "12:00",
		Timezone: dto.DefaultChannelAvailabilityTimezone,
	}

	t.Run("status changed", func(t *testing.T) {
		setupChannelAvailabilityServiceTest(t)
		statusInfo, err := common.Marshal(map[string]any{
			"status_reason": "failed before opening",
			"status_time":   time.Date(2026, 7, 28, 7, 30, 0, 0, location).Unix(),
		})
		require.NoError(t, err)
		channel := &model.Channel{
			Name:      "stale status",
			Key:       "sk-test",
			Status:    common.ChannelStatusAutoDisabled,
			OtherInfo: string(statusInfo),
		}
		channel.SetOtherSettings(dto.ChannelOtherSettings{AvailabilitySchedule: &schedule})
		require.NoError(t, model.DB.Create(channel).Error)
		stale, err := model.GetChannelById(channel.Id, true)
		require.NoError(t, err)
		require.True(t, model.UpdateChannelStatus(channel.Id, "", common.ChannelStatusManuallyDisabled, "manual operation"))

		changed, matched, err := applyChannelAvailabilityStatus(stale, common.ChannelStatusEnabled, channelAvailabilityOpenReason)
		require.NoError(t, err)
		assert.False(t, changed)
		assert.False(t, matched)
		assert.True(t, channelAvailabilityCachePending.Load())
		persisted, err := model.GetChannelById(channel.Id, true)
		require.NoError(t, err)
		assert.Equal(t, common.ChannelStatusManuallyDisabled, persisted.Status)
		assert.Equal(t, "manual operation", persisted.GetOtherInfo()["status_reason"])
	})

	t.Run("schedule changed", func(t *testing.T) {
		setupChannelAvailabilityServiceTest(t)
		channel := &model.Channel{Name: "stale schedule", Key: "sk-test", Status: common.ChannelStatusEnabled}
		channel.SetOtherSettings(dto.ChannelOtherSettings{AvailabilitySchedule: &schedule})
		require.NoError(t, model.DB.Create(channel).Error)
		stale, err := model.GetChannelById(channel.Id, true)
		require.NoError(t, err)
		_, err = model.MutateChannelOtherSettings(channel.Id, func(settings *dto.ChannelOtherSettings) error {
			updated := schedule
			updated.End = "13:00"
			settings.AvailabilitySchedule = &updated
			return nil
		})
		require.NoError(t, err)

		changed, matched, err := applyChannelAvailabilityStatus(stale, common.ChannelStatusManuallyDisabled, channelAvailabilityClosedReason)
		require.NoError(t, err)
		assert.False(t, changed)
		assert.False(t, matched)
		assert.True(t, channelAvailabilityCachePending.Load())
		persisted, err := model.GetChannelById(channel.Id, true)
		require.NoError(t, err)
		assert.Equal(t, common.ChannelStatusEnabled, persisted.Status)
	})

	t.Run("multi-key availability changed", func(t *testing.T) {
		setupChannelAvailabilityServiceTest(t)
		statusInfo, err := common.Marshal(map[string]any{
			"status_reason": channelAvailabilityClosedReason,
			"status_time":   time.Date(2026, 7, 27, 12, 0, 0, 0, location).Unix(),
		})
		require.NoError(t, err)
		channel := &model.Channel{
			Name:      "stale multi-key availability",
			Key:       "key-one\nkey-two",
			Status:    common.ChannelStatusManuallyDisabled,
			Models:    "gpt-test",
			Group:     "default",
			OtherInfo: string(statusInfo),
			ChannelInfo: model.ChannelInfo{
				IsMultiKey:         true,
				MultiKeySize:       2,
				MultiKeyStatusList: map[int]int{1: common.ChannelStatusAutoDisabled},
			},
		}
		channel.SetOtherSettings(dto.ChannelOtherSettings{AvailabilitySchedule: &schedule})
		require.NoError(t, model.DB.Create(channel).Error)
		require.NoError(t, model.DB.Create(&model.Ability{
			Group:     "default",
			Model:     "gpt-test",
			ChannelId: channel.Id,
			Enabled:   false,
		}).Error)

		stale, err := model.GetChannelById(channel.Id, true)
		require.NoError(t, err)
		latestInfo := stale.ChannelInfo
		latestInfo.MultiKeyStatusList = map[int]int{
			0: common.ChannelStatusAutoDisabled,
			1: common.ChannelStatusAutoDisabled,
		}
		require.NoError(t, model.DB.Model(&model.Channel{}).
			Where("id = ?", channel.Id).
			Update("channel_info", latestInfo).Error)

		changed, matched, err := applyChannelAvailabilityStatus(stale, common.ChannelStatusEnabled, channelAvailabilityOpenReason)
		require.NoError(t, err)
		assert.False(t, changed)
		assert.False(t, matched)
		assert.True(t, channelAvailabilityCachePending.Load())
		persisted, err := model.GetChannelById(channel.Id, true)
		require.NoError(t, err)
		assert.Equal(t, common.ChannelStatusManuallyDisabled, persisted.Status)
		assert.Equal(t, latestInfo.MultiKeyStatusList, persisted.ChannelInfo.MultiKeyStatusList)
		var ability model.Ability
		require.NoError(t, model.DB.Where("channel_id = ?", channel.Id).First(&ability).Error)
		assert.False(t, ability.Enabled)
	})
}

func TestChannelAvailabilityReconciliationPreservesCurrentWindowFailures(t *testing.T) {
	setupChannelAvailabilityServiceTest(t)
	location, err := time.LoadLocation(dto.DefaultChannelAvailabilityTimezone)
	require.NoError(t, err)
	schedule := dto.ChannelAvailabilitySchedule{
		Enabled:  true,
		Start:    "08:00",
		End:      "12:00",
		Timezone: dto.DefaultChannelAvailabilityTimezone,
	}

	for _, status := range []int{common.ChannelStatusManuallyDisabled, common.ChannelStatusAutoDisabled} {
		statusInfo, err := common.Marshal(map[string]any{
			"status_reason": "failed during current window",
			"status_time":   time.Date(2026, 7, 28, 9, 30, 0, 0, location).Unix(),
		})
		require.NoError(t, err)
		channel := &model.Channel{
			Name:      "current-window failure",
			Key:       "sk-test",
			Status:    status,
			Models:    "gpt-test",
			Group:     "default",
			OtherInfo: string(statusInfo),
		}
		channel.SetOtherSettings(dto.ChannelOtherSettings{AvailabilitySchedule: &schedule})
		require.NoError(t, model.DB.Create(channel).Error)
	}

	runChannelAvailabilityReconciliation(time.Date(2026, 7, 28, 10, 0, 0, 0, location))
	var channels []model.Channel
	require.NoError(t, model.DB.Order("id asc").Find(&channels).Error)
	require.Len(t, channels, 2)
	assert.Equal(t, common.ChannelStatusManuallyDisabled, channels[0].Status)
	assert.Equal(t, common.ChannelStatusAutoDisabled, channels[1].Status)
}

func TestChannelAvailabilityReconciliationCatchesMissedBoundaries(t *testing.T) {
	location, err := time.LoadLocation(dto.DefaultChannelAvailabilityTimezone)
	require.NoError(t, err)
	schedule := dto.ChannelAvailabilitySchedule{
		Enabled:  true,
		Start:    "08:00",
		End:      "12:00",
		Timezone: dto.DefaultChannelAvailabilityTimezone,
	}

	t.Run("missed opening", func(t *testing.T) {
		setupChannelAvailabilityServiceTest(t)
		statusInfo, err := common.Marshal(map[string]any{
			"status_reason": "failed before opening",
			"status_time":   time.Date(2026, 7, 28, 7, 30, 0, 0, location).Unix(),
		})
		require.NoError(t, err)
		channel := &model.Channel{Name: "open", Key: "sk", Status: common.ChannelStatusAutoDisabled, OtherInfo: string(statusInfo)}
		channel.SetOtherSettings(dto.ChannelOtherSettings{AvailabilitySchedule: &schedule})
		require.NoError(t, model.DB.Create(channel).Error)

		runChannelAvailabilityReconciliation(time.Date(2026, 7, 28, 9, 0, 0, 0, location))
		persisted, err := model.GetChannelById(channel.Id, false)
		require.NoError(t, err)
		assert.Equal(t, common.ChannelStatusEnabled, persisted.Status)
	})

	t.Run("missed closing", func(t *testing.T) {
		setupChannelAvailabilityServiceTest(t)
		channel := &model.Channel{Name: "close", Key: "sk", Status: common.ChannelStatusEnabled}
		channel.SetOtherSettings(dto.ChannelOtherSettings{AvailabilitySchedule: &schedule})
		require.NoError(t, model.DB.Create(channel).Error)

		runChannelAvailabilityReconciliation(time.Date(2026, 7, 28, 12, 5, 0, 0, location))
		persisted, err := model.GetChannelById(channel.Id, false)
		require.NoError(t, err)
		assert.Equal(t, common.ChannelStatusManuallyDisabled, persisted.Status)
		assert.Equal(t, channelAvailabilityClosedReason, persisted.GetOtherInfo()["status_reason"])
	})
}

func TestChannelAvailabilityOpeningPreservesMultiKeyHealth(t *testing.T) {
	location, err := time.LoadLocation(dto.DefaultChannelAvailabilityTimezone)
	require.NoError(t, err)

	t.Run("opens when at least one key remains enabled", func(t *testing.T) {
		setupChannelAvailabilityServiceTest(t)
		channel := &model.Channel{
			Name:   "partially available multi-key schedule",
			Key:    "key-one\nkey-two",
			Status: common.ChannelStatusManuallyDisabled,
			ChannelInfo: model.ChannelInfo{
				IsMultiKey:         true,
				MultiKeySize:       2,
				MultiKeyStatusList: map[int]int{1: common.ChannelStatusAutoDisabled},
			},
		}
		require.NoError(t, model.DB.Create(channel).Error)

		_, err := UpdateChannelAvailabilitySchedule(channel.Id, dto.ChannelAvailabilitySchedule{Enabled: true}, time.Date(2026, 7, 28, 9, 0, 0, 0, location))
		require.NoError(t, err)
		persisted, err := model.GetChannelById(channel.Id, true)
		require.NoError(t, err)
		assert.Equal(t, common.ChannelStatusEnabled, persisted.Status)
		assert.Equal(t, map[int]int{1: common.ChannelStatusAutoDisabled}, persisted.ChannelInfo.MultiKeyStatusList)
	})

	t.Run("keeps all-disabled keys out of the routing pool", func(t *testing.T) {
		setupChannelAvailabilityServiceTest(t)
		schedule := dto.ChannelAvailabilitySchedule{
			Enabled:  true,
			Start:    "08:00",
			End:      "12:00",
			Timezone: dto.DefaultChannelAvailabilityTimezone,
		}
		statusInfo, err := common.Marshal(map[string]any{
			"status_reason": channelAvailabilityClosedReason,
			"status_time":   time.Date(2026, 7, 27, 12, 0, 0, 0, location).Unix(),
		})
		require.NoError(t, err)
		channel := &model.Channel{
			Name:      "unavailable multi-key schedule",
			Key:       "key-one\nkey-two",
			Status:    common.ChannelStatusManuallyDisabled,
			Models:    "gpt-test",
			Group:     "default",
			OtherInfo: string(statusInfo),
			ChannelInfo: model.ChannelInfo{
				IsMultiKey:   true,
				MultiKeySize: 2,
				MultiKeyStatusList: map[int]int{
					0: common.ChannelStatusAutoDisabled,
					1: common.ChannelStatusAutoDisabled,
				},
			},
		}
		channel.SetOtherSettings(dto.ChannelOtherSettings{AvailabilitySchedule: &schedule})
		require.NoError(t, model.DB.Create(channel).Error)
		require.NoError(t, model.DB.Create(&model.Ability{
			Group:     "default",
			Model:     "gpt-test",
			ChannelId: channel.Id,
			Enabled:   false,
		}).Error)

		runChannelAvailabilityReconciliation(time.Date(2026, 7, 28, 9, 0, 0, 0, location))

		persisted, err := model.GetChannelById(channel.Id, true)
		require.NoError(t, err)
		assert.Equal(t, common.ChannelStatusAutoDisabled, persisted.Status)
		assert.Equal(t, model.ChannelStatusReasonAllKeysDisabled, persisted.GetOtherInfo()["status_reason"])
		assert.Equal(t, map[int]int{
			0: common.ChannelStatusAutoDisabled,
			1: common.ChannelStatusAutoDisabled,
		}, persisted.ChannelInfo.MultiKeyStatusList)
		var ability model.Ability
		require.NoError(t, model.DB.Where("channel_id = ?", channel.Id).First(&ability).Error)
		assert.False(t, ability.Enabled)
	})
}

func TestChannelAvailabilityPreservesTagDisableDuringCurrentWindow(t *testing.T) {
	setupChannelAvailabilityServiceTest(t)
	location, err := time.LoadLocation(dto.DefaultChannelAvailabilityTimezone)
	require.NoError(t, err)
	now := time.Now().In(location)
	schedule := dto.ChannelAvailabilitySchedule{
		Enabled:  true,
		Start:    now.Add(-time.Hour).Format("15:04"),
		End:      now.Add(time.Hour).Format("15:04"),
		Timezone: dto.DefaultChannelAvailabilityTimezone,
	}
	tag := "scheduled-tag"
	channel := &model.Channel{
		Name:   "tag-disabled schedule",
		Key:    "sk-test",
		Status: common.ChannelStatusEnabled,
		Models: "gpt-test",
		Group:  "default",
		Tag:    &tag,
	}
	channel.SetOtherSettings(dto.ChannelOtherSettings{AvailabilitySchedule: &schedule})
	require.NoError(t, model.DB.Create(channel).Error)
	require.NoError(t, model.DB.Create(&model.Ability{
		Group:     "default",
		Model:     "gpt-test",
		ChannelId: channel.Id,
		Enabled:   true,
		Tag:       &tag,
	}).Error)

	require.NoError(t, model.DisableChannelByTag(tag))
	runChannelAvailabilityReconciliation(now)

	persisted, err := model.GetChannelById(channel.Id, true)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusManuallyDisabled, persisted.Status)
	assert.Equal(t, "manual tag operation", persisted.GetOtherInfo()["status_reason"])
	statusTime, _ := channelAvailabilityStatusMetadata(persisted)
	assert.GreaterOrEqual(t, statusTime, now.Add(-time.Minute).Unix())
	var ability model.Ability
	require.NoError(t, model.DB.Where("channel_id = ?", channel.Id).First(&ability).Error)
	assert.False(t, ability.Enabled)
}
