package model

import (
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

type PromptFilterLog struct {
	Id              int                      `json:"id"`
	CreatedAt       int64                    `json:"created_at"`
	Source          string                   `json:"source"`
	Endpoint        string                   `json:"endpoint"`
	Model           string                   `json:"model"`
	Action          string                   `json:"action"`
	Mode            string                   `json:"mode"`
	Score           int                      `json:"score"`
	RawScore        int                      `json:"raw_score"`
	Threshold       int                      `json:"threshold"`
	StrictHit       bool                     `json:"strict_hit"`
	Matched         []map[string]interface{} `json:"matched"`
	TextPreview     string                   `json:"text_preview"`
	FullText        string                   `json:"full_text,omitempty"`
	UserId          int                      `json:"user_id"`
	Username        string                   `json:"username"`
	TokenId         int                      `json:"api_key_id"`
	TokenName       string                   `json:"api_key_name"`
	ChannelId       int                      `json:"channel_id"`
	ChannelName     string                   `json:"channel_name"`
	Group           string                   `json:"group"`
	Ip              string                   `json:"client_ip"`
	RequestId       string                   `json:"request_id"`
	ErrorCode       string                   `json:"error_code"`
	Reviewed        bool                     `json:"reviewed"`
	ReviewFlagged   bool                     `json:"review_flagged"`
	ReviewModel     string                   `json:"review_model"`
	ReviewError     string                   `json:"review_error"`
	ExtractedChars  int                      `json:"extracted_chars"`
	StatusCode      int                      `json:"status_code"`
	PromptFilterMsg string                   `json:"prompt_filter_msg"`
}

type PromptFilterLogQuery struct {
	Page     int
	PageSize int
	Source   string
	Action   string
	Endpoint string
	Model    string
	APIKeyID int
	Query    string
}

func ListPromptFilterLogsPage(query PromptFilterLogQuery) ([]*PromptFilterLog, int64, error) {
	pageSize := query.PageSize
	if pageSize <= 0 || pageSize > 200 {
		pageSize = 50
	}
	page := query.Page
	if page <= 0 {
		page = 1
	}

	tx, err := promptFilterLogBaseQuery(query)
	if err != nil {
		return nil, 0, err
	}
	var total int64
	if err := tx.Model(&Log{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	order := "logs.created_at desc, logs.id desc"
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		order = clickHouseLogOrder("logs.")
	}

	var logs []*Log
	if err := tx.Order(order).Limit(pageSize).Offset((page - 1) * pageSize).Find(&logs).Error; err != nil {
		return nil, 0, err
	}
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		assignDisplayLogIds(logs, (page-1)*pageSize)
	}

	items := make([]*PromptFilterLog, 0, len(logs))
	for _, rawLog := range logs {
		item := promptFilterLogFromRaw(rawLog)
		if item != nil {
			items = append(items, item)
		}
	}
	return items, total, nil
}

func CountPromptFilterLogs() (int64, error) {
	condition, pattern, err := buildLogLikeCondition("logs.other", `%"reject_reason":"prompt_filter"%`)
	if err != nil {
		return 0, err
	}
	var total int64
	if err := LOG_DB.Model(&Log{}).Where("logs.type = ?", LogTypeError).Where(condition, pattern).Count(&total).Error; err != nil {
		return 0, err
	}
	return total, nil
}

func ClearPromptFilterLogs() (int64, error) {
	condition, pattern, err := buildLogLikeCondition("other", `%"reject_reason":"prompt_filter"%`)
	if err != nil {
		return 0, err
	}
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		total, err := CountPromptFilterLogs()
		if err != nil {
			return 0, err
		}
		if total == 0 {
			return 0, nil
		}
		if err := LOG_DB.Exec(
			"ALTER TABLE logs DELETE WHERE type = ? AND "+condition+" SETTINGS mutations_sync = 1",
			LogTypeError,
			pattern,
		).Error; err != nil {
			return 0, err
		}
		return total, nil
	}
	result := LOG_DB.Where("type = ?", LogTypeError).Where(condition, pattern).Delete(&Log{})
	return result.RowsAffected, result.Error
}

