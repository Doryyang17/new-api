package system_setting

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
)

const (
	RequestRiskModeObserve = "observe"
	RequestRiskModeEnforce = "enforce"

	DefaultRequestRiskMode                  = RequestRiskModeObserve
	DefaultRequestRiskMediumCooldownSeconds = 10
	DefaultRequestRiskTokenBlockSeconds     = 300
	DefaultRequestRiskUserBlockSeconds      = 120
	DefaultRequestRiskIPBlockSeconds        = 60
	DefaultRequestRiskUserConcurrencyLimit  = 8
	DefaultRequestRiskTokenConcurrencyLimit = 4
	MaxRequestRiskConcurrencyLimit          = 1000
	DefaultRequestRiskMessage               = "请求过于频繁，请稍后再试"
	DefaultRequestConcurrencyMessage        = "当前并发请求过多，请稍后再试"
)

type RequestRiskSettings struct {
	Enabled               bool     `json:"enabled"`
	Mode                  string   `json:"mode"`
	LogMatches            bool     `json:"log_matches"`
	MediumCooldownSeconds int      `json:"medium_cooldown_seconds"`
	TokenBlockSeconds     int      `json:"token_block_seconds"`
	UserBlockSeconds      int      `json:"user_block_seconds"`
	IPBlockSeconds        int      `json:"ip_block_seconds"`
	UserConcurrencyLimit  int      `json:"user_concurrency_limit"`
	TokenConcurrencyLimit int      `json:"token_concurrency_limit"`
	GroupWhitelist        []string `json:"group_whitelist"`
}

var requestRiskSettings = RequestRiskSettings{
	Enabled:               false,
	Mode:                  DefaultRequestRiskMode,
	LogMatches:            true,
	MediumCooldownSeconds: DefaultRequestRiskMediumCooldownSeconds,
	TokenBlockSeconds:     DefaultRequestRiskTokenBlockSeconds,
	UserBlockSeconds:      DefaultRequestRiskUserBlockSeconds,
	IPBlockSeconds:        DefaultRequestRiskIPBlockSeconds,
	UserConcurrencyLimit:  DefaultRequestRiskUserConcurrencyLimit,
	TokenConcurrencyLimit: DefaultRequestRiskTokenConcurrencyLimit,
	GroupWhitelist:        []string{},
}

func init() {
	config.GlobalConfig.Register("request_risk_setting", &requestRiskSettings)
}

func GetRequestRiskSettings() RequestRiskSettings {
	common.OptionMapRWMutex.RLock()
	settings := requestRiskSettings
	settings.GroupWhitelist = append([]string(nil), requestRiskSettings.GroupWhitelist...)
	common.OptionMapRWMutex.RUnlock()
	settings.Mode = normalizeRequestRiskMode(settings.Mode)
	settings.MediumCooldownSeconds = normalizePositiveSeconds(settings.MediumCooldownSeconds, DefaultRequestRiskMediumCooldownSeconds)
	settings.TokenBlockSeconds = normalizePositiveSeconds(settings.TokenBlockSeconds, DefaultRequestRiskTokenBlockSeconds)
	settings.UserBlockSeconds = normalizePositiveSeconds(settings.UserBlockSeconds, DefaultRequestRiskUserBlockSeconds)
	settings.IPBlockSeconds = normalizePositiveSeconds(settings.IPBlockSeconds, DefaultRequestRiskIPBlockSeconds)
	settings.UserConcurrencyLimit = normalizeRequestRiskConcurrencyLimit(settings.UserConcurrencyLimit, DefaultRequestRiskUserConcurrencyLimit)
	settings.TokenConcurrencyLimit = normalizeRequestRiskConcurrencyLimit(settings.TokenConcurrencyLimit, DefaultRequestRiskTokenConcurrencyLimit)
	settings.GroupWhitelist = normalizeRequestRiskGroups(settings.GroupWhitelist)
	return settings
}

func ValidateRequestRiskOption(key string, value string) error {
	value = strings.TrimSpace(value)
	switch key {
	case "request_risk_setting.enabled", "request_risk_setting.log_matches":
		if _, err := strconv.ParseBool(value); err != nil {
			return fmt.Errorf("invalid request risk boolean value %q", value)
		}
	case "request_risk_setting.mode":
		if value != RequestRiskModeObserve && value != RequestRiskModeEnforce {
			return fmt.Errorf("invalid request risk mode %q", value)
		}
	case "request_risk_setting.medium_cooldown_seconds",
		"request_risk_setting.token_block_seconds",
		"request_risk_setting.user_block_seconds",
		"request_risk_setting.ip_block_seconds":
		seconds, err := strconv.Atoi(value)
		if err != nil || seconds < 1 || seconds > 86400 {
			return fmt.Errorf("request risk duration must be between 1 and 86400 seconds")
		}
	case "request_risk_setting.user_concurrency_limit", "request_risk_setting.token_concurrency_limit":
		limit, err := strconv.Atoi(value)
		if err != nil || limit < 0 || limit > MaxRequestRiskConcurrencyLimit {
			return fmt.Errorf("request concurrency limit must be between 0 and %d", MaxRequestRiskConcurrencyLimit)
		}
	case "request_risk_setting.group_whitelist":
		var groups []string
		if err := common.UnmarshalJsonStr(value, &groups); err != nil {
			return fmt.Errorf("invalid request risk group whitelist: %w", err)
		}
	default:
		if strings.HasPrefix(key, "request_risk_setting.") {
			return fmt.Errorf("unknown request risk option %q", key)
		}
	}
	return nil
}

func RequestRiskGroupWhitelisted(group string, settings RequestRiskSettings) bool {
	group = strings.TrimSpace(group)
	if group == "" {
		return false
	}
	for _, item := range settings.GroupWhitelist {
		if item == group {
			return true
		}
	}
	return false
}

func normalizeRequestRiskMode(value string) string {
	value = strings.TrimSpace(value)
	if value == RequestRiskModeEnforce {
		return RequestRiskModeEnforce
	}
	return RequestRiskModeObserve
}

func normalizePositiveSeconds(value int, fallback int) int {
	if value < 1 || value > 86400 {
		return fallback
	}
	return value
}

func normalizeRequestRiskConcurrencyLimit(value int, fallback int) int {
	if value < 0 || value > MaxRequestRiskConcurrencyLimit {
		return fallback
	}
	return value
}

func normalizeRequestRiskGroups(groups []string) []string {
	seen := make(map[string]struct{}, len(groups))
	result := make([]string, 0, len(groups))
	for _, group := range groups {
		group = strings.TrimSpace(group)
		if group == "" {
			continue
		}
		if _, ok := seen[group]; ok {
			continue
		}
		seen[group] = struct{}{}
		result = append(result, group)
	}
	return result
}
