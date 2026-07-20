package middleware

import (
	"bytes"
	"errors"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

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
	requestRiskMaxScoredTextBytes    = 1024
	requestRiskMaxLoggedTextBytes    = 80 << 10
	requestRiskTextPreviewRunes      = 240
	requestRiskMaxSanitizeDepth      = 64
)

func applyRequestRiskProtection(c *gin.Context, settings system_setting.RequestRiskSettings) *RequestProtectionRejection {
	if !isModelGenerationRequest(c) {
		return nil
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
		return &RequestProtectionRejection{
			RetryAfter: verdict.RetryAfter,
			Message:    system_setting.DefaultRequestRiskMessage,
			ErrorCode:  types.ErrorCodeRequestProbeRateLimited,
		}
	}

	populateRequestRiskProfile(c, &input)
	verdict := service.EvaluateRequestRiskBehavior(c.Request.Context(), input, settings)
	if verdict.Score >= 3 && settings.LogMatches && service.AcquireRequestRiskLogSlot(c.Request.Context(), verdict.ScopeKey) {
		populateRequestRiskLogDetails(c, &input)
		recordRequestRiskEvent(c, input, verdict, settings.Mode)
	}
	if !verdict.Blocked {
		return nil
	}
	return &RequestProtectionRejection{
		RetryAfter: verdict.RetryAfter,
		Message:    system_setting.DefaultRequestRiskMessage,
		ErrorCode:  types.ErrorCodeRequestProbeRateLimited,
	}
}

func populateRequestRiskProfile(c *gin.Context, input *service.RequestRiskInput) {
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		input.FullRequestUnavailableReason = "读取请求体失败"
		return
	}
	if storage == nil {
		input.FullRequestUnavailableReason = "请求体不可用"
		return
	}
	if storage.Size() > requestRiskMaxInspectedBodyBytes {
		input.FullRequestUnavailableReason = "请求体超过 1 MB，未记录完整内容"
		return
	}

	scoredText, scoreErr := service.ExtractRequestRiskTextFromRequestReadSeeker(
		storage,
		storage.Size(),
		c.Request.Header.Get("Content-Type"),
		c.Request.URL.Path,
		requestRiskMaxScoredTextBytes,
	)
	resetRequestRiskBody(c, storage)
	if scoreErr == nil {
		input.Text = scoredText
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

func populateRequestRiskLogDetails(c *gin.Context, input *service.RequestRiskInput) {
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		input.FullRequestUnavailableReason = "读取请求体失败"
		return
	}
	if storage == nil {
		input.FullRequestUnavailableReason = "请求体不可用"
		return
	}
	if storage.Size() > requestRiskMaxInspectedBodyBytes {
		input.FullRequestUnavailableReason = "请求体超过 1 MB，未记录完整内容"
		return
	}

	extractedText, extractErr := service.ExtractRequestRiskTextFromRequestReadSeeker(
		storage,
		storage.Size(),
		c.Request.Header.Get("Content-Type"),
		c.Request.URL.Path,
		requestRiskMaxLoggedTextBytes,
	)
	resetRequestRiskBody(c, storage)
	if extractErr == nil {
		input.ExtractedText = extractedText
	}
	input.FullRequest, input.FullRequestUnavailableReason = requestRiskFullRequest(
		storage,
		c.Request.Header.Get("Content-Type"),
	)
	resetRequestRiskBody(c, storage)
}

func requestRiskFullRequest(storage common.BodyStorage, contentType string) (string, string) {
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	isJSONContentType := mediaType == "application/json" || strings.HasSuffix(mediaType, "+json")
	if strings.HasPrefix(mediaType, "multipart/") {
		return "", "multipart 请求可能包含二进制文件，未记录完整请求体"
	}
	data, err := storage.Bytes()
	if err != nil {
		return "", "读取完整请求体失败"
	}
	if len(data) == 0 {
		return "", "请求体为空"
	}
	if !utf8.Valid(data) {
		return "", "请求体包含二进制内容，未记录完整请求体"
	}
	if sanitized, err := sanitizeRequestRiskJSON(data); err == nil {
		return string(sanitized), ""
	}
	if isJSONContentType {
		return "", "JSON 请求体格式不正确或嵌套过深，未记录原始内容"
	}
	trimmedData := bytes.TrimSpace(data)
	if len(trimmedData) > 0 && (trimmedData[0] == '{' || trimmedData[0] == '[') {
		return "", "请求体疑似 JSON 但格式不正确，未记录原始内容"
	}
	return "", "非 JSON 请求体未记录完整内容"
}

