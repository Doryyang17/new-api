package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/go-redis/redis/v8"
)

const (
	promptFilterBlockedMessageTTL            = 24 * time.Hour
	promptFilterBlockedMessageMemoryMaxItems = 50000
	promptFilterBlockedMessageScopeMaxTexts  = 64
)

type promptFilterBlockedMessageMemoryEntry struct {
	ExpiresAt time.Time
	UpdatedAt time.Time
}

var promptFilterBlockedMessageMemoryStore = struct {
	sync.Mutex
	scopes map[string]map[string]promptFilterBlockedMessageMemoryEntry
}{
	scopes: make(map[string]map[string]promptFilterBlockedMessageMemoryEntry),
}

func RecordPromptFilterBlockedMessage(userID int, tokenID int, text string, _ ...[]PromptFilterMatch) {
	canonical := promptFilterBlockedMessageCanonicalText(text)
	if canonical == "" {
		return
	}
	scope := promptFilterBlockedMessageScope(userID, tokenID)
	if common.RedisEnabled && common.RDB != nil {
		ctx := context.Background()
		if err := redisRecordPromptFilterBlockedMessage(ctx, scope, canonical); err == nil {
			return
		} else {
			common.SysLog("prompt filter blocked message redis record failed: " + err.Error())
		}
	}
	memoryRecordPromptFilterBlockedMessage(scope, canonical)
}

func PromptFilterBlockedMessageExists(userID int, tokenID int, text string) bool {
	canonical := promptFilterBlockedMessageCanonicalText(text)
	if canonical == "" {
		return false
	}
	scope := promptFilterBlockedMessageScope(userID, tokenID)
	if common.RedisEnabled && common.RDB != nil {
		ctx := context.Background()
		_, err := common.RDB.ZScore(ctx, promptFilterBlockedMessagesRedisKey(scope), canonical).Result()
		if err == nil {
			return true
		}
		if err != redis.Nil {
			common.SysLog("prompt filter blocked message redis check failed: " + err.Error())
		}
	}
	return memoryPromptFilterBlockedMessageExists(scope, canonical)
}

func PromptFilterBlockedMessages(userID int, tokenID int) []string {
	scope := promptFilterBlockedMessageScope(userID, tokenID)
	if common.RedisEnabled && common.RDB != nil {
		ctx := context.Background()
		texts, err := common.RDB.ZRevRange(ctx, promptFilterBlockedMessagesRedisKey(scope), 0, promptFilterBlockedMessageScopeMaxTexts-1).Result()
		if err == nil {
			return promptFilterSortBlockedMessages(texts)
		}
		common.SysLog("prompt filter blocked messages redis read failed: " + err.Error())
	}
	return memoryPromptFilterBlockedMessages(scope)
}

func promptFilterBlockedMessageScope(userID int, tokenID int) string {
	scope := common.GenerateHMAC(fmt.Sprintf("prompt-filter-blocked-message-scope:v2:%d:%d", userID, tokenID))
	if len(scope) > 16 {
		scope = scope[:16]
	}
	return scope
}

func promptFilterBlockedMessagesRedisKey(scope string) string {
	return "promptFilter:blockedMessages:v2:" + scope
}

func promptFilterBlockedMessageCanonicalText(text string) string {
	return promptFilterNormalizeForScan(strings.TrimSpace(text))
}

func promptFilterSortBlockedMessages(texts []string) []string {
	seen := make(map[string]struct{}, len(texts))
	unique := make([]string, 0, len(texts))
	for _, text := range texts {
		canonical := promptFilterBlockedMessageCanonicalText(text)
		if canonical == "" {
			continue
		}
		if _, ok := seen[canonical]; ok {
			continue
		}
		seen[canonical] = struct{}{}
		unique = append(unique, canonical)
	}
	sort.Slice(unique, func(i, j int) bool {
		if len(unique[i]) == len(unique[j]) {
			return unique[i] < unique[j]
		}
		return len(unique[i]) > len(unique[j])
	})
	if len(unique) > promptFilterBlockedMessageScopeMaxTexts {
		return unique[:promptFilterBlockedMessageScopeMaxTexts]
	}
	return unique
}

func redisRecordPromptFilterBlockedMessage(ctx context.Context, scope string, canonical string) error {
	key := promptFilterBlockedMessagesRedisKey(scope)
	txn := common.RDB.TxPipeline()
	txn.ZAdd(ctx, key, &redis.Z{
		Score:  float64(time.Now().UnixNano()),
		Member: canonical,
	})
	txn.ZRemRangeByRank(ctx, key, 0, -promptFilterBlockedMessageScopeMaxTexts-1)
	txn.Expire(ctx, key, promptFilterBlockedMessageTTL)
	_, err := txn.Exec(ctx)
	return err
}

