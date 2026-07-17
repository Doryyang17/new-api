package model

import (
	"errors"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

const (
	CheckinBonusStatusActive   = "active"
	CheckinBonusStatusConsumed = "consumed"
	CheckinBonusStatusExpired  = "expired"

	CheckinBonusUsageStatusReserved = "reserved"
	CheckinBonusUsageStatusSettled  = "settled"
	CheckinBonusUsageStatusRefunded = "refunded"

	CheckinBonusOriginalFundingWallet       = "wallet"
	CheckinBonusOriginalFundingSubscription = "subscription"
)

// CheckinBonus is an independent bonus balance created by a successful
// check-in. Amounts use quota units, but the awarded balance is intentionally
// isolated from users.quota and recharge records. Requests paid by this bonus
// still report their full usage to the existing logs, statistics and rankings.
type CheckinBonus struct {
	Id              int64  `json:"id" gorm:"primaryKey;autoIncrement"`
	UserId          int    `json:"user_id" gorm:"not null;index:idx_checkin_bonus_user_expire"`
	CheckinId       int    `json:"checkin_id" gorm:"not null;uniqueIndex"`
	Amount          int    `json:"amount" gorm:"not null"`
	RemainingAmount int    `json:"remaining_amount" gorm:"not null"`
	CreatedAt       int64  `json:"created_at" gorm:"not null"`
	ExpireAt        int64  `json:"expire_at" gorm:"not null;index:idx_checkin_bonus_user_expire"`
	Status          string `json:"status" gorm:"type:varchar(20);not null;index"`
}

func (CheckinBonus) TableName() string { return "checkin_bonuses" }

// CheckinBonusUsage is the request-scoped reservation ledger used to make
// pre-consume, settlement and refund idempotent and auditable.
type CheckinBonusUsage struct {
	Id                     int64  `json:"id" gorm:"primaryKey;autoIncrement"`
	RequestId              string `json:"request_id" gorm:"type:varchar(191);not null;uniqueIndex"`
	UserId                 int    `json:"user_id" gorm:"not null;index"`
	BonusId                int64  `json:"bonus_id" gorm:"not null;index"`
	ReservedAmount         int    `json:"reserved_amount" gorm:"not null"`
	ConsumedAmount         int    `json:"consumed_amount" gorm:"not null"`
	OriginalFundingSource  string `json:"original_funding_source" gorm:"type:varchar(20)"`
	OriginalFundingId      int    `json:"original_funding_id" gorm:"index"`
	OriginalReservedAmount int    `json:"original_reserved_amount"`
	OriginalConsumedAmount int    `json:"original_consumed_amount"`
	TokenId                int    `json:"token_id" gorm:"index"`
	TokenReservedAmount    int    `json:"token_reserved_amount"`
	TokenConsumedAmount    int    `json:"token_consumed_amount"`
	OwnerNode              string `json:"owner_node" gorm:"type:varchar(128);index:idx_checkin_bonus_usage_owner"`
	OwnerStartedAt         int64  `json:"owner_started_at" gorm:"index:idx_checkin_bonus_usage_owner"`
	OwnerInstanceId        string `json:"owner_instance_id" gorm:"type:varchar(191);index"`
	Status                 string `json:"status" gorm:"type:varchar(20);not null;index"`
	CreatedAt              int64  `json:"created_at" gorm:"not null"`
	UpdatedAt              int64  `json:"updated_at" gorm:"not null"`
}

func (CheckinBonusUsage) TableName() string { return "checkin_bonus_usages" }

// CheckinBonusProcessLease distinguishes a dead process from an older process
// that is still draining requests during a rolling restart.
type CheckinBonusProcessLease struct {
	InstanceId string `json:"instance_id" gorm:"type:varchar(191);primaryKey"`
	NodeName   string `json:"node_name" gorm:"type:varchar(128);index"`
	StartedAt  int64  `json:"started_at" gorm:"not null"`
	LastSeenAt int64  `json:"last_seen_at" gorm:"not null;index"`
}

func (CheckinBonusProcessLease) TableName() string { return "checkin_bonus_process_leases" }

func UpsertCheckinBonusProcessLease(instanceId string, nodeName string, startedAt int64, lastSeenAt int64) error {
	if instanceId == "" || lastSeenAt <= 0 {
		return errors.New("invalid check-in bonus process lease")
	}
	lease := &CheckinBonusProcessLease{
		InstanceId: instanceId,
		NodeName:   nodeName,
		StartedAt:  startedAt,
		LastSeenAt: lastSeenAt,
	}
	return DB.Where("instance_id = ?", instanceId).Assign(lease).FirstOrCreate(lease).Error
}

func nextLocalMidnight(now time.Time) time.Time {
	year, month, day := now.Date()
	return time.Date(year, month, day+1, 0, 0, 0, 0, now.Location())
}

func newCheckinBonus(userId int, checkinId int, amount int, now time.Time) *CheckinBonus {
	if amount <= 0 {
		return nil
	}
	return &CheckinBonus{
		UserId:          userId,
		CheckinId:       checkinId,
		Amount:          amount,
		RemainingAmount: amount,
		CreatedAt:       now.Unix(),
		ExpireAt:        nextLocalMidnight(now).Unix(),
		Status:          CheckinBonusStatusActive,
	}
}

func normalizeCheckinBonusStatus(bonus *CheckinBonus, nowUnix int64) string {
	if bonus.ExpireAt <= nowUnix {
		return CheckinBonusStatusExpired
	}
	if bonus.RemainingAmount <= 0 {
		return CheckinBonusStatusConsumed
	}
	return CheckinBonusStatusActive
}

// ExpireCheckinBonuses marks due balances as expired without deleting their
// audit records. It is safe to run concurrently on multiple nodes.
func ExpireCheckinBonuses(nowUnix int64, batchSize int) (int64, error) {
	if batchSize <= 0 {
		batchSize = 500
	}
	var ids []int64
	if err := DB.Model(&CheckinBonus{}).
		Where("status = ? AND expire_at <= ?", CheckinBonusStatusActive, nowUnix).
		Order("id ASC").Limit(batchSize).Pluck("id", &ids).Error; err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	result := DB.Model(&CheckinBonus{}).
		Where("id IN ? AND status = ? AND expire_at <= ?", ids, CheckinBonusStatusActive, nowUnix).
		Update("status", CheckinBonusStatusExpired)
	return result.RowsAffected, result.Error
}

func GetActiveCheckinBonus(userId int, now time.Time) (*CheckinBonus, error) {
	nowUnix := now.Unix()
	_ = DB.Model(&CheckinBonus{}).
		Where("user_id = ? AND status = ? AND expire_at <= ?", userId, CheckinBonusStatusActive, nowUnix).
		Update("status", CheckinBonusStatusExpired).Error

	var bonus CheckinBonus
	err := DB.Where("user_id = ? AND status = ? AND remaining_amount > 0 AND expire_at > ?",
		userId, CheckinBonusStatusActive, nowUnix).
		Order("expire_at ASC, id ASC").First(&bonus).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &bonus, nil
}

func GetActiveCheckinBonusAmount(userId int, now time.Time) (int, error) {
	bonus, err := GetActiveCheckinBonus(userId, now)
	if err != nil || bonus == nil {
		return 0, err
	}
	return bonus.RemainingAmount, nil
}

func GetLatestCheckinBonus(userId int, now time.Time) (*CheckinBonus, error) {
	nowUnix := now.Unix()
	_ = DB.Model(&CheckinBonus{}).
		Where("user_id = ? AND status = ? AND expire_at <= ?", userId, CheckinBonusStatusActive, nowUnix).
		Update("status", CheckinBonusStatusExpired).Error
	var bonus CheckinBonus
	err := DB.Where("user_id = ?", userId).Order("created_at DESC, id DESC").First(&bonus).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &bonus, nil
}

// ReserveCheckinBonus reserves up to amount for a request. Repeated calls with
// the same request ID extend the same ledger row, which is required by stream
// reservation and task settlement.
func reserveCheckinBonusTx(tx *gorm.DB, userId int, requestId string, amount int, nowUnix int64) (*CheckinBonusUsage, int, error) {
	if amount <= 0 {
		return nil, 0, nil
	}
	if requestId == "" {
		return nil, 0, errors.New("check-in bonus request id is empty")
	}
	if err := tx.Model(&CheckinBonus{}).
		Where("user_id = ? AND status = ? AND expire_at <= ?", userId, CheckinBonusStatusActive, nowUnix).
		Update("status", CheckinBonusStatusExpired).Error; err != nil {
		return nil, 0, err
	}
	var usage CheckinBonusUsage
	usageErr := lockForUpdate(tx).Where("request_id = ?", requestId).First(&usage).Error
	if usageErr != nil && !errors.Is(usageErr, gorm.ErrRecordNotFound) {
		return nil, 0, usageErr
	}
	if usageErr == nil {
		if usage.UserId != userId {
			return nil, 0, errors.New("check-in bonus request belongs to another user")
		}
		if usage.Status == CheckinBonusUsageStatusRefunded {
			return &usage, 0, nil
		}
		var bonus CheckinBonus
		if err := lockForUpdate(tx).First(&bonus, usage.BonusId).Error; err != nil {
			return nil, 0, err
		}
		if bonus.ExpireAt <= nowUnix || bonus.RemainingAmount <= 0 {
			status := normalizeCheckinBonusStatus(&bonus, nowUnix)
			if status != bonus.Status {
				if err := tx.Model(&bonus).Update("status", status).Error; err != nil {
					return nil, 0, err
				}
			}
			return &usage, 0, nil
		}
		reserved := min(amount, bonus.RemainingAmount)
		bonus.RemainingAmount -= reserved
		bonus.Status = normalizeCheckinBonusStatus(&bonus, nowUnix)
		if err := tx.Model(&bonus).Updates(map[string]interface{}{
			"remaining_amount": bonus.RemainingAmount,
			"status":           bonus.Status,
		}).Error; err != nil {
			return nil, 0, err
		}
		updates := map[string]interface{}{
			"reserved_amount": gorm.Expr("reserved_amount + ?", reserved),
			"updated_at":      nowUnix,
		}
		usage.ReservedAmount += reserved
		usage.UpdatedAt = nowUnix
		if usage.Status == CheckinBonusUsageStatusSettled {
			updates["consumed_amount"] = gorm.Expr("consumed_amount + ?", reserved)
			usage.ConsumedAmount += reserved
		}
		if err := tx.Model(&usage).Updates(updates).Error; err != nil {
			return nil, 0, err
		}
		return &usage, reserved, nil
	}

	var bonus CheckinBonus
	query := lockForUpdate(tx).
		Where("user_id = ? AND status = ? AND remaining_amount > 0 AND expire_at > ?",
			userId, CheckinBonusStatusActive, nowUnix).
		Order("expire_at ASC, id ASC")
	if err := query.First(&bonus).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, 0, nil
	} else if err != nil {
		return nil, 0, err
	}

	reserved := min(amount, bonus.RemainingAmount)
	bonus.RemainingAmount -= reserved
	bonus.Status = normalizeCheckinBonusStatus(&bonus, nowUnix)
	if err := tx.Model(&bonus).Updates(map[string]interface{}{
		"remaining_amount": bonus.RemainingAmount,
		"status":           bonus.Status,
	}).Error; err != nil {
		return nil, 0, err
	}
	usage = CheckinBonusUsage{
		RequestId:      requestId,
		UserId:         userId,
		BonusId:        bonus.Id,
		ReservedAmount: reserved,
		Status:         CheckinBonusUsageStatusReserved,
		CreatedAt:      nowUnix,
		UpdatedAt:      nowUnix,
	}
	if err := tx.Create(&usage).Error; err != nil {
		return nil, 0, err
	}
	return &usage, reserved, nil
}

