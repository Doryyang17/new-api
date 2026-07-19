package model

import (
	"errors"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const (
	RequestRiskLogKindProbe       = "probe"
	RequestRiskLogKindConcurrency = "concurrency"
)

type RequestRiskLog struct {
	Id                           int      `json:"id"`
	CreatedAt                    int64    `json:"created_at"`
	Kind                         string   `json:"kind"`
	Blocked                      bool     `json:"blocked"`
	Mode                         string   `json:"mode"`
	RiskLevel                    string   `json:"risk_level"`
	Score                        int      `json:"score"`
	Factors                      []string `json:"factors"`
	MatchedKeywords              []string `json:"matched_keywords"`
	TextPreview                  string   `json:"text_preview"`
	ExtractedText                string   `json:"extracted_text,omitempty"`
	FullRequest                  string   `json:"full_request,omitempty"`
	FullRequestAvailable         bool     `json:"full_request_available"`
	FullRequestUnavailableReason string   `json:"full_request_unavailable_reason,omitempty"`
	ExtractedChars               int      `json:"extracted_chars"`
	Endpoint                     string   `json:"endpoint"`
	Model                        string   `json:"model"`
	UserId                       int      `json:"user_id"`
	Username                     string   `json:"username"`
	TokenId                      int      `json:"api_key_id"`
	TokenName                    string   `json:"api_key_name"`
	Group                        string   `json:"group"`
	Ip                           string   `json:"client_ip"`
	RequestId                    string   `json:"request_id"`
	RequestCount10s              int      `json:"request_count_10s"`
	RequestCount60s              int      `json:"request_count_60s"`
	IPRequestCount60s            int      `json:"ip_request_count_60s"`
	RepeatCount60s               int      `json:"repeat_count_60s"`
	DistinctModels60s            int      `json:"distinct_models_60s"`
	FailureCount30s              int      `json:"failure_count_30s"`
	UserInFlight                 int      `json:"user_in_flight"`
	UserLimit                    int      `json:"user_limit"`
	TokenInFlight                int      `json:"token_in_flight"`
	TokenLimit                   int      `json:"token_limit"`
}

type RequestRiskLogDetail struct {
	Id            int    `json:"-"`
	RequestId     string `json:"-" gorm:"type:varchar(64);index:idx_request_risk_detail_locator,priority:1"`
	CreatedAt     int64  `json:"-" gorm:"bigint;index;index:idx_request_risk_detail_locator,priority:2"`
	Kind          string `json:"-" gorm:"type:varchar(32);index:idx_request_risk_detail_locator,priority:3"`
	ExtractedText string `json:"-"`
	FullRequest   string `json:"-"`
}

type RequestRiskLogQuery struct {
	Page     int
	PageSize int
	Kind     string
	Action   string
	Level    string
	Model    string
	APIKeyID int
	Query    string
}

func ListRequestRiskLogsPage(query RequestRiskLogQuery) ([]*RequestRiskLog, int64, error) {
	pageSize := query.PageSize
	if pageSize <= 0 || pageSize > 200 {
		pageSize = 50
	}
	page := query.Page
	if page <= 0 {
		page = 1
	}

	tx, err := requestRiskLogBaseQuery(query)
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

	items := make([]*RequestRiskLog, 0, len(logs))
	for _, rawLog := range logs {
		if item := requestRiskLogFromRaw(rawLog, false); item != nil {
			items = append(items, item)
		}
	}
	return items, total, nil
}

func GetRequestRiskLogDetail(requestId string, kind string, createdAt int64) (*RequestRiskLog, error) {
	requestId = strings.TrimSpace(requestId)
	kindPattern := requestRiskLogKindPattern(kind)
	if requestId == "" || createdAt <= 0 || kindPattern == "" {
		return nil, errors.New("风控日志详情定位参数不正确")
	}

	tx := LOG_DB.Model(&Log{}).
		Where("logs.type = ?", LogTypeError).
		Where("logs.request_id = ?", requestId).
		Where("logs.created_at = ?", createdAt)
	var err error
	if tx, err = applyExplicitLogTextFilter(tx, "logs.other", kindPattern); err != nil {
		return nil, err
	}
	var rawLog Log
	if err := tx.Take(&rawLog).Error; err != nil {
		return nil, err
	}
	item := requestRiskLogFromRaw(&rawLog, true)
	if item == nil {
		return nil, errors.New("风控日志详情格式不正确")
	}

	var detail RequestRiskLogDetail
	detailErr := LOG_DB.
		Where("request_id = ? AND created_at = ? AND kind = ?", requestId, createdAt, item.Kind).
		Take(&detail).Error
	if detailErr == nil {
		item.ExtractedText = detail.ExtractedText
		item.FullRequest = detail.FullRequest
	} else if !errors.Is(detailErr, gorm.ErrRecordNotFound) {
		return nil, detailErr
	}
	return item, nil
}

func ClearRequestRiskLogs() (int64, error) {
	condition, args, err := requestRiskLogReasonCondition("other")
	if err != nil {
		return 0, err
	}
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		var total int64
		if err := LOG_DB.Model(&Log{}).Where("type = ?", LogTypeError).Where(condition, args...).Count(&total).Error; err != nil {
			return 0, err
		}
		if total == 0 {
			return 0, clearRequestRiskLogDetails()
		}
		if err := clearRequestRiskLogDetails(); err != nil {
			return 0, err
		}
		if err := LOG_DB.Exec(
			"ALTER TABLE logs DELETE WHERE type = ? AND "+condition+" SETTINGS mutations_sync = 1",
			append([]interface{}{LogTypeError}, args...)...,
		).Error; err != nil {
			return 0, err
		}
		return total, nil
	}

	var deleted int64
	err = LOG_DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&RequestRiskLogDetail{}).Error; err != nil {
			return err
		}
		result := tx.Where("type = ?", LogTypeError).Where(condition, args...).Delete(&Log{})
		deleted = result.RowsAffected
		return result.Error
	})
	return deleted, err
}

