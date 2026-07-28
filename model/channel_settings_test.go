package model

import (
	"errors"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMutateChannelOtherSettingsMergesConcurrentFields(t *testing.T) {
	truncateTables(t)
	channel := &Channel{Name: "settings-cas", Key: "sk-test", Status: common.ChannelStatusEnabled}
	channel.SetOtherSettings(dto.ChannelOtherSettings{})
	require.NoError(t, DB.Create(channel).Error)

	ready := make(chan struct{}, 2)
	release := make(chan struct{})
	var initialMutations atomic.Int32
	errors := make(chan error, 2)
	schedule := dto.ChannelAvailabilitySchedule{
		Enabled:  true,
		Start:    "08:00",
		End:      "12:00",
		Timezone: "Asia/Shanghai",
	}
	mutate := func(apply func(*dto.ChannelOtherSettings)) {
		_, err := MutateChannelOtherSettings(channel.Id, func(settings *dto.ChannelOtherSettings) error {
			if initialMutations.Add(1) <= 2 {
				ready <- struct{}{}
				<-release
			}
			apply(settings)
			return nil
		})
		errors <- err
	}

	go mutate(func(settings *dto.ChannelOtherSettings) {
		settings.AllowSpeed = true
	})
	go mutate(func(settings *dto.ChannelOtherSettings) {
		settings.AvailabilitySchedule = &schedule
	})
	<-ready
	<-ready
	close(release)
	require.NoError(t, <-errors)
	require.NoError(t, <-errors)

	persisted, err := GetChannelById(channel.Id, true)
	require.NoError(t, err)
	settings := persisted.GetOtherSettings()
	assert.True(t, settings.AllowSpeed)
	require.NotNil(t, settings.AvailabilitySchedule)
	assert.Equal(t, schedule, *settings.AvailabilitySchedule)
}

func TestChannelUpdatePreservesLatestAvailabilitySchedule(t *testing.T) {
	truncateTables(t)
	channel := &Channel{
		Name:   "stale channel edit",
		Key:    "sk-test",
		Status: common.ChannelStatusEnabled,
		Models: "gpt-test",
		Group:  "default",
	}
	channel.SetOtherSettings(dto.ChannelOtherSettings{})
	require.NoError(t, DB.Create(channel).Error)

	stale, err := GetChannelById(channel.Id, true)
	require.NoError(t, err)
	schedule := dto.ChannelAvailabilitySchedule{
		Enabled:  true,
		Start:    "08:00",
		End:      "12:00",
		Timezone: "Asia/Shanghai",
	}
	_, err = MutateChannelOtherSettings(channel.Id, func(settings *dto.ChannelOtherSettings) error {
		settings.AvailabilitySchedule = &schedule
		return nil
	})
	require.NoError(t, err)

	staleSettings := stale.GetOtherSettings()
	staleSettings.AllowSpeed = true
	stale.SetOtherSettings(staleSettings)
	require.NoError(t, stale.Update())

	persisted, err := GetChannelById(channel.Id, true)
	require.NoError(t, err)
	settings := persisted.GetOtherSettings()
	assert.True(t, settings.AllowSpeed)
	require.NotNil(t, settings.AvailabilitySchedule)
	assert.Equal(t, schedule, *settings.AvailabilitySchedule)
}

func TestChannelUpdateRollsBackWhenAbilityUpdateFails(t *testing.T) {
	truncateTables(t)
	channel := &Channel{
		Name:   "atomic channel update",
		Key:    "sk-test",
		Status: common.ChannelStatusEnabled,
		Models: "gpt-old",
		Group:  "default",
	}
	require.NoError(t, DB.Create(channel).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     "gpt-old",
		ChannelId: channel.Id,
		Enabled:   true,
	}).Error)

	callbackName := "test:fail_channel_update_ability_create"
	require.NoError(t, DB.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "abilities" {
			_ = tx.AddError(errors.New("forced ability write failure"))
		}
	}))
	t.Cleanup(func() {
		_ = DB.Callback().Create().Remove(callbackName)
	})

	channel.Name = "should roll back"
	channel.Models = "gpt-new"
	err := channel.Update()
	require.ErrorContains(t, err, "forced ability write failure")

	persisted, err := GetChannelById(channel.Id, true)
	require.NoError(t, err)
	assert.Equal(t, "atomic channel update", persisted.Name)
	assert.Equal(t, "gpt-old", persisted.Models)
	var abilities []Ability
	require.NoError(t, DB.Where("channel_id = ?", channel.Id).Find(&abilities).Error)
	require.Len(t, abilities, 1)
	assert.Equal(t, "gpt-old", abilities[0].Model)
	assert.True(t, abilities[0].Enabled)
}

