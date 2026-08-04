package user_level_setting

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"sync/atomic"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
)

const (
	OptionKey     = "user_level_setting.config"
	BaseLevelID   = "base"
	SchemaVersion = 1
	MaxLevelCount = 20
	// MaxThresholdQuota keeps progress values exactly representable by the
	// JavaScript admin/profile clients while the database stores them as BIGINT.
	MaxThresholdQuota int64 = 9_007_199_254_740_991
	maxLevelNameLen         = 20
	maxDescription          = 80
)

var levelIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

var validBadgeColors = map[string]struct{}{
	"neutral": {},
	"info":    {},
	"success": {},
	"warning": {},
	"purple":  {},
	"blue":    {},
	"cyan":    {},
	"green":   {},
	"orange":  {},
	"pink":    {},
}

type Level struct {
	ID             string  `json:"id"`
	Name           string  `json:"name"`
	Description    string  `json:"description,omitempty"`
	ThresholdQuota int64   `json:"threshold_quota"`
	Ratio          float64 `json:"ratio"`
	BadgeColor     string  `json:"badge_color"`
	Archived       bool    `json:"archived"`
}

type UserLevelConfig struct {
	SchemaVersion int     `json:"schema_version"`
	Enabled       bool    `json:"enabled"`
	Levels        []Level `json:"levels"`
}

type BillingLevel struct {
	Enabled    bool
	ID         string
	Name       string
	Ratio      float64
	BadgeColor string
}

type configValue struct {
	snapshot atomic.Pointer[UserLevelConfig]
}

func newConfigValue(value UserLevelConfig) *configValue {
	store := &configValue{}
	store.Store(value)
	return store
}

func (value *configValue) Store(next UserLevelConfig) {
	copyValue := cloneConfig(next)
	value.snapshot.Store(&copyValue)
}

func (value *configValue) Load() UserLevelConfig {
	if value == nil {
		return DefaultConfig()
	}
	current := value.snapshot.Load()
	if current == nil {
		return DefaultConfig()
	}
	return cloneConfig(*current)
}

func (value *configValue) MarshalJSON() ([]byte, error) {
	return common.Marshal(value.Load())
}

func (value *configValue) UnmarshalJSON(data []byte) error {
	next, err := ParseConfigJSON(data)
	if err != nil {
		return err
	}
	value.Store(next)
	return nil
}

type settings struct {
	Config *configValue `json:"config"`
}

var currentSettings = settings{Config: newConfigValue(DefaultConfig())}

func init() {
	config.GlobalConfig.Register("user_level_setting", &currentSettings)
}

func DefaultConfig() UserLevelConfig {
	return UserLevelConfig{
		SchemaVersion: SchemaVersion,
		Enabled:       false,
		Levels: []Level{
			{
				ID:             BaseLevelID,
				Name:           "普通用户",
				Description:    "默认等级",
				ThresholdQuota: 0,
				Ratio:          1,
				BadgeColor:     "neutral",
			},
		},
	}
}

func GetConfig() UserLevelConfig {
	return currentSettings.Config.Load()
}

func IsEnabled() bool {
	return GetConfig().Enabled
}

func ParseConfigJSON(data []byte) (UserLevelConfig, error) {
	var value UserLevelConfig
	if err := common.Unmarshal(data, &value); err != nil {
		return UserLevelConfig{}, fmt.Errorf("用户等级配置不是有效的 JSON: %w", err)
	}
	return NormalizeAndValidate(value)
}

func ValidateConfigJSON(value string) error {
	next, err := ParseConfigJSON([]byte(value))
	if err != nil {
		return err
	}
	return ValidateTransition(GetConfig(), next)
}

