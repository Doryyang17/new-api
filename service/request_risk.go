package service

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/go-redis/redis/v8"
	"github.com/tidwall/gjson"
)

const (
	RequestRiskLevelLow    = "low"
	RequestRiskLevelMedium = "medium"
	RequestRiskLevelHigh   = "high"

	requestRiskMemoryMaxItems = 50000
	requestRiskBodyReadLimit  = 1 << 20
	requestRiskTextRuneLimit  = 1024
	requestRiskPruneInterval  = 5 * time.Second
	requestRiskLogCooldown    = 30 * time.Second
	requestRiskFailureWindow  = 30 * time.Second
	requestRiskFastRetryTTL   = 3 * time.Second
	requestRiskModelSetLimit  = 8
)

//go:embed lua/request_risk_model_cardinality.lua
var requestRiskModelCardinalityLua string

var requestRiskMeaninglessTexts = map[string]struct{}{
	"hello":       {},
	"hey":         {},
	"hi":          {},
	"ping":        {},
	"test":        {},
	"testing":     {},
	"who are you": {},
	"你是谁":         {},
	"你好":          {},
	"在吗":          {},
	"您好":          {},
}

type RequestRiskInput struct {
	UserID                       int
	TokenID                      int
	ClientIP                     string
	Model                        string
	Text                         string
	ExtractedText                string
	FullRequest                  string
	FullRequestUnavailableReason string
}

type RequestRiskMetrics struct {
	RequestCount10s   int64 `json:"request_count_10s"`
	RequestCount60s   int64 `json:"request_count_60s"`
	IPRequestCount60s int64 `json:"ip_request_count_60s"`
	RepeatCount60s    int64 `json:"repeat_count_60s"`
	DistinctModels60s int64 `json:"distinct_models_60s"`
	FailureCount30s   int64 `json:"failure_count_30s"`
}

type RequestRiskVerdict struct {
	Level           string
	Score           int
	Factors         []string
	MatchedKeywords []string
	Metrics         RequestRiskMetrics
	RetryAfter      time.Duration
	Blocked         bool
	Observed        bool
	ExistingBlock   bool
	Fingerprint     string
	ScopeKey        string
}

type requestRiskSnapshot struct {
	UserCount10s     int64
	TokenCount10s    int64
	UserCount60s     int64
	TokenCount60s    int64
	IPCount60s       int64
	RepeatCount60s   int64
	DistinctModels60 int64
	FailureCount30s  int64
	FastFailureRetry bool
}

type requestRiskMemoryEntry struct {
	Count     int64
	Values    map[string]struct{}
	Value     string
	ExpiresAt time.Time
}

type requestRiskBlockSpec struct {
	Key      string
	Level    string
	Duration time.Duration
}

var requestRiskMemory = struct {
	sync.Mutex
	items      map[string]requestRiskMemoryEntry
	lastPruned time.Time
}{
	items: make(map[string]requestRiskMemoryEntry),
}

func EvaluateRequestRisk(ctx context.Context, input RequestRiskInput, settings system_setting.RequestRiskSettings) RequestRiskVerdict {
	if verdict, found := GetActiveRequestRiskBlockVerdict(ctx, input, settings); found {
		return verdict
	}
	return EvaluateRequestRiskBehavior(ctx, input, settings)
}

func GetActiveRequestRiskBlockVerdict(ctx context.Context, input RequestRiskInput, settings system_setting.RequestRiskSettings) (RequestRiskVerdict, bool) {
	input.Model = strings.TrimSpace(input.Model)
	normalizedText := normalizeRequestRiskText(input.Text)
	fingerprint := requestRiskFingerprint(normalizedText)
	scopeKey := requestRiskPrimaryScope(input)

	if settings.Mode != system_setting.RequestRiskModeEnforce {
		return RequestRiskVerdict{}, false
	}
	level, retryAfter, found := activeRequestRiskBlock(ctx, input)
	if !found {
		return RequestRiskVerdict{}, false
	}
	score := 3
	if level == RequestRiskLevelHigh {
		score = 5
	}
	return RequestRiskVerdict{
		Level:         level,
		Score:         score,
		Factors:       []string{"temporary_block"},
		RetryAfter:    retryAfter,
		Blocked:       true,
		ExistingBlock: true,
		Fingerprint:   fingerprint,
		ScopeKey:      scopeKey,
	}, true
}

