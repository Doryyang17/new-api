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
		PromptTokens:     100,
		CompletionTokens: 40,
	}).Error)
	require.NoError(t, LOG_DB.Create(&Log{
		CreatedAt:        dayStart + 120,
		Type:             LogTypeConsume,
		PromptTokens:     25,
		CompletionTokens: 35,
	}).Error)
	require.NoError(t, LOG_DB.Create(&Log{
		CreatedAt:        dayStart + 180,
		Type:             LogTypeError,
		PromptTokens:     999,
		CompletionTokens: 999,
	}).Error)

	usedTokens, err := RefreshSystemDailyUsageSnapshot(dayStart, "UTC", dayStart+300)
	require.NoError(t, err)
	assert.Equal(t, int64(200), usedTokens)

	snapshotTokens, err := GetSystemDailyUsageSnapshotTokens(dayStart, "UTC")
	require.NoError(t, err)
	assert.Equal(t, int64(200), snapshotTokens)
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

	usedTokens, err := RefreshSystemDailyUsageSnapshot(dayStart, "UTC", dayStart+180)
	require.NoError(t, err)
	assert.Equal(t, int64(100), usedTokens)

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
