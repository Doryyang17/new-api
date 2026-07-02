package system_setting

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
)

const (
	PromptFilterModeBlock   = "block"
	PromptFilterModeWarn    = "warn"
	PromptFilterModeMonitor = "monitor"

	DefaultPromptFilterMode            = PromptFilterModeBlock
	DefaultPromptFilterThreshold       = 50
	DefaultPromptFilterStrictThreshold = 90
	DefaultPromptFilterMaxTextLength   = 80 * 1024
	DefaultPromptFilterMessage         = "Request contains content blocked by prompt filter"
	DefaultPromptFilterBlockStatusCode = 460
	DefaultPromptFilterBlockErrorCode  = "prompt_blocked"

	DefaultPromptFilterReviewBaseURL        = "https://api.openai.com"
	DefaultPromptFilterReviewModel          = "omni-moderation-latest"
	DefaultPromptFilterReviewTimeoutSeconds = 10
)

type PromptFilterCustomPattern struct {
	Name     string `json:"name"`
	Pattern  string `json:"pattern"`
	Weight   int    `json:"weight"`
	Category string `json:"category,omitempty"`
	Strict   bool   `json:"strict,omitempty"`
	Enabled  *bool  `json:"enabled,omitempty"`
}

type PromptFilterLexiconFile struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	OriginalName string `json:"original_name"`
	StoredName   string `json:"stored_name"`
	SHA256       string `json:"sha256"`
	Size         int64  `json:"size"`
	WordCount    int    `json:"word_count"`
	Category     string `json:"category,omitempty"`
	Weight       int    `json:"weight"`
	Strict       bool   `json:"strict"`
	Enabled      bool   `json:"enabled"`
	Source       string `json:"source,omitempty"`
	UploadedAt   int64  `json:"uploaded_at"`
}

type PromptFilterSettings struct {
	Mode             string                      `json:"mode"`
	Threshold        int                         `json:"threshold"`
	StrictThreshold  int                         `json:"strict_threshold"`
	LogMatches       bool                        `json:"log_matches"`
	MaxTextLength    int                         `json:"max_text_length"`
	Message          string                      `json:"message"`
	BlockStatusCode  int                         `json:"block_status_code"`
	BlockErrorCode   string                      `json:"block_error_code"`
	GroupWhitelist   []string                    `json:"group_whitelist"`
	ChannelWhitelist []int                       `json:"channel_whitelist"`
	CustomPatterns   []PromptFilterCustomPattern `json:"custom_patterns"`
	DisabledPatterns []string                    `json:"disabled_patterns"`
	LexiconFiles     []PromptFilterLexiconFile   `json:"lexicon_files"`

	ReviewEnabled        bool   `json:"review_enabled"`
	ReviewAPIKey         string `json:"review_api_key,omitempty"`
	ReviewBaseURL        string `json:"review_base_url"`
	ReviewModel          string `json:"review_model"`
	ReviewTimeoutSeconds int    `json:"review_timeout_seconds"`
	ReviewFailClosed     bool   `json:"review_fail_closed"`
}

var promptFilterSettings = PromptFilterSettings{
	Mode:                 DefaultPromptFilterMode,
	Threshold:            DefaultPromptFilterThreshold,
	StrictThreshold:      DefaultPromptFilterStrictThreshold,
	LogMatches:           true,
	MaxTextLength:        DefaultPromptFilterMaxTextLength,
	Message:              DefaultPromptFilterMessage,
	BlockStatusCode:      DefaultPromptFilterBlockStatusCode,
	BlockErrorCode:       DefaultPromptFilterBlockErrorCode,
	GroupWhitelist:       []string{},
	ChannelWhitelist:     []int{},
	CustomPatterns:       []PromptFilterCustomPattern{},
	DisabledPatterns:     []string{},
	LexiconFiles:         []PromptFilterLexiconFile{},
	ReviewBaseURL:        DefaultPromptFilterReviewBaseURL,
	ReviewModel:          DefaultPromptFilterReviewModel,
	ReviewTimeoutSeconds: DefaultPromptFilterReviewTimeoutSeconds,
	ReviewFailClosed:     true,
}

