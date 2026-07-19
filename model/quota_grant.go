package model

import (
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	MaxQuotaGrantTargets   = 10000
	quotaGrantLogBatchSize = 500
)

var (
	ErrQuotaGrantAlreadyProcessed    = errors.New("quota grant batch already processed")
	ErrQuotaGrantIdempotencyConflict = errors.New("幂等键已被其他发放请求使用")
	ErrQuotaGrantSeparateLogDatabase = errors.New("批量发放要求使用主数据库记录日志，当前独立日志数据库无法保证额度与日志原子写入")
)

type QuotaGrantBatch struct {
	Id             int    `json:"id"`
	RequestId      string `json:"request_id" gorm:"type:varchar(64);uniqueIndex"`
	OperatorUserId int    `json:"operator_user_id" gorm:"index"`
	Quota          int    `json:"quota"`
	AmountUsd      string `json:"amount_usd" gorm:"type:varchar(32)"`
	Reason         string `json:"reason" gorm:"type:varchar(255)"`
	TargetCount    int    `json:"target_count"`
	TargetHash     string `json:"-" gorm:"type:varchar(64)"`
	CreatedAt      int64  `json:"created_at" gorm:"autoCreateTime"`
}

type QuotaGrantBatchParams struct {
	RequestId      string
	OperatorUserId int
	OperatorName   string
	TargetUserIds  []int
	Quota          int
	AmountUsd      string
	Reason         string
	Ip             string
	AdminInfo      map[string]interface{}
}

type QuotaGrantBatchResult struct {
	Batch            QuotaGrantBatch `json:"batch"`
	AlreadyProcessed bool            `json:"already_processed"`
	CacheSyncPending bool            `json:"cache_sync_pending"`
}

func ListQuotaGrantTargets(keyword string, roles []int, statuses []int, startIdx int, pageSize int) ([]*User, int64, error) {
	query := quotaGrantTargetQuery(DB, keyword, roles, statuses)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var users []*User
	err := query.
		Select("id", "username", "display_name", "email", "quota", "role", "status", commonGroupCol, "created_at").
		Order("id desc").
		Limit(pageSize).
		Offset(startIdx).
		Find(&users).Error
	return users, total, err
}

func ListQuotaGrantTargetIds(keyword string, roles []int, statuses []int) ([]int, error) {
	var ids []int
	err := quotaGrantTargetQuery(DB, keyword, roles, statuses).
		Order("id desc").
		Pluck("id", &ids).Error
	return ids, err
}

func quotaGrantTargetQuery(tx *gorm.DB, keyword string, roles []int, statuses []int) *gorm.DB {
	query := tx.Model(&User{}).
		Where("role IN ?", roles).
		Where("status IN ?", statuses)
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return query
	}

	condition := "username LIKE ? OR email LIKE ? OR display_name LIKE ?"
	likeKeyword := "%" + keyword + "%"
	args := []interface{}{likeKeyword, likeKeyword, likeKeyword}
	if id, err := strconv.Atoi(keyword); err == nil {
		condition = "id = ? OR " + condition
		args = append([]interface{}{id}, args...)
	}
	return query.Where("("+condition+")", args...)
}

