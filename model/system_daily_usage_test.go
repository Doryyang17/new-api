package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRefreshSystemDailyUsageSnapshotFromLogs(t *testing.T) {
	truncateTables(t)
	dayStart := time.Date(2026, 7, 6, 0, 0, 0, 0, time.UTC).Unix()

	require.NoError(t, LOG_DB.Create(&Log{
		CreatedAt:        dayStart + 60,
		Type:             LogTypeConsume,
		ModelName:        "gpt-4",
		PromptTokens:     100,
		CompletionTokens: 40,
	}).Error)
	require.NoError(t, LOG_DB.Create(&Log{
		CreatedAt:        dayStart + 120,
		Type:             LogTypeConsume,
		ModelName:        "claude-3",
		PromptTokens:     25,
		CompletionTokens: 35,
	}).Error)
	require.NoError(t, LOG_DB.Create(&Log{
		CreatedAt:        dayStart + 150,
		Type:             LogTypeConsume,
		ModelName:        "gpt-4-gizmo-*",
		PromptTokens:     30,
		CompletionTokens: 20,
	}).Error)
	require.NoError(t, LOG_DB.Create(&Log{
		CreatedAt:        dayStart + 180,
		Type:             LogTypeError,
		PromptTokens:     999,
		CompletionTokens: 999,
	}).Error)

	allTokens, err := GetSystemDailyUsageLogTokens(dayStart, "UTC", nil)
	require.NoError(t, err)
	assert.Equal(t, int64(250), allTokens)

	gptTokens, err := GetSystemDailyUsageLogTokens(dayStart, "UTC", []string{"gpt-4"})
	require.NoError(t, err)
	assert.Equal(t, int64(140), gptTokens)

	selectedTokens, err := GetSystemDailyUsageLogTokens(dayStart, "UTC", []string{"gpt-4", "claude-3"})
	require.NoError(t, err)
	assert.Equal(t, int64(200), selectedTokens)

	gizmoTokens, err := GetSystemDailyUsageLogTokens(dayStart, "UTC", []string{"gpt-4-gizmo-abc123"})
	require.NoError(t, err)
	assert.Equal(t, int64(50), gizmoTokens)

	emptyScopeTokens, err := GetSystemDailyUsageLogTokens(dayStart, "UTC", []string{})
	require.NoError(t, err)
	assert.Equal(t, int64(0), emptyScopeTokens)

	modelTotals, err := GetSystemDailyUsageLogTokensByModel(dayStart, "UTC", []string{"gpt-4", "claude-3", "gpt-4-gizmo-abc123"})
	require.NoError(t, err)
	assert.Equal(t, map[string]int64{
		"gpt-4":              140,
		"claude-3":           60,
		"gpt-4-gizmo-abc123": 50,
	}, modelTotals)

	snapshot, err := RefreshSystemDailyUsageSnapshot(dayStart, "UTC", dayStart+300, []string{"gpt-4"})
	require.NoError(t, err)
	assert.Equal(t, int64(250), snapshot.UsedTokens)
	assert.Equal(t, map[string]int64{"gpt-4": 140}, snapshot.ModelUsedTokens)

	snapshotTokens, err := GetSystemDailyUsageSnapshotTokens(dayStart, "UTC")
	require.NoError(t, err)
	assert.Equal(t, int64(250), snapshotTokens)
}

func TestNormalizeUsageModelName(t *testing.T) {
	assert.Equal(t, "gpt-4-gizmo-*", NormalizeUsageModelName("gpt-4-gizmo-abc123"))
	assert.Equal(t, "gpt-4o-gizmo-*", NormalizeUsageModelName("gpt-4o-gizmo-abc123"))
	assert.Equal(t, "gpt-4", NormalizeUsageModelName("gpt-4"))
	assert.True(t, UsageModelMatches("gpt-4-gizmo-abc123", "gpt-4-gizmo-*"))
	assert.True(t, UsageModelMatches("gemini-2.5-flash-thinking-*", "gemini-2.5-flash-thinking-1024"))
	assert.False(t, UsageModelMatches("GLM-5.2", "GLM-5.1"))
}

func TestRefreshSystemDailyUsageSnapshotOverwritesOldValue(t *testing.T) {
	truncateTables(t)
	dayStart := time.Date(2026, 7, 6, 0, 0, 0, 0, time.UTC).Unix()

	require.NoError(t, SaveSystemDailyUsageSnapshot(dayStart, "UTC", 300, dayStart+60))
	require.NoError(t, LOG_DB.Create(&Log{
		CreatedAt:        dayStart + 120,
		Type:             LogTypeConsume,
		PromptTokens:     80,
		CompletionTokens: 20,
	}).Error)

	snapshot, err := RefreshSystemDailyUsageSnapshot(dayStart, "UTC", dayStart+180, nil)
	require.NoError(t, err)
	assert.Equal(t, int64(100), snapshot.UsedTokens)

	snapshotTokens, err := GetSystemDailyUsageSnapshotTokens(dayStart, "UTC")
	require.NoError(t, err)
	assert.Equal(t, int64(100), snapshotTokens)
}

func TestRecordConsumeLogDoesNotUpdateSnapshotWhenConsumeLogsDisabled(t *testing.T) {
	truncateTables(t)
	originalLogConsumeEnabled := common.LogConsumeEnabled
	common.LogConsumeEnabled = false
	t.Cleanup(func() {
		common.LogConsumeEnabled = originalLogConsumeEnabled
	})

	RecordConsumeLog(&gin.Context{}, 1, RecordConsumeLogParams{
		PromptTokens:     100,
		CompletionTokens: 40,
	})

	var counters []SystemDailyUsageCounter
	require.NoError(t, DB.Find(&counters).Error)
	require.Empty(t, counters)

	var logs []Log
	require.NoError(t, DB.Find(&logs).Error)
	require.Empty(t, logs)
}

func TestSumUsedQuotaExcludesUsageOnlyLogsFromRPM(t *testing.T) {
	truncateTables(t)
	now := time.Now().Unix()

	require.NoError(t, LOG_DB.Create(&Log{
		CreatedAt:        now,
		Type:             LogTypeConsume,
		Quota:            5,
		PromptTokens:     10,
		CompletionTokens: 1,
		Other:            "{}",
	}).Error)
	require.NoError(t, LOG_DB.Create(&Log{
		CreatedAt:        now,
		Type:             LogTypeConsume,
		Quota:            0,
		PromptTokens:     20,
		CompletionTokens: 2,
		Other:            `{"usage_only":true}`,
	}).Error)

	stat, err := SumUsedQuota(LogTypeConsume, now-10, now+10, "", "", "", 0, "")
	require.NoError(t, err)
	assert.Equal(t, 5, stat.Quota)
	assert.Equal(t, 1, stat.Rpm)
	assert.Equal(t, 33, stat.Tpm)
}