func ReserveCheckinBonus(userId int, requestId string, amount int, now time.Time) (int, error) {
	reserved := 0
	err := DB.Transaction(func(tx *gorm.DB) error {
		_, value, err := reserveCheckinBonusTx(tx, userId, requestId, amount, now.Unix())
		reserved = value
		return err
	})
	return reserved, err
}

func syncCheckinBonusWalletQuotaCache(userId int, quotaDelta int) {
	if quotaDelta == 0 {
		return
	}
	var err error
	if quotaDelta > 0 {
		err = cacheIncrUserQuota(userId, int64(quotaDelta))
	} else {
		err = cacheDecrUserQuota(userId, int64(-quotaDelta))
	}
	if err != nil {
		common.SysLog("failed to sync user quota cache after check-in bonus funding update: " + err.Error())
	}
}

func addCheckinBonusTokenReservation(usage *CheckinBonusUsage, tokenId int, amount int) error {
	if usage == nil || tokenId <= 0 || amount <= 0 {
		return nil
	}
	if usage.TokenId != 0 && usage.TokenId != tokenId {
		return errors.New("check-in bonus request uses another token")
	}
	usage.TokenId = tokenId
	usage.TokenReservedAmount += amount
	return nil
}

type CheckinBonusImmediateChargeParams struct {
	UserId                int
	RequestId             string
	Amount                int
	OriginalFundingSource string
	SubscriptionId        int
	OwnerNode             string
	OwnerStartedAt        int64
	OwnerInstanceId       string
	TokenId               int
	TokenKey              string
	DeductToken           bool
	MidjourneyTaskId      int
	Now                   time.Time
}

type CheckinBonusImmediateChargeResult struct {
	BonusConsumed               int
	OriginalConsumed            int
	SubscriptionId              int
	SubscriptionAmountTotal     int64
	SubscriptionAmountUsedAfter int64
}