func clearRequestRiskLogDetails() error {
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		return LOG_DB.Exec(
			"ALTER TABLE request_risk_log_details DELETE WHERE 1 SETTINGS mutations_sync = 1",
		).Error
	}
	return LOG_DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&RequestRiskLogDetail{}).Error
}

func requestRiskLogBaseQuery(query RequestRiskLogQuery) (*gorm.DB, error) {
	tx := LOG_DB.Model(&Log{}).Where("logs.type = ?", LogTypeError)
	condition, args, err := requestRiskLogReasonCondition("logs.other")
	if err != nil {
		return nil, err
	}
	tx = tx.Where(condition, args...)

	if kindPattern := requestRiskLogKindPattern(query.Kind); kindPattern != "" {
		if tx, err = applyExplicitLogTextFilter(tx, "logs.other", kindPattern); err != nil {
			return nil, err
		}
	}
	if action := strings.TrimSpace(query.Action); action == "blocked" || action == "observed" {
		rejectCondition, rejectArgs, buildErr := requestRiskLogRejectCondition("logs.other")
		if buildErr != nil {
			return nil, buildErr
		}
		if action == "blocked" {
			tx = tx.Where(rejectCondition, rejectArgs...)
		} else {
			tx = tx.Where("NOT ("+rejectCondition+")", rejectArgs...)
		}
	}
	if level := strings.TrimSpace(query.Level); level != "" && level != "all" {
		if tx, err = applyExplicitLogTextFilter(tx, "logs.other", `%"risk_level":"`+level+`"%`); err != nil {
			return nil, err
		}
	}
	if strings.TrimSpace(query.Model) != "" {
		if tx, err = applyExplicitLogTextFilter(tx, "logs.model_name", query.Model); err != nil {
			return nil, err
		}
	}
	if query.APIKeyID > 0 {
		apiKeyID := strconv.Itoa(query.APIKeyID)
		adminCommaCondition, adminCommaPattern, buildErr := buildLogLikeCondition("logs.other", `%"token_id":`+apiKeyID+`,%`)
		if buildErr != nil {
			return nil, buildErr
		}
		adminEndCondition, adminEndPattern, buildErr := buildLogLikeCondition("logs.other", `%"token_id":`+apiKeyID+`}%`)
		if buildErr != nil {
			return nil, buildErr
		}
		tx = tx.Where(
			"(logs.token_id = ? OR "+adminCommaCondition+" OR "+adminEndCondition+")",
			query.APIKeyID,
			adminCommaPattern,
			adminEndPattern,
		)
	}
	if q := strings.TrimSpace(query.Query); q != "" {
		searchCondition, searchPattern, buildErr := buildLogLikeCondition("logs.other", "%"+q+"%")
		if buildErr != nil {
			return nil, buildErr
		}
		tx = tx.Where("(logs.username = ? OR logs.token_name = ? OR "+searchCondition+")", q, q, searchPattern)
	}
	return tx, nil
}

