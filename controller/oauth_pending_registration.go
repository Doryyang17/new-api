package controller

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/oauth"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
)

const (
	oauthRegistrationCodeRequiredAction = "registration_code_required"
	oauthPendingRegistrationTTL         = 10 * time.Minute
	oauthPendingRegistrationExpiredMsg  = "OAuth 注册状态已过期，请重新使用第三方账号登录"
	oauthPendingRegistrationStorePrefix = "oauthPendingRegistration:"
)

type oauthPendingRegistration struct {
	Ticket         string `json:"ticket"`
	Provider       string `json:"provider"`
	ProviderName   string `json:"provider_name"`
	ProviderUserID string `json:"provider_user_id"`
	LegacyUserID   string `json:"legacy_user_id,omitempty"`
	Username       string `json:"username,omitempty"`
	DisplayName    string `json:"display_name,omitempty"`
	Email          string `json:"email,omitempty"`
	AffiliateCode  string `json:"affiliate_code,omitempty"`
	ExpiresAt      int64  `json:"expires_at"`
}

type completeOAuthRegistrationRequest struct {
	Ticket           string `json:"ticket"`
	RegistrationCode string `json:"registration_code"`
}

var oauthPendingRegistrationMemoryStore = struct {
	sync.Mutex
	items map[string]oauthPendingRegistration
}{
	items: make(map[string]oauthPendingRegistration),
}

func oauthPendingRegistrationStoreKey(ticket string) string {
	return oauthPendingRegistrationStorePrefix + ticket
}

func saveOAuthPendingRegistration(pending oauthPendingRegistration) error {
	oauthPendingRegistrationMemoryStore.Lock()
	now := time.Now().Unix()
	for ticket, item := range oauthPendingRegistrationMemoryStore.items {
		if item.ExpiresAt < now {
			delete(oauthPendingRegistrationMemoryStore.items, ticket)
		}
	}
	oauthPendingRegistrationMemoryStore.items[pending.Ticket] = pending
	oauthPendingRegistrationMemoryStore.Unlock()

	if common.RedisEnabled && common.RDB != nil {
		payload, err := common.Marshal(pending)
		if err != nil {
			return err
		}
		if err := common.RDB.Set(context.Background(), oauthPendingRegistrationStoreKey(pending.Ticket), string(payload), oauthPendingRegistrationTTL).Err(); err != nil {
			common.SysLog(fmt.Sprintf("save oauth pending registration to redis failed: %v", err))
		}
	}
	return nil
}

func loadOAuthPendingRegistrationByTicket(ticket string) (*oauthPendingRegistration, bool) {
	ticket = strings.TrimSpace(ticket)
	if ticket == "" {
		return nil, false
	}

	if common.RedisEnabled && common.RDB != nil {
		raw, err := common.RDB.Get(context.Background(), oauthPendingRegistrationStoreKey(ticket)).Result()
		if err == nil && strings.TrimSpace(raw) != "" {
			var pending oauthPendingRegistration
			if err := common.UnmarshalJsonStr(raw, &pending); err == nil && pending.Ticket == ticket {
				if pending.ExpiresAt < time.Now().Unix() {
					deleteOAuthPendingRegistration(ticket)
					return nil, false
				}
				return &pending, true
			}
		} else if err != nil && !errors.Is(err, redis.Nil) {
			common.SysLog(fmt.Sprintf("load oauth pending registration from redis failed: %v", err))
		}
	}

	oauthPendingRegistrationMemoryStore.Lock()
	defer oauthPendingRegistrationMemoryStore.Unlock()
	pending, ok := oauthPendingRegistrationMemoryStore.items[ticket]
	if !ok {
		return nil, false
	}
	if pending.ExpiresAt < time.Now().Unix() {
		delete(oauthPendingRegistrationMemoryStore.items, ticket)
		return nil, false
	}
	return &pending, true
}

func deleteOAuthPendingRegistration(ticket string) {
	ticket = strings.TrimSpace(ticket)
	if ticket == "" {
		return
	}
	oauthPendingRegistrationMemoryStore.Lock()
	delete(oauthPendingRegistrationMemoryStore.items, ticket)
	oauthPendingRegistrationMemoryStore.Unlock()
	if common.RedisEnabled && common.RDB != nil {
		if err := common.RDB.Del(context.Background(), oauthPendingRegistrationStoreKey(ticket)).Err(); err != nil {
			common.SysLog(fmt.Sprintf("delete oauth pending registration from redis failed: %v", err))
		}
	}
}

