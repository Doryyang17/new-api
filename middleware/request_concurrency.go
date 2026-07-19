package middleware

import (
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

const requestConcurrencyRetryAfter = time.Second

func RequestConcurrencyGuard() gin.HandlerFunc {
	return func(c *gin.Context) {
		settings := system_setting.GetRequestRiskSettings()
		if !settings.Enabled || (settings.UserConcurrencyLimit <= 0 && settings.TokenConcurrencyLimit <= 0) || !isRequestConcurrencyCandidate(c) {
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
		lease, verdict := service.AcquireRequestConcurrency(c.Request.Context(), input, settings)
		if verdict.Exceeded && settings.LogMatches && service.AcquireRequestGuardLogSlot(c.Request.Context(), "concurrency", verdict.ScopeKey) {
			recordRequestConcurrencyEvent(c, verdict, settings.Mode)
		}
		if !verdict.Allowed {
			writeRequestProtectionResponse(
				c,
				requestConcurrencyRetryAfter,
				system_setting.DefaultRequestConcurrencyMessage,
				types.ErrorCodeRequestConcurrencyLimited,
			)
			return
		}
		defer service.ReleaseRequestConcurrency(lease)
		stopHeartbeat := service.StartRequestConcurrencyLeaseHeartbeat(lease)
		defer stopHeartbeat()
		c.Next()
	}
}

func recordRequestConcurrencyEvent(c *gin.Context, verdict service.RequestConcurrencyVerdict, mode string) {
	adminInfo := map[string]interface{}{
		"endpoint":                        c.Request.URL.Path,
		"risk_factors":                    verdict.Factors,
		"request_risk_mode":               mode,
		"would_block":                     verdict.Exceeded,
		"user_in_flight":                  verdict.UserCount,
		"user_limit":                      verdict.UserLimit,
		"token_in_flight":                 verdict.TokenCount,
		"token_limit":                     verdict.TokenLimit,
		"full_request_available":          false,
		"full_request_unavailable_reason": "并发保护在读取请求体前命中",
	}
	other := map[string]interface{}{"admin_info": adminInfo}
	content := "请求并发保护观察命中"
	if !verdict.Allowed {
		content = "请求并发保护已限制请求"
	}
	model.RecordRequestGuardLog(c, content, "request_concurrency_guard", other, !verdict.Allowed)
}