// ConsumeCheckinBonusFunding immediately settles a positive charge. The
// bonus/original funding split, token quota and optional Midjourney billing
// metadata are committed in one database transaction, so callers never leave
// a live-process reservation behind when a later step fails.
func ConsumeCheckinBonusFunding(params CheckinBonusImmediateChargeParams) (*CheckinBonusImmediateChargeResult, error) {
	if params.UserId <= 0 || params.Amount <= 0 || params.RequestId == "" {
		return nil, errors.New("invalid immediate check-in bonus charge")
	}
	if params.Now.IsZero() {
		params.Now = time.Now()
	}
	if params.OriginalFundingSource != CheckinBonusOriginalFundingWallet &&
		params.OriginalFundingSource != CheckinBonusOriginalFundingSubscription {
		return nil, errors.New("invalid immediate charge funding source")
	}
	if params.OriginalFundingSource == CheckinBonusOriginalFundingSubscription && params.SubscriptionId <= 0 {
		return nil, errors.New("subscription id is missing")
	}
	if params.DeductToken && params.TokenId <= 0 {
		return nil, errors.New("token id is missing")
	}

	nowUnix := params.Now.Unix()
	result := &CheckinBonusImmediateChargeResult{SubscriptionId: params.SubscriptionId}
	err := DB.Transaction(func(tx *gorm.DB) error {
		usage, bonusConsumed, reserveErr := reserveCheckinBonusTx(
			tx, params.UserId, params.RequestId, params.Amount, nowUnix,
		)
		if reserveErr != nil {
			return reserveErr
		}
		if usage == nil {
			usage = &CheckinBonusUsage{
				RequestId: params.RequestId,
				UserId:    params.UserId,
				Status:    CheckinBonusUsageStatusReserved,
				CreatedAt: nowUnix,
				UpdatedAt: nowUnix,
			}
			if createErr := tx.Create(usage).Error; createErr != nil {
				return createErr
			}
		}
		if usage.Status != CheckinBonusUsageStatusReserved {
			return errors.New("immediate check-in bonus usage is not reservable")
		}
		if usage.OriginalFundingSource != "" && usage.OriginalFundingSource != params.OriginalFundingSource {
			return errors.New("check-in bonus request uses another funding source")
		}

		originalConsumed := params.Amount - bonusConsumed
		switch params.OriginalFundingSource {
		case CheckinBonusOriginalFundingWallet:
			if originalConsumed > 0 {
				var user User
				if lockErr := lockForUpdate(tx).Select("id", "quota").First(&user, params.UserId).Error; lockErr != nil {
					return lockErr
				}
				if user.Quota < originalConsumed {
					return fmt.Errorf("insufficient user quota for immediate charge: have=%d need=%d", user.Quota, originalConsumed)
				}
				if updateErr := tx.Model(&User{}).Where("id = ?", params.UserId).
					Update("quota", gorm.Expr("quota - ?", originalConsumed)).Error; updateErr != nil {
					return updateErr
				}
			}
		case CheckinBonusOriginalFundingSubscription:
			if originalConsumed > 0 {
				if updateErr := postConsumeUserSubscriptionDeltaTx(tx, params.SubscriptionId, int64(originalConsumed)); updateErr != nil {
					return updateErr
				}
			}
			var subscription UserSubscription
			if loadErr := lockForUpdate(tx).First(&subscription, params.SubscriptionId).Error; loadErr != nil {
				return loadErr
			}
			result.SubscriptionAmountTotal = subscription.AmountTotal
			result.SubscriptionAmountUsedAfter = subscription.AmountUsed
		}

		if params.DeductToken {
			if trackErr := addCheckinBonusTokenReservation(usage, params.TokenId, params.Amount); trackErr != nil {
				return trackErr
			}
			update := tx.Model(&Token{}).Where("id = ?", params.TokenId).Updates(map[string]interface{}{
				"remain_quota":  gorm.Expr("remain_quota - ?", params.Amount),
				"used_quota":    gorm.Expr("used_quota + ?", params.Amount),
				"accessed_time": nowUnix,
			})
			if update.Error != nil {
				return update.Error
			}
			if update.RowsAffected != 1 {
				return errors.New("token not found for immediate charge")
			}
		}

		usage.OriginalFundingSource = params.OriginalFundingSource
		usage.OriginalFundingId = params.SubscriptionId
		usage.OriginalReservedAmount += originalConsumed
		usage.OriginalConsumedAmount = usage.OriginalReservedAmount
		usage.ConsumedAmount = usage.ReservedAmount
		usage.TokenConsumedAmount = usage.TokenReservedAmount
		usage.OwnerNode = params.OwnerNode
		usage.OwnerStartedAt = params.OwnerStartedAt
		usage.OwnerInstanceId = params.OwnerInstanceId
		usage.Status = CheckinBonusUsageStatusSettled
		usage.UpdatedAt = nowUnix
		if updateErr := tx.Model(usage).Updates(map[string]interface{}{
			"consumed_amount":          usage.ConsumedAmount,
			"original_funding_source":  usage.OriginalFundingSource,
			"original_funding_id":      usage.OriginalFundingId,
			"original_reserved_amount": usage.OriginalReservedAmount,
			"original_consumed_amount": usage.OriginalConsumedAmount,
			"token_id":                 usage.TokenId,
			"token_reserved_amount":    usage.TokenReservedAmount,
			"token_consumed_amount":    usage.TokenConsumedAmount,
			"owner_node":               usage.OwnerNode,
			"owner_started_at":         usage.OwnerStartedAt,
			"owner_instance_id":        usage.OwnerInstanceId,
			"status":                   usage.Status,
			"updated_at":               usage.UpdatedAt,
		}).Error; updateErr != nil {
			return updateErr
		}

		if params.MidjourneyTaskId > 0 {
			update := tx.Model(&Midjourney{}).
				Where("id = ? AND billing_status = ?", params.MidjourneyTaskId, MidjourneyBillingStatusPending).
				Updates(map[string]interface{}{
					"billing_request_id":     params.RequestId,
					"billing_source":         params.OriginalFundingSource,
					"subscription_id":        params.SubscriptionId,
					"checkin_bonus_consumed": usage.ConsumedAmount,
					"token_id":               params.TokenId,
					"billing_status":         MidjourneyBillingStatusCharged,
					"billing_pending_at":     0,
				})
			if update.Error != nil {
				return update.Error
			}
			if update.RowsAffected != 1 {
				return errors.New("midjourney task is not pending billing")
			}
		}

		result.BonusConsumed = usage.ConsumedAmount
		result.OriginalConsumed = usage.OriginalConsumedAmount
		return nil
	})
	if err != nil {
		return nil, err
	}
	if params.OriginalFundingSource == CheckinBonusOriginalFundingWallet {
		syncCheckinBonusWalletQuotaCache(params.UserId, -result.OriginalConsumed)
	}
	if params.DeductToken && common.RedisEnabled {
		if cacheErr := cacheDecrTokenQuota(params.TokenKey, int64(params.Amount)); cacheErr != nil {
			common.SysLog("failed to sync token quota cache after immediate charge: " + cacheErr.Error())
		}
	}
	return result, nil
}

