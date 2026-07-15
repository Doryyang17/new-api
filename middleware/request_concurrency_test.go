package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func withMiddlewareRequestConcurrencySettings(t *testing.T, mode string, userLimit int, tokenLimit int, whitelist []string) {
	t.Helper()
	cfg, ok := config.GlobalConfig.Get("request_risk_setting").(*system_setting.RequestRiskSettings)
	require.True(t, ok)
	previous := *cfg
	previous.GroupWhitelist = append([]string(nil), cfg.GroupWhitelist...)
	*cfg = system_setting.RequestRiskSettings{
		Enabled:               true,
		Mode:                  mode,
		LogMatches:            false,
		UserConcurrencyLimit:  userLimit,
		TokenConcurrencyLimit: tokenLimit,
		GroupWhitelist:        append([]string(nil), whitelist...),
	}
	t.Cleanup(func() {
		*cfg = previous
	})
}

func newRequestConcurrencyTestEngine(userID int, tokenID int, group string, started chan<- struct{}, release <-chan struct{}) *gin.Engine {
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set("id", userID)
		c.Set("token_id", tokenID)
		c.Set("group", group)
		c.Next()
	})
	engine.Use(RequestConcurrencyGuard())
	engine.POST("/v1/chat/completions", func(c *gin.Context) {
		started <- struct{}{}
		<-release
		c.Status(http.StatusOK)
	})
	return engine
}

func performConcurrentRequest(engine *gin.Engine) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5","messages":[]}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	engine.ServeHTTP(resp, req)
	return resp
}

func TestRequestConcurrencyGuardBlocksAndReleasesTokenSlot(t *testing.T) {
	withMiddlewareRequestConcurrencySettings(t, system_setting.RequestRiskModeEnforce, 4, 1, nil)
	oldRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = oldRedisEnabled })

	started := make(chan struct{}, 2)
	release := make(chan struct{})
	engine := newRequestConcurrencyTestEngine(61001, 62001, "default", started, release)

	var first *httptest.ResponseRecorder
	var wait sync.WaitGroup
	wait.Add(1)
	go func() {
		defer wait.Done()
		first = performConcurrentRequest(engine)
	}()
	<-started

	blocked := performConcurrentRequest(engine)
	assert.Equal(t, http.StatusTooManyRequests, blocked.Code)
	assert.Equal(t, "1", blocked.Header().Get("Retry-After"))
	assert.Contains(t, blocked.Body.String(), string(types.ErrorCodeRequestConcurrencyLimited))

	close(release)
	wait.Wait()
	require.NotNil(t, first)
	assert.Equal(t, http.StatusOK, first.Code)

	thirdRelease := make(chan struct{})
	thirdStarted := make(chan struct{}, 1)
	thirdEngine := newRequestConcurrencyTestEngine(61001, 62001, "default", thirdStarted, thirdRelease)
	var third *httptest.ResponseRecorder
	wait.Add(1)
	go func() {
		defer wait.Done()
		third = performConcurrentRequest(thirdEngine)
	}()
	<-thirdStarted
	close(thirdRelease)
	wait.Wait()
	require.NotNil(t, third)
	assert.Equal(t, http.StatusOK, third.Code)
}

func TestRequestConcurrencyGuardObserveModeDoesNotBlock(t *testing.T) {
	withMiddlewareRequestConcurrencySettings(t, system_setting.RequestRiskModeObserve, 1, 1, nil)
	oldRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = oldRedisEnabled })

	started := make(chan struct{}, 2)
	release := make(chan struct{})
	engine := newRequestConcurrencyTestEngine(61002, 62002, "default", started, release)

	responses := make([]*httptest.ResponseRecorder, 2)
	var wait sync.WaitGroup
	for index := range responses {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			responses[index] = performConcurrentRequest(engine)
		}(index)
	}
	<-started
	<-started
	close(release)
	wait.Wait()

	assert.Equal(t, http.StatusOK, responses[0].Code)
	assert.Equal(t, http.StatusOK, responses[1].Code)
}

func TestRequestConcurrencyGuardSkipsWhitelistedGroup(t *testing.T) {
	withMiddlewareRequestConcurrencySettings(t, system_setting.RequestRiskModeEnforce, 1, 1, []string{"trusted"})
	oldRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = oldRedisEnabled })

	started := make(chan struct{}, 2)
	release := make(chan struct{})
	engine := newRequestConcurrencyTestEngine(61003, 62003, "trusted", started, release)

	responses := make([]*httptest.ResponseRecorder, 2)
	var wait sync.WaitGroup
	for index := range responses {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			responses[index] = performConcurrentRequest(engine)
		}(index)
	}
	<-started
	<-started
	close(release)
	wait.Wait()

	assert.Equal(t, http.StatusOK, responses[0].Code)
	assert.Equal(t, http.StatusOK, responses[1].Code)
}
