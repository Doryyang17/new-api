package service

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func requestConcurrencyTestSettings(mode string, userLimit int, tokenLimit int) system_setting.RequestRiskSettings {
	return system_setting.RequestRiskSettings{
		Enabled:               true,
		Mode:                  mode,
		UserConcurrencyLimit:  userLimit,
		TokenConcurrencyLimit: tokenLimit,
	}
}

func withRequestConcurrencyMemoryStore(t *testing.T) {
	t.Helper()
	oldRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	resetRequestConcurrencyMemoryForTest()
	t.Cleanup(func() {
		common.RedisEnabled = oldRedisEnabled
		resetRequestConcurrencyMemoryForTest()
	})
}

func TestRequestConcurrencyTokenLimitBlocksAndReleaseRestoresCapacity(t *testing.T) {
	withRequestConcurrencyMemoryStore(t)
	settings := requestConcurrencyTestSettings(system_setting.RequestRiskModeEnforce, 8, 2)
	input := RequestRiskInput{UserID: 1, TokenID: 11}

	first, firstVerdict := AcquireRequestConcurrency(context.Background(), input, settings)
	second, secondVerdict := AcquireRequestConcurrency(context.Background(), input, settings)
	blocked, blockedVerdict := AcquireRequestConcurrency(context.Background(), input, settings)

	require.NotNil(t, first)
	require.NotNil(t, second)
	assert.True(t, firstVerdict.Allowed)
	assert.True(t, secondVerdict.Allowed)
	assert.Nil(t, blocked)
	assert.False(t, blockedVerdict.Allowed)
	assert.True(t, blockedVerdict.TokenExceeded)
	assert.Contains(t, blockedVerdict.Factors, "token_concurrency_limit")

	ReleaseRequestConcurrency(first)
	replacement, replacementVerdict := AcquireRequestConcurrency(context.Background(), input, settings)
	require.NotNil(t, replacement)
	assert.True(t, replacementVerdict.Allowed)

	ReleaseRequestConcurrency(second)
	ReleaseRequestConcurrency(replacement)
}

func TestRequestConcurrencyUserLimitCombinesDifferentTokens(t *testing.T) {
	withRequestConcurrencyMemoryStore(t)
	settings := requestConcurrencyTestSettings(system_setting.RequestRiskModeEnforce, 2, 2)

	first, _ := AcquireRequestConcurrency(context.Background(), RequestRiskInput{UserID: 2, TokenID: 21}, settings)
	second, _ := AcquireRequestConcurrency(context.Background(), RequestRiskInput{UserID: 2, TokenID: 22}, settings)
	third, verdict := AcquireRequestConcurrency(context.Background(), RequestRiskInput{UserID: 2, TokenID: 23}, settings)

	require.NotNil(t, first)
	require.NotNil(t, second)
	assert.Nil(t, third)
	assert.False(t, verdict.Allowed)
	assert.True(t, verdict.UserExceeded)
	assert.False(t, verdict.TokenExceeded)
	assert.Equal(t, requestRiskUserScope(2), verdict.ScopeKey)

	ReleaseRequestConcurrency(first)
	ReleaseRequestConcurrency(second)
}

func TestRequestConcurrencyObserveModeRecordsWithoutBlocking(t *testing.T) {
	withRequestConcurrencyMemoryStore(t)
	settings := requestConcurrencyTestSettings(system_setting.RequestRiskModeObserve, 1, 1)
	input := RequestRiskInput{UserID: 3, TokenID: 31}

	first, _ := AcquireRequestConcurrency(context.Background(), input, settings)
	observed, verdict := AcquireRequestConcurrency(context.Background(), input, settings)

	require.NotNil(t, first)
	require.NotNil(t, observed)
	assert.True(t, verdict.Allowed)
	assert.True(t, verdict.Exceeded)
	assert.True(t, verdict.Observed)
	assert.Equal(t, int64(2), verdict.UserCount)
	assert.Equal(t, int64(2), verdict.TokenCount)

	ReleaseRequestConcurrency(first)
	ReleaseRequestConcurrency(observed)
}

func TestRequestConcurrencyZeroLimitsDisableLeases(t *testing.T) {
	withRequestConcurrencyMemoryStore(t)
	settings := requestConcurrencyTestSettings(system_setting.RequestRiskModeEnforce, 0, 0)

	lease, verdict := AcquireRequestConcurrency(context.Background(), RequestRiskInput{UserID: 4, TokenID: 41}, settings)

	assert.Nil(t, lease)
	assert.True(t, verdict.Allowed)
	assert.False(t, verdict.Exceeded)
}

func TestRequestConcurrencyLeaseTTLTracksStreamingTimeoutWithinBounds(t *testing.T) {
	oldStreamingTimeout := constant.StreamingTimeout
	t.Cleanup(func() { constant.StreamingTimeout = oldStreamingTimeout })

	constant.StreamingTimeout = 30
	assert.Equal(t, 10*time.Minute, requestConcurrencyLeaseTTL())

	constant.StreamingTimeout = 900
	assert.Equal(t, 30*time.Minute, requestConcurrencyLeaseTTL())

	constant.StreamingTimeout = 100000
	assert.Equal(t, 24*time.Hour, requestConcurrencyLeaseTTL())
}

func TestRequestConcurrencyHeartbeatIntervalRenewsBeforeExpiry(t *testing.T) {
	assert.Equal(t, 200*time.Second, requestConcurrencyHeartbeatInterval(10*time.Minute))
	assert.Equal(t, time.Second, requestConcurrencyHeartbeatInterval(2*time.Second))
}