// ReserveCheckinBonusWallet atomically reserves the independent bonus and the
// wallet remainder. When no bonus participates, tracked is false and the
// caller keeps using the ordinary wallet path.
func ReserveCheckinBonusWallet(
	userId int,
	requestId string,
	amount int,
	now time.Time,
	ownerNode string,
	ownerStartedAt int64,
	ownerInstanceId string,
	tokenId int,
	requireSufficientWallet bool,
) (bonusReserved int, walletReserved int, tracked bool, err error) {
	if amount <= 0 {
		return 0, 0, false, nil
	}
	nowUnix := now.Unix()
	err = DB.Transaction(func(tx *gorm.DB) error {
		usage, reserved, reserveErr := reserveCheckinBonusTx(tx, userId, requestId, amount, nowUnix)
		if reserveErr != nil {
			return reserveErr
		}
		bonusReserved = reserved
		walletReserved = amount - reserved
		if usage == nil {
			return nil
		}
		if usage.Status != CheckinBonusUsageStatusReserved {
			return errors.New("check-in bonus wallet usage is not reservable")
		}
		if usage.OriginalFundingSource != "" && usage.OriginalFundingSource != CheckinBonusOriginalFundingWallet {
			return errors.New("check-in bonus request uses another funding source")
		}
		tracked = true
		if walletReserved > 0 {
			var user User
			if lockErr := lockForUpdate(tx).Select("id", "quota").First(&user, userId).Error; lockErr != nil {
				return lockErr
			}
			if requireSufficientWallet && user.Quota < walletReserved {
				return errors.New("insufficient user quota for check-in bonus wallet reservation")
			}
			if updateErr := tx.Model(&User{}).Where("id = ?", userId).
				Update("quota", gorm.Expr("quota - ?", walletReserved)).Error; updateErr != nil {
				return updateErr
			}
		}
		usage.OriginalFundingSource = CheckinBonusOriginalFundingWallet
		usage.OriginalFundingId = 0
		usage.OriginalReservedAmount += walletReserved
		usage.OwnerNode = ownerNode
		usage.OwnerStartedAt = ownerStartedAt
		usage.OwnerInstanceId = ownerInstanceId
		if trackErr := addCheckinBonusTokenReservation(usage, tokenId, amount); trackErr != nil {
			return trackErr
		}
		usage.UpdatedAt = nowUnix
		return tx.Model(usage).Updates(map[string]interface{}{
			"original_funding_source":  usage.OriginalFundingSource,
			"original_funding_id":      usage.OriginalFundingId,
			"original_reserved_amount": usage.OriginalReservedAmount,
			"owner_node":               usage.OwnerNode,
			"owner_started_at":         usage.OwnerStartedAt,
			"owner_instance_id":        usage.OwnerInstanceId,
			"token_id":                 usage.TokenId,
			"token_reserved_amount":    usage.TokenReservedAmount,
			"updated_at":               usage.UpdatedAt,
		}).Error
	})
	if err != nil {
		return 0, 0, false, err
	}
	if !tracked {
		return 0, amount, false, nil
	}
	syncCheckinBonusWalletQuotaCache(userId, -walletReserved)
	return bonusReserved, walletReserved, true, nil
}

// SettleCheckinBonusWalletCharge atomically commits a postpaid wallet charge
// when a trusted request did not pre-consume funding. When no bonus is active,
// tracked is false and the caller keeps using the ordinary wallet settlement.
func SettleCheckinBonusWalletCharge(
	userId int,
	requestId string,
	amount int,
	now time.Time,
	ownerNode string,
	ownerStartedAt int64,
	ownerInstanceId string,
) (bonusConsumed int, walletConsumed int, tracked bool, err error) {
	if amount <= 0 {
		return 0, 0, false, nil
	}
	nowUnix := now.Unix()
	err = DB.Transaction(func(tx *gorm.DB) error {
		usage, reserved, reserveErr := reserveCheckinBonusTx(tx, userId, requestId, amount, nowUnix)
		if reserveErr != nil {
			return reserveErr
		}
		bonusConsumed = reserved
		walletConsumed = amount - reserved
		if usage == nil {
			return nil
		}
		if usage.Status != CheckinBonusUsageStatusReserved {
			return errors.New("postpaid check-in bonus wallet usage is not reservable")
		}
		if usage.OriginalFundingSource != "" && usage.OriginalFundingSource != CheckinBonusOriginalFundingWallet {
			return errors.New("check-in bonus request uses another funding source")
		}
		tracked = true
		if walletConsumed > 0 {
			var user User
			if lockErr := lockForUpdate(tx).Select("id", "quota").First(&user, userId).Error; lockErr != nil {
				return lockErr
			}
			if updateErr := tx.Model(&User{}).Where("id = ?", userId).
				Update("quota", gorm.Expr("quota - ?", walletConsumed)).Error; updateErr != nil {
				return updateErr
			}
		}

		usage.OriginalFundingSource = CheckinBonusOriginalFundingWallet
		usage.OriginalFundingId = 0
		usage.OriginalReservedAmount += walletConsumed
		usage.OriginalConsumedAmount = usage.OriginalReservedAmount
		usage.ConsumedAmount = usage.ReservedAmount
		usage.OwnerNode = ownerNode
		usage.OwnerStartedAt = ownerStartedAt
		usage.OwnerInstanceId = ownerInstanceId
		usage.Status = CheckinBonusUsageStatusSettled
		usage.UpdatedAt = nowUnix
		return tx.Model(usage).Updates(map[string]interface{}{
			"consumed_amount":          usage.ConsumedAmount,
			"original_funding_source":  usage.OriginalFundingSource,
			"original_funding_id":      usage.OriginalFundingId,
			"original_reserved_amount": usage.OriginalReservedAmount,
			"original_consumed_amount": usage.OriginalConsumedAmount,
			"owner_node":               usage.OwnerNode,
			"owner_started_at":         usage.OwnerStartedAt,
			"owner_instance_id":        usage.OwnerInstanceId,
			"status":                   usage.Status,
			"updated_at":               usage.UpdatedAt,
		}).Error
	})
	if err != nil {
		return 0, 0, false, err
	}
	if !tracked {
		return 0, amount, false, nil
	}
	syncCheckinBonusWalletQuotaCache(userId, -walletConsumed)
	return bonusConsumed, walletConsumed, true, nil
}

// ReserveCheckinBonusSubscription atomically reserves the independent bonus
// and the remaining subscription quota in the same database transaction.
func ReserveCheckinBonusSubscription(
	userId int,
	requestId string,
	modelName string,
	amount int,
	now time.Time,
	ownerNode string,
	ownerStartedAt int64,
	ownerInstanceId string,
	tokenId int,
) (bonusReserved int, subscriptionReserved int, tracked bool, subscriptionResult *SubscriptionPreConsumeResult, err error) {
	if amount <= 0 {
		return 0, 0, false, nil, nil
	}
	nowUnix := now.Unix()
	err = DB.Transaction(func(tx *gorm.DB) error {
		usage, reserved, reserveErr := reserveCheckinBonusTx(tx, userId, requestId, amount, nowUnix)
		if reserveErr != nil {
			return reserveErr
		}
		bonusReserved = reserved
		subscriptionReserved = amount - reserved
		if usage == nil {
			return nil
		}
		if usage.Status != CheckinBonusUsageStatusReserved {
			return errors.New("check-in bonus subscription usage is not reservable")
		}
		if usage.OriginalFundingSource != "" && usage.OriginalFundingSource != CheckinBonusOriginalFundingSubscription {
			return errors.New("check-in bonus request uses another funding source")
		}
		tracked = true

		if subscriptionReserved > 0 {
			if usage.OriginalFundingId > 0 {
				if updateErr := postConsumeUserSubscriptionDeltaTx(tx, usage.OriginalFundingId, int64(subscriptionReserved)); updateErr != nil {
					return updateErr
				}
			} else {
				result, preConsumeErr := preConsumeUserSubscriptionTx(
					tx,
					requestId,
					userId,
					modelName,
					0,
					int64(subscriptionReserved),
					nowUnix,
				)
				if preConsumeErr != nil {
					return preConsumeErr
				}
				subscriptionResult = result
				usage.OriginalFundingId = result.UserSubscriptionId
			}
		}

		usage.OriginalFundingSource = CheckinBonusOriginalFundingSubscription
		usage.OriginalReservedAmount += subscriptionReserved
		usage.OwnerNode = ownerNode
		usage.OwnerStartedAt = ownerStartedAt
		usage.OwnerInstanceId = ownerInstanceId
		if trackErr := addCheckinBonusTokenReservation(usage, tokenId, amount); trackErr != nil {
			return trackErr
		}
		usage.UpdatedAt = nowUnix
		return tx.Model(usage).Updates(map[string]interface{}{
			"original_funding_source":  usage.OriginalFundingSource,
			"original_funding_id":      usage.OriginalFundingId,
			"original_reserved_amount": usage.OriginalReservedAmount,
			"owner_node":               usage.OwnerNode,
			"owner_started_at":         usage.OwnerStartedAt,
			"owner_instance_id":        usage.OwnerInstanceId,
			"token_id":                 usage.TokenId,
			"token_reserved_amount":    usage.TokenReservedAmount,
			"updated_at":               usage.UpdatedAt,
		}).Error
	})
	if err != nil {
		return 0, 0, false, nil, err
	}
	if !tracked {
		return 0, amount, false, nil, nil
	}
	return bonusReserved, subscriptionReserved, true, subscriptionResult, nil
}

