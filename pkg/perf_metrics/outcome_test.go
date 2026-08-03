package perfmetrics

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShouldRecordRelayFailure(t *testing.T) {
	t.Parallel()

	mappedClientError := types.WithOpenAIError(types.OpenAIError{
		Message: "context is too long",
		Type:    "invalid_request_error",
		Code:    "context_length_exceeded",
	}, http.StatusBadRequest)
	mappedClientError.SetMappedStatusCode(http.StatusServiceUnavailable)

	mappedServiceError := types.WithOpenAIError(types.OpenAIError{
		Message: "upstream failed",
		Type:    "server_error",
		Code:    "server_error",
	}, http.StatusInternalServerError)
	mappedServiceError.SetMappedStatusCode(http.StatusBadRequest)

	_, reasoningConversionErr := relayconvert.OpenAIChatRequestToClaudeMessages(context.Background(), nil, dto.GeneralOpenAIRequest{
		Model:     "claude-test",
		Messages:  []dto.Message{{Role: "user", Content: "hello"}},
		MaxTokens: common.GetPointer[uint](16),
		Reasoning: []byte(`"invalid"`),
	})
	require.Error(t, reasoningConversionErr)
	clientReasoningConversionError := types.NewError(reasoningConversionErr, types.ErrorCodeConvertRequestFailed)
	require.Equal(t, http.StatusBadRequest, clientReasoningConversionError.GetOriginalStatusCode())
	require.Equal(t, types.ErrorCodeInvalidRequest, clientReasoningConversionError.GetErrorCode())

	_, contentConversionErr := relayconvert.ClaudeMessagesRequestToOpenAIChat(dto.ClaudeRequest{
		Model:    "claude-test",
		Messages: []dto.ClaudeMessage{{Role: "user", Content: 123}},
	}, nil)
	require.Error(t, contentConversionErr)
	clientContentConversionError := types.NewError(contentConversionErr, types.ErrorCodeConvertRequestFailed)
	require.Equal(t, http.StatusBadRequest, clientContentConversionError.GetOriginalStatusCode())
	require.Equal(t, types.ErrorCodeInvalidRequest, clientContentConversionError.GetErrorCode())

	testCases := []struct {
		name     string
		err      *types.NewAPIError
		expected bool
	}{
		{name: "nil is not a failure sample"},
		{
			name: "local invalid request is excluded",
			err:  types.NewErrorWithStatusCode(errors.New("invalid request"), types.ErrorCodeInvalidRequest, http.StatusBadRequest),
		},
		{
			name:     "internal invalid request failure is recorded",
			err:      types.NewError(errors.New("request invariant failed"), types.ErrorCodeInvalidRequest),
			expected: true,
		},
		{
			name:     "upstream request serialization failure is recorded",
			err:      types.NewError(errors.New("failed to marshal upstream request"), types.ErrorCodeBadRequestBody),
			expected: true,
		},
		{
			name:     "internal request conversion failure is recorded",
			err:      types.NewError(errors.New("failed to convert request"), types.ErrorCodeConvertRequestFailed),
			expected: true,
		},
		{
			name: "request body too large is excluded",
			err:  types.NewErrorWithStatusCode(errors.New("too large"), types.ErrorCodeReadRequestBodyFailed, http.StatusRequestEntityTooLarge),
		},
		{
			name:     "internal request body failure is recorded",
			err:      types.NewError(errors.New("failed to seek body storage"), types.ErrorCodeReadRequestBodyFailed),
			expected: true,
		},
		{
			name: "client reasoning conversion error is excluded",
			err:  clientReasoningConversionError,
		},
		{
			name: "client content conversion error is excluded",
			err:  clientContentConversionError,
		},
		{
			name: "client cancellation is excluded",
			err:  types.NewOpenAIError(fmt.Errorf("send request: %w", context.Canceled), types.ErrorCodeDoRequestFailed, http.StatusInternalServerError),
		},
		{
			name: "structured context limit is excluded",
			err: types.WithOpenAIError(types.OpenAIError{
				Message: "context is too long",
				Type:    "invalid_request_error",
				Code:    "context_length_exceeded",
			}, http.StatusBadRequest),
		},
		{
			name: "gemini invalid argument is excluded",
			err: types.WithOpenAIError(types.OpenAIError{
				Message:        "request is malformed",
				Code:           400,
				UpstreamStatus: "INVALID_ARGUMENT",
			}, http.StatusBadRequest),
		},
		{
			name:     "channel error is recorded",
			err:      types.NewErrorWithStatusCode(errors.New("channel failed"), types.ErrorCodeChannelModelMappedError, http.StatusBadRequest),
			expected: true,
		},
		{
			name: "upstream quota is recorded despite invalid request type",
			err: types.WithOpenAIError(types.OpenAIError{
				Message: "provider quota exhausted",
				Type:    "invalid_request_error",
				Code:    "insufficient_quota",
			}, http.StatusBadRequest),
			expected: true,
		},
		{
			name: "invalid upstream key is recorded",
			err: types.WithOpenAIError(types.OpenAIError{
				Message: "invalid key",
				Type:    "authentication_error",
				Code:    "invalid_api_key",
			}, http.StatusUnauthorized),
			expected: true,
		},
		{
			name: "upstream 500 wins over request identifier",
			err: types.WithOpenAIError(types.OpenAIError{
				Message: "upstream mislabeled failure",
				Type:    "invalid_request_error",
				Code:    "invalid_argument",
			}, http.StatusInternalServerError),
			expected: true,
		},
		{
			name:     "unknown upstream 400 is recorded conservatively",
			err:      types.NewOpenAIError(errors.New("plain upstream failure"), types.ErrorCodeBadResponseStatusCode, http.StatusBadRequest),
			expected: true,
		},
		{
			name: "mapped client status keeps original attribution",
			err:  mappedClientError,
		},
		{
			name:     "mapped service status keeps original attribution",
			err:      mappedServiceError,
			expected: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.expected, ShouldRecordRelayFailure(tc.err))
		})
	}
}

func TestShouldRecordRelayFailureUsesParsedGeminiStatus(t *testing.T) {
	t.Parallel()

	var response dto.GeneralErrorResponse
	require.NoError(t, common.Unmarshal([]byte(`{"error":{"code":400,"message":"bad request","status":"INVALID_ARGUMENT"}}`), &response))
	openAIError := response.TryToOpenAIError()
	require.NotNil(t, openAIError)
	assert.Equal(t, "INVALID_ARGUMENT", openAIError.UpstreamStatus)

	apiError := types.WithOpenAIError(*openAIError, http.StatusBadRequest)
	assert.False(t, ShouldRecordRelayFailure(apiError))
}
