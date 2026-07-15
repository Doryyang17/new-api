package middleware

import (
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

const (
	requestRiskMaxInspectedBodyBytes = 1 << 20
	requestRiskMaxExtractedTextBytes = 1024
)

func RequestRiskGuard() gin.HandlerFunc {
	return func(c *gin.Context) {
		settings := system_setting.GetRequestRiskSettings()
		if !settings.Enabled || !isModelGenerationRequest(c) {
			c.Next()
			return
		}

		group := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
		if system_setting.RequestRiskGroupWhitelisted(group, settings) {
			c.Next()
			return
		}

		input := service.RequestRiskInput{
			UserID:  c.GetInt("id"),
			TokenID: c.GetInt("token_id"),
		}
		if common.IsClientIPTrustConfigured() {
			input.ClientIP = c.ClientIP()
		}

		if verdict, found := service.GetActiveRequestRiskBlockVerdict(c.Request.Context(), input, settings); found {
			if settings.LogMatches && service.AcquireRequestRiskLogSlot(c.Request.Context(), verdict.ScopeKey) {
				recordRequestRiskEvent(c, input, verdict, settings.Mode)
			}
			writeRequestRiskResponse(c, verdict.RetryAfter)
			return
		}

		populateRequestRiskProfile(c, &input)
		verdict := service.EvaluateRequestRiskBehavior(c.Request.Context(), input, settings)
		if verdict.Score >= 3 && settings.LogMatches && service.AcquireRequestRiskLogSlot(c.Request.Context(), verdict.ScopeKey) {
			recordRequestRiskEvent(c, input, verdict, settings.Mode)
		}
		if verdict.Blocked {
			writeRequestRiskResponse(c, verdict.RetryAfter)
			return
		}

		c.Next()
		if shouldRecordRequestRiskFailure(c) {
			service.RecordRequestRiskFailure(c.Request.Context(), input, verdict.Fingerprint)
		}
	}
}

func populateRequestRiskProfile(c *gin.Context, input *service.RequestRiskInput) {
	storage, err := common.GetBodyStorage(c)
	if err != nil || storage == nil || storage.Size() > requestRiskMaxInspectedBodyBytes {
		return
	}

	text, err := service.ExtractRequestRiskTextFromRequestReadSeeker(
		storage,
		storage.Size(),
		c.Request.Header.Get("Content-Type"),
		c.Request.URL.Path,
		requestRiskMaxExtractedTextBytes,
	)
	resetRequestRiskBody(c, storage)
	if err == nil {
		input.Text = text
	}

	modelRequest, _, modelErr := getModelRequest(c)
	if modelErr == nil && modelRequest != nil {
		input.Model = modelRequest.Model
		if input.Model != "" {
			common.SetContextKey(c, constant.ContextKeyOriginalModel, input.Model)
		}
	}
	resetRequestRiskBody(c, storage)
}

func resetRequestRiskBody(c *gin.Context, storage common.BodyStorage) {
	if c == nil || c.Request == nil || storage == nil {
		return
	}
	if _, err := storage.Seek(0, io.SeekStart); err == nil {
		c.Request.Body = io.NopCloser(storage)
	}
}

func shouldRecordRequestRiskFailure(c *gin.Context) bool {
	if c == nil || c.Request == nil || c.Request.Context().Err() != nil {
		return false
	}
	status := c.Writer.Status()
	if status < http.StatusBadRequest {
		return false
	}
	return !common.GetContextKeyBool(c, constant.ContextKeyPromptFilterBlocked)
}

func recordRequestRiskEvent(c *gin.Context, input service.RequestRiskInput, verdict service.RequestRiskVerdict, mode string) {
	adminInfo := map[string]interface{}{
		"risk_level":           verdict.Level,
		"risk_score":           verdict.Score,
		"risk_factors":         verdict.Factors,
		"request_risk_mode":    mode,
		"would_block":          verdict.Score >= 3,
		"retry_after_seconds":  retryAfterSeconds(verdict.RetryAfter),
		"request_count_10s":    verdict.Metrics.RequestCount10s,
		"request_count_60s":    verdict.Metrics.RequestCount60s,
		"ip_request_count_60s": verdict.Metrics.IPRequestCount60s,
		"repeat_count_60s":     verdict.Metrics.RepeatCount60s,
		"distinct_models_60s":  verdict.Metrics.DistinctModels60s,
		"failure_count_30s":    verdict.Metrics.FailureCount30s,
	}
	if input.ClientIP != "" {
		adminInfo["client_ip"] = input.ClientIP
	}
	if verdict.Fingerprint != "" {
		prefixLength := 12
		if len(verdict.Fingerprint) < prefixLength {
			prefixLength = len(verdict.Fingerprint)
		}
		adminInfo["fingerprint_prefix"] = verdict.Fingerprint[:prefixLength]
	}

	other := map[string]interface{}{"admin_info": adminInfo}
	content := "批量测活防护观察命中"
	if verdict.Blocked {
		content = "批量测活防护已限制请求"
	}
	model.RecordRequestGuardLog(c, content, "request_probe_guard", other, verdict.Blocked)
}

func writeRequestRiskResponse(c *gin.Context, retryAfter time.Duration) {
	writeRequestProtectionResponse(
		c,
		retryAfter,
		system_setting.DefaultRequestRiskMessage,
		types.ErrorCodeRequestProbeRateLimited,
	)
}

func writeRequestProtectionResponse(c *gin.Context, retryAfter time.Duration, rawMessage string, errorCode types.ErrorCode) {
	retrySeconds := retryAfterSeconds(retryAfter)
	if retrySeconds < 1 {
		retrySeconds = 1
	}
	c.Header("Retry-After", strconv.Itoa(retrySeconds))
	c.Header("Cache-Control", "no-store")
	message := common.MessageWithRequestId(rawMessage, c.GetString(common.RequestIdKey))
	path := c.FullPath()
	if path == "" {
		path = c.Request.URL.Path
	}
	switch {
	case strings.HasPrefix(path, "/v1/messages"):
		c.JSON(http.StatusTooManyRequests, gin.H{
			"type": "error",
			"error": gin.H{
				"type":    "rate_limit_error",
				"message": message,
			},
		})
	case isMidjourneyAvailabilityPath(path):
		c.JSON(http.StatusTooManyRequests, gin.H{
			"description": message,
			"type":        "rate_limit_error",
			"code":        errorCode,
		})
	case strings.HasPrefix(path, "/suno") || strings.HasPrefix(path, "/kling/") || strings.HasPrefix(path, "/jimeng"):
		c.JSON(http.StatusTooManyRequests, gin.H{
			"code":    errorCode,
			"message": message,
		})
	default:
		c.JSON(http.StatusTooManyRequests, gin.H{
			"error": types.OpenAIError{
				Message: message,
				Type:    "rate_limit_error",
				Code:    errorCode,
			},
		})
	}
	c.Abort()
}

func retryAfterSeconds(retryAfter time.Duration) int {
	if retryAfter <= 0 {
		return 0
	}
	return int(math.Ceil(retryAfter.Seconds()))
}