func EvaluateRequestRiskBehavior(ctx context.Context, input RequestRiskInput, settings system_setting.RequestRiskSettings) RequestRiskVerdict {
	input.Model = strings.TrimSpace(input.Model)
	normalizedText := normalizeRequestRiskText(input.Text)
	fingerprint := requestRiskFingerprint(normalizedText)
	scopeKey := requestRiskPrimaryScope(input)
	snapshot := collectRequestRiskSnapshot(ctx, input, fingerprint)
	verdict := scoreRequestRisk(snapshot, normalizedText)
	verdict.Fingerprint = fingerprint
	verdict.ScopeKey = scopeKey

	if verdict.Score < 3 {
		return verdict
	}
	verdict.RetryAfter = proposedRequestRiskRetryAfter(input, verdict, settings)
	if settings.Mode != system_setting.RequestRiskModeEnforce {
		verdict.Observed = true
		return verdict
	}

	verdict.Blocked = true
	verdict.RetryAfter = applyRequestRiskBlock(ctx, input, verdict, settings)
	return verdict
}

func ExtractRequestRiskTextFromRequestReadSeeker(body io.ReadSeeker, size int64, contentType string, endpoint string, maxLen int) (string, error) {
	if body == nil {
		return "", nil
	}
	if _, err := body.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	readLimit := int64(requestRiskBodyReadLimit)
	if size >= 0 && size < readLimit {
		readLimit = size
	}
	data, err := io.ReadAll(io.LimitReader(body, readLimit+1))
	if err != nil {
		return "", err
	}
	if int64(len(data)) > readLimit {
		return "", fmt.Errorf("request risk body exceeds %d bytes", readLimit)
	}
	if _, err := body.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	if gjson.ValidBytes(data) {
		return ExtractRequestRiskText(data, endpoint, maxLen), nil
	}
	return ExtractPromptTextFromRequestReadSeeker(body, size, contentType, endpoint, maxLen)
}

func ExtractRequestRiskText(body []byte, endpoint string, maxLen int) string {
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return ""
	}
	endpoint = strings.ToLower(strings.TrimSpace(endpoint))
	var text string
	switch {
	case strings.Contains(endpoint, "/chat/completions"), endpoint == "chat", endpoint == "chat_completions", endpoint == "/v1/messages", endpoint == "messages", endpoint == "claude":
		text = requestRiskLastUserText(gjson.GetBytes(body, "messages"))
	case strings.Contains(endpoint, "/responses"), endpoint == "responses", endpoint == "openai_responses", endpoint == "openai_responses_compaction":
		input := gjson.GetBytes(body, "input")
		text = requestRiskLastUserText(input)
		if text == "" {
			text = requestRiskResultText(input)
		}
	case strings.Contains(endpoint, "/v1beta/models"), endpoint == "gemini":
		contents := gjson.GetBytes(body, "contents")
		text = requestRiskLastUserText(contents)
		if text == "" {
			text = requestRiskLastItemText(contents)
		}
	default:
		text = requestRiskLastUserText(gjson.GetBytes(body, "messages"))
	}
	if text == "" {
		for _, path := range []string{"prompt", "input", "content", "metadata.prompt", "metadata.content"} {
			if text = requestRiskResultText(gjson.GetBytes(body, path)); text != "" {
				break
			}
		}
	}
	return promptFilterLimitScanText(text, maxLen)
}

func requestRiskLastUserText(result gjson.Result) string {
	if !result.Exists() {
		return ""
	}
	items := result.Array()
	for index := len(items) - 1; index >= 0; index-- {
		if !strings.EqualFold(strings.TrimSpace(items[index].Get("role").String()), "user") {
			continue
		}
		if text := requestRiskResultText(items[index]); text != "" {
			return text
		}
	}
	return ""
}

func requestRiskLastItemText(result gjson.Result) string {
	items := result.Array()
	if len(items) == 0 {
		return requestRiskResultText(result)
	}
	return requestRiskResultText(items[len(items)-1])
}

