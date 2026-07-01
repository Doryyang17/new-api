package service

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegistrationRiskMemoryPrunesExpiredAndLimitsSize(t *testing.T) {
	now := time.Now()
	registrationRiskMemoryStore.Lock()
	registrationRiskMemoryStore.items = map[string]registrationRiskState{
		"expired": {
			ConsecutiveExpiresAt: now.Add(-time.Second),
			CumulativeExpiresAt:  now.Add(-time.Second),
			BlockedUntil:         now.Add(-time.Second),
		},
		"blocked": {
			BlockedUntil: now.Add(time.Hour),
		},
	}
	registrationRiskMemoryStore.Unlock()
	t.Cleanup(func() {
		registrationRiskMemoryStore.Lock()
		registrationRiskMemoryStore.items = make(map[string]registrationRiskState)
		registrationRiskMemoryStore.Unlock()
	})

	blocked, retryAfter := memoryRegistrationRiskBlocked([]string{"blocked"})

	require.True(t, blocked)
	assert.Greater(t, retryAfter, time.Duration(0))

	registrationRiskMemoryStore.Lock()
	_, expiredExists := registrationRiskMemoryStore.items["expired"]
	registrationRiskMemoryStore.Unlock()
	assert.False(t, expiredExists)

	keys := make([]string, 0, registrationRiskMemoryMaxItems+2)
	for i := 0; i < registrationRiskMemoryMaxItems+2; i++ {
		keys = append(keys, fmt.Sprintf("key:%d", i))
	}

	memoryRecordRegistrationCodeFailure(keys)

	registrationRiskMemoryStore.Lock()
	itemCount := len(registrationRiskMemoryStore.items)
	registrationRiskMemoryStore.Unlock()
	assert.LessOrEqual(t, itemCount, registrationRiskMemoryMaxItems)
}
