package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/require"
)

// TestFormatUserLogsStripsQuotaSaturation verifies the admin-only quota
// saturation marker (nested under other.admin_info) is removed for non-admin
// log views, since formatUserLogs strips the whole admin_info object.
func TestFormatUserLogsStripsQuotaSaturation(t *testing.T) {
	other := common.MapToJsonStr(map[string]interface{}{
		"model_price": 0.004,
		"admin_info": map[string]interface{}{
			"quota_saturation": map[string]interface{}{
				"op":      "QuotaFromDecimal",
				"kind":    "overflow",
				"clamped": common.MaxQuota,
			},
		},
	})
	logs := []*Log{{Other: other}}

	formatUserLogs(logs, 0)

	parsed, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	_, hasAdminInfo := parsed["admin_info"]
	require.False(t, hasAdminInfo, "admin_info (and nested quota_saturation) must be stripped for non-admin views")
	// Non-admin billing fields remain visible.
	require.Contains(t, parsed, "model_price")
}

func TestFormatUserLogsStripsPromptFilterFullText(t *testing.T) {
	other := common.MapToJsonStr(map[string]interface{}{
		"reject_reason":           "prompt_filter",
		"text_preview":            "[REDACTED] preview",
		"full_text":               "legacy full prompt secret",
		"prompt_filter_full_text": "legacy admin prompt secret",
		"admin_info": map[string]interface{}{
			"prompt_filter_full_text": "admin prompt secret",
		},
	})
	logs := []*Log{{Other: other}}

	formatUserLogs(logs, 0)

	parsed, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	require.NotContains(t, parsed, "full_text")
	require.NotContains(t, parsed, "prompt_filter_full_text")
	require.NotContains(t, parsed, "admin_info")
	require.Equal(t, "[REDACTED] preview", parsed["text_preview"])
	require.NotContains(t, logs[0].Other, "secret")
}

func TestPromptFilterLogFromRawReadsAdminInfoFullText(t *testing.T) {
	other := common.MapToJsonStr(map[string]interface{}{
		"reject_reason": "prompt_filter",
		"text_preview":  "[REDACTED] preview",
		"full_text":     "legacy redacted preview",
		"admin_info": map[string]interface{}{
			"prompt_filter_full_text": "admin original prompt",
		},
	})

	log := promptFilterLogFromRaw(&Log{Other: other})

	require.NotNil(t, log)
	require.Equal(t, "admin original prompt", log.FullText)
	require.Equal(t, "[REDACTED] preview", log.TextPreview)
}