func requestRiskResultText(result gjson.Result) string {
	parts := make([]string, 0, 4)
	collectPromptGJSONText(result, &parts)
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func proposedRequestRiskRetryAfter(input RequestRiskInput, verdict RequestRiskVerdict, settings system_setting.RequestRiskSettings) time.Duration {
	if verdict.Level == RequestRiskLevelMedium {
		return time.Duration(settings.MediumCooldownSeconds) * time.Second
	}
	retryAfter := time.Duration(settings.UserBlockSeconds) * time.Second
	if input.TokenID > 0 {
		retryAfter = time.Duration(settings.TokenBlockSeconds) * time.Second
	}
	if verdict.Metrics.IPRequestCount60s >= 360 {
		ipBlock := time.Duration(settings.IPBlockSeconds) * time.Second
		if ipBlock > retryAfter {
			retryAfter = ipBlock
		}
	}
	return retryAfter
}

func RecordRequestRiskFailure(ctx context.Context, input RequestRiskInput, fingerprint string) {
	scope := requestRiskBehaviorScope(input)
	if scope == "" {
		return
	}
	now := time.Now()
	failureKey := requestRiskWindowKey("failure", scope, now, int64(requestRiskFailureWindow.Seconds()))
	lastFailureKey := requestRiskKey("last-failure", scope)

	if common.RedisEnabled && common.RDB != nil {
		pipe := common.RDB.Pipeline()
		pipe.Incr(ctx, failureKey)
		pipe.Expire(ctx, failureKey, requestRiskFailureWindow*2)
		if fingerprint != "" {
			pipe.Set(ctx, lastFailureKey, fingerprint, requestRiskFastRetryTTL)
		}
		if _, err := pipe.Exec(ctx); err == nil {
			return
		} else {
			common.SysLog("request risk redis failure record failed: " + err.Error())
		}
	}

	requestRiskMemory.Lock()
	defer requestRiskMemory.Unlock()
	pruneRequestRiskMemory(now)
	memoryRequestRiskIncrement(failureKey, requestRiskFailureWindow*2, now)
	if fingerprint != "" {
		memoryRequestRiskSetValue(lastFailureKey, fingerprint, requestRiskFastRetryTTL, now)
	}
	trimRequestRiskMemory(now)
}

func AcquireRequestRiskLogSlot(ctx context.Context, scope string) bool {
	return AcquireRequestGuardLogSlot(ctx, "risk", scope)
}

func AcquireRequestGuardLogSlot(ctx context.Context, namespace string, scope string) bool {
	if scope == "" {
		return false
	}
	key := requestRiskKey("log:"+namespace, scope)
	if common.RedisEnabled && common.RDB != nil {
		acquired, err := common.RDB.SetNX(ctx, key, "1", requestRiskLogCooldown).Result()
		if err == nil {
			return acquired
		}
		common.SysLog("request risk redis log dedup failed: " + err.Error())
	}

	now := time.Now()
	requestRiskMemory.Lock()
	defer requestRiskMemory.Unlock()
	pruneRequestRiskMemory(now)
	if entry, ok := requestRiskMemory.items[key]; ok && entry.ExpiresAt.After(now) {
		return false
	}
	memoryRequestRiskSetValue(key, "1", requestRiskLogCooldown, now)
	trimRequestRiskMemory(now)
	return true
}

func scoreRequestRisk(snapshot requestRiskSnapshot, normalizedText string) RequestRiskVerdict {
	verdict := RequestRiskVerdict{Level: RequestRiskLevelLow}
	verdict.Metrics = RequestRiskMetrics{
		RequestCount10s:   maxInt64(snapshot.UserCount10s, snapshot.TokenCount10s),
		RequestCount60s:   maxInt64(snapshot.UserCount60s, snapshot.TokenCount60s),
		IPRequestCount60s: snapshot.IPCount60s,
		RepeatCount60s:    snapshot.RepeatCount60s,
		DistinctModels60s: snapshot.DistinctModels60,
		FailureCount30s:   snapshot.FailureCount30s,
	}

	if verdict.Metrics.RequestCount10s >= 25 {
		verdict.Score += 3
		verdict.Factors = append(verdict.Factors, "burst_10s_high")
	} else if verdict.Metrics.RequestCount10s >= 10 {
		verdict.Score++
		verdict.Factors = append(verdict.Factors, "burst_10s")
	}

	if verdict.Metrics.RequestCount60s >= 120 {
		verdict.Score += 3
		verdict.Factors = append(verdict.Factors, "request_volume_60s_high")
	} else if verdict.Metrics.RequestCount60s >= 60 {
		verdict.Score++
		verdict.Factors = append(verdict.Factors, "request_volume_60s")
	}

	if snapshot.IPCount60s >= 360 {
		verdict.Score += 3
		verdict.Factors = append(verdict.Factors, "ip_volume_60s_high")
	} else if snapshot.IPCount60s >= 180 {
		verdict.Score++
		verdict.Factors = append(verdict.Factors, "ip_volume_60s")
	}

	if _, found := requestRiskMeaninglessTexts[normalizedText]; found {
		verdict.Score++
		verdict.Factors = append(verdict.Factors, "meaningless_exact_match")
		verdict.MatchedKeywords = append(verdict.MatchedKeywords, normalizedText)
	}

	if snapshot.RepeatCount60s >= 6 {
		verdict.Score += 2
		verdict.Factors = append(verdict.Factors, "repeated_content_high")
	} else if snapshot.RepeatCount60s >= 3 {
		verdict.Score++
		verdict.Factors = append(verdict.Factors, "repeated_content")
	}

	if snapshot.DistinctModels60 >= 8 {
		verdict.Score += 3
		verdict.Factors = append(verdict.Factors, "model_sweep_high")
	} else if snapshot.DistinctModels60 >= 4 {
		verdict.Score += 2
		verdict.Factors = append(verdict.Factors, "model_sweep")
	}

	if snapshot.FailureCount30s >= 12 {
		verdict.Score += 2
		verdict.Factors = append(verdict.Factors, "failure_burst_high")
	} else if snapshot.FailureCount30s >= 5 {
		verdict.Score++
		verdict.Factors = append(verdict.Factors, "failure_burst")
	}

	if snapshot.FastFailureRetry {
		verdict.Score += 2
		verdict.Factors = append(verdict.Factors, "fast_failure_retry")
	}

	if verdict.Score >= 5 {
		verdict.Level = RequestRiskLevelHigh
	} else if verdict.Score >= 3 {
		verdict.Level = RequestRiskLevelMedium
	}
	return verdict
}

func collectRequestRiskSnapshot(ctx context.Context, input RequestRiskInput, fingerprint string) requestRiskSnapshot {
	if common.RedisEnabled && common.RDB != nil {
		snapshot, err := collectRedisRequestRiskSnapshot(ctx, input, fingerprint)
		if err == nil {
			return snapshot
		}
		common.SysLog("request risk redis evaluation failed: " + err.Error())
	}
	return collectMemoryRequestRiskSnapshot(input, fingerprint, time.Now())
}

func collectRedisRequestRiskSnapshot(ctx context.Context, input RequestRiskInput, fingerprint string) (requestRiskSnapshot, error) {
	now := time.Now()
	pipe := common.RDB.Pipeline()
	increment := func(key string, ttl time.Duration) *redis.IntCmd {
		cmd := pipe.Incr(ctx, key)
		pipe.Expire(ctx, key, ttl)
		return cmd
	}

	userScope := requestRiskUserScope(input.UserID)
	tokenScope := requestRiskTokenScope(input.TokenID)
	ipScope := requestRiskIPScope(input.ClientIP)
	behaviorScope := requestRiskBehaviorScope(input)
	var user10, user60, token10, token60, ip60 *redis.IntCmd
	if userScope != "" {
		user10 = increment(requestRiskWindowKey("count-10s", userScope, now, 10), 25*time.Second)
		user60 = increment(requestRiskWindowKey("count-60s", userScope, now, 60), 125*time.Second)
	}
	if tokenScope != "" {
		token10 = increment(requestRiskWindowKey("count-10s", tokenScope, now, 10), 25*time.Second)
		token60 = increment(requestRiskWindowKey("count-60s", tokenScope, now, 60), 125*time.Second)
	}
	if ipScope != "" {
		ip60 = increment(requestRiskWindowKey("count-60s", ipScope, now, 60), 125*time.Second)
	}

	var repeat60 *redis.IntCmd
	var distinctModels *redis.Cmd
	if fingerprint != "" && behaviorScope != "" {
		fingerprintScope := requestRiskFingerprintScope(input, fingerprint)
		repeat60 = increment(requestRiskWindowKey("fingerprint", fingerprintScope, now, 60), 125*time.Second)
	}
	if input.Model != "" && behaviorScope != "" {
		modelsKey := requestRiskWindowKey("models", behaviorScope, now, 60)
		distinctModels = pipe.Eval(
			ctx,
			requestRiskModelCardinalityLua,
			[]string{modelsKey},
			requestRiskFingerprint(input.Model),
			requestRiskModelSetLimit,
			int64((125 * time.Second).Seconds()),
		)
	}

	var failureCount, lastFailure *redis.StringCmd
	if behaviorScope != "" {
		failureKey := requestRiskWindowKey("failure", behaviorScope, now, int64(requestRiskFailureWindow.Seconds()))
		failureCount = pipe.Get(ctx, failureKey)
		lastFailure = pipe.Get(ctx, requestRiskKey("last-failure", behaviorScope))
	}
	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		return requestRiskSnapshot{}, err
	}

	snapshot := requestRiskSnapshot{}
	if user10 != nil {
		snapshot.UserCount10s = user10.Val()
		snapshot.UserCount60s = user60.Val()
	}
	if token10 != nil {
		snapshot.TokenCount10s = token10.Val()
		snapshot.TokenCount60s = token60.Val()
	}
	if ip60 != nil {
		snapshot.IPCount60s = ip60.Val()
	}
	if repeat60 != nil {
		snapshot.RepeatCount60s = repeat60.Val()
	}
	if distinctModels != nil {
		snapshot.DistinctModels60 = redisResultInt(distinctModels.Val())
	}
	if failureCount != nil {
		snapshot.FailureCount30s = parseRedisInt(failureCount.Val())
		snapshot.FastFailureRetry = fingerprint != "" && lastFailure.Val() == fingerprint
	}
	return snapshot, nil
}