func storeOAuthPendingRegistration(
	c *gin.Context,
	providerName string,
	provider oauth.Provider,
	oauthUser *oauth.OAuthUser,
	affiliateCode string,
) {
	pending := oauthPendingRegistration{
		Ticket:         common.GetRandomString(32),
		Provider:       providerName,
		ProviderName:   provider.GetName(),
		ProviderUserID: oauthUser.ProviderUserID,
		Username:       oauthUser.Username,
		DisplayName:    oauthUser.DisplayName,
		Email:          oauthUser.Email,
		AffiliateCode:  strings.TrimSpace(affiliateCode),
		ExpiresAt:      time.Now().Add(oauthPendingRegistrationTTL).Unix(),
	}
	if legacyID, ok := oauthUser.Extra["legacy_id"].(string); ok {
		pending.LegacyUserID = legacyID
	}

	if err := saveOAuthPendingRegistration(pending); err != nil {
		common.ApiError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": oauthRegistrationCodeRequiredAction,
		"data": gin.H{
			"action":        oauthRegistrationCodeRequiredAction,
			"ticket":        pending.Ticket,
			"provider":      pending.Provider,
			"provider_name": pending.ProviderName,
			"display_name":  pending.DisplayName,
			"username":      pending.Username,
			"expires_at":    pending.ExpiresAt,
		},
	})
}
func (pending *oauthPendingRegistration) oauthUser() *oauth.OAuthUser {
	extra := map[string]any{}
	if pending.LegacyUserID != "" {
		extra["legacy_id"] = pending.LegacyUserID
	}
	return &oauth.OAuthUser{
		ProviderUserID: pending.ProviderUserID,
		Username:       pending.Username,
		DisplayName:    pending.DisplayName,
		Email:          pending.Email,
		Extra:          extra,
	}
}

func CompleteOAuthRegistration(c *gin.Context) {
	providerName := c.Param("provider")
	provider := oauth.GetProvider(providerName)
	if provider == nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": i18n.T(c, i18n.MsgOAuthUnknownProvider),
		})
		return
	}
	if !provider.IsEnabled() {
		common.ApiErrorI18n(c, i18n.MsgOAuthNotEnabled, providerParams(provider.GetName()))
		return
	}

	var request completeOAuthRegistrationRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	request.Ticket = strings.TrimSpace(request.Ticket)

	pending, ok := loadOAuthPendingRegistrationByTicket(request.Ticket)
	if !ok ||
		request.Ticket == "" ||
		pending.Ticket != request.Ticket ||
		pending.Provider != providerName ||
		pending.ProviderUserID == "" ||
		pending.ExpiresAt < time.Now().Unix() {
		deleteOAuthPendingRegistration(request.Ticket)
		common.ApiErrorMsg(c, oauthPendingRegistrationExpiredMsg)
		return
	}

	registrationCode, registrationRiskKeys, valid := validateRegistrationCodeForNewUser(c, request.RegistrationCode)
	if !valid {
		return
	}

	user, err := findOrCreateOAuthUserWithRegistrationCode(
		provider,
		pending.oauthUser(),
		pending.AffiliateCode,
		registrationCode,
		registrationRiskKeys,
	)
	if err != nil {
		if errors.Is(err, model.ErrRegistrationCodeInvalid) {
			common.ApiErrorMsg(c, registrationCodeInvalidMessage)
			return
		}
		switch e := err.(type) {
		case *OAuthUserDeletedError:
			common.ApiErrorI18n(c, i18n.MsgOAuthUserDeleted)
		case *OAuthRegistrationDisabledError:
			common.ApiErrorI18n(c, i18n.MsgUserRegisterDisabled)
		case *OAuthRegistrationRiskBlockedError:
			respondRegistrationRiskBlocked(c, e.RetryAfter)
		default:
			common.ApiError(c, err)
		}
		return
	}

	if user.Status != common.UserStatusEnabled {
		common.ApiErrorI18n(c, i18n.MsgOAuthUserBanned)
		return
	}

	deleteOAuthPendingRegistration(pending.Ticket)
	setupLogin(user, c)
}
