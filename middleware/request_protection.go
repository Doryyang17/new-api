package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

const requestProtectionStateContextKey = "request_protection_state"

type RequestProtectionRejection struct {
	RetryAfter time.Duration
	Message    string
	ErrorCode  types.ErrorCode
}

type requestProtectionState struct {
	rejection     *RequestProtectionRejection
	lease         *service.RequestConcurrencyLease
	stopHeartbeat func()
	releaseOnce   sync.Once
}

func ApplyRequestProtection(c *gin.Context) *RequestProtectionRejection {
	if value, exists := c.Get(requestProtectionStateContextKey); exists {
		if state, ok := value.(*requestProtectionState); ok {
			return state.rejection
		}
	}

	state := &requestProtectionState{}
	c.Set(requestProtectionStateContextKey, state)

	settings := system_setting.GetRequestRiskSettings()
	if !settings.Enabled {
		return nil
	}
	group := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	if system_setting.RequestRiskGroupWhitelisted(group, settings) {
		return nil
	}

	state.lease, state.rejection = applyRequestConcurrencyProtection(c, settings)
	if state.lease != nil {
		state.stopHeartbeat = service.StartRequestConcurrencyLeaseHeartbeat(state.lease)
	}
	if state.rejection == nil {
		state.rejection = applyRequestRiskProtection(c, settings)
	}
	if state.rejection != nil {
		ReleaseRequestProtection(c)
	}
	return state.rejection
}

func ReleaseRequestProtection(c *gin.Context) {
	value, exists := c.Get(requestProtectionStateContextKey)
	if !exists {
		return
	}
	state, ok := value.(*requestProtectionState)
	if !ok || state == nil {
		return
	}
	state.releaseOnce.Do(func() {
		if state.stopHeartbeat != nil {
			state.stopHeartbeat()
		}
		service.ReleaseRequestConcurrency(state.lease)
	})
}

func RequestProtectionBlocked(c *gin.Context) bool {
	value, exists := c.Get(requestProtectionStateContextKey)
	if !exists {
		return false
	}
	state, ok := value.(*requestProtectionState)
	return ok && state != nil && state.rejection != nil
}

func WriteRequestProtectionResponse(c *gin.Context, rejection *RequestProtectionRejection) {
	if rejection == nil {
		return
	}
	writeRequestProtectionResponse(c, rejection.RetryAfter, rejection.Message, rejection.ErrorCode)
}

func NewRequestProtectionAPIError(c *gin.Context, rejection *RequestProtectionRejection) *types.NewAPIError {
	if rejection == nil {
		return nil
	}
	message := common.MessageWithRequestId(rejection.Message, c.GetString(common.RequestIdKey))
	return types.WithOpenAIError(types.OpenAIError{
		Message: message,
		Type:    "rate_limit_error",
		Code:    rejection.ErrorCode,
	}, http.StatusTooManyRequests, types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
}