func collectMemoryRequestRiskSnapshot(input RequestRiskInput, fingerprint string, now time.Time) requestRiskSnapshot {
	requestRiskMemory.Lock()
	defer requestRiskMemory.Unlock()
	pruneRequestRiskMemory(now)

	userScope := requestRiskUserScope(input.UserID)
	tokenScope := requestRiskTokenScope(input.TokenID)
	ipScope := requestRiskIPScope(input.ClientIP)
	behaviorScope := requestRiskBehaviorScope(input)
	snapshot := requestRiskSnapshot{}
	if userScope != "" {
		snapshot.UserCount10s = memoryRequestRiskIncrement(requestRiskWindowKey("count-10s", userScope, now, 10), 25*time.Second, now)
		snapshot.UserCount60s = memoryRequestRiskIncrement(requestRiskWindowKey("count-60s", userScope, now, 60), 125*time.Second, now)
	}
	if tokenScope != "" {
		snapshot.TokenCount10s = memoryRequestRiskIncrement(requestRiskWindowKey("count-10s", tokenScope, now, 10), 25*time.Second, now)
		snapshot.TokenCount60s = memoryRequestRiskIncrement(requestRiskWindowKey("count-60s", tokenScope, now, 60), 125*time.Second, now)
	}
	if ipScope != "" {
		snapshot.IPCount60s = memoryRequestRiskIncrement(requestRiskWindowKey("count-60s", ipScope, now, 60), 125*time.Second, now)
	}
	if fingerprint != "" && behaviorScope != "" {
		fingerprintScope := requestRiskFingerprintScope(input, fingerprint)
		snapshot.RepeatCount60s = memoryRequestRiskIncrement(requestRiskWindowKey("fingerprint", fingerprintScope, now, 60), 125*time.Second, now)
	}
	if input.Model != "" && behaviorScope != "" {
		snapshot.DistinctModels60 = memoryRequestRiskSetAdd(
			requestRiskWindowKey("models", behaviorScope, now, 60),
			requestRiskFingerprint(input.Model),
			requestRiskModelSetLimit,
			125*time.Second,
			now,
		)
	}
	if behaviorScope != "" {
		snapshot.FailureCount30s = memoryRequestRiskCount(requestRiskWindowKey("failure", behaviorScope, now, int64(requestRiskFailureWindow.Seconds())), now)
		snapshot.FastFailureRetry = fingerprint != "" && memoryRequestRiskValue(requestRiskKey("last-failure", behaviorScope), now) == fingerprint
	}
	trimRequestRiskMemory(now)
	return snapshot
}