// ReleaseCheckinBonusFundingReservation atomically rolls back the latest
// stream reservation across the bonus and its original funding source.
func ReleaseCheckinBonusFundingReservation(
	requestId string,
	bonusAmount int,
	originalAmount int,
	now time.Time,
) error {
	if requestId == "" || (bonusAmount <= 0 && originalAmount <= 0) {
		return nil
	}
	nowUnix := now.Unix()
	userId := 0
	walletRefund := 0
	err := DB.Transaction(func(tx *gorm.DB) error {
		var usage CheckinBonusUsage
		if err := lockForUpdate(tx).Where("request_id = ?", requestId).First(&usage).Error; err != nil {
			return err
		}
		if usage.Status != CheckinBonusUsageStatusReserved {
			return errors.New("check-in bonus wallet reservation is not reserved")
		}
		if bonusAmount > usage.ReservedAmount || originalAmount > usage.OriginalReservedAmount {
			return errors.New("check-in bonus wallet rollback exceeds reserved amount")
		}
		tokenAmount := 0
		if usage.TokenId > 0 {
			tokenAmount = bonusAmount + originalAmount
			if tokenAmount > usage.TokenReservedAmount {
				return errors.New("check-in bonus token rollback exceeds reserved amount")
			}
		}
		if bonusAmount > 0 {
			var bonus CheckinBonus
			if err := lockForUpdate(tx).First(&bonus, usage.BonusId).Error; err != nil {
				return err
			}
			if err := restoreCheckinBonusAmount(tx, &bonus, bonusAmount, nowUnix); err != nil {
				return err
			}
		}
		if originalAmount > 0 {
			switch usage.OriginalFundingSource {
			case CheckinBonusOriginalFundingWallet:
				if err := tx.Model(&User{}).Where("id = ?", usage.UserId).
					Update("quota", gorm.Expr("quota + ?", originalAmount)).Error; err != nil {
					return err
				}
				walletRefund = originalAmount
			case CheckinBonusOriginalFundingSubscription:
				if usage.OriginalFundingId <= 0 {
					return errors.New("subscription id is missing for check-in bonus rollback")
				}
				if err := postConsumeUserSubscriptionDeltaTx(tx, usage.OriginalFundingId, -int64(originalAmount)); err != nil {
					return err
				}
			default:
				return errors.New("check-in bonus original funding source is missing")
			}
		}
		usage.ReservedAmount -= bonusAmount
		usage.OriginalReservedAmount -= originalAmount
		usage.TokenReservedAmount -= tokenAmount
		usage.UpdatedAt = nowUnix
		if usage.ReservedAmount == 0 && usage.OriginalReservedAmount == 0 && usage.TokenReservedAmount == 0 {
			usage.Status = CheckinBonusUsageStatusRefunded
			if usage.OriginalFundingSource == CheckinBonusOriginalFundingSubscription {
				if err := tx.Model(&SubscriptionPreConsumeRecord{}).
					Where("request_id = ?", requestId).
					Update("status", "refunded").Error; err != nil {
					return err
				}
			}
		}
		userId = usage.UserId
		return tx.Model(&usage).Updates(map[string]interface{}{
			"reserved_amount":          usage.ReservedAmount,
			"original_reserved_amount": usage.OriginalReservedAmount,
			"token_reserved_amount":    usage.TokenReservedAmount,
			"status":                   usage.Status,
			"updated_at":               usage.UpdatedAt,
		}).Error
	})
	if err == nil {
		syncCheckinBonusWalletQuotaCache(userId, walletRefund)
	}
	return err
}

func ReleaseCheckinBonusWalletReservation(requestId string, bonusAmount int, walletAmount int, now time.Time) error {
	return ReleaseCheckinBonusFundingReservation(requestId, bonusAmount, walletAmount, now)
}

func restoreCheckinBonusAmount(tx *gorm.DB, bonus *CheckinBonus, amount int, nowUnix int64) error {
	if amount <= 0 {
		return nil
	}
	bonus.RemainingAmount += amount
	if bonus.RemainingAmount > bonus.Amount {
		return fmt.Errorf("check-in bonus refund exceeds awarded amount: bonus=%d", bonus.Id)
	}
	bonus.Status = normalizeCheckinBonusStatus(bonus, nowUnix)
	return tx.Model(bonus).Updates(map[string]interface{}{
		"remaining_amount": bonus.RemainingAmount,
		"status":           bonus.Status,
	}).Error
}

// ReleaseCheckinBonusReservation rolls back a still-reserved amount. It is
// used when the original funding source or token reservation fails.
func ReleaseCheckinBonusReservation(requestId string, amount int, now time.Time) error {
	if requestId == "" || amount <= 0 {
		return nil
	}
	nowUnix := now.Unix()
	return DB.Transaction(func(tx *gorm.DB) error {
		var usage CheckinBonusUsage
		if err := lockForUpdate(tx).Where("request_id = ?", requestId).First(&usage).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		if usage.Status != CheckinBonusUsageStatusReserved {
			return nil
		}
		release := min(amount, usage.ReservedAmount)
		if release <= 0 {
			return nil
		}
		var bonus CheckinBonus
		if err := lockForUpdate(tx).First(&bonus, usage.BonusId).Error; err != nil {
			return err
		}
		if err := restoreCheckinBonusAmount(tx, &bonus, release, nowUnix); err != nil {
			return err
		}
		usage.ReservedAmount -= release
		usage.UpdatedAt = nowUnix
		if usage.ReservedAmount == 0 {
			usage.Status = CheckinBonusUsageStatusRefunded
		}
		return tx.Model(&usage).Updates(map[string]interface{}{
			"reserved_amount": usage.ReservedAmount,
			"status":          usage.Status,
			"updated_at":      usage.UpdatedAt,
		}).Error
	})
}

