package perfmetrics

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/types"
)

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
