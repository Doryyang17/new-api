package service

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
)

const (
	registrationRiskConsecutiveLimit = 3
	registrationRiskCumulativeLimit  = 10
	registrationRiskShortBlock       = time.Hour
	registrationRiskLongBlock        = 24 * time.Hour
	registrationRiskConsecutiveTTL   = time.Hour
	registrationRiskCumulativeTTL    = 24 * time.Hour
	registrationRiskMemoryMaxItems   = 10000
)

var registrationFingerprintPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,128}$`)

type registrationRiskState struct {
	ConsecutiveCount     int
	ConsecutiveExpiresAt time.Time
	CumulativeCount      int
	CumulativeExpiresAt  time.Time
	BlockedUntil         time.Time
}

var registrationRiskMemoryStore = struct {
	sync.Mutex
	items map[string]registrationRiskState
}{
	items: make(map[string]registrationRiskState),
}

func BuildRegistrationRiskKeys(clientIP string, header http.Header) []string {
	keys := make([]string, 0, 2)
	if clientIP = strings.TrimSpace(clientIP); clientIP != "" {
		keys = append(keys, "ip:"+common.GenerateHMAC("registration-risk-ip:"+clientIP))
	}

	fingerprint := strings.TrimSpace(header.Get("X-Registration-Fingerprint"))
	if !registrationFingerprintPattern.MatchString(fingerprint) {
		fingerprint = strings.Join([]string{
			header.Get("User-Agent"),
			header.Get("Accept-Language"),
			header.Get("Sec-CH-UA"),
			header.Get("Sec-CH-UA-Platform"),
		}, "|")
	}
	fingerprint = strings.TrimSpace(fingerprint)
	if fingerprint != "" {
		keys = append(keys, "browser:"+common.GenerateHMAC("registration-risk-browser:"+fingerprint))
	}

	if len(keys) <= 1 {
		return keys
	}
	if keys[0] == keys[1] {
		return keys[:1]
	}
	return keys
}

func IsRegistrationRiskBlocked(keys []string) (bool, time.Duration) {
	if len(keys) == 0 {
		return false, 0
	}
	if common.RedisEnabled && common.RDB != nil {
		blocked, retryAfter, err := redisRegistrationRiskBlocked(keys)
		if err == nil {
			return blocked, retryAfter
		}
		common.SysLog("registration risk redis check failed: " + err.Error())
	}
	return memoryRegistrationRiskBlocked(keys)
}

func RecordRegistrationCodeFailure(keys []string) {
	if len(keys) == 0 {
		return
	}
	if common.RedisEnabled && common.RDB != nil {
		if err := redisRecordRegistrationCodeFailure(keys); err == nil {
			return
		} else {
			common.SysLog("registration risk redis record failed: " + err.Error())
		}
	}
	memoryRecordRegistrationCodeFailure(keys)
}

func ResetRegistrationCodeFailures(keys []string) {
	if len(keys) == 0 {
		return
	}
	if common.RedisEnabled && common.RDB != nil {
		ctx := context.Background()
		for _, key := range keys {
			base := registrationRiskRedisKey(key)
			_ = common.RDB.Del(ctx, base+":consecutive", base+":cumulative").Err()
		}
		return
	}
	registrationRiskMemoryStore.Lock()
	defer registrationRiskMemoryStore.Unlock()
	for _, key := range keys {
		delete(registrationRiskMemoryStore.items, key)
	}
}

func registrationRiskRedisKey(key string) string {
	return "registrationCodeRisk:" + key
}

func redisRegistrationRiskBlocked(keys []string) (bool, time.Duration, error) {
	ctx := context.Background()
	var maxTTL time.Duration
	for _, key := range keys {
		ttl, err := common.RDB.TTL(ctx, registrationRiskRedisKey(key)+":block").Result()
		if err != nil {
			return false, 0, err
		}
		if ttl > maxTTL {
			maxTTL = ttl
		}
	}
	return maxTTL > 0, maxTTL, nil
}

func redisRecordRegistrationCodeFailure(keys []string) error {
	ctx := context.Background()
	for _, key := range keys {
		base := registrationRiskRedisKey(key)
		consecutive, err := common.RDB.Incr(ctx, base+":consecutive").Result()
		if err != nil {
			return err
		}
		if consecutive == 1 {
			if err := common.RDB.Expire(ctx, base+":consecutive", registrationRiskConsecutiveTTL).Err(); err != nil {
				return err
			}
		}
		cumulative, err := common.RDB.Incr(ctx, base+":cumulative").Result()
		if err != nil {
			return err
		}
		if cumulative == 1 {
			if err := common.RDB.Expire(ctx, base+":cumulative", registrationRiskCumulativeTTL).Err(); err != nil {
				return err
			}
		}
		if consecutive >= registrationRiskConsecutiveLimit {
			if err := common.RDB.Set(ctx, base+":block", "1", registrationRiskShortBlock).Err(); err != nil {
				return err
			}
		}
		if cumulative >= registrationRiskCumulativeLimit {
			if err := common.RDB.Set(ctx, base+":block", "1", registrationRiskLongBlock).Err(); err != nil {
				return err
			}
		}
	}
	return nil
}

func memoryRegistrationRiskBlocked(keys []string) (bool, time.Duration) {
	registrationRiskMemoryStore.Lock()
	defer registrationRiskMemoryStore.Unlock()
	now := time.Now()
	pruneRegistrationRiskMemory(now)
	var maxRetryAfter time.Duration
	for _, key := range keys {
		state, ok := registrationRiskMemoryStore.items[key]
		if !ok {
			continue
		}
		if !state.BlockedUntil.After(now) {
			continue
		}
		retryAfter := state.BlockedUntil.Sub(now)
		if retryAfter > maxRetryAfter {
			maxRetryAfter = retryAfter
		}
	}
	return maxRetryAfter > 0, maxRetryAfter
}

func memoryRecordRegistrationCodeFailure(keys []string) {
	registrationRiskMemoryStore.Lock()
	defer registrationRiskMemoryStore.Unlock()
	now := time.Now()
	pruneRegistrationRiskMemory(now)
	for _, key := range keys {
		state := registrationRiskMemoryStore.items[key]
		if !state.ConsecutiveExpiresAt.After(now) {
			state.ConsecutiveCount = 0
			state.ConsecutiveExpiresAt = now.Add(registrationRiskConsecutiveTTL)
		}
		if !state.CumulativeExpiresAt.After(now) {
			state.CumulativeCount = 0
			state.CumulativeExpiresAt = now.Add(registrationRiskCumulativeTTL)
		}
		state.ConsecutiveCount++
		state.CumulativeCount++
		if state.ConsecutiveCount >= registrationRiskConsecutiveLimit {
			state.BlockedUntil = now.Add(registrationRiskShortBlock)
		}
		if state.CumulativeCount >= registrationRiskCumulativeLimit {
			state.BlockedUntil = now.Add(registrationRiskLongBlock)
		}
		registrationRiskMemoryStore.items[key] = state
	}
	trimRegistrationRiskMemory(now)
}

func pruneRegistrationRiskMemory(now time.Time) {
	for key, state := range registrationRiskMemoryStore.items {
		if state.BlockedUntil.After(now) ||
			state.ConsecutiveExpiresAt.After(now) ||
			state.CumulativeExpiresAt.After(now) {
			continue
		}
		delete(registrationRiskMemoryStore.items, key)
	}
}

func trimRegistrationRiskMemory(now time.Time) {
	overflow := len(registrationRiskMemoryStore.items) - registrationRiskMemoryMaxItems
	if overflow <= 0 {
		return
	}
	for key, state := range registrationRiskMemoryStore.items {
		if overflow <= 0 {
			return
		}
		if state.BlockedUntil.After(now) {
			continue
		}
		delete(registrationRiskMemoryStore.items, key)
		overflow--
	}
	for key := range registrationRiskMemoryStore.items {
		if overflow <= 0 {
			return
		}
		delete(registrationRiskMemoryStore.items, key)
		overflow--
	}
}