// SettleCheckinBonusUsage finalizes a request and returns any over-reserved
// bonus. Repeating the same settlement is a no-op.
func SettleCheckinBonusUsage(requestId string, consumed int, now time.Time) error {
	if requestId == "" {
		return nil
	}
	if consumed < 0 {
		return errors.New("check-in bonus consumed amount cannot be negative")
	}
	nowUnix := now.Unix()
	return DB.Transaction(func(tx *gorm.DB) error {
		var usage CheckinBonusUsage
		if err := lockForUpdate(tx).Where("request_id = ?", requestId).First(&usage).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) && consumed == 0 {
				return nil
			}
			return err
		}
		if usage.Status == CheckinBonusUsageStatusSettled && usage.ConsumedAmount == consumed {
			return nil
		}
		if usage.Status != CheckinBonusUsageStatusReserved || consumed > usage.ReservedAmount {
			return errors.New("invalid check-in bonus settlement state")
		}
		refund := usage.ReservedAmount - consumed
		if refund > 0 {
			var bonus CheckinBonus
			if err := lockForUpdate(tx).First(&bonus, usage.BonusId).Error; err != nil {
				return err
			}
			if err := restoreCheckinBonusAmount(tx, &bonus, refund, nowUnix); err != nil {
				return err
			}
		}
		return tx.Model(&usage).Updates(map[string]interface{}{
			"consumed_amount": consumed,
			"status":          CheckinBonusUsageStatusSettled,
			"updated_at":      nowUnix,
		}).Error
	})
}

// SettleCheckinBonusFundingUsage atomically finalizes the bonus/original split.
// handled is false when the request never used a check-in bonus ledger.
func SettleCheckinBonusFundingUsage(
	requestId string,
	bonusConsumed int,
	walletConsumed int,
	now time.Time,
) (handled bool, err error) {
	if requestId == "" {
		return false, nil
	}
	if bonusConsumed < 0 || walletConsumed < 0 {
		return false, errors.New("check-in bonus wallet consumed amount cannot be negative")
	}
	nowUnix := now.Unix()
	userId := 0
	walletQuotaDelta := 0
	err = DB.Transaction(func(tx *gorm.DB) error {
		var usage CheckinBonusUsage
		if findErr := lockForUpdate(tx).Where("request_id = ?", requestId).First(&usage).Error; findErr != nil {
			if errors.Is(findErr, gorm.ErrRecordNotFound) {
				return nil
			}
			return findErr
		}
		if usage.OriginalFundingSource != CheckinBonusOriginalFundingWallet &&
			usage.OriginalFundingSource != CheckinBonusOriginalFundingSubscription {
			return nil
		}
		handled = true
		if usage.Status == CheckinBonusUsageStatusSettled &&
			usage.ConsumedAmount == bonusConsumed &&
			usage.OriginalConsumedAmount == walletConsumed {
			return nil
		}
		if usage.Status != CheckinBonusUsageStatusReserved || bonusConsumed > usage.ReservedAmount {
			return errors.New("invalid check-in bonus wallet settlement state")
		}
		tokenConsumed := 0
		if usage.TokenId > 0 {
			tokenConsumed = bonusConsumed + walletConsumed
			if tokenConsumed > usage.TokenReservedAmount {
				return errors.New("check-in bonus token settlement exceeds reserved amount")
			}
		}

		bonusRefund := usage.ReservedAmount - bonusConsumed
		if bonusRefund > 0 {
			var bonus CheckinBonus
			if lockErr := lockForUpdate(tx).First(&bonus, usage.BonusId).Error; lockErr != nil {
				return lockErr
			}
			if restoreErr := restoreCheckinBonusAmount(tx, &bonus, bonusRefund, nowUnix); restoreErr != nil {
				return restoreErr
			}
		}

		walletDelta := walletConsumed - usage.OriginalReservedAmount
		if walletDelta != 0 {
			switch usage.OriginalFundingSource {
			case CheckinBonusOriginalFundingWallet:
				if walletDelta > 0 {
					if updateErr := tx.Model(&User{}).Where("id = ?", usage.UserId).
						Update("quota", gorm.Expr("quota - ?", walletDelta)).Error; updateErr != nil {
						return updateErr
					}
				} else if updateErr := tx.Model(&User{}).Where("id = ?", usage.UserId).
					Update("quota", gorm.Expr("quota + ?", -walletDelta)).Error; updateErr != nil {
					return updateErr
				}
			case CheckinBonusOriginalFundingSubscription:
				if usage.OriginalFundingId <= 0 {
					return errors.New("subscription id is missing for check-in bonus settlement")
				}
				if updateErr := postConsumeUserSubscriptionDeltaTx(tx, usage.OriginalFundingId, int64(walletDelta)); updateErr != nil {
					return updateErr
				}
			}
		}

		userId = usage.UserId
		if usage.OriginalFundingSource == CheckinBonusOriginalFundingWallet {
			walletQuotaDelta = -walletDelta
		} else if updateErr := tx.Model(&SubscriptionPreConsumeRecord{}).
			Where("request_id = ?", requestId).
			Update("status", "settled").Error; updateErr != nil {
			return updateErr
		}
		return tx.Model(&usage).Updates(map[string]interface{}{
			"consumed_amount":          bonusConsumed,
			"original_consumed_amount": walletConsumed,
			"token_consumed_amount":    tokenConsumed,
			"status":                   CheckinBonusUsageStatusSettled,
			"updated_at":               nowUnix,
		}).Error
	})
	if err == nil && handled {
		syncCheckinBonusWalletQuotaCache(userId, walletQuotaDelta)
	}
	return handled, err
}

func SettleCheckinBonusWalletUsage(requestId string, bonusConsumed int, walletConsumed int, now time.Time) (bool, error) {
	return SettleCheckinBonusFundingUsage(requestId, bonusConsumed, walletConsumed, now)
}

