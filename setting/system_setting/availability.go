package system_setting

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/setting/config"
)

const (
	DefaultUnavailableStart   = "22:00"
	DefaultUnavailableEnd     = "08:00"
	DefaultAvailabilityTZ     = "Asia/Shanghai"
	DefaultAvailabilityMsg    = "当前处于宵禁状态，22:00-8:00期间服务不可用，敬请谅解~"
	AvailabilityRejectCode    = "system_curfew"
	availabilityTimeLayout    = "15:04"
	availabilitySecondsPerDay = 24 * 60 * 60
)

type AvailabilitySettings struct {
	Enabled          bool   `json:"enabled"`
	UnavailableStart string `json:"unavailable_start"`
	UnavailableEnd   string `json:"unavailable_end"`
	Timezone         string `json:"timezone"`
	Message          string `json:"message"`
}

type AvailabilityStatus struct {
	Enabled           bool
	Unavailable       bool
	Message           string
	Code              string
	Timezone          string
	UnavailableStart  string
	UnavailableEnd    string
	RetryAfterSeconds int
	EvaluationError   string
}

var availabilitySettings = AvailabilitySettings{
	Enabled:          false,
	UnavailableStart: DefaultUnavailableStart,
	UnavailableEnd:   DefaultUnavailableEnd,
	Timezone:         DefaultAvailabilityTZ,
	Message:          DefaultAvailabilityMsg,
}

func init() {
	config.GlobalConfig.Register("availability_setting", &availabilitySettings)
}

func GetAvailabilitySettings() AvailabilitySettings {
	return availabilitySettings
}

func GetAvailabilityStatus() AvailabilityStatus {
	return GetAvailabilityStatusAt(time.Now())
}

func GetAvailabilityStatusAt(now time.Time) AvailabilityStatus {
	settings := GetAvailabilitySettings()
	message := strings.TrimSpace(settings.Message)
	if message == "" {
		message = DefaultAvailabilityMsg
	}
	status := AvailabilityStatus{
		Enabled:          settings.Enabled,
		Message:          message,
		Code:             AvailabilityRejectCode,
		Timezone:         normalizedAvailabilityTimezone(settings.Timezone),
		UnavailableStart: normalizedAvailabilityClock(settings.UnavailableStart, DefaultUnavailableStart),
		UnavailableEnd:   normalizedAvailabilityClock(settings.UnavailableEnd, DefaultUnavailableEnd),
	}
	if !settings.Enabled {
		return status
	}

	location, err := time.LoadLocation(status.Timezone)
	if err != nil {
		return status.failClosed(fmt.Sprintf("invalid timezone %q: %v", status.Timezone, err))
	}
	startSeconds, err := parseAvailabilityClock(status.UnavailableStart)
	if err != nil {
		return status.failClosed(err.Error())
	}
	endSeconds, err := parseAvailabilityClock(status.UnavailableEnd)
	if err != nil {
		return status.failClosed(err.Error())
	}
	if startSeconds == endSeconds {
		status.Unavailable = true
		status.RetryAfterSeconds = availabilitySecondsPerDay
		return status
	}

	localNow := now.In(location)
	currentSeconds := localNow.Hour()*3600 + localNow.Minute()*60 + localNow.Second()
	unavailable := false
	retryAfter := 0
	switch {
	case startSeconds < endSeconds:
		unavailable = currentSeconds >= startSeconds && currentSeconds < endSeconds
		if unavailable {
			retryAfter = endSeconds - currentSeconds
		}
	default:
		unavailable = currentSeconds >= startSeconds || currentSeconds < endSeconds
		if unavailable {
			if currentSeconds >= startSeconds {
				retryAfter = availabilitySecondsPerDay - currentSeconds + endSeconds
			} else {
				retryAfter = endSeconds - currentSeconds
			}
		}
	}

	status.Unavailable = unavailable
	status.RetryAfterSeconds = retryAfter
	return status
}

func ValidateAvailabilityOption(key string, value string) error {
	value = strings.TrimSpace(value)
	switch key {
	case "availability_setting.enabled":
		if _, err := strconv.ParseBool(value); err != nil {
			return fmt.Errorf("invalid availability enabled value %q", value)
		}
	case "availability_setting.unavailable_start", "availability_setting.unavailable_end":
		if _, err := parseAvailabilityClock(value); err != nil {
			return err
		}
	case "availability_setting.timezone":
		if value == "" {
			return fmt.Errorf("availability timezone cannot be empty")
		}
		if _, err := time.LoadLocation(value); err != nil {
			return fmt.Errorf("invalid availability timezone %q: %v", value, err)
		}
	case "availability_setting.message":
		if value == "" {
			return fmt.Errorf("availability message cannot be empty")
		}
	}
	return nil
}

func normalizedAvailabilityTimezone(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return DefaultAvailabilityTZ
	}
	return value
}

func normalizedAvailabilityClock(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func parseAvailabilityClock(value string) (int, error) {
	parsed, err := time.Parse(availabilityTimeLayout, value)
	if err != nil {
		return 0, fmt.Errorf("invalid availability time %q, expected HH:MM", value)
	}
	return parsed.Hour()*3600 + parsed.Minute()*60, nil
}

func (s AvailabilityStatus) failClosed(reason string) AvailabilityStatus {
	s.Unavailable = true
	s.RetryAfterSeconds = availabilitySecondsPerDay
	s.EvaluationError = reason
	return s
}