func activeRequestRiskBlock(ctx context.Context, input RequestRiskInput) (string, time.Duration, bool) {
	keys := requestRiskBlockKeys(input)
	if len(keys) == 0 {
		return "", 0, false
	}
	if common.RedisEnabled && common.RDB != nil {
		level, retryAfter, found, err := activeRedisRequestRiskBlock(ctx, keys)
		if err == nil {
			return level, retryAfter, found
		}
		common.SysLog("request risk redis block check failed: " + err.Error())
	}
	return activeMemoryRequestRiskBlock(keys, time.Now())
}

type requestRiskBlockKey struct {
	Key string
}

func activeRedisRequestRiskBlock(ctx context.Context, keys []requestRiskBlockKey) (string, time.Duration, bool, error) {
	pipe := common.RDB.Pipeline()
	type command struct {
		level *redis.StringCmd
		ttl   *redis.DurationCmd
	}
	commands := make([]command, 0, len(keys))
	for _, key := range keys {
		commands = append(commands, command{
			level: pipe.Get(ctx, key.Key),
			ttl:   pipe.TTL(ctx, key.Key),
		})
	}
	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		return "", 0, false, err
	}
	level := ""
	var retryAfter time.Duration
	for _, cmd := range commands {
		if cmd.ttl.Val() <= 0 || cmd.level.Val() == "" {
			continue
		}
		if cmd.level.Val() == RequestRiskLevelHigh {
			level = RequestRiskLevelHigh
		} else if level == "" {
			level = RequestRiskLevelMedium
		}
		if cmd.ttl.Val() > retryAfter {
			retryAfter = cmd.ttl.Val()
		}
	}
	return level, retryAfter, level != "", nil
}