func GrantUserQuotaBatch(params QuotaGrantBatchParams) (*QuotaGrantBatchResult, error) {
	if LOG_DB != DB {
		return nil, ErrQuotaGrantSeparateLogDatabase
	}
	if params.Quota <= 0 || params.Quota > common.MaxQuota {
		return nil, errors.New("发放额度必须大于 0 且不超过系统额度上限")
	}

	targetIds := append([]int(nil), params.TargetUserIds...)
	sort.Ints(targetIds)
	targetIds = compactQuotaGrantTargetIds(targetIds)
	if len(targetIds) == 0 || len(targetIds) > MaxQuotaGrantTargets {
		return nil, fmt.Errorf("发放用户数量必须在 1 到 %d 之间", MaxQuotaGrantTargets)
	}

	targetHash := quotaGrantTargetHash(targetIds)
	batch := QuotaGrantBatch{
		RequestId:      params.RequestId,
		OperatorUserId: params.OperatorUserId,
		Quota:          params.Quota,
		AmountUsd:      params.AmountUsd,
		Reason:         params.Reason,
		TargetCount:    len(targetIds),
		TargetHash:     targetHash,
	}

	if existing, found, err := findQuotaGrantBatch(params.RequestId); err != nil {
		return nil, err
	} else if found {
		result, err := quotaGrantExistingResult(existing, batch)
		if err != nil {
			return nil, err
		}
		return finalizeQuotaGrantResult(result, targetIds)
	}

	err := DB.Transaction(func(tx *gorm.DB) error {
		createResult := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "request_id"}},
			DoNothing: true,
		}).Create(&batch)
		if createResult.Error != nil {
			return createResult.Error
		}
		if createResult.RowsAffected == 0 {
			return ErrQuotaGrantAlreadyProcessed
		}

		var users []*User
		if err := lockForUpdate(tx).
			Where("id IN ?", targetIds).
			Where("role IN ?", []int{common.RoleCommonUser, common.RoleAdminUser}).
			Where("status IN ?", []int{common.UserStatusEnabled, common.UserStatusDisabled}).
			Order("id asc").
			Find(&users).Error; err != nil {
			return err
		}
		if len(users) != len(targetIds) {
			return errors.New("部分用户已不存在或不再符合发放条件，请刷新用户清单后重试")
		}
		for _, user := range users {
			if user.Quota > common.MaxQuota-params.Quota {
				return fmt.Errorf("用户 %s（ID: %d）的余额将在发放后超过系统额度上限", user.Username, user.Id)
			}
		}

		updateResult := tx.Model(&User{}).
			Where("id IN ?", targetIds).
			Update("quota", gorm.Expr("quota + ?", params.Quota))
		if updateResult.Error != nil {
			return updateResult.Error
		}
		if updateResult.RowsAffected != int64(len(targetIds)) {
			return errors.New("用户额度更新数量与目标数量不一致")
		}

		createdAt := common.GetTimestamp()
		userParams := map[string]interface{}{
			"amount_usd": params.AmountUsd,
			"reason":     params.Reason,
		}
		userOther := map[string]interface{}{
			"op": buildOpField("user.quota_grant", userParams),
		}
		if len(params.AdminInfo) > 0 {
			userOther["admin_info"] = params.AdminInfo
		}
		userLogOther := common.MapToJsonStr(userOther)
		logs := make([]*Log, 0, len(users)+1)
		for _, user := range users {
			logs = append(logs, &Log{
				UserId:    user.Id,
				Username:  user.Username,
				CreatedAt: createdAt,
				Type:      LogTypeManage,
				Content:   fmt.Sprintf("Received an administrator quota grant of $%s: %s", params.AmountUsd, params.Reason),
				RequestId: params.RequestId,
				Other:     userLogOther,
			})
		}

		operatorParams := map[string]interface{}{
			"amount_usd": params.AmountUsd,
			"count":      len(users),
			"reason":     params.Reason,
		}
		operatorOther := map[string]interface{}{
			"op":         buildOpField("user.quota_grant_batch", operatorParams),
			"admin_info": params.AdminInfo,
		}
		logs = append(logs, &Log{
			UserId:    params.OperatorUserId,
			Username:  params.OperatorName,
			CreatedAt: createdAt,
			Type:      LogTypeManage,
			Content:   fmt.Sprintf("Granted $%s to %d users: %s", params.AmountUsd, len(users), params.Reason),
			Ip:        params.Ip,
			RequestId: params.RequestId,
			Other:     common.MapToJsonStr(operatorOther),
		})

		return tx.CreateInBatches(&logs, quotaGrantLogBatchSize).Error
	})
	if errors.Is(err, ErrQuotaGrantAlreadyProcessed) {
		existing, found, findErr := findQuotaGrantBatch(params.RequestId)
		if findErr != nil {
			return nil, findErr
		}
		if !found {
			return nil, err
		}
		result, resultErr := quotaGrantExistingResult(existing, batch)
		if resultErr != nil {
			return nil, resultErr
		}
		return finalizeQuotaGrantResult(result, targetIds)
	}
	if err != nil {
		return nil, err
	}

	return finalizeQuotaGrantResult(&QuotaGrantBatchResult{Batch: batch}, targetIds)
}

func finalizeQuotaGrantResult(result *QuotaGrantBatchResult, targetIds []int) (*QuotaGrantBatchResult, error) {
	if err := invalidateUserCaches(targetIds); err != nil {
		common.SysError(fmt.Sprintf("quota grant %s committed but user cache invalidation failed: %s", result.Batch.RequestId, err.Error()))
		result.CacheSyncPending = true
		scheduleUserCacheInvalidationRetry(targetIds)
	}
	return result, nil
}

func findQuotaGrantBatch(requestId string) (QuotaGrantBatch, bool, error) {
	var batch QuotaGrantBatch
	err := DB.Where("request_id = ?", requestId).First(&batch).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return QuotaGrantBatch{}, false, nil
	}
	return batch, err == nil, err
}

func quotaGrantExistingResult(existing QuotaGrantBatch, expected QuotaGrantBatch) (*QuotaGrantBatchResult, error) {
	if existing.OperatorUserId != expected.OperatorUserId ||
		existing.Quota != expected.Quota ||
		existing.AmountUsd != expected.AmountUsd ||
		existing.Reason != expected.Reason ||
		existing.TargetCount != expected.TargetCount ||
		existing.TargetHash != expected.TargetHash {
		return nil, ErrQuotaGrantIdempotencyConflict
	}
	return &QuotaGrantBatchResult{Batch: existing, AlreadyProcessed: true}, nil
}

func compactQuotaGrantTargetIds(ids []int) []int {
	if len(ids) == 0 {
		return ids
	}
	result := ids[:0]
	for _, id := range ids {
		if id <= 0 || len(result) > 0 && result[len(result)-1] == id {
			continue
		}
		result = append(result, id)
	}
	return result
}

func quotaGrantTargetHash(ids []int) string {
	var builder strings.Builder
	for index, id := range ids {
		if index > 0 {
			builder.WriteByte(',')
		}
		builder.WriteString(strconv.Itoa(id))
	}
	return hex.EncodeToString(common.Sha256Raw([]byte(builder.String())))
}
