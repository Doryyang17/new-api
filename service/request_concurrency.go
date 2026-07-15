package service

import (
	"context"
	_ "embed"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
)

const (
	requestConcurrencyBackendMemory  = "memory"
	requestConcurrencyBackendRedis   = "redis"
	requestConcurrencyMinLeaseTTL    = 10 * time.Minute
	requestConcurrencyMaxLeaseTTL    = 24 * time.Hour
	requestConcurrencyCleanupTimeout = 200 * time.Millisecond
)

//go:embed lua/request_concurrency_acquire.lua
var requestConcurrencyAcquireLua string

//go:embed lua/request_concurrency_release.lua
var requestConcurrencyReleaseLua string

//go:embed lua/request_concurrency_renew.lua
var requestConcurrencyRenewLua string

var (
	requestConcurrencyAcquireScript = redis.NewScript(requestConcurrencyAcquireLua)
	requestConcurrencyReleaseScript = redis.NewScript(requestConcurrencyReleaseLua)
	requestConcurrencyRenewScript   = redis.NewScript(requestConcurrencyRenewLua)
	requestConcurrencyMemory        = struct {
		sync.Mutex
		counts map[string]int64
	}{counts: make(map[string]int64)}
)

type RequestConcurrencyVerdict struct {
	Allowed       bool
	Exceeded      bool
	Observed      bool
	UserExceeded  bool
	TokenExceeded bool
	UserCount     int64
	TokenCount    int64
	UserLimit     int
	TokenLimit    int
	Factors       []string
	ScopeKey      string
}

type RequestConcurrencyLease struct {
	backend  string
	leaseID  string
	ttl      time.Duration
	userKey  string
	tokenKey string
}

func AcquireRequestConcurrency(ctx context.Context, input RequestRiskInput, settings system_setting.RequestRiskSettings) (*RequestConcurrencyLease, RequestConcurrencyVerdict) {
	verdict := RequestConcurrencyVerdict{
		Allowed:    true,
		UserLimit:  settings.UserConcurrencyLimit,
		TokenLimit: settings.TokenConcurrencyLimit,
		ScopeKey:   requestRiskPrimaryScope(input),
	}
	userKey := requestConcurrencyKey("user", input.UserID, settings.UserConcurrencyLimit)
	tokenKey := requestConcurrencyKey("token", input.TokenID, settings.TokenConcurrencyLimit)
	if userKey == "" && tokenKey == "" {
		return nil, verdict
	}

	enforce := settings.Mode == system_setting.RequestRiskModeEnforce
	if common.RedisEnabled && common.RDB != nil {
		lease, redisVerdict, err := acquireRedisRequestConcurrency(ctx, userKey, tokenKey, settings, enforce)
		if err == nil {
			redisVerdict.ScopeKey = requestConcurrencyLogScope(input, redisVerdict)
			return lease, redisVerdict
		}
		common.SysLog("request concurrency redis acquire failed: " + err.Error())
	}

	lease, memoryVerdict := acquireMemoryRequestConcurrency(userKey, tokenKey, settings, enforce)
	memoryVerdict.ScopeKey = requestConcurrencyLogScope(input, memoryVerdict)
	return lease, memoryVerdict
}

