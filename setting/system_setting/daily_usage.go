package system_setting

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
)

const (
	DefaultDailyUsageLimitTZ       = DefaultAvailabilityTZ
	DefaultDailyUsageLimitMsg      = "当日系统使用量已超上限，请每天再来。"
	DailyUsageLimitRejectCode      = "system_daily_usage_exceeded"
	ModelDailyUsageLimitRejectCode = "model_daily_usage_exceeded"
	DailyUsageRequiresLogsMsg      = "每日使用量限制依赖消费日志，请先开启消费日志"
	ModelLimitLessThanGlobalMsg    = "模型限制必须小于全站限制"
)

type DailyUsageModelLimit struct {
	ModelName string `json:"model_name"`
	MaxUsage  int64  `json:"model_max_usage"`
	Enabled   bool   `json:"enabled"`
}

type DailyUsageLimitSettings struct {
	Enabled     bool                   `json:"enabled"`
	LimitTokens int64                  `json:"limit_tokens"`
	Timezone    string                 `json:"timezone"`
	Message     string                 `json:"message"`
	ModelLimits []DailyUsageModelLimit `json:"model_limits"`
}

var dailyUsageLimitSettings = DailyUsageLimitSettings{
	Enabled:     false,
	LimitTokens: 0,
	Timezone:    DefaultDailyUsageLimitTZ,
	Message:     DefaultDailyUsageLimitMsg,
	ModelLimits: []DailyUsageModelLimit{},
}

func init() {
	config.GlobalConfig.Register("daily_usage_setting", &dailyUsageLimitSettings)
}

func GetDailyUsageLimitSettings() DailyUsageLimitSettings {
	settings := dailyUsageLimitSettings
	settings.Timezone = normalizedDailyUsageLimitTimezone(settings.Timezone)
	settings.Message = normalizedDailyUsageLimitMessage(settings.Message)
	settings.ModelLimits = normalizeDailyUsageModelLimits(settings.ModelLimits)
	return settings
}

func (s DailyUsageLimitSettings) AnyLimitEnabled() bool {
	if s.Enabled {
		return true
	}
	for _, limit := range s.ModelLimits {
		if limit.Enabled {
			return true
		}
	}
	return false
}

func (s DailyUsageLimitSettings) ModelNames() []string {
	models := make([]string, 0, len(s.ModelLimits))
	for _, limit := range s.ModelLimits {
		models = append(models, limit.ModelName)
	}
	return models
}

func ValidateDailyUsageLimitOption(key string, value string) error {
	value = strings.TrimSpace(value)
	switch key {
	case "daily_usage_setting.enabled":
		if _, err := strconv.ParseBool(value); err != nil {
			return fmt.Errorf("invalid daily usage limit enabled value %q", value)
		}
	case "daily_usage_setting.limit_tokens":
		limit, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid daily usage token limit %q", value)
		}
		if limit < 0 {
			return fmt.Errorf("daily usage token limit cannot be negative")
		}
	case "daily_usage_setting.timezone":
		if value == "" {
			return fmt.Errorf("daily usage timezone cannot be empty")
		}
		if _, err := time.LoadLocation(value); err != nil {
			return fmt.Errorf("invalid daily usage timezone %q: %v", value, err)
		}
	case "daily_usage_setting.message":
		if value == "" {
			return fmt.Errorf("daily usage limit message cannot be empty")
		}
	case "daily_usage_setting.model_limits":
		if _, err := ParseDailyUsageModelLimits(value); err != nil {
			return err
		}
	}
	return nil
}

func ValidateDailyUsageLimitSettings(settings DailyUsageLimitSettings) error {
	if err := ValidateGlobalDailyUsageLimit(settings); err != nil {
		return err
	}
	seen := make(map[string]struct{}, len(settings.ModelLimits))
	for _, limit := range settings.ModelLimits {
		if _, exists := seen[limit.ModelName]; exists {
			return fmt.Errorf("duplicate daily usage model limit %q", limit.ModelName)
		}
		seen[limit.ModelName] = struct{}{}
		if err := ValidateDailyUsageModelLimit(settings.LimitTokens, limit); err != nil {
			return err
		}
	}
	return nil
}

func ValidateGlobalDailyUsageLimit(settings DailyUsageLimitSettings) error {
	if settings.Enabled && settings.LimitTokens <= 0 {
		return fmt.Errorf("daily usage token limit must be greater than 0 when enabled")
	}
	return nil
}

func ValidateDailyUsageModelLimit(globalMax int64, limit DailyUsageModelLimit) error {
	if strings.TrimSpace(limit.ModelName) == "" {
		return fmt.Errorf("daily usage model name cannot be empty")
	}
	if limit.MaxUsage <= 0 {
		return fmt.Errorf("model daily usage limit must be greater than 0")
	}
	if globalMax <= 0 || limit.MaxUsage >= globalMax {
		return fmt.Errorf("%s", ModelLimitLessThanGlobalMsg)
	}
	return nil
}

func ParseDailyUsageModelLimits(value string) ([]DailyUsageModelLimit, error) {
	var limits []DailyUsageModelLimit
	if err := common.UnmarshalJsonStr(value, &limits); err != nil {
		return nil, fmt.Errorf("invalid daily usage model limits: %v", err)
	}
	return normalizeDailyUsageModelLimits(limits), nil
}

func normalizedDailyUsageLimitTimezone(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return DefaultDailyUsageLimitTZ
	}
	return value
}

func normalizedDailyUsageLimitMessage(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return DefaultDailyUsageLimitMsg
	}
	return value
}

func normalizeDailyUsageModelLimits(limits []DailyUsageModelLimit) []DailyUsageModelLimit {
	normalized := make([]DailyUsageModelLimit, 0, len(limits))
	for _, limit := range limits {
		limit.ModelName = strings.TrimSpace(limit.ModelName)
		normalized = append(normalized, limit)
	}
	return normalized
}