// ConfigRevision returns an opaque revision for optimistic concurrency on the
// admin configuration endpoint. Normalization makes equivalent configurations
// produce the same revision regardless of input level order.
func ConfigRevision(value UserLevelConfig) (string, error) {
	normalized, err := NormalizeAndValidate(value)
	if err != nil {
		return "", err
	}
	data, err := common.Marshal(normalized)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

// ValidateTransition preserves the durable identity and relative order of
// levels that may already be referenced by users and claim-history rows.
func ValidateTransition(previous, next UserLevelConfig) error {
	nextIndexes := make(map[string]int, len(next.Levels))
	for index, level := range next.Levels {
		nextIndexes[level.ID] = index
	}

	lastIndex := -1
	for _, level := range previous.Levels {
		index, exists := nextIndexes[level.ID]
		if !exists {
			return fmt.Errorf("已保存等级 %q 不能删除，请改为停止发放", level.Name)
		}
		if index <= lastIndex {
			return fmt.Errorf("已保存等级的相对顺序不能改变")
		}
		lastIndex = index
	}
	return nil
}

func NormalizeAndValidate(value UserLevelConfig) (UserLevelConfig, error) {
	if value.SchemaVersion == 0 {
		value.SchemaVersion = SchemaVersion
	}
	if value.SchemaVersion != SchemaVersion {
		return UserLevelConfig{}, fmt.Errorf("不支持的用户等级配置版本: %d", value.SchemaVersion)
	}
	if len(value.Levels) == 0 || len(value.Levels) > MaxLevelCount {
		return UserLevelConfig{}, fmt.Errorf("用户等级数量必须在 1 到 %d 之间", MaxLevelCount)
	}

	value.Levels = append([]Level(nil), value.Levels...)
	for index := range value.Levels {
		level := &value.Levels[index]
		level.ID = strings.TrimSpace(level.ID)
		level.Name = strings.TrimSpace(level.Name)
		level.Description = strings.TrimSpace(level.Description)
		level.BadgeColor = strings.TrimSpace(level.BadgeColor)
		if level.BadgeColor == "" {
			level.BadgeColor = "neutral"
		}
	}
	sort.SliceStable(value.Levels, func(i, j int) bool {
		return value.Levels[i].ThresholdQuota < value.Levels[j].ThresholdQuota
	})

	ids := make(map[string]struct{}, len(value.Levels))
	names := make(map[string]struct{}, len(value.Levels))
	var previousThreshold int64 = -1
	previousRatio := 1.0
	for index, level := range value.Levels {
		if !levelIDPattern.MatchString(level.ID) {
			return UserLevelConfig{}, fmt.Errorf("等级 ID %q 只能包含小写字母、数字、下划线和短横线", level.ID)
		}
		if _, exists := ids[level.ID]; exists {
			return UserLevelConfig{}, fmt.Errorf("等级 ID %q 重复", level.ID)
		}
		ids[level.ID] = struct{}{}
		if level.Name == "" || len([]rune(level.Name)) > maxLevelNameLen {
			return UserLevelConfig{}, fmt.Errorf("等级 %q 的名称长度必须在 1 到 %d 个字符之间", level.ID, maxLevelNameLen)
		}
		if _, exists := names[level.Name]; exists {
			return UserLevelConfig{}, fmt.Errorf("等级名称 %q 重复", level.Name)
		}
		names[level.Name] = struct{}{}
		if len([]rune(level.Description)) > maxDescription {
			return UserLevelConfig{}, fmt.Errorf("等级 %q 的说明不能超过 %d 个字符", level.ID, maxDescription)
		}
		if _, ok := validBadgeColors[level.BadgeColor]; !ok {
			return UserLevelConfig{}, fmt.Errorf("等级 %q 使用了不支持的标签颜色 %q", level.ID, level.BadgeColor)
		}
		if level.ThresholdQuota < 0 || level.ThresholdQuota <= previousThreshold {
			return UserLevelConfig{}, fmt.Errorf("等级门槛必须为非负数并严格递增")
		}
		if level.ThresholdQuota > MaxThresholdQuota {
			return UserLevelConfig{}, fmt.Errorf("等级 %q 的门槛不能超过 %d", level.ID, MaxThresholdQuota)
		}
		if level.Ratio <= 0 || level.Ratio > 1 || math.IsNaN(level.Ratio) || math.IsInf(level.Ratio, 0) {
			return UserLevelConfig{}, fmt.Errorf("等级 %q 的倍率必须大于 0 且不超过 1", level.ID)
		}
		if level.Ratio > previousRatio {
			return UserLevelConfig{}, fmt.Errorf("更高等级的倍率不能高于前一等级")
		}
		if index == 0 {
			if level.ID != BaseLevelID || level.ThresholdQuota != 0 || level.Ratio != 1 || level.Archived {
				return UserLevelConfig{}, fmt.Errorf("基础等级必须使用 ID %q、门槛 0、倍率 1，且不能归档", BaseLevelID)
			}
		} else if level.ThresholdQuota == 0 {
			return UserLevelConfig{}, fmt.Errorf("非基础等级的门槛必须大于 0")
		}
		previousThreshold = level.ThresholdQuota
		previousRatio = level.Ratio
	}

	return value, nil
}

func ResolveBillingLevel(levelID string) BillingLevel {
	current := GetConfig()
	level, ok := findLevel(current, levelID)
	if !ok {
		level, _ = findLevel(current, BaseLevelID)
	}
	ratio := 1.0
	if current.Enabled {
		ratio = level.Ratio
	}
	return BillingLevel{
		Enabled:    current.Enabled,
		ID:         level.ID,
		Name:       level.Name,
		Ratio:      ratio,
		BadgeColor: level.BadgeColor,
	}
}

func FindLevel(levelID string) (Level, bool) {
	return findLevel(GetConfig(), levelID)
}

func HighestClaimableLevel(totalQuota int64, currentLevelID string) (Level, bool) {
	current := GetConfig()
	currentIndex := levelIndex(current, currentLevelID)
	var target Level
	found := false
	for index, level := range current.Levels {
		if index <= currentIndex || level.Archived || level.ThresholdQuota > totalQuota {
			continue
		}
		target = level
		found = true
	}
	return target, found
}

func LevelIndex(levelID string) int {
	return levelIndex(GetConfig(), levelID)
}

func cloneConfig(value UserLevelConfig) UserLevelConfig {
	value.Levels = append([]Level(nil), value.Levels...)
	return value
}

func findLevel(value UserLevelConfig, levelID string) (Level, bool) {
	if levelID == "" {
		levelID = BaseLevelID
	}
	for _, level := range value.Levels {
		if level.ID == levelID {
			return level, true
		}
	}
	return Level{}, false
}

func levelIndex(value UserLevelConfig, levelID string) int {
	if levelID == "" {
		levelID = BaseLevelID
	}
	for index, level := range value.Levels {
		if level.ID == levelID {
			return index
		}
	}
	return 0
}
