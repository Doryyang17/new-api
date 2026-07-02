package system_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidatePromptFilterOption(t *testing.T) {
	require.NoError(t, ValidatePromptFilterOption("prompt_filter_setting.mode", PromptFilterModeBlock))
	require.NoError(t, ValidatePromptFilterOption("prompt_filter_setting.threshold", "50"))
	require.NoError(t, ValidatePromptFilterOption("prompt_filter_setting.strict_threshold", "90"))
	require.NoError(t, ValidatePromptFilterOption("prompt_filter_setting.max_text_length", "8192"))
	require.NoError(t, ValidatePromptFilterOption("prompt_filter_setting.message", DefaultPromptFilterMessage))
	require.NoError(t, ValidatePromptFilterOption("prompt_filter_setting.block_status_code", "460"))
	require.NoError(t, ValidatePromptFilterOption("prompt_filter_setting.block_error_code", "custom_prompt_policy"))
	require.NoError(t, ValidatePromptFilterOption("prompt_filter_setting.custom_patterns", `[{"name":"custom","pattern":"(?i)secret","weight":80}]`))
	require.NoError(t, ValidatePromptFilterOption("prompt_filter_setting.disabled_patterns", `["credential_theft"]`))
	require.NoError(t, ValidatePromptFilterOption("prompt_filter_setting.lexicon_files", `[{"id":"abc","name":"local","original_name":"local.txt","stored_name":"abc_local.txt","sha256":"abc","size":12,"word_count":1,"weight":100,"strict":true,"enabled":true,"uploaded_at":1}]`))

	assert.Error(t, ValidatePromptFilterOption("prompt_filter_setting.mode", "invalid"))
	assert.Error(t, ValidatePromptFilterOption("prompt_filter_setting.threshold", "0"))
	assert.Error(t, ValidatePromptFilterOption("prompt_filter_setting.strict_threshold", "-1"))
	assert.Error(t, ValidatePromptFilterOption("prompt_filter_setting.max_text_length", "0"))
	assert.Error(t, ValidatePromptFilterOption("prompt_filter_setting.max_text_length", "1048577"))
	assert.Error(t, ValidatePromptFilterOption("prompt_filter_setting.message", ""))
	assert.Error(t, ValidatePromptFilterOption("prompt_filter_setting.block_status_code", "401"))
	assert.Error(t, ValidatePromptFilterOption("prompt_filter_setting.block_status_code", "500"))
	assert.Error(t, ValidatePromptFilterOption("prompt_filter_setting.block_error_code", "PromptBlocked"))
	assert.Error(t, ValidatePromptFilterOption("prompt_filter_setting.block_error_code", ""))
	assert.Error(t, ValidatePromptFilterOption("prompt_filter_setting.custom_patterns", `[{"name":"","pattern":"x","weight":1}]`))
	assert.Error(t, ValidatePromptFilterOption("prompt_filter_setting.custom_patterns", `[{"name":"bad","pattern":"(","weight":1}]`))
	assert.Error(t, ValidatePromptFilterOption("prompt_filter_setting.custom_patterns", `[{"name":"bad","pattern":"x","weight":0}]`))
	assert.Error(t, ValidatePromptFilterOption("prompt_filter_setting.disabled_patterns", `{"name":"credential_theft"}`))
	assert.Error(t, ValidatePromptFilterOption("prompt_filter_setting.lexicon_files", `[{"id":"","name":"bad","stored_name":"bad.txt","weight":100}]`))
	assert.Error(t, ValidatePromptFilterOption("prompt_filter_setting.lexicon_files", `[{"id":"bad","name":"bad","stored_name":"bad.txt","weight":0}]`))
}

func TestGetPromptFilterSettingsNormalizesValues(t *testing.T) {
	oldSettings := promptFilterSettings
	enabled := true
	promptFilterSettings = PromptFilterSettings{
		Mode:            "",
		Threshold:       0,
		StrictThreshold: 1,
		MaxTextLength:   10,
		Message:         "",
		BlockStatusCode: 200,
		BlockErrorCode:  "PromptBlocked",
		CustomPatterns: []PromptFilterCustomPattern{
			{Name: " custom ", Pattern: "(?i)test", Weight: 20, Enabled: &enabled},
		},
		DisabledPatterns: []string{" credential_theft ", "", "credential_theft"},
		LexiconFiles: []PromptFilterLexiconFile{
			{ID: " lex ", Name: " local ", OriginalName: " local.txt ", StoredName: " lex_local.txt ", WordCount: 1, Weight: 100, Enabled: true},
		},
	}
	t.Cleanup(func() {
		promptFilterSettings = oldSettings
	})

	settings := GetPromptFilterSettings()

	assert.Equal(t, DefaultPromptFilterMode, settings.Mode)
	assert.Equal(t, DefaultPromptFilterThreshold, settings.Threshold)
	assert.Equal(t, DefaultPromptFilterThreshold, settings.StrictThreshold)
	assert.Equal(t, 1024, settings.MaxTextLength)
	assert.Equal(t, DefaultPromptFilterMessage, settings.Message)
	assert.Equal(t, DefaultPromptFilterBlockStatusCode, settings.BlockStatusCode)
	assert.Equal(t, DefaultPromptFilterBlockErrorCode, settings.BlockErrorCode)
	require.Len(t, settings.CustomPatterns, 1)
	assert.Equal(t, "custom", settings.CustomPatterns[0].Name)
	assert.Equal(t, []string{"credential_theft"}, settings.DisabledPatterns)
	require.Len(t, settings.LexiconFiles, 1)
	assert.Equal(t, "lex", settings.LexiconFiles[0].ID)
	assert.Equal(t, "local", settings.LexiconFiles[0].Name)
}