// RefundCheckinBonusFundingUsage atomically restores the bonus and its original
// wallet/subscription source. Repeated calls are idempotent.
func RefundCheckinBonusFundingUsage(requestId string, now time.Time) (bonusRefunded int, originalRefunded int, tokenRefunded int, tokenHandled bool, handled bool, err error) {
	if requestId == "" {
		return 0, 0, 0, false, false, nil
	}
	nowUnix := now.Unix()
	userId := 0
	walletSource := false
	tokenKey := ""
	err = DB.Transaction(func(tx *gorm.DB) error {
		var usage CheckinBonusUsage
		if findErr := lockForUpdate(tx).Where("request_id = ?", requestId).First(&usage).Error; findErr != nil {
			if errors.Is(findErr, gorm.ErrRecordNotFound) {
				return nil
			}
			return findErr
		}
		if usage.OriginalFundingSource != CheckinBonusOriginalFundingWallet &&
			usage.OriginalFundingSource != CheckinBonusOriginalFundingSubscription {
			return nil
		}
		handled = true
		walletSource = usage.OriginalFundingSource == CheckinBonusOriginalFundingWallet
		tokenHandled = usage.TokenId > 0
		if usage.Status == CheckinBonusUsageStatusRefunded {
			return nil
		}
		bonusRefunded = usage.ReservedAmount
		originalRefunded = usage.OriginalReservedAmount
		if usage.Status == CheckinBonusUsageStatusSettled {
			bonusRefunded = usage.ConsumedAmount
			originalRefunded = usage.OriginalConsumedAmount
			tokenRefunded = usage.TokenConsumedAmount
		} else {
			tokenRefunded = usage.TokenReservedAmount
		}
		if bonusRefunded > 0 {
			var bonus CheckinBonus
			if lockErr := lockForUpdate(tx).First(&bonus, usage.BonusId).Error; lockErr != nil {
				return lockErr
			}
			if restoreErr := restoreCheckinBonusAmount(tx, &bonus, bonusRefunded, nowUnix); restoreErr != nil {
				return restoreErr
			}
		}
		if originalRefunded > 0 {
			switch usage.OriginalFundingSource {
			case CheckinBonusOriginalFundingWallet:
				if updateErr := tx.Model(&User{}).Where("id = ?", usage.UserId).
					Update("quota", gorm.Expr("quota + ?", originalRefunded)).Error; updateErr != nil {
					return updateErr
				}
			case CheckinBonusOriginalFundingSubscription:
				if usage.OriginalFundingId <= 0 {
					return errors.New("subscription id is missing for check-in bonus refund")
				}
				if updateErr := postConsumeUserSubscriptionDeltaTx(tx, usage.OriginalFundingId, -int64(originalRefunded)); updateErr != nil {
					return updateErr
				}
			}
		}
		if tokenRefunded > 0 {
			if usage.TokenId <= 0 {
				return errors.New("token id is missing for check-in bonus refund")
			}
			var token Token
			loadErr := lockForUpdate(tx).Select("id", "key").First(&token, usage.TokenId).Error
			if errors.Is(loadErr, gorm.ErrRecordNotFound) {
				// The token can be deleted while an asynchronous request is still
				// running. Its missing quota row no longer needs restoration and
				// must not block the user's bonus/wallet/subscription refund.
				tokenRefunded = 0
			} else if loadErr != nil {
				return loadErr
			} else {
				tokenKey = token.Key
				if updateErr := tx.Model(&Token{}).Where("id = ?", usage.TokenId).Updates(map[string]interface{}{
					"remain_quota":  gorm.Expr("remain_quota + ?", tokenRefunded),
					"used_quota":    gorm.Expr("used_quota - ?", tokenRefunded),
					"accessed_time": nowUnix,
				}).Error; updateErr != nil {
					return updateErr
				}
			}
		}
		if usage.OriginalFundingSource == CheckinBonusOriginalFundingSubscription {
			if updateErr := tx.Model(&SubscriptionPreConsumeRecord{}).
				Where("request_id = ?", requestId).
				Update("status", "refunded").Error; updateErr != nil {
				return updateErr
			}
		}
		userId = usage.UserId
		return tx.Model(&usage).Updates(map[string]interface{}{
			"status":     CheckinBonusUsageStatusRefunded,
			"updated_at": nowUnix,
		}).Error
	})
	if err == nil && handled && walletSource {
		syncCheckinBonusWalletQuotaCache(userId, originalRefunded)
	}
	if err == nil && handled && tokenRefunded > 0 && common.RedisEnabled {
		if cacheErr := cacheIncrTokenQuota(tokenKey, int64(tokenRefunded)); cacheErr != nil {
			common.SysLog("failed to sync token quota cache after check-in bonus refund: " + cacheErr.Error())
		}
	}
	return bonusRefunded, originalRefunded, tokenRefunded, tokenHandled, handled, err
}

func RefundCheckinBonusWalletUsage(requestId string, now time.Time) (int, int, int, bool, bool, error) {
	return RefundCheckinBonusFundingUsage(requestId, now)
}

// RecoverOrphanedCheckinBonusUsages refunds reservations only after the owner
// process lease has gone stale. This permits old and new process incarnations
// to overlap safely during rolling restarts.
func RecoverOrphanedCheckinBonusUsages(currentInstanceId string, now time.Time, staleAfter time.Duration, batchSize int) (int, error) {
	if currentInstanceId == "" {
		return 0, nil
	}
	if staleAfter <= 0 {
		staleAfter = time.Minute
	}
	if batchSize <= 0 {
		batchSize = 200
	}
	cutoff := now.Add(-staleAfter).Unix()
	var usages []CheckinBonusUsage
	if err := DB.Where(
		"status = ? AND owner_instance_id <> ? AND owner_instance_id <> '' AND updated_at < ?",
		CheckinBonusUsageStatusReserved,
		currentInstanceId,
		cutoff,
	).Order("id ASC").Limit(batchSize).Find(&usages).Error; err != nil {
		return 0, err
	}
	if len(usages) == 0 {
		return 0, nil
	}
	ownerIds := make([]string, 0, len(usages))
	for _, usage := range usages {
		ownerIds = append(ownerIds, usage.OwnerInstanceId)
	}
	var leases []CheckinBonusProcessLease
	if err := DB.Where("instance_id IN ?", ownerIds).Find(&leases).Error; err != nil {
		return 0, err
	}
	liveOwners := make(map[string]struct{}, len(leases))
	for _, lease := range leases {
		if lease.LastSeenAt >= cutoff {
			liveOwners[lease.InstanceId] = struct{}{}
		}
	}
	recovered := 0
	for _, usage := range usages {
		if _, live := liveOwners[usage.OwnerInstanceId]; live {
			continue
		}
		_, _, _, _, handled, err := RefundCheckinBonusFundingUsage(usage.RequestId, now)
		if err != nil {
			return recovered, err
		}
		if handled {
			recovered++
		}
	}
	return recovered, nil
}

// AdjustSettledCheckinBonusUsage changes the final bonus portion for an
// asynchronous task. Positive deltas only use a still-valid bonus; negative
// deltas restore prior consumption even after expiry (the restored balance
// remains expired and cannot be reused).
func AdjustSettledCheckinBonusUsage(requestId string, delta int, now time.Time) (int, error) {
	if requestId == "" || delta == 0 {
		return 0, nil
	}
	nowUnix := now.Unix()
	adjusted := 0
	err := DB.Transaction(func(tx *gorm.DB) error {
		var usage CheckinBonusUsage
		if err := lockForUpdate(tx).Where("request_id = ?", requestId).First(&usage).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		if usage.Status != CheckinBonusUsageStatusSettled {
			return errors.New("check-in bonus usage is not settled")
		}
		var bonus CheckinBonus
		if err := lockForUpdate(tx).First(&bonus, usage.BonusId).Error; err != nil {
			return err
		}
		if delta > 0 {
			if bonus.ExpireAt <= nowUnix || bonus.RemainingAmount <= 0 {
				return nil
			}
			adjusted = min(delta, bonus.RemainingAmount)
			bonus.RemainingAmount -= adjusted
			usage.ReservedAmount += adjusted
			usage.ConsumedAmount += adjusted
		} else {
			adjusted = min(-delta, usage.ConsumedAmount)
			if adjusted == 0 {
				return nil
			}
			bonus.RemainingAmount += adjusted
			usage.ConsumedAmount -= adjusted
		}
		bonus.Status = normalizeCheckinBonusStatus(&bonus, nowUnix)
		if err := tx.Model(&bonus).Updates(map[string]interface{}{
			"remaining_amount": bonus.RemainingAmount,
			"status":           bonus.Status,
		}).Error; err != nil {
			return err
		}
		return tx.Model(&usage).Updates(map[string]interface{}{
			"reserved_amount": usage.ReservedAmount,
			"consumed_amount": usage.ConsumedAmount,
			"updated_at":      nowUnix,
		}).Error
	})
	return adjusted, err
}