func memoryRecordPromptFilterBlockedMessage(scope string, canonical string) {
	promptFilterBlockedMessageMemoryStore.Lock()
	defer promptFilterBlockedMessageMemoryStore.Unlock()

	now := time.Now()
	prunePromptFilterBlockedMessageMemory(now)
	scopeItems := promptFilterBlockedMessageMemoryStore.scopes[scope]
	if scopeItems == nil {
		scopeItems = map[string]promptFilterBlockedMessageMemoryEntry{}
		promptFilterBlockedMessageMemoryStore.scopes[scope] = scopeItems
	}
	scopeItems[canonical] = promptFilterBlockedMessageMemoryEntry{
		ExpiresAt: now.Add(promptFilterBlockedMessageTTL),
		UpdatedAt: now,
	}
	trimPromptFilterBlockedMessageScope(scope)
	trimPromptFilterBlockedMessageMemory(now)
}

func memoryPromptFilterBlockedMessageExists(scope string, canonical string) bool {
	promptFilterBlockedMessageMemoryStore.Lock()
	defer promptFilterBlockedMessageMemoryStore.Unlock()

	now := time.Now()
	prunePromptFilterBlockedMessageMemory(now)
	entry, ok := promptFilterBlockedMessageMemoryStore.scopes[scope][canonical]
	return ok && entry.ExpiresAt.After(now)
}

func memoryPromptFilterBlockedMessages(scope string) []string {
	promptFilterBlockedMessageMemoryStore.Lock()
	defer promptFilterBlockedMessageMemoryStore.Unlock()

	now := time.Now()
	prunePromptFilterBlockedMessageMemory(now)
	scopeItems := promptFilterBlockedMessageMemoryStore.scopes[scope]
	if len(scopeItems) == 0 {
		return nil
	}
	texts := make([]string, 0, len(scopeItems))
	for text, entry := range scopeItems {
		if entry.ExpiresAt.After(now) {
			texts = append(texts, text)
		}
	}
	return promptFilterSortBlockedMessages(texts)
}

func prunePromptFilterBlockedMessageMemory(now time.Time) {
	for scope, items := range promptFilterBlockedMessageMemoryStore.scopes {
		for text, entry := range items {
			if entry.ExpiresAt.After(now) {
				continue
			}
			delete(items, text)
		}
		if len(items) == 0 {
			delete(promptFilterBlockedMessageMemoryStore.scopes, scope)
		}
	}
}

func trimPromptFilterBlockedMessageScope(scope string) {
	items := promptFilterBlockedMessageMemoryStore.scopes[scope]
	overflow := len(items) - promptFilterBlockedMessageScopeMaxTexts
	if overflow <= 0 {
		return
	}
	type item struct {
		text    string
		updated time.Time
	}
	ordered := make([]item, 0, len(items))
	for text, entry := range items {
		ordered = append(ordered, item{text: text, updated: entry.UpdatedAt})
	}
	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i].updated.Before(ordered[j].updated)
	})
	for i := 0; i < overflow && i < len(ordered); i++ {
		delete(items, ordered[i].text)
	}
}

func trimPromptFilterBlockedMessageMemory(now time.Time) {
	overflow := promptFilterBlockedMessageMemorySize() - promptFilterBlockedMessageMemoryMaxItems
	if overflow <= 0 {
		return
	}
	for scope, items := range promptFilterBlockedMessageMemoryStore.scopes {
		for text, entry := range items {
			if overflow <= 0 {
				return
			}
			if entry.ExpiresAt.After(now) {
				continue
			}
			delete(items, text)
			overflow--
		}
		if len(items) == 0 {
			delete(promptFilterBlockedMessageMemoryStore.scopes, scope)
		}
	}
	for scope, items := range promptFilterBlockedMessageMemoryStore.scopes {
		for text := range items {
			if overflow <= 0 {
				return
			}
			delete(items, text)
			overflow--
		}
		if len(items) == 0 {
			delete(promptFilterBlockedMessageMemoryStore.scopes, scope)
		}
	}
}

func promptFilterBlockedMessageMemorySize() int {
	size := 0
	for _, items := range promptFilterBlockedMessageMemoryStore.scopes {
		size += len(items)
	}
	return size
}
