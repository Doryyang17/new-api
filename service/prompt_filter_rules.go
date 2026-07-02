package service

import (
	"regexp"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

type PromptFilterRule struct {
	Name     string `json:"name"`
	Pattern  string `json:"pattern"`
	Weight   int    `json:"weight"`
	Category string `json:"category,omitempty"`
	Strict   bool   `json:"strict,omitempty"`
	Enabled  bool   `json:"enabled"`
	Builtin  bool   `json:"builtin"`
}

type PromptFilterRules struct {
	BuiltinPatterns  []PromptFilterRule                         `json:"builtin_patterns"`
	CustomPatterns   []system_setting.PromptFilterCustomPattern `json:"custom_patterns"`
	DisabledPatterns []string                                   `json:"disabled_patterns"`
}

var promptFilterSensitiveRedactionPatterns = []struct {
	re          *regexp.Regexp
	replacement string
}{
	{regexp.MustCompile(`(?i)\b(authorization\s*[:=]\s*(?:bearer|basic)\s+)[^\s,;]+`), `${1}[REDACTED]`},
	{regexp.MustCompile(`(?i)(["']?\b(?:password|passwd|pwd|token|api[_-]?key|secret|client[_-]?secret|access[_-]?token|refresh[_-]?token|session[_-]?id)\b["']?\s*[:=]\s*["']?)[^"',\s}]+`), `${1}[REDACTED]`},
	{regexp.MustCompile(`(?i)\b(cookie\s*[:=]\s*)[^\n]+`), `${1}[REDACTED]`},
	{regexp.MustCompile(`\bsk-[A-Za-z0-9][A-Za-z0-9_-]{7,}\b`), `[REDACTED_API_KEY]`},
	{regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b`), `[REDACTED_JWT]`},
	{regexp.MustCompile(`\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}\b`), `[REDACTED_EMAIL]`},
}

func ListPromptFilterRules() PromptFilterRules {
	settings := system_setting.GetPromptFilterSettings()
	disabled := map[string]bool{}
	for _, name := range settings.DisabledPatterns {
		disabled[strings.ToLower(strings.TrimSpace(name))] = true
	}

	builtin := make([]PromptFilterRule, 0, len(promptFilterPatternConfigs))
	for _, pattern := range promptFilterPatternConfigs {
		name := strings.TrimSpace(pattern.name)
		builtin = append(builtin, PromptFilterRule{
			Name:     name,
			Pattern:  pattern.pattern,
			Weight:   pattern.weight,
			Category: pattern.category,
			Strict:   pattern.strict,
			Enabled:  !disabled[strings.ToLower(name)],
			Builtin:  true,
		})
	}
	return PromptFilterRules{
		BuiltinPatterns:  builtin,
		CustomPatterns:   settings.CustomPatterns,
		DisabledPatterns: settings.DisabledPatterns,
	}
}

func RedactPromptFilterSensitive(text string) string {
	if text == "" {
		return ""
	}
	redacted := text
	for _, pattern := range promptFilterSensitiveRedactionPatterns {
		redacted = pattern.re.ReplaceAllString(redacted, pattern.replacement)
	}
	return redacted
}

func RedactedPromptFilterPreview(text string, maxRunes int) string {
	return promptFilterPreview(RedactPromptFilterSensitive(text), maxRunes)
}

func PromptFilterMatchesJSON(matches []PromptFilterMatch) string {
	if len(matches) == 0 {
		return "[]"
	}
	data, err := common.Marshal(matches)
	if err != nil {
		return "[]"
	}
	return string(data)
}

func TestPromptFilterPattern(pattern string, text string) (bool, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return false, err
	}
	return re.MatchString(text), nil
}