func sanitizeRequestRiskJSON(data []byte) ([]byte, error) {
	var value interface{}
	if err := common.DecodeJsonUseNumber(bytes.NewReader(data), &value); err != nil {
		return nil, err
	}
	sanitized, err := sanitizeRequestRiskJSONValue(value, 0)
	if err != nil {
		return nil, err
	}
	return common.Marshal(sanitized)
}

func sanitizeRequestRiskJSONValue(value interface{}, depth int) (interface{}, error) {
	if depth > requestRiskMaxSanitizeDepth {
		return nil, errors.New("JSON request exceeds sanitization depth limit")
	}
	switch typed := value.(type) {
	case map[string]interface{}:
		for key, item := range typed {
			if requestRiskSensitiveField(key) {
				typed[key] = "[REDACTED]"
				continue
			}
			sanitized, err := sanitizeRequestRiskJSONValue(item, depth+1)
			if err != nil {
				return nil, err
			}
			typed[key] = sanitized
		}
		return typed, nil
	case []interface{}:
		for index, item := range typed {
			sanitized, err := sanitizeRequestRiskJSONValue(item, depth+1)
			if err != nil {
				return nil, err
			}
			typed[index] = sanitized
		}
		return typed, nil
	default:
		return value, nil
	}
}

func requestRiskSensitiveField(key string) bool {
	normalized := strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(strings.TrimSpace(key)))
	if normalized == "authorization" {
		return true
	}
	for _, suffix := range []string{
		"apikey", "token", "secret", "secretkey", "secretaccesskey", "privatekey",
		"password", "passwd", "credential", "credentials", "cookie", "accesskey", "accesskeyid", "clientkey",
	} {
		if strings.HasSuffix(normalized, suffix) {
			return true
		}
	}
	return false
}

func resetRequestRiskBody(c *gin.Context, storage common.BodyStorage) {
	if c == nil || c.Request == nil || storage == nil {
		return
	}
	if _, err := storage.Seek(0, io.SeekStart); err == nil {
		c.Request.Body = io.NopCloser(storage)
	}
}

func recordRequestRiskEvent(c *gin.Context, input service.RequestRiskInput, verdict service.RequestRiskVerdict, mode string) {
	adminInfo := map[string]interface{}{
		"endpoint":             c.Request.URL.Path,
		"risk_level":           verdict.Level,
		"risk_score":           verdict.Score,
		"risk_factors":         verdict.Factors,
		"request_risk_mode":    mode,
		"enforceable":          verdict.Enforceable,
		"would_block":          verdict.Score >= 3 && verdict.Enforceable,
		"retry_after_seconds":  retryAfterSeconds(verdict.RetryAfter),
		"request_count_10s":    verdict.Metrics.RequestCount10s,
		"request_count_60s":    verdict.Metrics.RequestCount60s,
		"ip_request_count_60s": verdict.Metrics.IPRequestCount60s,
		"repeat_count_60s":     verdict.Metrics.RepeatCount60s,
		"distinct_models_60s":  verdict.Metrics.DistinctModels60s,
	}
	trimmedText := strings.TrimSpace(input.ExtractedText)
	if trimmedText == "" {
		trimmedText = strings.TrimSpace(input.Text)
	}
	if trimmedText != "" {
		adminInfo["text_preview"] = requestRiskTextPreview(trimmedText)
		adminInfo["extracted_chars"] = len([]rune(trimmedText))
		if len(verdict.MatchedKeywords) > 0 {
			adminInfo["matched_keywords"] = verdict.MatchedKeywords
		}
	}
	if input.FullRequest != "" {
		adminInfo["full_request_available"] = true
	} else {
		adminInfo["full_request_available"] = false
		reason := input.FullRequestUnavailableReason
		if verdict.ExistingBlock {
			reason = "临时限制在读取请求体前命中"
		} else if reason == "" {
			reason = "请求未包含可记录的完整请求体"
		}
		adminInfo["full_request_unavailable_reason"] = reason
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
	var detail *model.RequestRiskLogDetail
	if trimmedText != "" || input.FullRequest != "" {
		detail = &model.RequestRiskLogDetail{
			ExtractedText: trimmedText,
			FullRequest:   input.FullRequest,
		}
	}
	model.RecordRequestGuardLogWithDetail(c, content, "request_probe_guard", other, verdict.Blocked, detail)
}

func requestRiskTextPreview(text string) string {
	runes := []rune(text)
	if len(runes) <= requestRiskTextPreviewRunes {
		return text
	}
	return string(runes[:requestRiskTextPreviewRunes]) + "…"
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