func activeMemoryRequestRiskBlock(keys []requestRiskBlockKey, now time.Time) (string, time.Duration, bool) {
	requestRiskMemory.Lock()
	defer requestRiskMemory.Unlock()
	pruneRequestRiskMemory(now)
	level := ""
	var retryAfter time.Duration
	for _, key := range keys {
		entry, ok := requestRiskMemory.items[key.Key]
		if !ok || !entry.ExpiresAt.After(now) || entry.Value == "" {
			continue
		}
		if entry.Value == RequestRiskLevelHigh {
			level = RequestRiskLevelHigh
		} else if level == "" {
			level = RequestRiskLevelMedium
		}
		if ttl := entry.ExpiresAt.Sub(now); ttl > retryAfter {
			retryAfter = ttl
		}
	}
	return level, retryAfter, level != ""
}

func applyRequestRiskBlock(ctx context.Context, input RequestRiskInput, verdict RequestRiskVerdict, settings system_setting.RequestRiskSettings) time.Duration {
	blocks := make([]requestRiskBlockSpec, 0, 3)
	if verdict.Level == RequestRiskLevelMedium {
		key := requestRiskBlockKeyForPrimaryScope(input)
		if key != "" {
			blocks = append(blocks, requestRiskBlockSpec{
				Key:      key,
				Level:    RequestRiskLevelMedium,
				Duration: time.Duration(settings.MediumCooldownSeconds) * time.Second,
			})
		}
	} else {
		if input.TokenID > 0 {
			blocks = append(blocks, requestRiskBlockSpec{
				Key:      requestRiskKey("block", requestRiskTokenScope(input.TokenID)),
				Level:    RequestRiskLevelHigh,
				Duration: time.Duration(settings.TokenBlockSeconds) * time.Second,
			})
		}
		if input.UserID > 0 {
			blocks = append(blocks, requestRiskBlockSpec{
				Key:      requestRiskKey("block", requestRiskUserScope(input.UserID)),
				Level:    RequestRiskLevelHigh,
				Duration: time.Duration(settings.UserBlockSeconds) * time.Second,
			})
		}
		if verdict.Metrics.IPRequestCount60s >= 360 && input.ClientIP != "" {
			blocks = append(blocks, requestRiskBlockSpec{
				Key:      requestRiskKey("block", requestRiskIPScope(input.ClientIP)),
				Level:    RequestRiskLevelHigh,
				Duration: time.Duration(settings.IPBlockSeconds) * time.Second,
			})
		}
	}
	if len(blocks) == 0 {
		return time.Duration(settings.MediumCooldownSeconds) * time.Second
	}

	if common.RedisEnabled && common.RDB != nil {
		pipe := common.RDB.Pipeline()
		for _, block := range blocks {
			pipe.Set(ctx, block.Key, block.Level, block.Duration)
		}
		if _, err := pipe.Exec(ctx); err == nil {
			return maxRequestRiskBlockDuration(blocks)
		} else {
			common.SysLog("request risk redis block write failed: " + err.Error())
		}
	}

	now := time.Now()
	requestRiskMemory.Lock()
	defer requestRiskMemory.Unlock()
	pruneRequestRiskMemory(now)
	for _, block := range blocks {
		memoryRequestRiskSetValue(block.Key, block.Level, block.Duration, now)
	}
	trimRequestRiskMemory(now)
	return maxRequestRiskBlockDuration(blocks)
}

