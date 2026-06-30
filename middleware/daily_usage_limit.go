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

func SystemDailyUsageLimitCheck() gin.HandlerFunc {
	return func(c *gin.Context) {
		status := service.GetSystemDailyUsageStatus()
		if !status.ShouldBlock() {
			c.Next()
			return
		}

		recordDailyUsageLimitReject(c, status)
		writeDailyUsageLimitResponse(c, status)
		c.Abort()
	}
}

func recordDailyUsageLimitReject(c *gin.Context, status service.SystemDailyUsageStatus) {
	populateAvailabilityTokenContext(c)
	other := map[string]interface{}{
		"code":                         status.Code,
		"method":                       c.Request.Method,
		"path":                         c.Request.URL.Path,
		"timezone":                     status.Timezone,
		"day_start":                    status.DayStart,
		"day_end":                      status.DayEnd,
		"limit_tokens":                 status.LimitTokens,
		"used_tokens":                  status.UsedTokens,
		"remaining_tokens":             status.RemainingTokens,
		"refreshed_at":                 status.RefreshedAt,
		"next_refresh_at":              status.NextRefreshAt,
		"refresh_interval_seconds":     status.RefreshIntervalSeconds,
		"retry_after_seconds":          status.RetryAfterSeconds,
		"daily_usage_limit_enabled":    status.Enabled,
		"daily_usage_limit_exceeded":   status.Exceeded,
		"daily_usage_limit_fail_error": status.EvaluationError,
		"status_code":                  http.StatusTooManyRequests,
	}
	model.RecordDailyUsageLimitRejectLog(c, status.Message, other)
}

func writeDailyUsageLimitResponse(c *gin.Context, status service.SystemDailyUsageStatus) {
	c.Header("Cache-Control", "no-store")
	if status.RetryAfterSeconds > 0 {
		c.Header("Retry-After", strconv.Itoa(status.RetryAfterSeconds))
	}
	path := c.Request.URL.Path
	switch {
	case strings.HasPrefix(path, "/api"):
		c.JSON(http.StatusTooManyRequests, gin.H{
			"success": false,
			"message": status.Message,
			"code":    status.Code,
		})
	case strings.HasPrefix(path, "/v1/messages"):
		c.JSON(http.StatusTooManyRequests, gin.H{
			"type": "error",
			"error": types.ClaudeError{
				Type:    status.Code,
				Message: status.Message,
			},
		})
	case isMidjourneyAvailabilityPath(path):
		c.JSON(http.StatusTooManyRequests, gin.H{
			"description": status.Message,
			"type":        "usage_limit",
			"code":        status.Code,
		})
	case strings.HasPrefix(path, "/suno") || strings.HasPrefix(path, "/kling/") || strings.HasPrefix(path, "/jimeng"):
		c.JSON(http.StatusTooManyRequests, gin.H{
			"code":    status.Code,
			"message": status.Message,
		})
	default:
		c.JSON(http.StatusTooManyRequests, gin.H{
			"error": types.OpenAIError{
				Message: status.Message,
				Type:    "usage_limit",
				Param:   "",
				Code:    types.ErrorCodeSystemDailyUsageExceeded,
			},
		})
	}
}
