package perfmetrics

import (
	"context"
	"errors"
	"fmt"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/reasoning"
	"net/http"
	"strings"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
)

type Outcome string

const (
	OutcomeSuccess Outcome = "success"
	OutcomeFailure Outcome = "failure"
	OutcomeIgnored Outcome = "ignored"
)

// ClassifyRelayOutcome decides whether one finished relay counts as a health
// sample. Business rejections and client cancellations are not samples; the
// classification is independent of retries, channel disabling and billing.
func ClassifyRelayOutcome(ctx context.Context, info *relaycommon.RelayInfo, apiErr *types.NewAPIError) Outcome {
	if info == nil || info.PerformanceBusinessRejection {
		return OutcomeIgnored
	}
	if ctx != nil && ctx.Err() == context.Canceled {
		return OutcomeIgnored
	}
	if apiErr != nil && errors.Is(apiErr, context.Canceled) {
		return OutcomeIgnored
	}
	stream := info.StreamStatus.OutcomeSnapshot()
	if stream.Response == relaycommon.ResponseOutcomeFailed {
		return classifyFailure(false, stream.ErrorCode, stream.ErrorType, stream.ErrorStatus)
	}
	if apiErr != nil {
		root := rootAPIError(apiErr)
		if root.GetErrorType() == types.ErrorTypeNewAPIError {
			if _, ignored := excludedLocalErrorCodes[root.GetErrorCode()]; ignored {
				return OutcomeIgnored
			}
			if strings.HasPrefix(string(root.GetErrorCode()), "violation_fee.") {
				return OutcomeIgnored
			}
		}
		if !ShouldRecordRelayFailure(root) {
			return OutcomeIgnored
		}
		if types.IsChannelError(root) {
			return OutcomeFailure
		}
		for _, identifier := range relayErrorIdentifiers(root) {
			if _, ok := serviceFailureIdentifiers[identifier]; ok {
				return OutcomeFailure
			}
		}
		local := root.GetErrorType() == types.ErrorTypeNewAPIError
		return classifyFailure(local, string(root.GetErrorCode()), root.ToOpenAIError().Type, root.StatusCode)
	}
	deadlineExceeded := info.StreamStatus != nil && errors.Is(info.StreamStatus.EndError, context.DeadlineExceeded)
	if stream.Response == relaycommon.ResponseOutcomeCancelled || stream.EndReason == relaycommon.StreamEndReasonPingFail {
		return OutcomeIgnored
	}
	if stream.EndReason == relaycommon.StreamEndReasonClientGone && !deadlineExceeded {
		return OutcomeIgnored
	}
	if stream.Response == relaycommon.ResponseOutcomeIncomplete {
		switch stream.IncompleteReason {
		case "max_output_tokens", "max_tokens":
			return OutcomeSuccess
		case "content_filter", "safety", "content_policy_violation":
			return OutcomeIgnored
		default:
			return OutcomeFailure
		}
	}
	if stream.HasErrors || deadlineExceeded {
		return OutcomeFailure
	}
	switch stream.EndReason {
	case relaycommon.StreamEndReasonTimeout, relaycommon.StreamEndReasonScannerErr, relaycommon.StreamEndReasonPanic:
		return OutcomeFailure
	}
	if stream.ExpectsTerminal && stream.Response == relaycommon.ResponseOutcomeUnknown && stream.EndReason != relaycommon.StreamEndReasonDone {
		return OutcomeFailure
	}
	return OutcomeSuccess
}

// rootAPIError follows host wrappers back to the error that describes what
// actually happened, e.g. an upstream credential error re-wrapped as a local
// invalid request.
func rootAPIError(apiErr *types.NewAPIError) *types.NewAPIError {
	for {
		var inner *types.NewAPIError
		if !errors.As(apiErr.Unwrap(), &inner) || inner == apiErr {
			return apiErr
		}
		apiErr = inner
	}
}

func classifyFailure(local bool, code, errorType string, status int) Outcome {
	code, errorType = strings.ToLower(code), strings.ToLower(errorType)
	if local {
		if strings.HasPrefix(code, "violation_fee.") {
			return OutcomeIgnored
		}
		switch types.ErrorCode(code) {
		case types.ErrorCodeInvalidRequest, types.ErrorCodeSensitiveWordsDetected, types.ErrorCodeReadRequestBodyFailed,
			types.ErrorCodeConvertRequestFailed, types.ErrorCodeAccessDenied, types.ErrorCodeBadRequestBody,
			types.ErrorCodeInsufficientUserQuota, types.ErrorCodePreConsumeTokenQuotaFailed, types.ErrorCodePromptBlocked:
			return OutcomeIgnored
		}
		return OutcomeFailure
	}
	// Specific codes take precedence over broad protocol types, because
	// providers also report invalid credentials as invalid_request_error.
	for _, value := range []string{code, errorType} {
		switch value {
		case "invalid_api_key", "api_key_invalid", "api_key_expired", "api_key_service_blocked", "invalid_authentication",
			"authentication_error", "unauthenticated", "permission_denied", "permission_error", "access_denied",
			"insufficient_quota", "quota_exceeded", "resource_exhausted", "rate_limit_exceeded", "rate_limit_error",
			"overloaded_error", "server_error", "internal_error", "service_unavailable", "model_not_found",
			"insufficient_user_quota", "pre_consume_token_quota_failed":
			return OutcomeFailure
		case "context_length_exceeded", "invalid_request", "invalid_request_error", "invalid_argument",
			"sensitive_words_detected", "prompt_blocked", "content_filter", "content_policy_violation", "safety":
			return OutcomeIgnored
		}
		if strings.HasPrefix(value, "violation_fee.") {
			return OutcomeIgnored
		}
	}
	switch status {
	case 400, 405, 409, 413, 415, 422:
		return OutcomeIgnored
	}
	return OutcomeFailure
}