func maxRequestRiskBlockDuration(blocks []requestRiskBlockSpec) time.Duration {
	var result time.Duration
	for _, block := range blocks {
		if block.Duration > result {
			result = block.Duration
		}
	}
	return result
}

func requestRiskBlockKeys(input RequestRiskInput) []requestRiskBlockKey {
	keys := make([]requestRiskBlockKey, 0, 3)
	if input.TokenID > 0 {
		scope := requestRiskTokenScope(input.TokenID)
		keys = append(keys, requestRiskBlockKey{Key: requestRiskKey("block", scope)})
	}
	if input.UserID > 0 {
		scope := requestRiskUserScope(input.UserID)
		keys = append(keys, requestRiskBlockKey{Key: requestRiskKey("block", scope)})
	}
	if input.ClientIP != "" {
		scope := requestRiskIPScope(input.ClientIP)
		keys = append(keys, requestRiskBlockKey{Key: requestRiskKey("block", scope)})
	}
	return keys
}

func requestRiskBlockKeyForPrimaryScope(input RequestRiskInput) string {
	scope := requestRiskPrimaryScope(input)
	if scope == "" {
		return ""
	}
	return requestRiskKey("block", scope)
}

func requestRiskPrimaryScope(input RequestRiskInput) string {
	if input.TokenID > 0 {
		return requestRiskTokenScope(input.TokenID)
	}
	if input.UserID > 0 {
		return requestRiskUserScope(input.UserID)
	}
	return requestRiskIPScope(input.ClientIP)
}

func requestRiskFingerprintScope(input RequestRiskInput, fingerprint string) string {
	return requestRiskBehaviorScope(input) + ":" + fingerprint
}

func requestRiskBehaviorScope(input RequestRiskInput) string {
	if input.UserID > 0 {
		return requestRiskUserScope(input.UserID)
	}
	if input.TokenID > 0 {
		return requestRiskTokenScope(input.TokenID)
	}
	return requestRiskIPScope(input.ClientIP)
}

func requestRiskUserScope(userID int) string {
	if userID <= 0 {
		return ""
	}
	return "user:" + common.GenerateHMAC(fmt.Sprintf("request-risk-user:%d", userID))
}

func requestRiskTokenScope(tokenID int) string {
	if tokenID <= 0 {
		return ""
	}
	return "token:" + common.GenerateHMAC(fmt.Sprintf("request-risk-token:%d", tokenID))
}

func requestRiskIPScope(clientIP string) string {
	clientIP = strings.TrimSpace(clientIP)
	if clientIP == "" {
		return ""
	}
	return "ip:" + common.GenerateHMAC("request-risk-ip:"+clientIP)
}