func ReleaseRequestConcurrency(lease *RequestConcurrencyLease) {
	if lease == nil {
		return
	}
	if lease.backend == requestConcurrencyBackendRedis {
		if common.RDB == nil {
			common.SysLog("request concurrency redis release skipped: redis client is unavailable")
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if _, err := requestConcurrencyReleaseScript.Run(ctx, common.RDB, []string{lease.userKey, lease.tokenKey}, lease.leaseID).Result(); err != nil {
			common.SysLog("request concurrency redis release failed: " + err.Error())
		}
		return
	}

	requestConcurrencyMemory.Lock()
	defer requestConcurrencyMemory.Unlock()
	releaseMemoryRequestConcurrencyKey(lease.userKey)
	releaseMemoryRequestConcurrencyKey(lease.tokenKey)
}

func StartRequestConcurrencyLeaseHeartbeat(lease *RequestConcurrencyLease) func() {
	if lease == nil || lease.backend != requestConcurrencyBackendRedis || lease.ttl <= 0 {
		return func() {}
	}
	ctx, cancel := context.WithCancel(context.Background())
	go keepRequestConcurrencyLeaseAlive(ctx, lease)
	return cancel
}

func keepRequestConcurrencyLeaseAlive(ctx context.Context, lease *RequestConcurrencyLease) {
	ticker := time.NewTicker(requestConcurrencyHeartbeatInterval(lease.ttl))
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if common.RDB == nil {
				common.SysLog("request concurrency lease renewal skipped: redis client is unavailable")
				continue
			}
			renewCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			_, err := requestConcurrencyRenewScript.Run(
				renewCtx,
				common.RDB,
				[]string{lease.userKey, lease.tokenKey},
				lease.leaseID,
				int64(lease.ttl.Seconds()),
			).Result()
			cancel()
			if err != nil {
				common.SysLog("request concurrency lease renewal failed: " + err.Error())
			}
		}
	}
}

func acquireRedisRequestConcurrency(ctx context.Context, userKey string, tokenKey string, settings system_setting.RequestRiskSettings, enforce bool) (*RequestConcurrencyLease, RequestConcurrencyVerdict, error) {
	rdb := common.RDB
	if rdb == nil {
		return nil, RequestConcurrencyVerdict{}, fmt.Errorf("redis client is unavailable")
	}
	leaseID := uuid.NewString()
	leaseTTL := requestConcurrencyLeaseTTL()
	result, err := requestConcurrencyAcquireScript.Run(
		ctx,
		rdb,
		[]string{userKey, tokenKey},
		settings.UserConcurrencyLimit,
		settings.TokenConcurrencyLimit,
		int64(leaseTTL.Seconds()),
		leaseID,
		boolToRedisInt(enforce),
	).Result()
	if err != nil {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), requestConcurrencyCleanupTimeout)
		_, cleanupErr := requestConcurrencyReleaseScript.Run(
			cleanupCtx,
			rdb,
			[]string{userKey, tokenKey},
			leaseID,
		).Result()
		cleanupCancel()
		if cleanupErr != nil {
			return nil, RequestConcurrencyVerdict{}, fmt.Errorf("redis concurrency acquire failed: %w; lease cleanup failed: %v", err, cleanupErr)
		}
		return nil, RequestConcurrencyVerdict{}, err
	}
	values, ok := result.([]interface{})
	if !ok || len(values) != 7 {
		return nil, RequestConcurrencyVerdict{}, fmt.Errorf("unexpected redis concurrency result %T", result)
	}

	verdict := RequestConcurrencyVerdict{
		Allowed:       redisResultInt(values[0]) == 1,
		UserCount:     redisResultInt(values[1]),
		TokenCount:    redisResultInt(values[2]),
		UserExceeded:  redisResultInt(values[3]) == 1,
		TokenExceeded: redisResultInt(values[4]) == 1,
		UserLimit:     settings.UserConcurrencyLimit,
		TokenLimit:    settings.TokenConcurrencyLimit,
	}
	completeRequestConcurrencyVerdict(&verdict, enforce)
	if !verdict.Allowed {
		return nil, verdict, nil
	}
	lease := &RequestConcurrencyLease{backend: requestConcurrencyBackendRedis, leaseID: leaseID, ttl: leaseTTL}
	if redisResultInt(values[5]) == 1 {
		lease.userKey = userKey
	}
	if redisResultInt(values[6]) == 1 {
		lease.tokenKey = tokenKey
	}
	return lease, verdict, nil
}

