package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemoryRateLimitSuccessReservationBlocksConcurrentRequest(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set("id", 71001)
		c.Next()
	})
	engine.Use(memoryRateLimitHandler(60, 0, 1))
	engine.GET("/test", func(c *gin.Context) {
		started <- struct{}{}
		<-release
		c.Status(http.StatusOK)
	})

	var first *httptest.ResponseRecorder
	var wait sync.WaitGroup
	wait.Add(1)
	go func() {
		defer wait.Done()
		first = httptest.NewRecorder()
		engine.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/test", nil))
	}()
	<-started

	second := httptest.NewRecorder()
	engine.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/test", nil))
	assert.Equal(t, http.StatusTooManyRequests, second.Code)

	close(release)
	wait.Wait()
	require.NotNil(t, first)
	assert.Equal(t, http.StatusOK, first.Code)
}

func TestMemoryRateLimitFailedRequestRollsBackReservation(t *testing.T) {
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set("id", 71002)
		c.Next()
	})
	engine.Use(memoryRateLimitHandler(60, 0, 1))
	engine.GET("/test", func(c *gin.Context) {
		if c.Query("fail") == "1" {
			c.Status(http.StatusInternalServerError)
			return
		}
		c.Status(http.StatusOK)
	})

	failed := httptest.NewRecorder()
	engine.ServeHTTP(failed, httptest.NewRequest(http.MethodGet, "/test?fail=1", nil))
	succeeded := httptest.NewRecorder()
	engine.ServeHTTP(succeeded, httptest.NewRequest(http.MethodGet, "/test", nil))

	assert.Equal(t, http.StatusInternalServerError, failed.Code)
	assert.Equal(t, http.StatusOK, succeeded.Code)
}

func TestMemoryRateLimitPanickingRequestRollsBackReservation(t *testing.T) {
	engine := gin.New()
	engine.Use(gin.CustomRecovery(func(c *gin.Context, _ any) {
		c.Status(http.StatusInternalServerError)
	}))
	engine.Use(func(c *gin.Context) {
		c.Set("id", 71003)
		c.Next()
	})
	engine.Use(memoryRateLimitHandler(60, 0, 1))
	engine.GET("/panic", func(_ *gin.Context) {
		panic("test panic")
	})
	engine.GET("/success", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	panicked := httptest.NewRecorder()
	engine.ServeHTTP(panicked, httptest.NewRequest(http.MethodGet, "/panic", nil))
	succeeded := httptest.NewRecorder()
	engine.ServeHTTP(succeeded, httptest.NewRequest(http.MethodGet, "/success", nil))

	assert.Equal(t, http.StatusInternalServerError, panicked.Code)
	assert.Equal(t, http.StatusOK, succeeded.Code)
}

func TestModelRedisRateLimitUsesUTCRegardlessOfLocalTimezone(t *testing.T) {
	redisServer, redisClient := useRateLimitMiniRedis(t)
	previousLocation := time.Local
	time.Local = time.FixedZone("test-utc-plus-eight", 8*60*60)
	t.Cleanup(func() { time.Local = previousLocation })

	ctx := context.Background()
	recordKey := "rateLimit:model-utc-record"
	recordRedisRequest(ctx, redisClient, recordKey, 2, 60)
	recorded, err := redisClient.LIndex(ctx, recordKey, 0).Result()
	require.NoError(t, err)
	recordedAt, err := time.Parse(modelRateLimitTimeFormat, recorded)
	require.NoError(t, err)
	assert.WithinDuration(t, time.Now().UTC(), recordedAt, 2*time.Second)

	checkKey := "rateLimit:model-utc-check"
	withinWindow := time.Now().UTC().Add(-30 * time.Second).Format(modelRateLimitTimeFormat)
	_, err = redisServer.Push(checkKey, withinWindow, withinWindow)
	require.NoError(t, err)
	allowed, err := checkRedisRateLimit(ctx, redisClient, checkKey, 2, 60)
	require.NoError(t, err)
	assert.False(t, allowed, "an existing UTC timestamp inside the window must remain limited on a non-UTC host")
}