func init() {
	config.GlobalConfig.Register("prompt_filter_setting", &promptFilterSettings)
}

func GetPromptFilterSettings() PromptFilterSettings {
	settings := promptFilterSettings
	settings.Mode = normalizedPromptFilterMode(settings.Mode)
	settings.Threshold = normalizedPromptFilterThreshold(settings.Threshold)
	settings.StrictThreshold = normalizedPromptFilterStrictThreshold(settings.StrictThreshold, settings.Threshold)
	settings.MaxTextLength = normalizedPromptFilterMaxTextLength(settings.MaxTextLength)
	settings.Message = normalizedPromptFilterMessage(settings.Message)
	settings.BlockStatusCode = normalizedPromptFilterBlockStatusCode(settings.BlockStatusCode)
	settings.BlockErrorCode = normalizedPromptFilterBlockErrorCode(settings.BlockErrorCode)
	settings.GroupWhitelist = normalizedPromptFilterStringList(settings.GroupWhitelist)
	settings.ChannelWhitelist = normalizedPromptFilterIntList(settings.ChannelWhitelist)
	settings.CustomPatterns = normalizedPromptFilterCustomPatterns(settings.CustomPatterns)
	settings.DisabledPatterns = normalizedPromptFilterDisabledPatterns(settings.DisabledPatterns)
	settings.LexiconFiles = normalizedPromptFilterLexiconFiles(settings.LexiconFiles)
	settings.ReviewAPIKey = strings.TrimSpace(settings.ReviewAPIKey)
	settings.ReviewBaseURL = normalizedPromptFilterReviewBaseURL(settings.ReviewBaseURL)
	settings.ReviewModel = normalizedPromptFilterReviewModel(settings.ReviewModel)
	settings.ReviewTimeoutSeconds = normalizedPromptFilterReviewTimeoutSeconds(settings.ReviewTimeoutSeconds)
	return settings
}

func ValidatePromptFilterOption(key string, value string) error {
	value = strings.TrimSpace(value)
	switch key {
	case "prompt_filter_setting.mode":
		switch value {
		case PromptFilterModeBlock, PromptFilterModeWarn, PromptFilterModeMonitor:
			return nil
		default:
			return fmt.Errorf("invalid prompt filter mode %q", value)
		}
	case "prompt_filter_setting.threshold":
		threshold, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid prompt filter threshold %q", value)
		}
		if threshold <= 0 {
			return fmt.Errorf("prompt filter threshold must be greater than 0")
		}
	case "prompt_filter_setting.strict_threshold":
		threshold, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid prompt filter strict threshold %q", value)
		}
		if threshold <= 0 {
			return fmt.Errorf("prompt filter strict threshold must be greater than 0")
		}
	case "prompt_filter_setting.log_matches":
		if _, err := strconv.ParseBool(value); err != nil {
			return fmt.Errorf("invalid prompt filter log matches value %q", value)
		}
	case "prompt_filter_setting.max_text_length":
		maxTextLength, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid prompt filter max text length %q", value)
		}
		if maxTextLength <= 0 {
			return fmt.Errorf("prompt filter max text length must be greater than 0")
		}
		if maxTextLength < 1024 || maxTextLength > 1024*1024 {
			return fmt.Errorf("prompt filter max text length must be between 1024 and 1048576")
		}
	case "prompt_filter_setting.message":
		if value == "" {
			return fmt.Errorf("prompt filter message cannot be empty")
		}
	case "prompt_filter_setting.block_status_code":
		statusCode, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid prompt filter block status code %q", value)
		}
		if !validPromptFilterBlockStatusCode(statusCode) {
			return fmt.Errorf("prompt filter block status code must be a 4xx status and cannot be 401")
		}
	case "prompt_filter_setting.block_error_code":
		if _, err := validatePromptFilterBlockErrorCode(value); err != nil {
			return err
		}
	case "prompt_filter_setting.group_whitelist":
		groups, err := parsePromptFilterStringList(value)
		if err != nil {
			return err
		}
		_ = normalizedPromptFilterStringList(groups)
	case "prompt_filter_setting.channel_whitelist":
		channels, err := parsePromptFilterIntList(value)
		if err != nil {
			return err
		}
		_ = normalizedPromptFilterIntList(channels)
	case "prompt_filter_setting.custom_patterns":
		patterns, err := parsePromptFilterCustomPatterns(value)
		if err != nil {
			return err
		}
		if _, err := validatePromptFilterCustomPatterns(patterns); err != nil {
			return err
		}
	case "prompt_filter_setting.disabled_patterns":
		patterns, err := parsePromptFilterDisabledPatterns(value)
		if err != nil {
			return err
		}
		if _, err := validatePromptFilterDisabledPatterns(patterns); err != nil {
			return err
		}
	case "prompt_filter_setting.lexicon_files":
		files, err := parsePromptFilterLexiconFiles(value)
		if err != nil {
			return err
		}
		if _, err := validatePromptFilterLexiconFiles(files); err != nil {
			return err
		}
	case "prompt_filter_setting.review_enabled":
		if _, err := strconv.ParseBool(value); err != nil {
			return fmt.Errorf("invalid prompt filter review enabled value %q", value)
		}
	case "prompt_filter_setting.review_api_key":
		return nil
	case "prompt_filter_setting.review_base_url":
		if _, err := validatePromptFilterReviewBaseURL(value); err != nil {
			return err
		}
	case "prompt_filter_setting.review_model":
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("prompt filter review model cannot be empty")
		}
	case "prompt_filter_setting.review_timeout_seconds":
		timeout, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid prompt filter review timeout %q", value)
		}
		if timeout <= 0 || timeout > 60 {
			return fmt.Errorf("prompt filter review timeout must be between 1 and 60 seconds")
		}
	case "prompt_filter_setting.review_fail_closed":
		if _, err := strconv.ParseBool(value); err != nil {
			return fmt.Errorf("invalid prompt filter review fail-closed value %q", value)
		}
	}
	return nil
}

