package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestIncrementSystemDailyUsageCounter(t *testing.T) {
	truncateTables(t)

	require.NoError(t, IncrementSystemDailyUsageCounter(1000, "UTC", 120, 1100))
	require.NoError(t, IncrementSystemDailyUsageCounter(1000, "UTC", 80, 1200))

	usedTokens, err := GetSystemDailyUsageTokens(1000, "UTC")
	require.NoError(t, err)
	require.Equal(t, int64(200), usedTokens)
}

func TestRecordConsumeLogUpdatesSystemDailyUsageWhenConsumeLogsDisabled(t *testing.T) {
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
	require.Len(t, counters, 1)
	require.Equal(t, int64(140), counters[0].UsedTokens)

	var logs []Log
	require.NoError(t, DB.Find(&logs).Error)
	require.Empty(t, logs)
}