func requestRiskKey(kind string, scope string) string {
	return "requestRisk:" + kind + ":" + scope
}

func requestRiskWindowKey(kind string, scope string, now time.Time, windowSeconds int64) string {
	return fmt.Sprintf("%s:%d", requestRiskKey(kind, scope), now.Unix()/windowSeconds)
}

func normalizeRequestRiskText(text string) string {
	text = strings.ToLower(text)
	text = strings.Join(strings.Fields(text), " ")
	text = strings.TrimRightFunc(text, func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune("!！?？.。", r)
	})
	runes := []rune(text)
	if len(runes) > requestRiskTextRuneLimit {
		half := requestRiskTextRuneLimit / 2
		text = string(runes[:half]) + "\n" + string(runes[len(runes)-half:])
	}
	return text
}

func requestRiskFingerprint(normalizedText string) string {
	if normalizedText == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(normalizedText))
	return hex.EncodeToString(sum[:])
}

func parseRedisInt(value string) int64 {
	result, _ := strconv.ParseInt(value, 10, 64)
	return result
}

func maxInt64(a int64, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func memoryRequestRiskIncrement(key string, ttl time.Duration, now time.Time) int64 {
	entry := requestRiskMemory.items[key]
	if !entry.ExpiresAt.After(now) {
		entry = requestRiskMemoryEntry{ExpiresAt: now.Add(ttl)}
	}
	entry.Count++
	requestRiskMemory.items[key] = entry
	return entry.Count
}

func memoryRequestRiskSetAdd(key string, value string, maxValues int, ttl time.Duration, now time.Time) int64 {
	entry := requestRiskMemory.items[key]
	if !entry.ExpiresAt.After(now) || entry.Values == nil {
		entry = requestRiskMemoryEntry{Values: make(map[string]struct{}), ExpiresAt: now.Add(ttl)}
	}
	if len(entry.Values) < maxValues {
		entry.Values[value] = struct{}{}
	}
	requestRiskMemory.items[key] = entry
	return int64(len(entry.Values))
}

func memoryRequestRiskCount(key string, now time.Time) int64 {
	entry, ok := requestRiskMemory.items[key]
	if !ok || !entry.ExpiresAt.After(now) {
		return 0
	}
	return entry.Count
}

func memoryRequestRiskValue(key string, now time.Time) string {
	entry, ok := requestRiskMemory.items[key]
	if !ok || !entry.ExpiresAt.After(now) {
		return ""
	}
	return entry.Value
}

func memoryRequestRiskSetValue(key string, value string, ttl time.Duration, now time.Time) {
	requestRiskMemory.items[key] = requestRiskMemoryEntry{Value: value, ExpiresAt: now.Add(ttl)}
}

func pruneRequestRiskMemory(now time.Time) {
	if !requestRiskMemory.lastPruned.IsZero() && now.Sub(requestRiskMemory.lastPruned) < requestRiskPruneInterval {
		return
	}
	requestRiskMemory.lastPruned = now
	for key, entry := range requestRiskMemory.items {
		if !entry.ExpiresAt.After(now) {
			delete(requestRiskMemory.items, key)
		}
	}
}

func trimRequestRiskMemory(now time.Time) {
	overflow := len(requestRiskMemory.items) - requestRiskMemoryMaxItems
	if overflow <= 0 {
		return
	}
	for key, entry := range requestRiskMemory.items {
		if overflow <= 0 {
			return
		}
		if entry.ExpiresAt.After(now) && strings.Contains(key, ":block:") {
			continue
		}
		delete(requestRiskMemory.items, key)
		overflow--
	}
	if overflow <= 0 {
		return
	}
	for key := range requestRiskMemory.items {
		if overflow <= 0 {
			return
		}
		delete(requestRiskMemory.items, key)
		overflow--
	}
}

func resetRequestRiskMemoryForTest() {
	requestRiskMemory.Lock()
	defer requestRiskMemory.Unlock()
	requestRiskMemory.items = make(map[string]requestRiskMemoryEntry)
	requestRiskMemory.lastPruned = time.Time{}
}