func PromptFilterReviewAPIKeyConfigured() bool {
	return strings.TrimSpace(promptFilterSettings.ReviewAPIKey) != ""
}

func parsePromptFilterCustomPatterns(value string) ([]PromptFilterCustomPattern, error) {
	if strings.TrimSpace(value) == "" {
		return []PromptFilterCustomPattern{}, nil
	}
	var patterns []PromptFilterCustomPattern
	if err := common.UnmarshalJsonStr(value, &patterns); err != nil {
		return nil, fmt.Errorf("invalid prompt filter custom patterns JSON: %w", err)
	}
	return patterns, nil
}

func parsePromptFilterDisabledPatterns(value string) ([]string, error) {
	if strings.TrimSpace(value) == "" {
		return []string{}, nil
	}
	var patterns []string
	if err := common.UnmarshalJsonStr(value, &patterns); err != nil {
		return nil, fmt.Errorf("invalid prompt filter disabled patterns JSON: %w", err)
	}
	return patterns, nil
}

func parsePromptFilterLexiconFiles(value string) ([]PromptFilterLexiconFile, error) {
	if strings.TrimSpace(value) == "" {
		return []PromptFilterLexiconFile{}, nil
	}
	var files []PromptFilterLexiconFile
	if err := common.UnmarshalJsonStr(value, &files); err != nil {
		return nil, fmt.Errorf("invalid prompt filter lexicon files JSON: %w", err)
	}
	return files, nil
}

func parsePromptFilterStringList(value string) ([]string, error) {
	if strings.TrimSpace(value) == "" {
		return []string{}, nil
	}
	var items []string
	if err := common.UnmarshalJsonStr(value, &items); err != nil {
		return nil, fmt.Errorf("invalid prompt filter string list JSON: %w", err)
	}
	return items, nil
}

