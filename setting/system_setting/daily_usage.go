package system_setting

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/setting/config"
)

const (
	DefaultDailyUsageLimitTZ  = DefaultAvailabilityTZ
	DefaultDailyUsageLimitMsg = "当日系统使用量已超上限，请每天再来。"
	DailyUsageLimitRejectCode = "system_daily_usage_exceeded"
	DailyUsageRequiresLogsMsg = "每日使用量限制依赖消费日志，请先开启消费日志"
)

type DailyUsageLimitSettings struct {
	Enabled     bool   `json:"enabled"`
	LimitTokens int64  `json:"limit_tokens"`
	Timezone    string `json:"timezone"`
	Message     string `json:"message"`
}

var dailyUsageLimitSettings = DailyUsageLimitSettings{
	Enabled:     false,
	LimitTokens: 0,
	Timezone:    DefaultDailyUsageLimitTZ,
	Message:     DefaultDailyUsageLimitMsg,
}

func init() {
	config.GlobalConfig.Register("daily_usage_setting", &dailyUsageLimitSettings)
}

func GetDailyUsageLimitSettings() DailyUsageLimitSettings {
	settings := dailyUsageLimitSettings
	settings.Timezone = normalizedDailyUsageLimitTimezone(settings.Timezone)
	settings.Message = normalizedDailyUsageLimitMessage(settings.Message)
	return settings
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
	}
	return nil
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
