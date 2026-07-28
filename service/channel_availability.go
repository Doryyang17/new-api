package service

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"

	"github.com/bytedance/gopkg/util/gopool"
)

const (
	channelAvailabilityTickInterval  = 15 * time.Second
	channelAvailabilityOpenReason    = "可用时间开始，定时启用"
	channelAvailabilityClosedReason  = "不在渠道可用时间内，定时禁用"
	channelAvailabilityInvalidReason = "渠道可用时间配置无效，已安全禁用"
)

var (
	channelAvailabilitySchedulerOnce sync.Once
	channelAvailabilityRunning       atomic.Bool
	channelAvailabilityCachePending  atomic.Bool
	channelAvailabilityCacheMu       sync.Mutex
)

type ChannelAvailabilityUpdateResult struct {
	Schedule         dto.ChannelAvailabilitySchedule `json:"schedule"`
	InWindow         bool                            `json:"in_window"`
	NextTransitionAt int64                           `json:"next_transition_at"`
	Status           int                             `json:"status"`
	StatusChanged    bool                            `json:"status_changed"`
}

func StartChannelAvailabilityScheduler() {
	channelAvailabilitySchedulerOnce.Do(func() {
		logger.LogInfo(context.Background(), fmt.Sprintf("channel availability scheduler started: tick=%s", channelAvailabilityTickInterval))
		runChannelAvailabilityReconciliation(time.Now())

		gopool.Go(func() {
			ticker := time.NewTicker(channelAvailabilityTickInterval)
			defer ticker.Stop()
			for now := range ticker.C {
				runChannelAvailabilityReconciliation(now)
			}
		})
	})
}

func UpdateChannelAvailabilitySchedule(channelID int, schedule dto.ChannelAvailabilitySchedule, now time.Time) (*ChannelAvailabilityUpdateResult, error) {
	if channelID <= 0 {
		return nil, fmt.Errorf("渠道 ID 不能为空")
	}
	normalized, err := schedule.Normalize()
	if err != nil {
		return nil, err
	}
	_, err = model.MutateChannelOtherSettings(channelID, func(settings *dto.ChannelOtherSettings) error {
		settings.AvailabilitySchedule = &normalized
		return nil
	})
	if err != nil {
		return nil, err
	}
	channel, err := model.GetChannelById(channelID, true)
	if err != nil {
		return nil, err
	}
	storedSettings := dto.ChannelOtherSettings{}
	if err := common.UnmarshalJsonStr(channel.OtherSettings, &storedSettings); err != nil {
		return nil, err
	}
	if storedSettings.AvailabilitySchedule == nil {
		return nil, fmt.Errorf("渠道 %d 可用时间配置未保存", channelID)
	}
	normalized, err = storedSettings.AvailabilitySchedule.Normalize()
	if err != nil {
		return nil, err
	}

	window, err := normalized.WindowAt(now)
	if err != nil {
		return nil, err
	}
	statusChanged := false
	if normalized.Enabled {
		targetStatus, reason, shouldChange := channelAvailabilityTransition(channel, window)
		if shouldChange {
			statusChanged, _, err = applyChannelAvailabilityStatus(channel, targetStatus, reason)
			if err != nil {
				return nil, err
			}
		}
	}

	channelAvailabilityCachePending.Store(true)
	if err := flushChannelAvailabilityCache(); err != nil {
		return nil, err
	}
	persisted, err := model.GetChannelById(channelID, false)
	if err != nil {
		return nil, err
	}
	persistedSettings := dto.ChannelOtherSettings{}
	if err := common.UnmarshalJsonStr(persisted.OtherSettings, &persistedSettings); err != nil {
		return nil, err
	}
	if persistedSettings.AvailabilitySchedule != nil {
		normalized, err = persistedSettings.AvailabilitySchedule.Normalize()
		if err != nil {
			return nil, err
		}
		window, err = normalized.WindowAt(now)
		if err != nil {
			return nil, err
		}
	}
	return &ChannelAvailabilityUpdateResult{
		Schedule:         normalized,
		InWindow:         normalized.Enabled && window.Available,
		NextTransitionAt: channelAvailabilityNextTransition(normalized.Enabled, window),
		Status:           persisted.Status,
		StatusChanged:    statusChanged,
	}, nil
}

