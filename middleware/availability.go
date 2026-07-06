package middleware

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

func SystemAvailabilityCheck() gin.HandlerFunc {
	return func(c *gin.Context) {
		if HandleSystemAvailability(c) {
			return
		}
		c.Next()
	}
}

func HandleSystemAvailability(c *gin.Context) bool {
	status := system_setting.GetAvailabilityStatus()
	if !status.Unavailable {
		return false
	}

	recordAvailabilityReject(c, status)
	writeAvailabilityResponse(c, status)
	c.Abort()
	return true
}

func recordAvailabilityReject(c *gin.Context, status system_setting.AvailabilityStatus) {
	populateAvailabilityTokenContext(c)
	other := map[string]interface{}{
		"code":                  status.Code,
		"method":                c.Request.Method,
		"path":                  c.Request.URL.Path,
		"timezone":              status.Timezone,
		"unavailable_start":     status.UnavailableStart,
		"unavailable_end":       status.UnavailableEnd,
		"retry_after_seconds":   status.RetryAfterSeconds,
		"availability_enabled":  status.Enabled,
		"availability_message":  status.Message,
		"availability_fail_err": status.EvaluationError,
		"status_code":           http.StatusServiceUnavailable,
	}
	model.RecordAvailabilityRejectLog(c, status.Message, other)
}

func populateAvailabilityTokenContext(c *gin.Context) {
	if c.GetInt("token_id") != 0 {
		return
	}
	key, _ := tokenKeyFromRequest(c)
	token, err := model.ValidateUserToken(key)
	if err != nil || token == nil {
		return
	}
	c.Set("id", token.UserId)
	if username, err := model.GetUsernameById(token.UserId, false); err == nil {
		c.Set("username", username)
	}
	c.Set("token_id", token.Id)
	c.Set("token_key", token.Key)
	c.Set("token_name", token.Name)
	c.Set("token_unlimited_quota", token.UnlimitedQuota)
	if !token.UnlimitedQuota {
		c.Set("token_quota", token.RemainQuota)
	}
	common.SetContextKey(c, constant.ContextKeyTokenGroup, token.Group)
	if token.Group != "" {
		common.SetContextKey(c, constant.ContextKeyUsingGroup, token.Group)
	}
}

func writeAvailabilityResponse(c *gin.Context, status system_setting.AvailabilityStatus) {
	c.Header("Cache-Control", "no-store")
	if status.RetryAfterSeconds > 0 {
		c.Header("Retry-After", strconv.Itoa(status.RetryAfterSeconds))
	}
	path := c.Request.URL.Path
	switch {
	case strings.HasPrefix(path, "/api"):
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"message": status.Message,
			"code":    status.Code,
		})
	case strings.HasPrefix(path, "/v1/messages"):
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"type": "error",
			"error": types.ClaudeError{
				Type:    status.Code,
				Message: status.Message,
			},
		})
	case isMidjourneyAvailabilityPath(path):
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"description": status.Message,
			"type":        "service_unavailable",
			"code":        status.Code,
		})
	case strings.HasPrefix(path, "/suno") || strings.HasPrefix(path, "/kling/") || strings.HasPrefix(path, "/jimeng"):
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"code":    status.Code,
			"message": status.Message,
		})
	default:
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": types.OpenAIError{
				Message: status.Message,
				Type:    "service_unavailable",
				Param:   "",
				Code:    types.ErrorCodeSystemCurfew,
			},
		})
	}
}

func isMidjourneyAvailabilityPath(path string) bool {
	return strings.HasPrefix(path, "/mj") || strings.Contains(path, "/mj/")
}
