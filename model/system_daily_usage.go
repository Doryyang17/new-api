package model

import (
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
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

type SystemDailyUsageSnapshot struct {
	UsedTokens           int64
	ModelUsedTokens      map[string]int64
	ModelEvaluationError string
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

func RefreshSystemDailyUsageSnapshot(dayStart int64, timezone string, updatedAt int64, models []string) (SystemDailyUsageSnapshot, error) {
	usedTokens, err := GetSystemDailyUsageLogTokens(dayStart, timezone, nil)
	if err != nil {
		common.SysError("failed to query system daily usage logs: " + err.Error())
		return SystemDailyUsageSnapshot{}, fmt.Errorf("查询统计数据失败")
	}
	snapshot := SystemDailyUsageSnapshot{
		UsedTokens:      usedTokens,
		ModelUsedTokens: make(map[string]int64, len(models)),
	}
	modelUsedTokens, err := GetSystemDailyUsageLogTokensByModel(dayStart, timezone, models)
	if err != nil {
		common.SysError("failed to query model daily usage logs: " + err.Error())
		snapshot.ModelEvaluationError = "查询模型统计数据失败"
	} else {
		snapshot.ModelUsedTokens = modelUsedTokens
	}
	if err := SaveSystemDailyUsageSnapshot(dayStart, timezone, usedTokens, updatedAt); err != nil {
		common.SysError("failed to save system daily usage snapshot: " + err.Error())
		return SystemDailyUsageSnapshot{}, fmt.Errorf("保存统计快照失败")
	}
	return snapshot, nil
}

func GetSystemDailyUsageSnapshotTokens(dayStart int64, timezone string) (int64, error) {
	var usedTokens int64
	err := DB.Model(&SystemDailyUsageCounter{}).
		Select("COALESCE(sum(used_tokens), 0)").
		Where("day_start = ? AND timezone = ?", dayStart, timezone).
		Scan(&usedTokens).Error
	return usedTokens, err
}

func GetSystemDailyUsageLogTokens(dayStart int64, timezone string, models []string) (int64, error) {
	dayEnd, err := systemDailyUsageDayEnd(dayStart, timezone)
	if err != nil {
		return 0, err
	}
	var usedTokens int64
	query := LOG_DB.Table("logs").
		Select("COALESCE(sum(prompt_tokens), 0) + COALESCE(sum(completion_tokens), 0)").
		Where("type = ?", LogTypeConsume).
		Where("created_at >= ? AND created_at < ?", dayStart, dayEnd)
	query = applyModelNameScope(query, models, common.UsingLogDatabase(common.DatabaseTypeClickHouse))
	err = query.Scan(&usedTokens).Error
	return usedTokens, err
}

func GetSystemDailyUsageLogTokensByModel(dayStart int64, timezone string, models []string) (map[string]int64, error) {
	totals := make(map[string]int64, len(models))
	if len(models) == 0 {
		return totals, nil
	}
	dayEnd, err := systemDailyUsageDayEnd(dayStart, timezone)
	if err != nil {
		return nil, err
	}
	var rows []struct {
		ModelName string `gorm:"column:model_name"`
		Tokens    int64  `gorm:"column:tokens"`
	}
	query := LOG_DB.Table("logs").
		Select("model_name, COALESCE(sum(prompt_tokens), 0) + COALESCE(sum(completion_tokens), 0) AS tokens").
		Where("type = ?", LogTypeConsume).
		Where("created_at >= ? AND created_at < ?", dayStart, dayEnd).
		Group("model_name")
	query = applyModelNameScope(query, models, common.UsingLogDatabase(common.DatabaseTypeClickHouse))
	if err := query.Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, configuredModel := range models {
		for _, row := range rows {
			if UsageModelMatches(configuredModel, row.ModelName) {
				totals[configuredModel] += row.Tokens
			}
		}
	}
	return totals, nil
}

func applyModelNameScope(query *gorm.DB, models []string, clickHouse bool) *gorm.DB {
	if models == nil {
		return query
	}
	if len(models) == 0 {
		return query.Where("1 = 0")
	}
	exactModels := make([]string, 0, len(models)*2)
	wildcardModels := make([]string, 0, len(models))
	for _, modelName := range expandUsageScopeModels(models) {
		exactModels = append(exactModels, modelName)
		if strings.HasSuffix(modelName, "*") {
			wildcardModels = append(wildcardModels, modelName)
		}
	}
	conditions := []string{"model_name IN ?"}
	args := []interface{}{exactModels}
	for _, modelName := range wildcardModels {
		condition := "model_name LIKE ? ESCAPE '!'"
		if clickHouse {
			condition = "model_name LIKE ?"
		}
		conditions = append(conditions, condition)
		args = append(args, usageModelLikePattern(modelName, clickHouse))
	}
	return query.Where("("+strings.Join(conditions, " OR ")+")", args...)
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
