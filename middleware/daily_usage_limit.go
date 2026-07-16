package middleware

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

type dailyUsageLimitViolation struct {
	Scope                  string
	ModelName              string
	Message                string
	Code                   string
	LimitTokens            int64
	UsedTokens             int64
	RemainingTokens        int64
	Enabled                bool
	Exceeded               bool
	EvaluationError        string
	Timezone               string
	DayStart               int64
	DayEnd                 int64
	RefreshedAt            int64
	NextRefreshAt          int64
	RefreshIntervalSeconds int
	RetryAfterSeconds      int
}

func SystemDailyUsageLimitCheck() gin.HandlerFunc {
	return func(c *gin.Context) {
		status := service.GetSystemDailyUsageStatus()
		if !status.ShouldBlock() {
			c.Next()
			return
		}

		rejectDailyUsageLimit(c, globalDailyUsageViolation(status))
	}
}

// CheckSystemModelDailyUsageLimit rejects only the requested model when its rule is exceeded.
func CheckSystemModelDailyUsageLimit(c *gin.Context, modelName string) bool {
	status := service.GetSystemDailyUsageStatus()
	if status.ShouldBlock() {
		rejectDailyUsageLimit(c, globalDailyUsageViolation(status))
		return true
	}
	modelStatus, shouldBlock := status.ShouldBlockModel(modelName)
	if !shouldBlock {
		return false
	}
	rejectDailyUsageLimit(c, modelDailyUsageViolation(status, modelStatus))
	return true
}

func globalDailyUsageViolation(status service.SystemDailyUsageStatus) dailyUsageLimitViolation {
	return dailyUsageLimitViolation{
		Scope:                  "global",
		Message:                status.Message,
		Code:                   status.Code,
		LimitTokens:            status.LimitTokens,
		UsedTokens:             status.UsedTokens,
		RemainingTokens:        status.RemainingTokens,
		Enabled:                status.Enabled,
		Exceeded:               status.Exceeded,
		EvaluationError:        status.EvaluationError,
		Timezone:               status.Timezone,
		DayStart:               status.DayStart,
		DayEnd:                 status.DayEnd,
		RefreshedAt:            status.RefreshedAt,
		NextRefreshAt:          status.NextRefreshAt,
		RefreshIntervalSeconds: status.RefreshIntervalSeconds,
		RetryAfterSeconds:      status.RetryAfterSeconds,
	}
}

func modelDailyUsageViolation(status service.SystemDailyUsageStatus, modelStatus service.ModelDailyUsageStatus) dailyUsageLimitViolation {
	return dailyUsageLimitViolation{
		Scope:                  "model",
		ModelName:              modelStatus.ModelName,
		Message:                modelStatus.Message,
		Code:                   modelStatus.Code,
		LimitTokens:            modelStatus.MaxUsage,
		UsedTokens:             modelStatus.CurrentUsage,
		RemainingTokens:        modelStatus.RemainingUsage,
		Enabled:                modelStatus.Enabled,
		Exceeded:               modelStatus.Exceeded,
		EvaluationError:        modelStatus.EvaluationError,
		Timezone:               status.Timezone,
		DayStart:               status.DayStart,
		DayEnd:                 status.DayEnd,
		RefreshedAt:            status.RefreshedAt,
		NextRefreshAt:          status.NextRefreshAt,
		RefreshIntervalSeconds: status.RefreshIntervalSeconds,
		RetryAfterSeconds:      status.RetryAfterSeconds,
	}
}

func rejectDailyUsageLimit(c *gin.Context, violation dailyUsageLimitViolation) {
	recordDailyUsageLimitReject(c, violation)
	writeDailyUsageLimitResponse(c, violation)
	c.Abort()
}

func recordDailyUsageLimitReject(c *gin.Context, violation dailyUsageLimitViolation) {
	populateAvailabilityTokenContext(c)
	other := map[string]interface{}{
		"code":                         violation.Code,
		"method":                       c.Request.Method,
		"path":                         c.Request.URL.Path,
		"usage_limit_scope":            violation.Scope,
		"model_name":                   violation.ModelName,
		"timezone":                     violation.Timezone,
		"day_start":                    violation.DayStart,
		"day_end":                      violation.DayEnd,
		"limit_tokens":                 violation.LimitTokens,
		"used_tokens":                  violation.UsedTokens,
		"remaining_tokens":             violation.RemainingTokens,
		"refreshed_at":                 violation.RefreshedAt,
		"next_refresh_at":              violation.NextRefreshAt,
		"refresh_interval_seconds":     violation.RefreshIntervalSeconds,
		"retry_after_seconds":          violation.RetryAfterSeconds,
		"daily_usage_limit_enabled":    violation.Enabled,
		"daily_usage_limit_exceeded":   violation.Exceeded,
		"daily_usage_limit_fail_error": violation.EvaluationError,
		"status_code":                  http.StatusTooManyRequests,
	}
	model.RecordDailyUsageLimitRejectLog(c, violation.Message, other)
}

func writeDailyUsageLimitResponse(c *gin.Context, violation dailyUsageLimitViolation) {
	c.Header("Cache-Control", "no-store")
	if violation.RetryAfterSeconds > 0 {
		c.Header("Retry-After", strconv.Itoa(violation.RetryAfterSeconds))
	}
	errorCode := types.ErrorCodeSystemDailyUsageExceeded
	if violation.Scope == "model" {
		errorCode = types.ErrorCodeModelDailyUsageExceeded
	}
	path := c.Request.URL.Path
	switch {
	case strings.HasPrefix(path, "/api"):
		c.JSON(http.StatusTooManyRequests, gin.H{
			"success": false,
			"message": violation.Message,
			"code":    violation.Code,
		})
	case strings.HasPrefix(path, "/v1/messages"):
		c.JSON(http.StatusTooManyRequests, gin.H{
			"type": "error",
			"error": types.ClaudeError{
				Type:    violation.Code,
				Message: violation.Message,
			},
		})
	case isMidjourneyAvailabilityPath(path):
		c.JSON(http.StatusTooManyRequests, gin.H{
			"description": violation.Message,
			"type":        "usage_limit",
			"code":        violation.Code,
		})
	case strings.HasPrefix(path, "/suno") || strings.HasPrefix(path, "/kling/") || strings.HasPrefix(path, "/jimeng"):
		c.JSON(http.StatusTooManyRequests, gin.H{
			"code":    violation.Code,
			"message": violation.Message,
		})
	default:
		c.JSON(http.StatusTooManyRequests, gin.H{
			"error": types.OpenAIError{
				Message: violation.Message,
				Type:    "usage_limit",
				Param:   "",
				Code:    errorCode,
			},
		})
	}
}
