package common

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInMemoryRateLimiterReservationCanRollback(t *testing.T) {
	limiter := InMemoryRateLimiter{}
	limiter.Init(0)

	reservation := limiter.Reserve("user:1", 1, 60)
	require.NotNil(t, reservation)
	assert.Nil(t, limiter.Reserve("user:1", 1, 60))
	reservation.Complete(false)
	next := limiter.Reserve("user:1", 1, 60)
	require.NotNil(t, next)
	next.Complete(true)
	assert.Nil(t, limiter.Reserve("user:1", 1, 60))
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
			if reservation := limiter.Reserve("user:2", 1, 60); reservation != nil {
				atomic.AddInt32(&allowedCount, 1)
			}
		}()
	}
	close(start)
	wait.Wait()

	assert.Equal(t, int32(1), allowedCount)
}
