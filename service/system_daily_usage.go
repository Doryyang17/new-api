package service

import (
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

const systemDailyUsageRefreshInterval = 5 * time.Minute

type SystemDailyUsageStatus struct {
	Enabled                bool   `json:"enabled"`
	LimitTokens            int64  `json:"limit_tokens"`
	UsedTokens             int64  `json:"used_tokens"`
	RemainingTokens        int64  `json:"remaining_tokens"`
	Exceeded               bool   `json:"exceeded"`
	Message                string `json:"message"`
	Code                   string `json:"code"`
	Timezone               string `json:"timezone"`
	DayStart               int64  `json:"day_start"`
	DayEnd                 int64  `json:"day_end"`
	RefreshedAt            int64  `json:"refreshed_at"`
	NextRefreshAt          int64  `json:"next_refresh_at"`
	RefreshIntervalSeconds int    `json:"refresh_interval_seconds"`
	RetryAfterSeconds      int    `json:"retry_after_seconds"`
	EvaluationError        string `json:"evaluation_error,omitempty"`
	settingsSignature      string
}

var (
	systemDailyUsageMu        sync.RWMutex
	systemDailyUsageRefreshMu sync.Mutex
	systemDailyUsageStatus    SystemDailyUsageStatus
	systemDailyUsageLoaded    bool
)

func StartSystemDailyUsageUpdater() {
	RefreshSystemDailyUsageStatus(time.Now())
	go func() {
		ticker := time.NewTicker(systemDailyUsageRefreshInterval)
		defer ticker.Stop()
		for now := range ticker.C {
			RefreshSystemDailyUsageStatus(now)
		}
	}()
}

func GetSystemDailyUsageStatus() SystemDailyUsageStatus {
	now := time.Now()
	settings := system_setting.GetDailyUsageLimitSettings()
	signature := dailyUsageSettingsSignature(settings)

	systemDailyUsageMu.RLock()
	status := systemDailyUsageStatus
	loaded := systemDailyUsageLoaded
	systemDailyUsageMu.RUnlock()

	if loaded && status.settingsSignature == signature && now.Unix() < status.NextRefreshAt {
		return status
	}
	return refreshSystemDailyUsageStatusOnce(settings, signature)
}

func RefreshSystemDailyUsageStatus(now time.Time) SystemDailyUsageStatus {
	settings := system_setting.GetDailyUsageLimitSettings()
	signature := dailyUsageSettingsSignature(settings)
	status := buildSystemDailyUsageStatus(now, settings, signature, model.RefreshSystemDailyUsageSnapshot)

	systemDailyUsageMu.Lock()
	systemDailyUsageStatus = status
	systemDailyUsageLoaded = true
	systemDailyUsageMu.Unlock()

	return status
}

func refreshSystemDailyUsageStatusOnce(settings system_setting.DailyUsageLimitSettings, signature string) SystemDailyUsageStatus {
	systemDailyUsageRefreshMu.Lock()
	defer systemDailyUsageRefreshMu.Unlock()

	now := time.Now()
	systemDailyUsageMu.RLock()
	status := systemDailyUsageStatus
	loaded := systemDailyUsageLoaded
	systemDailyUsageMu.RUnlock()

	if loaded && status.settingsSignature == signature && now.Unix() < status.NextRefreshAt {
		return status
	}

	status = buildSystemDailyUsageStatus(now, settings, signature, model.RefreshSystemDailyUsageSnapshot)
	systemDailyUsageMu.Lock()
	systemDailyUsageStatus = status
	systemDailyUsageLoaded = true
	systemDailyUsageMu.Unlock()
	return status
}

func (s SystemDailyUsageStatus) ShouldBlock() bool {
	return s.Enabled && s.Exceeded
}

func buildSystemDailyUsageStatus(now time.Time, settings system_setting.DailyUsageLimitSettings, signature string, refreshSnapshot func(int64, string, int64) (int64, error)) SystemDailyUsageStatus {
	status := baseSystemDailyUsageStatus(now, settings, signature)
	location, err := time.LoadLocation(status.Timezone)
	if err != nil {
		return status.failClosed(fmt.Sprintf("invalid daily usage timezone %q: %v", status.Timezone, err))
	}
	dayStart, dayEnd := dailyUsageDayRange(now, location)
	status.DayStart = dayStart.Unix()
	status.DayEnd = dayEnd.Unix()
	status.RetryAfterSeconds = secondsUntil(now, dayEnd)

	if settings.Enabled && settings.LimitTokens <= 0 {
		return status.failClosed("daily usage token limit must be greater than 0 when enabled")
	}
	if !common.LogConsumeEnabled {
		errMsg := system_setting.DailyUsageRequiresLogsMsg
		if settings.Enabled {
			return status.failClosed(errMsg)
		}
		status.EvaluationError = errMsg
		return status
	}

	usedTokens, err := refreshSnapshot(status.DayStart, status.Timezone, status.RefreshedAt)
	if err != nil {
		if settings.Enabled {
			return status.failClosed(err.Error())
		}
		status.EvaluationError = err.Error()
		return status
	}
	status.UsedTokens = usedTokens
	status.RemainingTokens = remainingDailyUsageTokens(settings.LimitTokens, usedTokens)
	status.Exceeded = settings.Enabled && settings.LimitTokens > 0 && usedTokens >= settings.LimitTokens
	return status
}

func baseSystemDailyUsageStatus(now time.Time, settings system_setting.DailyUsageLimitSettings, signature string) SystemDailyUsageStatus {
	refreshedAt := now.Unix()
	return SystemDailyUsageStatus{
		Enabled:                settings.Enabled,
		LimitTokens:            settings.LimitTokens,
		Message:                settings.Message,
		Code:                   system_setting.DailyUsageLimitRejectCode,
		Timezone:               settings.Timezone,
		RefreshedAt:            refreshedAt,
		NextRefreshAt:          now.Add(systemDailyUsageRefreshInterval).Unix(),
		RefreshIntervalSeconds: int(systemDailyUsageRefreshInterval.Seconds()),
		settingsSignature:      signature,
	}
}

func dailyUsageSettingsSignature(settings system_setting.DailyUsageLimitSettings) string {
	return fmt.Sprintf("%t|%d|%s|%s", settings.Enabled, settings.LimitTokens, settings.Timezone, settings.Message)
}

func dailyUsageDayRange(now time.Time, location *time.Location) (time.Time, time.Time) {
	localNow := now.In(location)
	start := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, location)
	return start, start.Add(24 * time.Hour)
}

func remainingDailyUsageTokens(limit int64, used int64) int64 {
	if limit <= 0 || used >= limit {
		return 0
	}
	return limit - used
}

func secondsUntil(now time.Time, deadline time.Time) int {
	seconds := int(deadline.Sub(now).Seconds())
	if seconds < 0 {
		return 0
	}
	return seconds
}

func (s SystemDailyUsageStatus) failClosed(reason string) SystemDailyUsageStatus {
	s.Exceeded = true
	s.RemainingTokens = 0
	s.EvaluationError = reason
	return s
}
