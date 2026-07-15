package middleware

import (
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

const promptFilterHistoryRecoveryMaxBodyBytes = 16 << 20

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
		if recoverPromptFilterBlockedHistory(c, storage, checkEndpoint, verdict) {
			c.Next()
			return
		}

		recordPromptFilterBlockedMessage(c, storage, checkEndpoint, verdict)
		recordPromptFilterReject(c, checkEndpoint, verdict)
		writePromptFilterResponse(c, verdict)
		c.Abort()
	}
}

func recoverPromptFilterBlockedHistory(c *gin.Context, storage common.BodyStorage, endpoint string, verdict service.PromptFilterVerdict) bool {
	if c == nil || c.Request == nil || storage == nil {
		return false
	}
	contentType := c.Request.Header.Get("Content-Type")
	if !service.PromptFilterBlockedHistoryRecoverySupported(contentType, endpoint) {
		return false
	}
	if storage.Size() > promptFilterHistoryRecoveryMaxBodyBytes {
		return false
	}
	body, err := storage.Bytes()
	if err != nil {
		return false
	}

	userID := c.GetInt("id")
	tokenID := c.GetInt("token_id")
	recovery, err := service.BuildPromptFilterBlockedHistoryRecovery(body, contentType, endpoint, userID, tokenID, verdict.Matched)
	if err != nil || !recovery.Supported || strings.TrimSpace(recovery.CurrentUserText) == "" {
		return false
	}

	if recovery.Removed == 0 || len(recovery.Body) == 0 {
		return false
	}

	sanitizedVerdict := service.InspectPromptRequestBodyWithContext(c.Request.Context(), recovery.Body, contentType, endpoint)
	if sanitizedVerdict.Action == service.PromptFilterActionBlock {
		return false
	}
	if err := replacePromptFilterBody(c, recovery.Body, storage); err != nil {
		return false
	}
	c.Header("X-Prompt-Filter-History-Sanitized", strconv.Itoa(recovery.Removed))
	return true
}

func recordPromptFilterBlockedMessage(c *gin.Context, storage common.BodyStorage, endpoint string, verdict service.PromptFilterVerdict) {
	if c == nil || c.Request == nil || storage == nil {
		service.RecordPromptFilterBlockedMessage(0, 0, verdict.FullText, verdict.Matched)
		return
	}
	userID := c.GetInt("id")
	tokenID := c.GetInt("token_id")
	contentType := c.Request.Header.Get("Content-Type")
	if !service.PromptFilterBlockedHistoryRecoverySupported(contentType, endpoint) || storage.Size() > promptFilterHistoryRecoveryMaxBodyBytes {
		service.RecordPromptFilterBlockedMessage(userID, tokenID, verdict.FullText, verdict.Matched)
		return
	}
	body, err := storage.Bytes()
	if err != nil {
		service.RecordPromptFilterBlockedMessage(userID, tokenID, verdict.FullText, verdict.Matched)
		return
	}
	service.RecordPromptFilterBlockedRequestMessages(body, contentType, endpoint, userID, tokenID, verdict.FullText, verdict.Matched)
}

func resetPromptFilterBody(c *gin.Context, storage common.BodyStorage) {
	if storage == nil {
		return
	}
	if _, err := storage.Seek(0, io.SeekStart); err == nil {
		c.Request.Body = io.NopCloser(storage)
	}
}

func replacePromptFilterBody(c *gin.Context, body []byte, previous common.BodyStorage) error {
	storage, err := common.CreateBodyStorage(body)
	if err != nil {
		return err
	}
	c.Set(common.KeyBodyStorage, storage)
	c.Set(common.KeyRequestBody, body)
	c.Request.Body = io.NopCloser(storage)
	c.Request.ContentLength = int64(len(body))
	c.Request.Header.Set("Content-Length", strconv.Itoa(len(body)))
	if previous != nil && previous != storage {
		_ = previous.Close()
	}
	return nil
}

func recordPromptFilterReject(c *gin.Context, endpoint string, verdict service.PromptFilterVerdict) {
	populateAvailabilityTokenContext(c)
	service.RecordPromptFilterRejectLog(c, endpoint, verdict)
}

func writePromptFilterResponse(c *gin.Context, verdict service.PromptFilterVerdict) {
	common.SetContextKey(c, constant.ContextKeyPromptFilterBlocked, true)
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
