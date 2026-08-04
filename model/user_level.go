package model

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/user_level_setting"

	"gorm.io/gorm"
)

var ErrUserLevelDisabled = errors.New("用户等级功能未开启")

type UserLevelClaim struct {
	Id              int     `json:"id" gorm:"primaryKey"`
	UserId          int     `json:"user_id" gorm:"not null;uniqueIndex:idx_user_level_claim,priority:1;index"`
	LevelId         string  `json:"level_id" gorm:"type:varchar(32);not null;uniqueIndex:idx_user_level_claim,priority:2;index"`
	PreviousLevelId string  `json:"previous_level_id" gorm:"type:varchar(32);not null"`
	LevelName       string  `json:"level_name" gorm:"type:varchar(64);not null"`
	LevelRatio      float64 `json:"level_ratio" gorm:"not null"`
	ThresholdQuota  int64   `json:"threshold_quota" gorm:"type:bigint;not null"`
	// Keep the existing column name so upgrades do not rewrite claim-history tables.
	ConsumedQuota int64 `json:"consumed_quota" gorm:"column:top_up_quota;type:bigint;not null"`
	ClaimedAt     int64 `json:"claimed_at" gorm:"type:bigint;not null;index"`
}

func (UserLevelClaim) TableName() string {
	return "user_level_claims"
}

type UserLevelClaimResult struct {
	PreviousLevel user_level_setting.Level
	CurrentLevel  user_level_setting.Level
	ConsumedQuota int64
	Changed       bool
}

type UserLevelMemberCount struct {
	LevelKey string `json:"level_key"`
	Count    int64  `json:"count"`
}

// GetUserLevelProgressFromDB reads the dedicated durable settled-usage total.
// Balance sources such as recharge and check-in grants do not affect it, and
// refundable asynchronous task charges are only added after terminal success.
func GetUserLevelProgressFromDB(userId int) (string, int64, error) {
	var user User
	err := DB.Select("level_key", "level_consumed_quota").Where("id = ?", userId).First(&user).Error
	if err != nil {
		return "", 0, err
	}
	consumedQuota := user.LevelUsageQuota
	if consumedQuota < 0 {
		consumedQuota = 0
	}
	return user.LevelKey, consumedQuota, nil
}

func CountUsersByLevel() (map[string]int64, error) {
	var rows []UserLevelMemberCount
	if err := DB.Model(&User{}).
		Select("level_key, COUNT(*) AS count").
		Group("level_key").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	counts := make(map[string]int64, len(rows))
	for _, row := range rows {
		levelKey := row.LevelKey
		if levelKey == "" {
			levelKey = user_level_setting.BaseLevelID
		}
		counts[levelKey] += row.Count
	}
	return counts, nil
}

func ClaimHighestEligibleUserLevel(userId int) (*UserLevelClaimResult, error) {
	if userId <= 0 {
		return nil, fmt.Errorf("无效的用户 ID")
	}
	currentConfig := user_level_setting.GetConfig()
	if !currentConfig.Enabled {
		return nil, ErrUserLevelDisabled
	}

	result := &UserLevelClaimResult{}
	err := DB.Transaction(func(tx *gorm.DB) error {
		var user User
		if err := lockForUpdate(tx).
			Select("id", "level_key", "level_consumed_quota").
			Where("id = ?", userId).
			First(&user).Error; err != nil {
			return err
		}

		consumedQuota := user.LevelUsageQuota
		if consumedQuota < 0 {
			consumedQuota = 0
		}
		result.ConsumedQuota = consumedQuota

		previousIndex := 0
		previousLevel := currentConfig.Levels[0]
		for index, level := range currentConfig.Levels {
			if level.ID == user.LevelKey || (user.LevelKey == "" && level.ID == user_level_setting.BaseLevelID) {
				previousIndex = index
				previousLevel = level
				break
			}
		}
		result.PreviousLevel = previousLevel
		result.CurrentLevel = previousLevel

		targetIndex := -1
		for index, level := range currentConfig.Levels {
			if index <= previousIndex || level.Archived || level.ThresholdQuota > consumedQuota {
				continue
			}
			targetIndex = index
		}
		if targetIndex == -1 {
			return nil
		}

		target := currentConfig.Levels[targetIndex]
		claim := UserLevelClaim{
			UserId:          userId,
			LevelId:         target.ID,
			PreviousLevelId: previousLevel.ID,
			LevelName:       target.Name,
			LevelRatio:      target.Ratio,
			ThresholdQuota:  target.ThresholdQuota,
			ConsumedQuota:   consumedQuota,
			ClaimedAt:       common.GetTimestamp(),
		}
		if err := tx.Create(&claim).Error; err != nil {
			return err
		}
		update := tx.Model(&User{}).Where("id = ?", userId).Update("level_key", target.ID)
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		result.CurrentLevel = target
		result.Changed = true
		return nil
	})
	if err != nil {
		return nil, err
	}

	if result.Changed {
		if err := RefreshUserLevelCache(userId); err != nil {
			common.SysError(fmt.Sprintf("failed to refresh user level cache for user %d: %s", userId, err.Error()))
			if invalidateErr := invalidateUserCache(userId); invalidateErr != nil {
				common.SysError(fmt.Sprintf("failed to invalidate user level cache for user %d: %s", userId, invalidateErr.Error()))
			}
			scheduleUserCacheInvalidationRetry([]int{userId})
		}
	}
	return result, nil
}