func TestMutateChannelOtherSettingsAndModelsRollsBackWhenAbilityUpdateFails(t *testing.T) {
	truncateTables(t)
	channel := &Channel{
		Name:   "atomic upstream model update",
		Key:    "sk-test",
		Status: common.ChannelStatusEnabled,
		Models: "gpt-old",
		Group:  "default",
	}
	channel.SetOtherSettings(dto.ChannelOtherSettings{})
	require.NoError(t, DB.Create(channel).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     "gpt-old",
		ChannelId: channel.Id,
		Enabled:   true,
	}).Error)

	callbackName := "test:fail_settings_models_ability_create"
	require.NoError(t, DB.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "abilities" {
			_ = tx.AddError(errors.New("forced ability write failure"))
		}
	}))
	t.Cleanup(func() {
		_ = DB.Callback().Create().Remove(callbackName)
	})

	_, err := MutateChannelOtherSettingsAndModels(channel.Id, "gpt-new", func(settings *dto.ChannelOtherSettings) error {
		settings.AllowSpeed = true
		return nil
	})
	require.ErrorContains(t, err, "forced ability write failure")

	persisted, err := GetChannelById(channel.Id, true)
	require.NoError(t, err)
	assert.Equal(t, "gpt-old", persisted.Models)
	assert.False(t, persisted.GetOtherSettings().AllowSpeed)
	var abilities []Ability
	require.NoError(t, DB.Where("channel_id = ?", channel.Id).Find(&abilities).Error)
	require.Len(t, abilities, 1)
	assert.Equal(t, "gpt-old", abilities[0].Model)
	assert.True(t, abilities[0].Enabled)
}

func TestChannelCacheDiffersUsesLockedSnapshot(t *testing.T) {
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	t.Cleanup(func() {
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
		InitChannelCache()
	})
	truncateTables(t)
	common.MemoryCacheEnabled = true

	channel := &Channel{
		Name:   "cache snapshot",
		Key:    "sk-test",
		Status: common.ChannelStatusEnabled,
	}
	channel.SetOtherSettings(dto.ChannelOtherSettings{})
	require.NoError(t, DB.Create(channel).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     "gpt-test",
		ChannelId: channel.Id,
		Enabled:   true,
	}).Error)
	InitChannelCache()

	persisted, err := GetChannelById(channel.Id, true)
	require.NoError(t, err)
	assert.False(t, ChannelCacheDiffers(persisted))

	require.NoError(t, DB.Model(&Channel{}).Where("id = ?", channel.Id).Update("status", common.ChannelStatusManuallyDisabled).Error)
	persisted, err = GetChannelById(channel.Id, true)
	require.NoError(t, err)
	assert.True(t, ChannelCacheDiffers(persisted))

	CacheUpdateChannelStatus(channel.Id, common.ChannelStatusManuallyDisabled)
	assert.False(t, ChannelCacheDiffers(persisted))
}

func TestGetChannelsForAvailabilityScheduleFiltersUnscheduledChannels(t *testing.T) {
	truncateTables(t)
	plain := &Channel{Name: "plain", Key: "sk-plain", Status: common.ChannelStatusEnabled}
	plain.SetOtherSettings(dto.ChannelOtherSettings{})
	require.NoError(t, DB.Create(plain).Error)

	schedule := dto.ChannelAvailabilitySchedule{
		Enabled:  true,
		Start:    "08:00",
		End:      "12:00",
		Timezone: "Asia/Shanghai",
	}
	scheduled := &Channel{Name: "scheduled", Key: "sk-scheduled", Status: common.ChannelStatusEnabled}
	scheduled.SetOtherSettings(dto.ChannelOtherSettings{AvailabilitySchedule: &schedule})
	require.NoError(t, DB.Create(scheduled).Error)

	channels, err := GetChannelsForAvailabilitySchedule()
	require.NoError(t, err)
	require.Len(t, channels, 1)
	assert.Equal(t, scheduled.Id, channels[0].Id)
}

func TestAdvancedCustomChannelRequiresModelListRouteOnlyWhenUpdateChecksEnabled(t *testing.T) {
	inferenceRoute := dto.AdvancedCustomRoute{
		IncomingPath: "/v1/chat/completions",
		UpstreamPath: "/v1/chat/completions",
		Converter:    "none",
	}

	tests := []struct {
		name          string
		checksEnabled bool
		routes        []dto.AdvancedCustomRoute
		wantErr       string
	}{
		{
			name:   "legacy channel without discovery route remains valid",
			routes: []dto.AdvancedCustomRoute{inferenceRoute},
		},
		{
			name:          "enabled checks require discovery route",
			checksEnabled: true,
			routes:        []dto.AdvancedCustomRoute{inferenceRoute},
			wantErr:       dto.AdvancedCustomModelListPath,
		},
		{
			name:          "enabled checks accept discovery route",
			checksEnabled: true,
			routes: []dto.AdvancedCustomRoute{
				inferenceRoute,
				{
					IncomingPath: dto.AdvancedCustomModelListPath,
					UpstreamPath: dto.AdvancedCustomModelListPath,
					Converter:    "none",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			channel := &Channel{Type: constant.ChannelTypeAdvancedCustom}
			channel.SetOtherSettings(dto.ChannelOtherSettings{
				UpstreamModelUpdateCheckEnabled: tt.checksEnabled,
				AdvancedCustom: &dto.AdvancedCustomConfig{
					Routes: tt.routes,
				},
			})

			err := channel.ValidateSettings()
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}
