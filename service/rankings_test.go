package service

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetRankingsSnapshotCacheHitUsesCurrentDailyUsage(t *testing.T) {
	now := time.Now()
	settings := system_setting.GetDailyUsageLimitSettings()
	signature := dailyUsageSettingsSignature(settings)
	currentUsage := baseSystemDailyUsageStatus(now, settings, signature)
	currentUsage.UsedTokens = 42
	cachedUsage := currentUsage
	cachedUsage.UsedTokens = 7

	rankingCacheMu.Lock()
	oldRankingCache := rankingCache
	rankingCache = map[string]rankingCacheItem{
		"today": {
			expiresAt: now.Add(time.Hour),
			data: &RankingsResponse{
				Models:     []RankedModel{{ModelName: "cached-model"}},
				DailyUsage: cachedUsage,
			},
		},
	}
	rankingCacheMu.Unlock()

	systemDailyUsageMu.Lock()
	oldSystemDailyUsageStatus := systemDailyUsageStatus
	oldSystemDailyUsageLoaded := systemDailyUsageLoaded
	systemDailyUsageStatus = currentUsage
	systemDailyUsageLoaded = true
	systemDailyUsageMu.Unlock()

	t.Cleanup(func() {
		rankingCacheMu.Lock()
		rankingCache = oldRankingCache
		rankingCacheMu.Unlock()

		systemDailyUsageMu.Lock()
		systemDailyUsageStatus = oldSystemDailyUsageStatus
		systemDailyUsageLoaded = oldSystemDailyUsageLoaded
		systemDailyUsageMu.Unlock()
	})

	response, err := GetRankingsSnapshot("today")
	require.NoError(t, err)
	require.NotNil(t, response)
	assert.Equal(t, int64(42), response.DailyUsage.UsedTokens)

	rankingCacheMu.Lock()
	storedUsage := rankingCache["today"].data.DailyUsage
	rankingCacheMu.Unlock()
	assert.Equal(t, int64(7), storedUsage.UsedTokens)
}
