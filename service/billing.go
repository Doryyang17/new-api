package service

import (
	"fmt"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

const (
	BillingSourceWallet       = "wallet"
	BillingSourceSubscription = "subscription"
)

func validateTokenQuotaAvailable(c *gin.Context, relayInfo *relaycommon.RelayInfo, quota int) *types.NewAPIError {
	if quota <= 0 || relayInfo.IsPlayground || relayInfo.TokenUnlimited {
		return nil
	}
	if c == nil {
		return types.NewErrorWithStatusCode(fmt.Errorf("token quota context is unavailable"), types.ErrorCodePreConsumeTokenQuotaFailed, http.StatusForbidden, types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
	}
	value, exists := c.Get("token_quota")
	tokenQuota, ok := value.(int)
	if !exists || !ok {
		return types.NewErrorWithStatusCode(fmt.Errorf("token quota context is unavailable"), types.ErrorCodePreConsumeTokenQuotaFailed, http.StatusForbidden, types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
	}
	if tokenQuota < quota {
		return types.NewErrorWithStatusCode(
			fmt.Errorf("token quota is not enough, token remain quota: %s, need quota: %s", logger.FormatQuota(tokenQuota), logger.FormatQuota(quota)),
			types.ErrorCodePreConsumeTokenQuotaFailed,
			http.StatusForbidden,
			types.ErrOptionWithSkipRetry(),
			types.ErrOptionWithNoRecordErrorLog(),
		)
	}
	return nil
}

// ValidatePreConsumeBilling performs the native billing eligibility checks
// without reserving or deducting quota. Request-protection modules can run
// after this check and before PreConsumeBilling without charging rejected
// requests. PreConsumeBilling remains authoritative and rechecks atomically.
func ValidatePreConsumeBilling(c *gin.Context, preConsumedQuota int, relayInfo *relaycommon.RelayInfo) *types.NewAPIError {
	if relayInfo == nil {
		return types.NewError(fmt.Errorf("relayInfo is nil"), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}
	if relayInfo.QuotaClamp != nil {
		return types.NewErrorWithStatusCode(
			relayInfo.QuotaClamp,
			types.ErrorCodeModelPriceError,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(),
		)
	}
	if preConsumedQuota < 0 {
		return types.NewErrorWithStatusCode(
			fmt.Errorf("pre-consume quota cannot be negative: %d", preConsumedQuota),
			types.ErrorCodeModelPriceError,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(),
		)
	}

	validateWallet := func() *types.NewAPIError {
		userQuota, err := model.GetUserQuota(relayInfo.UserId, false)
		if err != nil {
			return types.NewError(err, types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry())
		}
		bonusAmount, err := model.GetActiveCheckinBonusAmount(relayInfo.UserId, time.Now())
		if err != nil {
			return types.NewError(err, types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry())
		}
		if userQuota <= 0 && bonusAmount <= 0 {
			return types.NewErrorWithStatusCode(
				fmt.Errorf("用户额度不足, 剩余额度: %s", logger.FormatQuota(userQuota)),
				types.ErrorCodeInsufficientUserQuota,
				http.StatusForbidden,
				types.ErrOptionWithSkipRetry(),
				types.ErrOptionWithNoRecordErrorLog(),
			)
		}
		if userQuota+bonusAmount-preConsumedQuota < 0 {
			return types.NewErrorWithStatusCode(
				fmt.Errorf("预扣费额度失败, 用户剩余额度: %s, 需要预扣费额度: %s", logger.FormatQuota(userQuota), logger.FormatQuota(preConsumedQuota)),
				types.ErrorCodeInsufficientUserQuota,
				http.StatusForbidden,
				types.ErrOptionWithSkipRetry(),
				types.ErrOptionWithNoRecordErrorLog(),
			)
		}
		relayInfo.UserQuota = userQuota

		effectiveQuota := preConsumedQuota
		trustQuota := common.GetTrustQuota()
		tokenTrusted := relayInfo.TokenUnlimited
		if !tokenTrusted && c != nil {
			tokenTrusted = c.GetInt("token_quota") > trustQuota
		}
		if !relayInfo.ForcePreConsume && trustQuota > 0 && tokenTrusted && userQuota > trustQuota {
			effectiveQuota = 0
		}
		return validateTokenQuotaAvailable(c, relayInfo, effectiveQuota)
	}

	validateSubscription := func() *types.NewAPIError {
		subscriptionQuota := preConsumedQuota
		if subscriptionQuota <= 0 {
			subscriptionQuota = 1
		}
		if tokenErr := validateTokenQuotaAvailable(c, relayInfo, subscriptionQuota); tokenErr != nil {
			return tokenErr
		}
		bonusAmount, err := model.GetActiveCheckinBonusAmount(relayInfo.UserId, time.Now())
		if err != nil {
			return types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
		}
		remainingQuota := subscriptionQuota - bonusAmount
		if remainingQuota <= 0 {
			return nil
		}
		available, err := model.CanPreConsumeUserSubscription(relayInfo.UserId, int64(remainingQuota))
		if err != nil {
			return types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
		}
		if !available {
			return types.NewErrorWithStatusCode(
				fmt.Errorf("订阅额度不足或未配置订阅: subscription quota insufficient, need=%d", remainingQuota),
				types.ErrorCodeInsufficientUserQuota,
				http.StatusForbidden,
				types.ErrOptionWithSkipRetry(),
				types.ErrOptionWithNoRecordErrorLog(),
			)
		}
		return nil
	}

	pref := common.NormalizeBillingPreference(relayInfo.UserSetting.BillingPreference)
	switch pref {
	case "subscription_only":
		return validateSubscription()
	case "wallet_only":
		return validateWallet()
	case "wallet_first":
		walletErr := validateWallet()
		if walletErr == nil || walletErr.GetErrorCode() != types.ErrorCodeInsufficientUserQuota {
			return walletErr
		}
		return validateSubscription()
	case "subscription_first":
		fallthrough
	default:
		hasSubscription, err := model.HasActiveUserSubscription(relayInfo.UserId)
		if err != nil {
			return types.NewError(err, types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry())
		}
		if !hasSubscription {
			return validateWallet()
		}
		subscriptionErr := validateSubscription()
		if subscriptionErr == nil || subscriptionErr.GetErrorCode() != types.ErrorCodeInsufficientUserQuota {
			return subscriptionErr
		}
		allowOverflow, err := model.UserActiveSubscriptionsAllowWalletOverflow(relayInfo.UserId)
		if err != nil {
			return types.NewError(err, types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry())
		}
		if !allowOverflow {
			return subscriptionErr
		}
		return validateWallet()
	}
}

// ValidateImmediateWalletCharge checks the wallet, check-in bonus, and token
// quota used by immediate-charge paths such as Midjourney without mutating any
// balance.
func ValidateImmediateWalletCharge(c *gin.Context, quota int, relayInfo *relaycommon.RelayInfo) *types.NewAPIError {
	if relayInfo == nil {
		return types.NewError(fmt.Errorf("relayInfo is nil"), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}
	if relayInfo.QuotaClamp != nil {
		return types.NewErrorWithStatusCode(relayInfo.QuotaClamp, types.ErrorCodeModelPriceError, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	if quota < 0 {
		return types.NewErrorWithStatusCode(fmt.Errorf("quota cannot be negative: %d", quota), types.ErrorCodeModelPriceError, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	userQuota, err := model.GetUserQuota(relayInfo.UserId, false)
	if err != nil {
		return types.NewError(err, types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry())
	}
	bonusAmount, err := model.GetActiveCheckinBonusAmount(relayInfo.UserId, time.Now())
	if err != nil {
		return types.NewError(err, types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry())
	}
	if userQuota+bonusAmount-quota < 0 {
		return types.NewErrorWithStatusCode(
			fmt.Errorf("用户额度不足, 剩余额度: %s", logger.FormatQuota(userQuota)),
			types.ErrorCodeInsufficientUserQuota,
			http.StatusForbidden,
			types.ErrOptionWithSkipRetry(),
			types.ErrOptionWithNoRecordErrorLog(),
		)
	}
	relayInfo.UserQuota = userQuota
	return validateTokenQuotaAvailable(c, relayInfo, quota)
}

// PreConsumeBilling 根据用户计费偏好创建 BillingSession 并执行预扣费。
// 会话存储在 relayInfo.Billing 上，供后续 Settle / Refund 使用。
func PreConsumeBilling(c *gin.Context, preConsumedQuota int, relayInfo *relaycommon.RelayInfo) *types.NewAPIError {
	if relayInfo != nil && relayInfo.QuotaClamp != nil {
		return types.NewErrorWithStatusCode(
			relayInfo.QuotaClamp,
			types.ErrorCodeModelPriceError,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(),
		)
	}
	if preConsumedQuota < 0 {
		return types.NewErrorWithStatusCode(
			fmt.Errorf("pre-consume quota cannot be negative: %d", preConsumedQuota),
			types.ErrorCodeModelPriceError,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(),
		)
	}
	session, apiErr := NewBillingSession(c, relayInfo, preConsumedQuota)
	if apiErr != nil {
		return apiErr
	}
	relayInfo.Billing = session
	return nil
}

// ---------------------------------------------------------------------------
// SettleBilling — 后结算辅助函数
// ---------------------------------------------------------------------------

// SettleBilling 执行计费结算。如果 RelayInfo 上有 BillingSession 则通过 session 结算，
// 否则回退到旧的 PostConsumeQuota 路径（兼容按次计费等场景）。
func SettleBilling(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, actualQuota int) error {
	if relayInfo.Billing != nil {
		preConsumed := relayInfo.Billing.GetPreConsumedQuota()
		delta := actualQuota - preConsumed

		if delta > 0 {
			logger.LogInfo(ctx, fmt.Sprintf("预扣费后补扣费：%s（实际消耗：%s，预扣费：%s）",
				logger.FormatQuota(delta),
				logger.FormatQuota(actualQuota),
				logger.FormatQuota(preConsumed),
			))
		} else if delta < 0 {
			logger.LogInfo(ctx, fmt.Sprintf("预扣费后返还扣费：%s（实际消耗：%s，预扣费：%s）",
				logger.FormatQuota(-delta),
				logger.FormatQuota(actualQuota),
				logger.FormatQuota(preConsumed),
			))
		} else {
			logger.LogInfo(ctx, fmt.Sprintf("预扣费与实际消耗一致，无需调整：%s（按次计费）",
				logger.FormatQuota(actualQuota),
			))
		}

		if err := relayInfo.Billing.Settle(actualQuota); err != nil {
			return err
		}

		// 发送额度通知（订阅计费使用订阅剩余额度）
		if relayInfo.OriginalFundingConsumed > 0 {
			if relayInfo.BillingSource == BillingSourceSubscription {
				checkAndSendSubscriptionQuotaNotify(relayInfo)
			} else {
				// The wallet warning must only account for the portion actually
				// deducted from the wallet. A bonus-only request must not make the
				// unchanged wallet look closer to exhaustion.
				checkAndSendQuotaNotify(relayInfo, relayInfo.OriginalFundingConsumed, 0)
			}
		}
		return nil
	}

	// 回退：无 BillingSession 时使用旧路径
	quotaDelta := actualQuota - relayInfo.FinalPreConsumedQuota
	if quotaDelta != 0 {
		return PostConsumeQuota(relayInfo, quotaDelta, relayInfo.FinalPreConsumedQuota, true)
	}
	return nil
}
