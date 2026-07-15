package service

import (
	"context"
	"crypto/sha256"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func requestRiskTestSettings(mode string) system_setting.RequestRiskSettings {
	return system_setting.RequestRiskSettings{
		Enabled:               true,
		Mode:                  mode,
		LogMatches:            true,
		MediumCooldownSeconds: 10,
		TokenBlockSeconds:     300,
		UserBlockSeconds:      120,
		IPBlockSeconds:        60,
	}
}

func withRequestRiskMemoryStore(t *testing.T) {
	t.Helper()
	oldRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	resetRequestRiskMemoryForTest()
	t.Cleanup(func() {
		common.RedisEnabled = oldRedisEnabled
		resetRequestRiskMemoryForTest()
	})
}

func TestRequestRiskSingleShortPromptDoesNotBlock(t *testing.T) {
	withRequestRiskMemoryStore(t)
	verdict := EvaluateRequestRisk(context.Background(), RequestRiskInput{
		UserID: 1, TokenID: 2, Model: "gpt-5", Text: "你好",
	}, requestRiskTestSettings(system_setting.RequestRiskModeEnforce))

	assert.Equal(t, RequestRiskLevelLow, verdict.Level)
	assert.Equal(t, 1, verdict.Score)
	assert.False(t, verdict.Blocked)
}

func TestRequestRiskNormalShortQuestionHasNoContentRisk(t *testing.T) {
	withRequestRiskMemoryStore(t)
	verdict := EvaluateRequestRisk(context.Background(), RequestRiskInput{
		UserID: 1, TokenID: 2, Model: "gpt-5", Text: "1+1等于几？",
	}, requestRiskTestSettings(system_setting.RequestRiskModeEnforce))

	assert.Zero(t, verdict.Score)
	assert.False(t, verdict.Blocked)
}

func TestExtractRequestRiskTextUsesLatestUserMessage(t *testing.T) {
	longSystemPrompt := strings.Repeat("system rules ", 80)
	body := []byte(`{"model":"gpt-5","messages":[{"role":"system","content":"` + longSystemPrompt + `"},{"role":"user","content":"earlier question"},{"role":"assistant","content":"answer"},{"role":"user","content":"hi"}]}`)

	text := ExtractRequestRiskText(body, "/v1/chat/completions", 1024)

	assert.Equal(t, "hi", text)
}

func TestRequestRiskFingerprintKeepsDistinctLongTextTails(t *testing.T) {
	prefix := strings.Repeat("shared system prefix ", 80)
	first := requestRiskFingerprint(normalizeRequestRiskText(prefix + "first user question"))
	second := requestRiskFingerprint(normalizeRequestRiskText(prefix + "second user question"))

	assert.NotEqual(t, first, second)
}

func TestRequestRiskModelSweepTriggersMediumCooldown(t *testing.T) {
	withRequestRiskMemoryStore(t)
	settings := requestRiskTestSettings(system_setting.RequestRiskModeEnforce)
	var verdict RequestRiskVerdict
	for _, model := range []string{"gpt-a", "gpt-b", "gpt-c", "gpt-d"} {
		verdict = EvaluateRequestRisk(context.Background(), RequestRiskInput{
			UserID: 1, TokenID: 2, Model: model, Text: "hi",
		}, settings)
	}

	assert.Equal(t, RequestRiskLevelMedium, verdict.Level)
	assert.Equal(t, 4, verdict.Score)
	assert.True(t, verdict.Blocked)
	assert.Contains(t, verdict.Factors, "model_sweep")

	blocked := EvaluateRequestRisk(context.Background(), RequestRiskInput{
		UserID: 1, TokenID: 2, Model: "gpt-e", Text: "hi",
	}, settings)
	assert.True(t, blocked.Blocked)
	assert.True(t, blocked.ExistingBlock)
}

func TestRequestRiskModelSweepCombinesTokensForSameUser(t *testing.T) {
	withRequestRiskMemoryStore(t)
	settings := requestRiskTestSettings(system_setting.RequestRiskModeEnforce)
	models := []string{"gpt-a", "gpt-b", "gpt-c", "gpt-d"}
	var verdict RequestRiskVerdict
	for index, model := range models {
		verdict = EvaluateRequestRisk(context.Background(), RequestRiskInput{
			UserID: 7, TokenID: 20 + index, Model: model, Text: "hello",
		}, settings)
	}

	assert.Equal(t, RequestRiskLevelMedium, verdict.Level)
	assert.True(t, verdict.Blocked)
	assert.Contains(t, verdict.Factors, "model_sweep")
}

func TestRequestRiskModelSweepAcrossDifferentMeaninglessPrompts(t *testing.T) {
	withRequestRiskMemoryStore(t)
	settings := requestRiskTestSettings(system_setting.RequestRiskModeEnforce)
	models := []string{"gpt-a", "gpt-b", "gpt-c", "gpt-d"}
	prompts := []string{"hi", "hello", "test", "ping"}
	var verdict RequestRiskVerdict
	for index, model := range models {
		verdict = EvaluateRequestRisk(context.Background(), RequestRiskInput{
			UserID: 8, TokenID: 30, Model: model, Text: prompts[index],
		}, settings)
	}

	assert.Equal(t, RequestRiskLevelMedium, verdict.Level)
	assert.True(t, verdict.Blocked)
	assert.Contains(t, verdict.Factors, "model_sweep")
	assert.Contains(t, verdict.Factors, "meaningless_exact_match")
}

func TestRequestRiskModelCardinalitySaturatesAtDetectionThreshold(t *testing.T) {
	withRequestRiskMemoryStore(t)
	settings := requestRiskTestSettings(system_setting.RequestRiskModeObserve)
	var verdict RequestRiskVerdict
	for index := 0; index < requestRiskModelSetLimit+20; index++ {
		verdict = EvaluateRequestRisk(context.Background(), RequestRiskInput{
			UserID:  81,
			TokenID: 301,
			Model:   strings.Repeat("model-", 1000) + strconv.Itoa(index),
			Text:    "解释这个问题",
		}, settings)
	}

	assert.Equal(t, int64(requestRiskModelSetLimit), verdict.Metrics.DistinctModels60s)

	requestRiskMemory.Lock()
	defer requestRiskMemory.Unlock()
	for key, entry := range requestRiskMemory.items {
		if !strings.Contains(key, ":models:") {
			continue
		}
		assert.LessOrEqual(t, len(entry.Values), requestRiskModelSetLimit)
		for value := range entry.Values {
			assert.Len(t, value, sha256.Size*2)
		}
	}
}

func TestRequestRiskModelSweepAloneDoesNotBlock(t *testing.T) {
	withRequestRiskMemoryStore(t)
	settings := requestRiskTestSettings(system_setting.RequestRiskModeEnforce)
	models := []string{"gpt-a", "gpt-b", "gpt-c", "gpt-d"}
	prompts := []string{"解释勾股定理", "写一个排序示例", "总结这段文字", "给出旅行清单"}
	var verdict RequestRiskVerdict
	for index, model := range models {
		verdict = EvaluateRequestRisk(context.Background(), RequestRiskInput{
			UserID: 9, TokenID: 31, Model: model, Text: prompts[index],
		}, settings)
	}

	assert.Equal(t, RequestRiskLevelLow, verdict.Level)
	assert.Equal(t, 2, verdict.Score)
	assert.False(t, verdict.Blocked)
	assert.Contains(t, verdict.Factors, "model_sweep")
}

func TestRequestRiskObserveModeNeverBlocks(t *testing.T) {
	withRequestRiskMemoryStore(t)
	settings := requestRiskTestSettings(system_setting.RequestRiskModeObserve)
	var verdict RequestRiskVerdict
	for _, model := range []string{"gpt-a", "gpt-b", "gpt-c", "gpt-d"} {
		verdict = EvaluateRequestRisk(context.Background(), RequestRiskInput{
			UserID: 1, TokenID: 2, Model: model, Text: "test",
		}, settings)
	}

	assert.True(t, verdict.Observed)
	assert.False(t, verdict.Blocked)
	assert.Equal(t, RequestRiskLevelMedium, verdict.Level)
}

func TestRequestRiskFastFailureRetryAddsRisk(t *testing.T) {
	withRequestRiskMemoryStore(t)
	settings := requestRiskTestSettings(system_setting.RequestRiskModeEnforce)
	input := RequestRiskInput{UserID: 1, TokenID: 2, Model: "gpt-5", Text: "ping"}
	first := EvaluateRequestRisk(context.Background(), input, settings)
	require.NotEmpty(t, first.Fingerprint)
	RecordRequestRiskFailure(context.Background(), input, first.Fingerprint)

	second := EvaluateRequestRisk(context.Background(), input, settings)
	assert.Equal(t, RequestRiskLevelMedium, second.Level)
	assert.True(t, second.Blocked)
	assert.Contains(t, second.Factors, "fast_failure_retry")
}

func TestRequestRiskMemoryPruningIsRateLimited(t *testing.T) {
	withRequestRiskMemoryStore(t)
	now := time.Now()

	requestRiskMemory.Lock()
	requestRiskMemory.items["expired:first"] = requestRiskMemoryEntry{ExpiresAt: now.Add(-time.Second)}
	pruneRequestRiskMemory(now)
	_, firstExists := requestRiskMemory.items["expired:first"]
	requestRiskMemory.items["expired:second"] = requestRiskMemoryEntry{ExpiresAt: now.Add(-time.Second)}
	pruneRequestRiskMemory(now.Add(time.Second))
	_, secondExistsBeforeInterval := requestRiskMemory.items["expired:second"]
	pruneRequestRiskMemory(now.Add(requestRiskPruneInterval))
	_, secondExistsAfterInterval := requestRiskMemory.items["expired:second"]
	requestRiskMemory.Unlock()

	assert.False(t, firstExists)
	assert.True(t, secondExistsBeforeInterval)
	assert.False(t, secondExistsAfterInterval)
}