func parsePromptFilterIntList(value string) ([]int, error) {
	if strings.TrimSpace(value) == "" {
		return []int{}, nil
	}
	var items []int
	if err := common.UnmarshalJsonStr(value, &items); err != nil {
		return nil, fmt.Errorf("invalid prompt filter integer list JSON: %w", err)
	}
	for _, item := range items {
		if item <= 0 {
			return nil, fmt.Errorf("prompt filter channel whitelist values must be greater than 0")
		}
	}
	return items, nil
}

func validatePromptFilterCustomPatterns(patterns []PromptFilterCustomPattern) ([]PromptFilterCustomPattern, error) {
	seen := map[string]struct{}{}
	normalized := make([]PromptFilterCustomPattern, 0, len(patterns))
	for _, pattern := range patterns {
		pattern.Name = strings.TrimSpace(pattern.Name)
		pattern.Pattern = strings.TrimSpace(pattern.Pattern)
		pattern.Category = strings.TrimSpace(pattern.Category)
		if pattern.Name == "" {
			return nil, fmt.Errorf("prompt filter custom pattern name cannot be empty")
		}
		if _, exists := seen[pattern.Name]; exists {
			return nil, fmt.Errorf("duplicate prompt filter custom pattern %q", pattern.Name)
		}
		if pattern.Pattern == "" {
			return nil, fmt.Errorf("prompt filter custom pattern %q cannot be empty", pattern.Name)
		}
		if pattern.Weight <= 0 {
			return nil, fmt.Errorf("prompt filter custom pattern %q weight must be greater than 0", pattern.Name)
		}
		if _, err := regexp.Compile(pattern.Pattern); err != nil {
			return nil, fmt.Errorf("invalid prompt filter custom pattern %q: %w", pattern.Name, err)
		}
		seen[pattern.Name] = struct{}{}
		normalized = append(normalized, pattern)
	}
	return normalized, nil
}

func validatePromptFilterDisabledPatterns(patterns []string) ([]string, error) {
	seen := map[string]struct{}{}
	normalized := make([]string, 0, len(patterns))
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		if _, exists := seen[pattern]; exists {
			continue
		}
		seen[pattern] = struct{}{}
		normalized = append(normalized, pattern)
	}
	return normalized, nil
}

func validatePromptFilterLexiconFiles(files []PromptFilterLexiconFile) ([]PromptFilterLexiconFile, error) {
	seen := map[string]struct{}{}
	normalized := make([]PromptFilterLexiconFile, 0, len(files))
	for _, file := range files {
		file.ID = strings.TrimSpace(file.ID)
		file.Name = strings.TrimSpace(file.Name)
		file.OriginalName = strings.TrimSpace(file.OriginalName)
		file.StoredName = strings.TrimSpace(file.StoredName)
		file.SHA256 = strings.ToLower(strings.TrimSpace(file.SHA256))
		file.Category = strings.TrimSpace(file.Category)
		file.Source = strings.TrimSpace(file.Source)
		if file.Source == "" {
			file.Source = "upload"
		}
		if file.ID == "" {
			return nil, fmt.Errorf("prompt filter lexicon file id cannot be empty")
		}
		if _, exists := seen[file.ID]; exists {
			return nil, fmt.Errorf("duplicate prompt filter lexicon file %q", file.ID)
		}
		if file.Name == "" {
			return nil, fmt.Errorf("prompt filter lexicon file %q name cannot be empty", file.ID)
		}
		if file.StoredName == "" {
			return nil, fmt.Errorf("prompt filter lexicon file %q stored name cannot be empty", file.ID)
		}
		if file.WordCount < 0 {
			return nil, fmt.Errorf("prompt filter lexicon file %q word count cannot be negative", file.ID)
		}
		if file.Weight <= 0 {
			return nil, fmt.Errorf("prompt filter lexicon file %q weight must be greater than 0", file.ID)
		}
		seen[file.ID] = struct{}{}
		normalized = append(normalized, file)
	}
	return normalized, nil
}

func normalizedPromptFilterMode(value string) string {
	value = strings.TrimSpace(value)
	switch value {
	case PromptFilterModeBlock, PromptFilterModeWarn, PromptFilterModeMonitor:
		return value
	default:
		return DefaultPromptFilterMode
	}
}