var excludedLocalErrorCodes = map[types.ErrorCode]struct{}{
	types.ErrorCodeAccessDenied:               {},
	types.ErrorCodeInsufficientUserQuota:      {},
	types.ErrorCodeModelDailyUsageExceeded:    {},
	types.ErrorCodePreConsumeTokenQuotaFailed: {},
	types.ErrorCodePromptBlocked:              {},
	types.ErrorCodeReadRequestBodyFailed:      {},
	types.ErrorCodeRequestConcurrencyLimited:  {},
	types.ErrorCodeRequestProbeRateLimited:    {},
	types.ErrorCodeSensitiveWordsDetected:     {},
	types.ErrorCodeSystemCurfew:               {},
	types.ErrorCodeSystemDailyUsageExceeded:   {},
}

var serviceFailureIdentifiers = map[string]struct{}{
	"account_deactivated":          {},
	"api_error":                    {},
	"authentication_error":         {},
	"billing_hard_limit_reached":   {},
	"capacity_exceeded":            {},
	"deployment_not_found":         {},
	"insufficient_quota":           {},
	"internal_server_error":        {},
	"invalid_api_key":              {},
	"model_not_found":              {},
	"not_found_error":              {},
	"overloaded_error":             {},
	"permission_denied":            {},
	"permission_error":             {},
	"rate_limit_error":             {},
	"rate_limit_exceeded":          {},
	"resource_exhausted":           {},
	"resource_not_found_exception": {},
	"server_error":                 {},
	"service_unavailable":          {},
	"unsupported_model":            {},
}

var excludedRequestIdentifiers = map[string]struct{}{
	"bad_request":                 {},
	"content_filter":              {},
	"content_policy_violation":    {},
	"context_length_exceeded":     {},
	"context_window_exceeded":     {},
	"image_generation_user_error": {},
	"input_too_long":              {},
	"invalid_argument":            {},
	"invalid_parameter":           {},
	"invalid_prompt":              {},
	"invalid_request":             {},
	"invalid_request_error":       {},
	"invalid_value":               {},
	"max_tokens_exceeded":         {},
	"moderation_blocked":          {},
	"prompt_blocked":              {},
	"request_too_large":           {},
	"safety_blocked":              {},
	"unprocessable_entity":        {},
	"unsupported_value":           {},
	"validation_error":            {},
	"validation_exception":        {},
	"violation_fee_grok_csam":     {},
}

// ShouldRecordRelayFailure reports whether a failed relay belongs in model
// availability metrics. Unknown failures stay included to avoid hiding real
// service problems.
func ShouldRecordRelayFailure(err *types.NewAPIError) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	if types.IsChannelError(err) {
		return true
	}
	if reasoning.IsClientError(err) {
		return false
	}
	if err.GetOriginalStatusCode() >= http.StatusInternalServerError {
		return true
	}
	if err.GetErrorType() == types.ErrorTypeNewAPIError {
		if _, ok := excludedLocalErrorCodes[err.GetErrorCode()]; ok {
			return false
		}
	}

	identifiers := relayErrorIdentifiers(err)
	for _, identifier := range identifiers {
		if _, ok := serviceFailureIdentifiers[identifier]; ok {
			return true
		}
	}
	for _, identifier := range identifiers {
		if _, ok := excludedRequestIdentifiers[identifier]; ok {
			return false
		}
	}

	switch err.GetOriginalStatusCode() {
	case http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity, 499:
		return false
	default:
		return true
	}
}

func relayErrorIdentifiers(err *types.NewAPIError) []string {
	identifiers := []string{normalizeErrorIdentifier(string(err.GetErrorCode()))}
	switch relayErr := err.RelayError.(type) {
	case types.OpenAIError:
		identifiers = append(identifiers, normalizeErrorIdentifier(relayErr.Type))
		identifiers = append(identifiers, normalizeErrorIdentifier(relayErr.UpstreamStatus))
		if relayErr.Code != nil {
			identifiers = append(identifiers, normalizeErrorIdentifier(fmt.Sprint(relayErr.Code)))
		}
	case types.ClaudeError:
		identifiers = append(identifiers, normalizeErrorIdentifier(relayErr.Type))
	}
	return identifiers
}

func normalizeErrorIdentifier(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.NewReplacer("-", "_", " ", "_", ".", "_").Replace(value)
}