func acquireMemoryRequestConcurrency(userKey string, tokenKey string, settings system_setting.RequestRiskSettings, enforce bool) (*RequestConcurrencyLease, RequestConcurrencyVerdict) {
	requestConcurrencyMemory.Lock()
	defer requestConcurrencyMemory.Unlock()

	verdict := RequestConcurrencyVerdict{
		Allowed:    true,
		UserCount:  requestConcurrencyMemory.counts[userKey],
		TokenCount: requestConcurrencyMemory.counts[tokenKey],
		UserLimit:  settings.UserConcurrencyLimit,
		TokenLimit: settings.TokenConcurrencyLimit,
	}
	verdict.UserExceeded = userKey != "" && settings.UserConcurrencyLimit > 0 && verdict.UserCount >= int64(settings.UserConcurrencyLimit)
	verdict.TokenExceeded = tokenKey != "" && settings.TokenConcurrencyLimit > 0 && verdict.TokenCount >= int64(settings.TokenConcurrencyLimit)
	if enforce && (verdict.UserExceeded || verdict.TokenExceeded) {
		verdict.Allowed = false
		completeRequestConcurrencyVerdict(&verdict, enforce)
		return nil, verdict
	}

	lease := &RequestConcurrencyLease{backend: requestConcurrencyBackendMemory}
	if userKey != "" {
		requestConcurrencyMemory.counts[userKey]++
		verdict.UserCount++
		lease.userKey = userKey
	}
	if tokenKey != "" {
		requestConcurrencyMemory.counts[tokenKey]++
		verdict.TokenCount++
		lease.tokenKey = tokenKey
	}
	completeRequestConcurrencyVerdict(&verdict, enforce)
	return lease, verdict
}

func completeRequestConcurrencyVerdict(verdict *RequestConcurrencyVerdict, enforce bool) {
	verdict.Exceeded = verdict.UserExceeded || verdict.TokenExceeded
	verdict.Observed = verdict.Exceeded && !enforce
	if verdict.UserExceeded {
		verdict.Factors = append(verdict.Factors, "user_concurrency_limit")
	}
	if verdict.TokenExceeded {
		verdict.Factors = append(verdict.Factors, "token_concurrency_limit")
	}
}

func requestConcurrencyKey(scope string, id int, limit int) string {
	if id <= 0 || limit <= 0 {
		return ""
	}
	return "requestConcurrency:" + scope + ":" + common.GenerateHMAC(fmt.Sprintf("request-concurrency-%s:%d", scope, id))
}

func requestConcurrencyLogScope(input RequestRiskInput, verdict RequestConcurrencyVerdict) string {
	if verdict.UserExceeded && input.UserID > 0 {
		return requestRiskUserScope(input.UserID)
	}
	if verdict.TokenExceeded && input.TokenID > 0 {
		return requestRiskTokenScope(input.TokenID)
	}
	return requestRiskPrimaryScope(input)
}

func requestConcurrencyLeaseTTL() time.Duration {
	streamingTimeoutSeconds := int64(constant.StreamingTimeout)
	if streamingTimeoutSeconds > int64(requestConcurrencyMaxLeaseTTL/time.Second)/2 {
		return requestConcurrencyMaxLeaseTTL
	}
	ttl := time.Duration(streamingTimeoutSeconds*2) * time.Second
	if ttl < requestConcurrencyMinLeaseTTL {
		return requestConcurrencyMinLeaseTTL
	}
	if ttl > requestConcurrencyMaxLeaseTTL {
		return requestConcurrencyMaxLeaseTTL
	}
	return ttl
}

func requestConcurrencyHeartbeatInterval(ttl time.Duration) time.Duration {
	interval := ttl / 3
	if interval < time.Second {
		return time.Second
	}
	return interval
}

func releaseMemoryRequestConcurrencyKey(key string) {
	if key == "" {
		return
	}
	count := requestConcurrencyMemory.counts[key]
	if count <= 1 {
		delete(requestConcurrencyMemory.counts, key)
		return
	}
	requestConcurrencyMemory.counts[key] = count - 1
}

func redisResultInt(value interface{}) int64 {
	switch typed := value.(type) {
	case int64:
		return typed
	case string:
		result, _ := strconv.ParseInt(typed, 10, 64)
		return result
	case []byte:
		result, _ := strconv.ParseInt(string(typed), 10, 64)
		return result
	default:
		return 0
	}
}

func boolToRedisInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func resetRequestConcurrencyMemoryForTest() {
	requestConcurrencyMemory.Lock()
	defer requestConcurrencyMemory.Unlock()
	requestConcurrencyMemory.counts = make(map[string]int64)
}