func normalizedPromptFilterThreshold(value int) int {
	if value <= 0 {
		return DefaultPromptFilterThreshold
	}
	return value
}

func normalizedPromptFilterStrictThreshold(value int, threshold int) int {
	if value <= 0 {
		return DefaultPromptFilterStrictThreshold
	}
	if value < threshold {
		return threshold
	}
	return value
}

func normalizedPromptFilterMaxTextLength(value int) int {
	if value <= 0 {
		return DefaultPromptFilterMaxTextLength
	}
	if value < 1024 {
		return 1024
	}
	if value > 1024*1024 {
		return 1024 * 1024
	}
	return value
}

func normalizedPromptFilterMessage(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return DefaultPromptFilterMessage
	}
	return value
}

func normalizedPromptFilterBlockStatusCode(value int) int {
	if !validPromptFilterBlockStatusCode(value) {
		return DefaultPromptFilterBlockStatusCode
	}
	return value
}

func validPromptFilterBlockStatusCode(value int) bool {
	return value >= 400 && value <= 499 && value != 401
}

func normalizedPromptFilterBlockErrorCode(value string) string {
	normalized, err := validatePromptFilterBlockErrorCode(value)
	if err != nil {
		return DefaultPromptFilterBlockErrorCode
	}
	return normalized
}

func validatePromptFilterBlockErrorCode(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("prompt filter block error code cannot be empty")
	}
	if len(value) > 64 {
		return "", fmt.Errorf("prompt filter block error code cannot exceed 64 characters")
	}
	if !regexp.MustCompile(`^[a-z][a-z0-9_:-]*$`).MatchString(value) {
		return "", fmt.Errorf("prompt filter block error code must start with a lowercase letter and contain only lowercase letters, numbers, underscores, colons, or hyphens")
	}
	return value, nil
}

func normalizedPromptFilterCustomPatterns(patterns []PromptFilterCustomPattern) []PromptFilterCustomPattern {
	normalized, err := validatePromptFilterCustomPatterns(patterns)
	if err != nil {
		return []PromptFilterCustomPattern{}
	}
	return normalized
}

func normalizedPromptFilterDisabledPatterns(patterns []string) []string {
	normalized, err := validatePromptFilterDisabledPatterns(patterns)
	if err != nil {
		return []string{}
	}
	return normalized
}

func normalizedPromptFilterLexiconFiles(files []PromptFilterLexiconFile) []PromptFilterLexiconFile {
	normalized, err := validatePromptFilterLexiconFiles(files)
	if err != nil {
		return []PromptFilterLexiconFile{}
	}
	return normalized
}

func normalizedPromptFilterStringList(items []string) []string {
	seen := map[string]struct{}{}
	normalized := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, exists := seen[item]; exists {
			continue
		}
		seen[item] = struct{}{}
		normalized = append(normalized, item)
	}
	return normalized
}

func normalizedPromptFilterIntList(items []int) []int {
	seen := map[int]struct{}{}
	normalized := make([]int, 0, len(items))
	for _, item := range items {
		if item <= 0 {
			continue
		}
		if _, exists := seen[item]; exists {
			continue
		}
		seen[item] = struct{}{}
		normalized = append(normalized, item)
	}
	return normalized
}

func normalizedPromptFilterReviewBaseURL(value string) string {
	value = strings.TrimRight(strings.TrimSpace(value), "/")
	if value == "" {
		return DefaultPromptFilterReviewBaseURL
	}
	return value
}

func normalizedPromptFilterReviewModel(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return DefaultPromptFilterReviewModel
	}
	return value
}

func normalizedPromptFilterReviewTimeoutSeconds(value int) int {
	if value <= 0 {
		return DefaultPromptFilterReviewTimeoutSeconds
	}
	if value > 60 {
		return 60
	}
	return value
}

func validatePromptFilterReviewBaseURL(value string) (string, error) {
	value = normalizedPromptFilterReviewBaseURL(value)
	parsed, err := url.Parse(value)
	if err != nil {
		return "", err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("prompt filter review base URL must start with http:// or https://")
	}
	return value, nil
}