// AdjustSettledCheckinBonusFundingUsage atomically recalculates a settled
// async task across the bonus and its original wallet/subscription source.
func AdjustSettledCheckinBonusFundingUsage(requestId string, quotaDelta int, now time.Time) (bonusDelta int, originalDelta int, handled bool, err error) {
	if requestId == "" || quotaDelta == 0 {
		return 0, 0, false, nil
	}
	nowUnix := now.Unix()
	userId := 0
	walletQuotaDelta := 0
	err = DB.Transaction(func(tx *gorm.DB) error {
		var usage CheckinBonusUsage
		if findErr := lockForUpdate(tx).Where("request_id = ?", requestId).First(&usage).Error; findErr != nil {
			if errors.Is(findErr, gorm.ErrRecordNotFound) {
				return nil
			}
			return findErr
		}
		if usage.Status != CheckinBonusUsageStatusSettled {
			return errors.New("check-in bonus usage is not settled")
		}
		if usage.OriginalFundingSource != CheckinBonusOriginalFundingWallet &&
			usage.OriginalFundingSource != CheckinBonusOriginalFundingSubscription {
			return nil
		}
		handled = true
		userId = usage.UserId

		var bonus CheckinBonus
		if lockErr := lockForUpdate(tx).First(&bonus, usage.BonusId).Error; lockErr != nil {
			return lockErr
		}

		if quotaDelta > 0 {
			if bonus.ExpireAt > nowUnix && bonus.RemainingAmount > 0 {
				bonusDelta = min(quotaDelta, bonus.RemainingAmount)
			}
			originalDelta = quotaDelta - bonusDelta
			if originalDelta > 0 {
				switch usage.OriginalFundingSource {
				case CheckinBonusOriginalFundingWallet:
					if updateErr := tx.Model(&User{}).Where("id = ?", usage.UserId).
						Update("quota", gorm.Expr("quota - ?", originalDelta)).Error; updateErr != nil {
						return updateErr
					}
					walletQuotaDelta = -originalDelta
				case CheckinBonusOriginalFundingSubscription:
					if usage.OriginalFundingId <= 0 {
						result, preConsumeErr := preConsumeUserSubscriptionTx(
							tx,
							requestId,
							usage.UserId,
							"",
							0,
							int64(originalDelta),
							nowUnix,
						)
						if preConsumeErr != nil {
							return preConsumeErr
						}
						usage.OriginalFundingId = result.UserSubscriptionId
						if updateErr := tx.Model(&SubscriptionPreConsumeRecord{}).
							Where("request_id = ?", requestId).
							Update("status", "settled").Error; updateErr != nil {
							return updateErr
						}
					} else if updateErr := postConsumeUserSubscriptionDeltaTx(tx, usage.OriginalFundingId, int64(originalDelta)); updateErr != nil {
						return updateErr
					}
				}
			}
			if bonusDelta > 0 {
				bonus.RemainingAmount -= bonusDelta
				bonus.Status = normalizeCheckinBonusStatus(&bonus, nowUnix)
				usage.ReservedAmount += bonusDelta
				usage.ConsumedAmount += bonusDelta
			}
			usage.OriginalReservedAmount += originalDelta
			usage.OriginalConsumedAmount += originalDelta
		} else {
			refund := -quotaDelta
			originalRefund := min(refund, usage.OriginalConsumedAmount)
			if originalRefund > 0 {
				switch usage.OriginalFundingSource {
				case CheckinBonusOriginalFundingWallet:
					if updateErr := tx.Model(&User{}).Where("id = ?", usage.UserId).
						Update("quota", gorm.Expr("quota + ?", originalRefund)).Error; updateErr != nil {
						return updateErr
					}
					walletQuotaDelta = originalRefund
				case CheckinBonusOriginalFundingSubscription:
					if usage.OriginalFundingId <= 0 {
						return errors.New("subscription id is missing for task refund")
					}
					if updateErr := postConsumeUserSubscriptionDeltaTx(tx, usage.OriginalFundingId, -int64(originalRefund)); updateErr != nil {
						return updateErr
					}
				}
				usage.OriginalConsumedAmount -= originalRefund
				refund -= originalRefund
				originalDelta = -originalRefund
			}
			bonusRefund := min(refund, usage.ConsumedAmount)
			if bonusRefund > 0 {
				if restoreErr := restoreCheckinBonusAmount(tx, &bonus, bonusRefund, nowUnix); restoreErr != nil {
					return restoreErr
				}
				usage.ConsumedAmount -= bonusRefund
				bonusDelta = -bonusRefund
			}
		}
		if usage.TokenId > 0 {
			usage.TokenConsumedAmount += quotaDelta
			if usage.TokenConsumedAmount < 0 {
				return errors.New("check-in bonus token consumption cannot be negative")
			}
		}

		if bonusDelta > 0 {
			if updateErr := tx.Model(&bonus).Updates(map[string]interface{}{
				"remaining_amount": bonus.RemainingAmount,
				"status":           bonus.Status,
			}).Error; updateErr != nil {
				return updateErr
			}
		}
		return tx.Model(&usage).Updates(map[string]interface{}{
			"reserved_amount":          usage.ReservedAmount,
			"consumed_amount":          usage.ConsumedAmount,
			"original_funding_id":      usage.OriginalFundingId,
			"original_reserved_amount": usage.OriginalReservedAmount,
			"original_consumed_amount": usage.OriginalConsumedAmount,
			"token_consumed_amount":    usage.TokenConsumedAmount,
			"updated_at":               nowUnix,
		}).Error
	})
	if err == nil && handled {
		syncCheckinBonusWalletQuotaCache(userId, walletQuotaDelta)
	}
	return bonusDelta, originalDelta, handled, err
}

func RefundCheckinBonusUsage(requestId string, now time.Time) (int, error) {
	if requestId == "" {
		return 0, nil
	}
	nowUnix := now.Unix()
	refunded := 0
	err := DB.Transaction(func(tx *gorm.DB) error {
		var usage CheckinBonusUsage
		if err := lockForUpdate(tx).Where("request_id = ?", requestId).First(&usage).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		if usage.Status == CheckinBonusUsageStatusRefunded {
			return nil
		}
		if usage.Status == CheckinBonusUsageStatusSettled {
			refunded = usage.ConsumedAmount
		} else {
			refunded = usage.ReservedAmount
		}
		if refunded > 0 {
			var bonus CheckinBonus
			if err := lockForUpdate(tx).First(&bonus, usage.BonusId).Error; err != nil {
				return err
			}
			if err := restoreCheckinBonusAmount(tx, &bonus, refunded, nowUnix); err != nil {
				return err
			}
		}
		return tx.Model(&usage).Updates(map[string]interface{}{
			"status":     CheckinBonusUsageStatusRefunded,
			"updated_at": nowUnix,
		}).Error
	})
	return refunded, err
}