func runChannelAvailabilityReconciliation(now time.Time) {
	if !channelAvailabilityRunning.CompareAndSwap(false, true) {
		return
	}
	defer channelAvailabilityRunning.Store(false)
	defer func() {
		if recovered := recover(); recovered != nil {
			logger.LogWarn(context.Background(), fmt.Sprintf("channel availability scheduler panic: %v", recovered))
		}
	}()

	channels, err := model.GetChannelsForAvailabilitySchedule()
	if err != nil {
		logger.LogWarn(context.Background(), fmt.Sprintf("channel availability scheduler query failed: %v", err))
		return
	}

	for _, channel := range channels {
		if channel == nil {
			continue
		}
		if model.ChannelCacheDiffers(channel) {
			channelAvailabilityCachePending.Store(true)
		}

		settings := dto.ChannelOtherSettings{}
		if err := common.UnmarshalJsonStr(channel.OtherSettings, &settings); err != nil {
			continue
		}
		schedule := settings.AvailabilitySchedule
		if schedule == nil || !schedule.Enabled {
			continue
		}

		normalized, err := schedule.Normalize()
		if err != nil {
			if channel.Status == common.ChannelStatusManuallyDisabled {
				continue
			}
			changed, matched, updateErr := applyChannelAvailabilityStatus(channel, common.ChannelStatusManuallyDisabled, channelAvailabilityInvalidReason)
			if updateErr != nil {
				logger.LogWarn(context.Background(), fmt.Sprintf("channel availability invalid-config disable failed: channel_id=%d err=%v", channel.Id, updateErr))
				continue
			}
			if !matched {
				continue
			}
			if changed {
				channelAvailabilityCachePending.Store(true)
				logger.LogWarn(context.Background(), fmt.Sprintf("channel availability config invalid; channel disabled: channel_id=%d err=%v", channel.Id, err))
			}
			continue
		}

		window, err := normalized.WindowAt(now)
		if err != nil {
			continue
		}
		targetStatus, reason, shouldChange := channelAvailabilityTransition(channel, window)
		if !shouldChange {
			continue
		}
		changed, matched, err := applyChannelAvailabilityStatus(channel, targetStatus, reason)
		if err != nil {
			logger.LogWarn(context.Background(), fmt.Sprintf("channel availability status update failed: channel_id=%d target=%d err=%v", channel.Id, targetStatus, err))
			continue
		}
		if !matched {
			continue
		}
		if changed {
			channelAvailabilityCachePending.Store(true)
			logger.LogInfo(context.Background(), fmt.Sprintf("channel availability status changed: channel_id=%d name=%s status=%d", channel.Id, channel.Name, targetStatus))
		}
	}

	if channelAvailabilityCachePending.Load() {
		if err := flushChannelAvailabilityCache(); err != nil {
			logger.LogWarn(context.Background(), fmt.Sprintf("channel availability cache refresh failed: %v", err))
		}
	}
}

func channelAvailabilityTransition(channel *model.Channel, window dto.ChannelAvailabilityWindow) (int, string, bool) {
	if !window.Available {
		if channel.Status == common.ChannelStatusManuallyDisabled {
			return 0, "", false
		}
		return common.ChannelStatusManuallyDisabled, channelAvailabilityClosedReason, true
	}
	if channel.Status == common.ChannelStatusEnabled {
		return 0, "", false
	}
	statusTime, statusReason := channelAvailabilityStatusMetadata(channel)
	schedulerDisabled := statusReason == channelAvailabilityClosedReason ||
		statusReason == channelAvailabilityInvalidReason
	if statusTime >= window.StartAt.Unix() && !schedulerDisabled {
		return 0, "", false
	}
	if channel.ChannelInfo.IsMultiKey && !channel.HasEnabledMultiKey() {
		if channel.Status == common.ChannelStatusAutoDisabled {
			return 0, "", false
		}
		return common.ChannelStatusAutoDisabled, model.ChannelStatusReasonAllKeysDisabled, true
	}
	return common.ChannelStatusEnabled, channelAvailabilityOpenReason, true
}

func channelAvailabilityStatusMetadata(channel *model.Channel) (int64, string) {
	info := channel.GetOtherInfo()
	statusReason, _ := info["status_reason"].(string)
	value := info["status_time"]
	switch typed := value.(type) {
	case float64:
		return int64(typed), statusReason
	case int64:
		return typed, statusReason
	case int:
		return int64(typed), statusReason
	default:
		return 0, statusReason
	}
}

func applyChannelAvailabilityStatus(channel *model.Channel, targetStatus int, reason string) (bool, bool, error) {
	if channel.Status == targetStatus {
		return false, true, nil
	}
	changed, matched, err := model.UpdateChannelStatusIfUnchanged(channel, targetStatus, reason)
	if err != nil {
		return false, false, err
	}
	if !matched {
		channelAvailabilityCachePending.Store(true)
		return false, false, nil
	}
	channel.Status = targetStatus
	return changed, true, nil
}

func channelAvailabilityNextTransition(enabled bool, window dto.ChannelAvailabilityWindow) int64 {
	if !enabled || window.NextTransitionAt.IsZero() {
		return 0
	}
	return window.NextTransitionAt.Unix()
}

func flushChannelAvailabilityCache() (err error) {
	channelAvailabilityCacheMu.Lock()
	defer channelAvailabilityCacheMu.Unlock()
	if !channelAvailabilityCachePending.Load() {
		return nil
	}
	channelAvailabilityCachePending.Store(false)
	defer func() {
		if recovered := recover(); recovered != nil {
			channelAvailabilityCachePending.Store(true)
			err = fmt.Errorf("刷新渠道缓存失败: %v", recovered)
		}
	}()
	model.InitChannelCache()
	return nil
}
