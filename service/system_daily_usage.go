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

type ModelDailyUsageStatus struct {
	ModelName       string `json:"model_name"`
	MaxUsage        int64  `json:"model_max_usage"`
	CurrentUsage    int64  `json:"model_current_usage"`
	RemainingUsage  int64  `json:"remaining_usage"`
	Enabled         bool   `json:"enabled"`
	Exceeded        bool   `json:"exceeded"`
	Message         string `json:"message"`
	Code            string `json:"code"`
	EvaluationError string `json:"evaluation_error,omitempty"`
}

type SystemDailyUsageStatus struct {
	Enabled                bool                    `json:"enabled"`
	LimitTokens            int64                   `json:"limit_tokens"`
	UsedTokens             int64                   `json:"used_tokens"`
	RemainingTokens        int64                   `json:"remaining_tokens"`
	Exceeded               bool                    `json:"exceeded"`
	Message                string                  `json:"message"`
	Code                   string                  `json:"code"`
	Timezone               string                  `json:"timezone"`
	DayStart               int64                   `json:"day_start"`
	DayEnd                 int64                   `json:"day_end"`
	RefreshedAt            int64                   `json:"refreshed_at"`
	NextRefreshAt          int64                   `json:"next_refresh_at"`
	RefreshIntervalSeconds int                     `json:"refresh_interval_seconds"`
	RetryAfterSeconds      int                     `json:"retry_after_seconds"`
	EvaluationError        string                  `json:"evaluation_error,omitempty"`
	ModelLimits            []ModelDailyUsageStatus `json:"model_limits"`
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

func (s SystemDailyUsageStatus) ShouldBlockModel(modelName string) (ModelDailyUsageStatus, bool) {
	if modelName == "" {
		return ModelDailyUsageStatus{}, false
	}
	for _, limit := range s.ModelLimits {
		if limit.Enabled && limit.ModelName == modelName {
			return limit, limit.Exceeded
		}
	}
	for _, limit := range s.ModelLimits {
		if limit.Enabled && model.UsageModelMatches(limit.ModelName, modelName) {
			return limit, limit.Exceeded
		}
	}
	return ModelDailyUsageStatus{}, false
}

func buildSystemDailyUsageStatus(now time.Time, settings system_setting.DailyUsageLimitSettings, signature string, refreshSnapshot func(int64, string, int64, []string) (model.SystemDailyUsageSnapshot, error)) SystemDailyUsageStatus {
	status := baseSystemDailyUsageStatus(now, settings, signature)
	location, err := time.LoadLocation(status.Timezone)
	if err != nil {
		return status.failClosed(fmt.Sprintf("invalid daily usage timezone %q: %v", status.Timezone, err))
	}
	dayStart, dayEnd := dailyUsageDayRange(now, location)
	status.DayStart = dayStart.Unix()
	status.DayEnd = dayEnd.Unix()
	status.RetryAfterSeconds = secondsUntil(now, dayEnd)

	globalInvalid := false
	if err := system_setting.ValidateGlobalDailyUsageLimit(settings); err != nil {
		globalInvalid = settings.Enabled
		status.EvaluationError = err.Error()
		if globalInvalid {
			status.Exceeded = true
			status.RemainingTokens = 0
		}
	}
	invalidModels := make(map[string]string)
	for _, limit := range settings.ModelLimits {
		if err := system_setting.ValidateDailyUsageModelLimit(settings.LimitTokens, limit); err != nil {
			invalidModels[limit.ModelName] = err.Error()
		}
	}

	if !common.LogConsumeEnabled {
		return status.failClosed(system_setting.DailyUsageRequiresLogsMsg)
	}

	snapshot, err := refreshSnapshot(status.DayStart, status.Timezone, status.RefreshedAt, settings.ModelNames())
	if err != nil {
		return status.failClosed(err.Error())
	}
	status.UsedTokens = snapshot.UsedTokens
	status.RemainingTokens = remainingDailyUsageTokens(settings.LimitTokens, snapshot.UsedTokens)
	status.Exceeded = globalInvalid || settings.Enabled && settings.LimitTokens > 0 && snapshot.UsedTokens >= settings.LimitTokens
	if globalInvalid {
		status.RemainingTokens = 0
	}
	for i := range status.ModelLimits {
		limit := &status.ModelLimits[i]
		limit.CurrentUsage = snapshot.ModelUsedTokens[limit.ModelName]
		limit.RemainingUsage = remainingDailyUsageTokens(limit.MaxUsage, limit.CurrentUsage)
		limit.Exceeded = limit.Enabled && limit.MaxUsage > 0 && limit.CurrentUsage >= limit.MaxUsage
		if reason := invalidModels[limit.ModelName]; reason != "" {
			limit.EvaluationError = reason
			if limit.Enabled {
				limit.Exceeded = true
				limit.RemainingUsage = 0
			}
			continue
		}
		if snapshot.ModelEvaluationError != "" {
			limit.EvaluationError = snapshot.ModelEvaluationError
			if limit.Enabled {
				limit.Exceeded = true
				limit.RemainingUsage = 0
			}
		}
	}
	return status
}

func baseSystemDailyUsageStatus(now time.Time, settings system_setting.DailyUsageLimitSettings, signature string) SystemDailyUsageStatus {
	refreshedAt := now.Unix()
	modelLimits := make([]ModelDailyUsageStatus, 0, len(settings.ModelLimits))
	for _, limit := range settings.ModelLimits {
		modelLimits = append(modelLimits, ModelDailyUsageStatus{
			ModelName:      limit.ModelName,
			MaxUsage:       limit.MaxUsage,
			RemainingUsage: limit.MaxUsage,
			Enabled:        limit.Enabled,
			Message:        fmt.Sprintf("模型 %s 当日使用量已达上限，请每天再来。", limit.ModelName),
			Code:           system_setting.ModelDailyUsageLimitRejectCode,
		})
	}
	return SystemDailyUsageStatus{
		Enabled:                settings.Enabled,
		LimitTokens:            settings.LimitTokens,
		RemainingTokens:        settings.LimitTokens,
		Message:                settings.Message,
		Code:                   system_setting.DailyUsageLimitRejectCode,
		Timezone:               settings.Timezone,
		RefreshedAt:            refreshedAt,
		NextRefreshAt:          now.Add(systemDailyUsageRefreshInterval).Unix(),
		RefreshIntervalSeconds: int(systemDailyUsageRefreshInterval.Seconds()),
		ModelLimits:            modelLimits,
		settingsSignature:      signature,
	}
}

func dailyUsageSettingsSignature(settings system_setting.DailyUsageLimitSettings) string {
	modelLimitsJSON, err := common.Marshal(settings.ModelLimits)
	if err != nil {
		modelLimitsJSON = []byte("[]")
	}
	return fmt.Sprintf("%t|%d|%s|%s|%s", settings.Enabled, settings.LimitTokens, settings.Timezone, settings.Message, modelLimitsJSON)
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
	s.EvaluationError = reason
	if s.Enabled {
		s.Exceeded = true
		s.RemainingTokens = 0
	}
	for i := range s.ModelLimits {
		if !s.ModelLimits[i].Enabled {
			continue
		}
		s.ModelLimits[i].Exceeded = true
		s.ModelLimits[i].RemainingUsage = 0
		s.ModelLimits[i].EvaluationError = reason
	}
	return s
}