func requestRiskLogReasonCondition(column string) (string, []interface{}, error) {
	probeCondition, probePattern, err := buildLogLikeCondition(column, `%"risk_reason":"request_probe_guard"%`)
	if err != nil {
		return "", nil, err
	}
	concurrencyCondition, concurrencyPattern, err := buildLogLikeCondition(column, `%"risk_reason":"request_concurrency_guard"%`)
	if err != nil {
		return "", nil, err
	}
	return "(" + probeCondition + " OR " + concurrencyCondition + ")", []interface{}{probePattern, concurrencyPattern}, nil
}

func requestRiskLogRejectCondition(column string) (string, []interface{}, error) {
	probeCondition, probePattern, err := buildLogLikeCondition(column, `%"reject_reason":"request_probe_guard"%`)
	if err != nil {
		return "", nil, err
	}
	concurrencyCondition, concurrencyPattern, err := buildLogLikeCondition(column, `%"reject_reason":"request_concurrency_guard"%`)
	if err != nil {
		return "", nil, err
	}
	return "(" + probeCondition + " OR " + concurrencyCondition + ")", []interface{}{probePattern, concurrencyPattern}, nil
}

func requestRiskLogKindPattern(kind string) string {
	switch strings.TrimSpace(kind) {
	case RequestRiskLogKindProbe:
		return `%"risk_reason":"request_probe_guard"%`
	case RequestRiskLogKindConcurrency:
		return `%"risk_reason":"request_concurrency_guard"%`
	default:
		return ""
	}
}

func requestRiskLogKindFromReason(reason string) string {
	switch strings.TrimSpace(reason) {
	case "request_probe_guard":
		return RequestRiskLogKindProbe
	case "request_concurrency_guard":
		return RequestRiskLogKindConcurrency
	default:
		return ""
	}
}

