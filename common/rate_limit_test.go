package common

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInMemoryRateLimiterReservationCanRollback(t *testing.T) {
	limiter := InMemoryRateLimiter{}
	limiter.Init(0)

	reservation, allowed := limiter.Reserve("user:1", 1, 60)
	assert.True(t, allowed)
	assert.False(t, limiter.Request("user:1", 1, 60))
	assert.True(t, limiter.Rollback("user:1", reservation))
	assert.True(t, limiter.Request("user:1", 1, 60))
}

func TestInMemoryRateLimiterConcurrentReservationsRespectLimit(t *testing.T) {
	limiter := InMemoryRateLimiter{}
	limiter.Init(0)

	start := make(chan struct{})
	var allowedCount int32
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			if _, allowed := limiter.Reserve("user:2", 1, 60); allowed {
				atomic.AddInt32(&allowedCount, 1)
			}
		}()
	}
	close(start)
	wait.Wait()

	assert.Equal(t, int32(1), allowedCount)
}