func promptFilterLogBaseQuery(query PromptFilterLogQuery) (*gorm.DB, error) {
	tx := LOG_DB.Model(&Log{}).Where("logs.type = ?", LogTypeError)
	condition, pattern, err := buildLogLikeCondition("logs.other", `%"reject_reason":"prompt_filter"%`)
	if err != nil {
		return nil, err
	}
	tx = tx.Where(condition, pattern)

	if query.APIKeyID > 0 {
		tx = tx.Where("logs.token_id = ?", query.APIKeyID)
	}
	if strings.TrimSpace(query.Model) != "" {
		if tx, err = applyExplicitLogTextFilter(tx, "logs.model_name", query.Model); err != nil {
			return nil, err
		}
	}
	for _, filter := range []struct {
		column string
		value  string
	}{
		{column: "logs.other", value: promptFilterOtherLike("source", query.Source)},
		{column: "logs.other", value: promptFilterOtherLike("action", query.Action)},
		{column: "logs.other", value: promptFilterOtherLike("endpoint", query.Endpoint)},
	} {
		if filter.value == "" {
			continue
		}
		if tx, err = applyExplicitLogTextFilter(tx, filter.column, filter.value); err != nil {
			return nil, err
		}
	}
	if q := strings.TrimSpace(query.Query); q != "" {
		searchPattern := "%" + q + "%"
		condition, pattern, err := buildLogLikeCondition("logs.other", searchPattern)
		if err != nil {
			return nil, err
		}
		tx = tx.Where(condition, pattern)
	}
	return tx, nil
}

func promptFilterOtherLike(key string, value string) string {
	value = strings.TrimSpace(value)
	if value == "" || value == "all" {
		return ""
	}
	return `%"` + key + `":"` + value + `"%`
}

func promptFilterLogFromRaw(rawLog *Log) *PromptFilterLog {
	if rawLog == nil {
		return nil
	}
	other, _ := common.StrToMap(rawLog.Other)
	if other == nil {
		other = map[string]interface{}{}
	}
	if common.Interface2String(other["reject_reason"]) != "prompt_filter" {
		return nil
	}

	item := &PromptFilterLog{
		Id:              rawLog.Id,
		CreatedAt:       rawLog.CreatedAt,
		Source:          promptFilterMapString(other, "source", "local_filter"),
		Endpoint:        promptFilterMapString(other, "endpoint", ""),
		Model:           rawLog.ModelName,
		Action:          promptFilterMapString(other, "action", ""),
		Mode:            promptFilterMapString(other, "mode", ""),
		Score:           promptFilterMapInt(other, "score"),
		RawScore:        promptFilterMapInt(other, "raw_score"),
		Threshold:       promptFilterMapInt(other, "threshold"),
		StrictHit:       promptFilterMapBool(other, "strict_hit"),
		Matched:         promptFilterMapMatches(other),
		TextPreview:     promptFilterMapString(other, "text_preview", ""),
		FullText:        promptFilterLogFullText(other),
		UserId:          rawLog.UserId,
		Username:        rawLog.Username,
		TokenId:         rawLog.TokenId,
		TokenName:       rawLog.TokenName,
		ChannelId:       rawLog.ChannelId,
		ChannelName:     rawLog.ChannelName,
		Group:           rawLog.Group,
		Ip:              rawLog.Ip,
		RequestId:       rawLog.RequestId,
		ErrorCode:       promptFilterMapString(other, "code", ""),
		Reviewed:        promptFilterMapBool(other, "reviewed"),
		ReviewFlagged:   promptFilterMapBool(other, "review_flagged"),
		ReviewModel:     promptFilterMapString(other, "review_model", ""),
		ReviewError:     promptFilterMapString(other, "review_error", ""),
		ExtractedChars:  promptFilterMapInt(other, "extracted_chars"),
		StatusCode:      promptFilterMapInt(other, "status_code"),
		PromptFilterMsg: promptFilterMapString(other, "prompt_filter_msg", rawLog.Content),
	}
	if item.Mode == "" {
		item.Mode = item.Action
	}
	if item.ErrorCode == "" {
		item.ErrorCode = promptFilterMapString(other, "error_code", "")
	}
	return item
}

func promptFilterMapString(m map[string]interface{}, key string, fallback string) string {
	value := strings.TrimSpace(common.Interface2String(m[key]))
	if value == "" {
		return fallback
	}
	return value
}

func promptFilterLogFullText(other map[string]interface{}) string {
	adminInfo, _ := other["admin_info"].(map[string]interface{})
	if adminInfo != nil {
		if fullText := promptFilterMapString(adminInfo, "prompt_filter_full_text", ""); fullText != "" {
			return fullText
		}
	}
	return promptFilterMapString(other, "full_text", "")
}

func promptFilterMapInt(m map[string]interface{}, key string) int {
	value := m[key]
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		parsed, _ := strconv.Atoi(strings.TrimSpace(typed))
		return parsed
	default:
		return 0
	}
}

func promptFilterMapBool(m map[string]interface{}, key string) bool {
	value := m[key]
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		parsed, _ := strconv.ParseBool(strings.TrimSpace(typed))
		return parsed
	default:
		return false
	}
}

func promptFilterMapMatches(m map[string]interface{}) []map[string]interface{} {
	raw, ok := m["matched"].([]interface{})
	if !ok {
		return []map[string]interface{}{}
	}
	matches := make([]map[string]interface{}, 0, len(raw))
	for _, item := range raw {
		match, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		matches = append(matches, match)
	}
	return matches
}