func requestRiskLogFromRaw(rawLog *Log, includeDetails bool) *RequestRiskLog {
	if rawLog == nil {
		return nil
	}
	other, _ := common.StrToMap(rawLog.Other)
	if other == nil {
		return nil
	}
	reason := common.Interface2String(other["risk_reason"])
	kind := requestRiskLogKindFromReason(reason)
	if kind == "" {
		return nil
	}
	adminInfo, _ := other["admin_info"].(map[string]interface{})
	if adminInfo == nil {
		adminInfo = map[string]interface{}{}
	}
	blocked := common.Interface2String(other["reject_reason"]) == reason
	userID := rawLog.UserId
	if userID == 0 {
		userID = promptFilterMapInt(adminInfo, "user_id")
	}
	tokenID := rawLog.TokenId
	if tokenID == 0 {
		tokenID = promptFilterMapInt(adminInfo, "token_id")
	}
	tokenName := rawLog.TokenName
	if tokenName == "" {
		tokenName = promptFilterMapString(adminInfo, "token_name", "")
	}
	ip := rawLog.Ip
	if ip == "" {
		ip = promptFilterMapString(adminInfo, "client_ip", "")
	}
	extractedText := ""
	fullRequest := ""
	if includeDetails {
		extractedText = promptFilterMapString(adminInfo, "request_risk_extracted_text", "")
		if extractedText == "" {
			extractedText = promptFilterMapString(adminInfo, "request_risk_full_text", "")
		}
		fullRequest = promptFilterMapString(adminInfo, "request_risk_full_request", "")
	}
	fullRequestAvailable := promptFilterMapBool(adminInfo, "full_request_available")
	fullRequestUnavailableReason := promptFilterMapString(adminInfo, "full_request_unavailable_reason", "")
	if includeDetails && !fullRequestAvailable && fullRequestUnavailableReason == "" && extractedText != "" {
		fullRequestUnavailableReason = "该历史日志只记录了提取文本，未保存完整请求体"
	}
	return &RequestRiskLog{
		Id:                           rawLog.Id,
		CreatedAt:                    rawLog.CreatedAt,
		Kind:                         kind,
		Blocked:                      blocked,
		Mode:                         promptFilterMapString(adminInfo, "request_risk_mode", ""),
		RiskLevel:                    promptFilterMapString(adminInfo, "risk_level", ""),
		Score:                        promptFilterMapInt(adminInfo, "risk_score"),
		Factors:                      requestRiskMapStringSlice(adminInfo, "risk_factors"),
		MatchedKeywords:              requestRiskMapStringSlice(adminInfo, "matched_keywords"),
		TextPreview:                  promptFilterMapString(adminInfo, "text_preview", ""),
		ExtractedText:                extractedText,
		FullRequest:                  fullRequest,
		FullRequestAvailable:         fullRequestAvailable,
		FullRequestUnavailableReason: fullRequestUnavailableReason,
		ExtractedChars:               promptFilterMapInt(adminInfo, "extracted_chars"),
		Endpoint:                     promptFilterMapString(adminInfo, "endpoint", ""),
		Model:                        rawLog.ModelName,
		UserId:                       userID,
		Username:                     rawLog.Username,
		TokenId:                      tokenID,
		TokenName:                    tokenName,
		Group:                        rawLog.Group,
		Ip:                           ip,
		RequestId:                    rawLog.RequestId,
		RequestCount10s:              promptFilterMapInt(adminInfo, "request_count_10s"),
		RequestCount60s:              promptFilterMapInt(adminInfo, "request_count_60s"),
		IPRequestCount60s:            promptFilterMapInt(adminInfo, "ip_request_count_60s"),
		RepeatCount60s:               promptFilterMapInt(adminInfo, "repeat_count_60s"),
		DistinctModels60s:            promptFilterMapInt(adminInfo, "distinct_models_60s"),
		FailureCount30s:              promptFilterMapInt(adminInfo, "failure_count_30s"),
		UserInFlight:                 promptFilterMapInt(adminInfo, "user_in_flight"),
		UserLimit:                    promptFilterMapInt(adminInfo, "user_limit"),
		TokenInFlight:                promptFilterMapInt(adminInfo, "token_in_flight"),
		TokenLimit:                   promptFilterMapInt(adminInfo, "token_limit"),
	}
}

func requestRiskMapStringSlice(m map[string]interface{}, key string) []string {
	raw := m[key]
	values := make([]string, 0)
	switch items := raw.(type) {
	case []interface{}:
		for _, item := range items {
			if value := strings.TrimSpace(common.Interface2String(item)); value != "" {
				values = append(values, value)
			}
		}
	case []string:
		for _, item := range items {
			if value := strings.TrimSpace(item); value != "" {
				values = append(values, value)
			}
		}
	}
	return values
}
