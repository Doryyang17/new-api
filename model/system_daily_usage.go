package model

import (
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"gorm.io/gorm"
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

func RecordSystemDailyUsageTokens(timestamp int64, tokenUsed int64) error {
	if tokenUsed <= 0 {
		return nil
	}
	settings := system_setting.GetDailyUsageLimitSettings()
	timezone := settings.Timezone
	dayStart, err := systemDailyUsageDayStart(timestamp, timezone)
	if err != nil {
		common.SysError(fmt.Sprintf("invalid daily usage timezone %q, fallback to %s: %s", timezone, system_setting.DefaultDailyUsageLimitTZ, err.Error()))
		timezone = system_setting.DefaultDailyUsageLimitTZ
		dayStart, err = systemDailyUsageDayStart(timestamp, timezone)
		if err != nil {
			return err
		}
	}
	return IncrementSystemDailyUsageCounter(dayStart, timezone, tokenUsed, timestamp)
}

func IncrementSystemDailyUsageCounter(dayStart int64, timezone string, tokenUsed int64, updatedAt int64) error {
	if tokenUsed <= 0 {
		return nil
	}
	counter := SystemDailyUsageCounter{
		DayStart:   dayStart,
		Timezone:   timezone,
		UsedTokens: tokenUsed,
		CreatedAt:  updatedAt,
		UpdatedAt:  updatedAt,
	}
	return DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "day_start"},
			{Name: "timezone"},
		},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"used_tokens": gorm.Expr("used_tokens + ?", tokenUsed),
			"updated_at":  updatedAt,
		}),
	}).Create(&counter).Error
}

func GetSystemDailyUsageTokens(dayStart int64, timezone string) (int64, error) {
	var counter SystemDailyUsageCounter
	if err := DB.Where("day_start = ? AND timezone = ?", dayStart, timezone).Limit(1).Find(&counter).Error; err != nil {
		common.SysError("failed to query system daily usage counter: " + err.Error())
		return 0, fmt.Errorf("查询统计数据失败")
	}
	if counter.Id == 0 {
		return 0, nil
	}
	return counter.UsedTokens, nil
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
