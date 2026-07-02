package middleware

import (
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

func PromptComplianceCheck(endpoint string) gin.HandlerFunc {
	return func(c *gin.Context) {
		settings := system_setting.GetPromptFilterSettings()
		if !setting.ShouldCheckPromptSensitive() || service.PromptFilterRequestWhitelisted(c, settings) || c.Request == nil || c.Request.Body == nil || c.Request.Method == http.MethodGet {
			c.Next()
			return
		}

		storage, err := common.GetBodyStorage(c)
		if err != nil {
			if common.IsRequestBodyTooLargeError(err) {
				abortWithOpenAiMessage(c, http.StatusRequestEntityTooLarge, "request body too large", types.ErrorCodeReadRequestBodyFailed)
				return
			}
			abortWithOpenAiMessage(c, http.StatusBadRequest, "failed to read request body", types.ErrorCodeReadRequestBodyFailed)
			return
		}

		checkEndpoint := endpoint
		if strings.TrimSpace(checkEndpoint) == "" {
			checkEndpoint = c.Request.URL.Path
		}
		verdict, err := service.InspectPromptRequestReadSeekerWithContext(c.Request.Context(), storage, storage.Size(), c.Request.Header.Get("Content-Type"), checkEndpoint)
		resetPromptFilterBody(c, storage)
		if err != nil {
			abortWithOpenAiMessage(c, http.StatusBadRequest, "failed to read request body", types.ErrorCodeReadRequestBodyFailed)
			return
		}
		if verdict.Action != service.PromptFilterActionBlock {
			if verdict.Action == service.PromptFilterActionWarn || verdict.Action == service.PromptFilterActionWatch {
				c.Header("X-Prompt-Filter-Action", verdict.Action)
				if strings.TrimSpace(verdict.Reason) != "" {
					c.Header("X-Prompt-Filter-Reason", verdict.Reason)
				}
				recordPromptFilterReject(c, checkEndpoint, verdict)
			}
			c.Next()
			return
		}

		recordPromptFilterReject(c, checkEndpoint, verdict)
		writePromptFilterResponse(c, verdict)
		c.Abort()
	}
}

func resetPromptFilterBody(c *gin.Context, storage common.BodyStorage) {
	if storage == nil {
		return
	}
	if _, err := storage.Seek(0, io.SeekStart); err == nil {
		c.Request.Body = io.NopCloser(storage)
	}
}

func recordPromptFilterReject(c *gin.Context, endpoint string, verdict service.PromptFilterVerdict) {
	populateAvailabilityTokenContext(c)
	service.RecordPromptFilterRejectLog(c, endpoint, verdict)
}

func writePromptFilterResponse(c *gin.Context, verdict service.PromptFilterVerdict) {
	c.Header("Cache-Control", "no-store")
	settings := system_setting.GetPromptFilterSettings()
	message := settings.Message
	statusCode := settings.BlockStatusCode
	errorCode := settings.BlockErrorCode
	if strings.TrimSpace(verdict.Reason) != "" {
		c.Header("X-Prompt-Filter-Reason", verdict.Reason)
	}
	path := c.Request.URL.Path
	switch {
	case strings.HasPrefix(path, "/api"):
		c.JSON(statusCode, gin.H{
			"success": false,
			"message": message,
			"code":    errorCode,
		})
	case strings.HasPrefix(path, "/v1/messages"):
		c.JSON(statusCode, gin.H{
			"type": "error",
			"error": types.ClaudeError{
				Type:    errorCode,
				Message: message,
			},
		})
	case isMidjourneyAvailabilityPath(path):
		c.JSON(statusCode, gin.H{
			"description": message,
			"type":        errorCode,
			"code":        errorCode,
		})
	case strings.HasPrefix(path, "/suno") || strings.HasPrefix(path, "/kling/") || strings.HasPrefix(path, "/jimeng"):
		c.JSON(statusCode, gin.H{
			"code":    errorCode,
			"message": message,
		})
	default:
		c.JSON(statusCode, gin.H{
			"error": types.OpenAIError{
				Message: message,
				Type:    "invalid_request_error",
				Param:   "",
				Code:    errorCode,
			},
		})
	}
}
