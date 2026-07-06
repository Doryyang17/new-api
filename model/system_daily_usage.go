package model

import (
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm/clause"
)

type SystemDailyUsageCounter struct {
	Id         int64  `json:"id"`
	DayStart   int64  `json:"day_start" gorm:"bigint;uniqueIndex:idx_system_daily_usage_day_tz,priority:1;index"`
	Timezone   string `json:"timezone" gorm:"size:64;uniqueIndex:idx_system_daily_usage_day_tz,priority:2;default:''"`
	UsedTokens int64  `json:"used_tokens" gorm:"default:0"`
	CreatedAt  int64  `json:"created_at" gorm:"bigint"`
	UpdatedAt  int64  `json:"updated_at" gorm:"bigint"`
}

func (SystemDailyUsageCounter) TableName() string {
	return "system_daily_usage_counters"
}

func SaveSystemDailyUsageSnapshot(dayStart int64, timezone string, usedTokens int64, updatedAt int64) error {
	if usedTokens < 0 {
		usedTokens = 0
	}
	counter := SystemDailyUsageCounter{
		DayStart:   dayStart,
		Timezone:   timezone,
		UsedTokens: usedTokens,
		CreatedAt:  updatedAt,
		UpdatedAt:  updatedAt,
	}
	return DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "day_start"},
			{Name: "timezone"},
		},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"used_tokens": usedTokens,
			"updated_at":  updatedAt,
		}),
	}).Create(&counter).Error
}

func RefreshSystemDailyUsageSnapshot(dayStart int64, timezone string, updatedAt int64) (int64, error) {
	usedTokens, err := GetSystemDailyUsageLogTokens(dayStart, timezone)
	if err != nil {
		common.SysError("failed to query system daily usage logs: " + err.Error())
		return 0, fmt.Errorf("查询统计数据失败")
	}
	if err := SaveSystemDailyUsageSnapshot(dayStart, timezone, usedTokens, updatedAt); err != nil {
		common.SysError("failed to save system daily usage snapshot: " + err.Error())
		return 0, fmt.Errorf("保存统计快照失败")
	}
	return usedTokens, nil
}

func GetSystemDailyUsageSnapshotTokens(dayStart int64, timezone string) (int64, error) {
	var usedTokens int64
	err := DB.Model(&SystemDailyUsageCounter{}).
		Select("COALESCE(sum(used_tokens), 0)").
		Where("day_start = ? AND timezone = ?", dayStart, timezone).
		Scan(&usedTokens).Error
	return usedTokens, err
}

func GetSystemDailyUsageLogTokens(dayStart int64, timezone string) (int64, error) {
	dayEnd, err := systemDailyUsageDayEnd(dayStart, timezone)
	if err != nil {
		return 0, err
	}
	var usedTokens int64
	err = LOG_DB.Table("logs").
		Select("COALESCE(sum(prompt_tokens), 0) + COALESCE(sum(completion_tokens), 0)").
		Where("type = ?", LogTypeConsume).
		Where("created_at >= ? AND created_at < ?", dayStart, dayEnd).
		Scan(&usedTokens).Error
	return usedTokens, err
}

func systemDailyUsageDayStart(timestamp int64, timezone string) (int64, error) {
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return 0, err
	}
	localTime := time.Unix(timestamp, 0).In(location)
	dayStart := time.Date(localTime.Year(), localTime.Month(), localTime.Day(), 0, 0, 0, 0, location)
	return dayStart.Unix(), nil
}

func systemDailyUsageDayEnd(dayStart int64, timezone string) (int64, error) {
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return 0, err
	}
	return time.Unix(dayStart, 0).In(location).AddDate(0, 0, 1).Unix(), nil
}
