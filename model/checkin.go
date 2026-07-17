package model

import (
	"errors"
	"fmt"
	"math/rand"
	"time"

	"github.com/QuantumNous/new-api/setting/operation_setting"
	"gorm.io/gorm"
)

// Checkin 签到记录
type Checkin struct {
	Id           int    `json:"id" gorm:"primaryKey;autoIncrement"`
	UserId       int    `json:"user_id" gorm:"not null;uniqueIndex:idx_user_checkin_date"`
	CheckinDate  string `json:"checkin_date" gorm:"type:varchar(10);not null;uniqueIndex:idx_user_checkin_date"` // 格式: YYYY-MM-DD
	QuotaAwarded int    `json:"quota_awarded" gorm:"not null"`
	CreatedAt    int64  `json:"created_at" gorm:"bigint"`
}

// CheckinRecord 用于API返回的签到记录（不包含敏感字段）
type CheckinRecord struct {
	CheckinDate  string `json:"checkin_date"`
	QuotaAwarded int    `json:"quota_awarded"`
	BonusAwarded int    `json:"bonus_awarded"`
}

func (Checkin) TableName() string {
	return "checkins"
}

// GetUserCheckinRecords 获取用户在指定日期范围内的签到记录
func GetUserCheckinRecords(userId int, startDate, endDate string) ([]Checkin, error) {
	var records []Checkin
	err := DB.Where("user_id = ? AND checkin_date >= ? AND checkin_date <= ?",
		userId, startDate, endDate).
		Order("checkin_date DESC").
		Find(&records).Error
	return records, err
}

// HasCheckedInToday 检查用户今天是否已签到
func HasCheckedInToday(userId int) (bool, error) {
	today := time.Now().Format("2006-01-02")
	var count int64
	err := DB.Model(&Checkin{}).
		Where("user_id = ? AND checkin_date = ?", userId, today).
		Count(&count).Error
	return count > 0, err
}

// UserCheckin 执行用户签到
// 所有支持的数据库都使用同一事务保证签到记录与奖励原子写入。
func UserCheckin(userId int) (*Checkin, *CheckinBonus, error) {
	setting := operation_setting.GetCheckinSetting()
	if !setting.Enabled {
		return nil, nil, errors.New("签到功能未启用")
	}

	// 检查今天是否已签到
	hasChecked, err := HasCheckedInToday(userId)
	if err != nil {
		return nil, nil, err
	}
	if hasChecked {
		return nil, nil, errors.New("今日已签到")
	}

	now := time.Now()
	today := now.Format("2006-01-02")
	quotaAwarded := 0
	var bonus *CheckinBonus
	bonusSetting := operation_setting.GetCheckinBonusSetting()
	if bonusSetting.Enabled {
		if err := operation_setting.ValidateCheckinBonusRange(bonusSetting.MinAmount, bonusSetting.MaxAmount); err != nil {
			return nil, nil, fmt.Errorf("签到赠金配置无效")
		}
		bonusAmount := bonusSetting.MinAmount
		if bonusSetting.MaxAmount > bonusSetting.MinAmount {
			bonusAmount += int(rand.Int63n(int64(bonusSetting.MaxAmount-bonusSetting.MinAmount) + 1))
		}
		bonus = newCheckinBonus(userId, 0, bonusAmount, now)
	} else {
		// 原余额签到与独立赠金签到互斥：仅在赠金模式关闭时发放余额。
		quotaAwarded = setting.MinQuota
		if setting.MaxQuota > setting.MinQuota {
			quotaAwarded = setting.MinQuota + rand.Intn(setting.MaxQuota-setting.MinQuota+1)
		}
	}
	checkin := &Checkin{
		UserId:       userId,
		CheckinDate:  today,
		QuotaAwarded: quotaAwarded,
		CreatedAt:    now.Unix(),
	}

	return userCheckinWithTransaction(checkin, bonus, userId, quotaAwarded)
}

// userCheckinWithTransaction 使用事务执行签到。
func userCheckinWithTransaction(checkin *Checkin, bonus *CheckinBonus, userId int, quotaAwarded int) (*Checkin, *CheckinBonus, error) {
	err := DB.Transaction(func(tx *gorm.DB) error {
		// 步骤1: 创建签到记录
		// 数据库有唯一约束 (user_id, checkin_date)，可以防止并发重复签到
		if err := tx.Create(checkin).Error; err != nil {
			return errors.New("签到失败，请稍后重试")
		}

		// 步骤2: 仅余额签到模式增加用户额度；赠金模式保持 users.quota 不变。
		if quotaAwarded > 0 {
			if err := tx.Model(&User{}).Where("id = ?", userId).
				Update("quota", gorm.Expr("quota + ?", quotaAwarded)).Error; err != nil {
				return errors.New("签到失败：更新额度出错")
			}
		}

		if bonus != nil {
			bonus.CheckinId = checkin.Id
			if err := tx.Create(bonus).Error; err != nil {
				return errors.New("签到失败：创建赠金记录出错")
			}
		}

		return nil
	})

	if err != nil {
		return nil, nil, err
	}

	// 事务成功后，异步更新缓存
	if quotaAwarded > 0 {
		go func() {
			_ = cacheIncrUserQuota(userId, int64(quotaAwarded))
		}()
	}

	return checkin, bonus, nil
}

// GetUserCheckinStats 获取用户签到统计信息
func GetUserCheckinStats(userId int, month string) (map[string]interface{}, error) {
	// 获取指定月份的所有签到记录
	startDate := month + "-01"
	endDate := month + "-31"

	records, err := GetUserCheckinRecords(userId, startDate, endDate)
	if err != nil {
		return nil, err
	}

	bonusByCheckinId := make(map[int]int, len(records))
	checkinIds := make([]int, 0, len(records))
	for _, record := range records {
		checkinIds = append(checkinIds, record.Id)
	}
	if len(checkinIds) > 0 {
		var bonuses []CheckinBonus
		if err := DB.Select("checkin_id", "amount").Where("checkin_id IN ?", checkinIds).Find(&bonuses).Error; err != nil {
			return nil, err
		}
		for _, bonus := range bonuses {
			bonusByCheckinId[bonus.CheckinId] = bonus.Amount
		}
	}

	// 转换为不包含敏感字段的记录
	checkinRecords := make([]CheckinRecord, len(records))
	var monthlyReward int64
	for i, r := range records {
		bonusAwarded := bonusByCheckinId[r.Id]
		checkinRecords[i] = CheckinRecord{
			CheckinDate:  r.CheckinDate,
			QuotaAwarded: r.QuotaAwarded,
			BonusAwarded: bonusAwarded,
		}
		monthlyReward += int64(r.QuotaAwarded + bonusAwarded)
	}

	// 检查今天是否已签到
	hasCheckedToday, _ := HasCheckedInToday(userId)

	// 获取用户所有时间的签到统计
	var totalCheckins int64
	var totalQuota int64
	var totalBonus int64
	DB.Model(&Checkin{}).Where("user_id = ?", userId).Count(&totalCheckins)
	DB.Model(&Checkin{}).Where("user_id = ?", userId).Select("COALESCE(SUM(quota_awarded), 0)").Scan(&totalQuota)
	DB.Model(&CheckinBonus{}).Where("user_id = ?", userId).Select("COALESCE(SUM(amount), 0)").Scan(&totalBonus)

	return map[string]interface{}{
		"total_quota":      totalQuota, // 所有时间累计获得的账户余额奖励
		"total_bonus":      totalBonus, // 所有时间累计获得的独立赠金
		"total_reward":     totalQuota + totalBonus,
		"monthly_reward":   monthlyReward,
		"total_checkins":   totalCheckins,   // 所有时间累计签到次数
		"checkin_count":    len(records),    // 本月签到次数
		"checked_in_today": hasCheckedToday, // 今天是否已签到
		"records":          checkinRecords,  // 本月签到记录详情（不含id和user_id）
	}, nil
}
