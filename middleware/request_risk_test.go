package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	appI18n "github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type trackingReadCloser struct {
	reader    io.Reader
	bytesRead int
}

func (r *trackingReadCloser) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.bytesRead += n
	return n, err
}

func (r *trackingReadCloser) Close() error {
	return nil
}

func withMiddlewareRequestRiskSettings(t *testing.T, mode string, whitelist ...string) {
	t.Helper()
	cfg, ok := config.GlobalConfig.Get("request_risk_setting").(*system_setting.RequestRiskSettings)
	require.True(t, ok)
	previous := *cfg
	previous.GroupWhitelist = append([]string(nil), cfg.GroupWhitelist...)
	*cfg = system_setting.RequestRiskSettings{
		Enabled:               true,
		Mode:                  mode,
		LogMatches:            false,
		MediumCooldownSeconds: 10,
		TokenBlockSeconds:     300,
		UserBlockSeconds:      120,
		IPBlockSeconds:        60,
		GroupWhitelist:        append([]string(nil), whitelist...),
	}
	t.Cleanup(func() {
		*cfg = previous
	})
}

func newRequestRiskTestEngine(userID int, tokenID int, handled *int) *gin.Engine {
	engine := gin.New()
	engine.Use(BodyStorageCleanup())
	engine.Use(func(c *gin.Context) {
		c.Set("id", userID)
		c.Set("token_id", tokenID)
		c.Set("group", "default")
		c.Next()
	})
	engine.Use(RequestRiskGuard())
	engine.POST("/v1/chat/completions", func(c *gin.Context) {
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		if len(body) > 0 {
			(*handled)++
		}
		c.Status(http.StatusOK)
	})
	return engine
}

func performRequestRiskRequest(engine *gin.Engine, model string, text string) *httptest.ResponseRecorder {
	body := `{"model":"` + model + `","messages":[{"role":"user","content":"` + text + `"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	engine.ServeHTTP(resp, req)
	return resp
}

func TestRequestRiskGuardKeepsSingleShortPromptBodyReusable(t *testing.T) {
	withMiddlewareRequestRiskSettings(t, system_setting.RequestRiskModeEnforce)
	oldRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = oldRedisEnabled })

	handled := 0
	engine := newRequestRiskTestEngine(51001, 52001, &handled)
	resp := performRequestRiskRequest(engine, "gpt-a", "你好")

	assert.Equal(t, http.StatusOK, resp.Code)
	assert.Equal(t, 1, handled)
}

func TestRequestRiskFullRequestCapturesJSONBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	payload := `{"model":"gpt-test","seed":9007199254740993,"api_key":"secret-key","messages":[{"role":"user","content":"你好"}]}`
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(payload))
	c.Request.Header.Set("Content-Type", "application/json")
	storage, err := common.GetBodyStorage(c)
	require.NoError(t, err)
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	fullRequest, reason := requestRiskFullRequest(storage, c.Request.Header.Get("Content-Type"))

	assert.JSONEq(t, `{"model":"gpt-test","seed":9007199254740993,"api_key":"[REDACTED]","messages":[{"role":"user","content":"你好"}]}`, fullRequest)
	assert.Contains(t, fullRequest, "9007199254740993")
	assert.NotContains(t, fullRequest, "secret-key")
	assert.Empty(t, reason)
}

func TestRequestRiskFullRequestSanitizesJSONWithoutContentType(t *testing.T) {
	gin.SetMode(gin.TestMode)
	payload := `{"model":"gpt-test","token":"secret-token","messages":[{"role":"user","content":"你好"}]}`
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(payload))
	storage, err := common.GetBodyStorage(c)
	require.NoError(t, err)
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	fullRequest, reason := requestRiskFullRequest(storage, "")

	assert.JSONEq(t, `{"model":"gpt-test","token":"[REDACTED]","messages":[{"role":"user","content":"你好"}]}`, fullRequest)
	assert.NotContains(t, fullRequest, "secret-token")
	assert.Empty(t, reason)
}

func TestRequestRiskFullRequestRejectsMalformedJSONWithoutRawFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	payload := `{"model":"gpt-test","api_key":"secret-key"`
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(payload))
	c.Request.Header.Set("Content-Type", "application/json")
	storage, err := common.GetBodyStorage(c)
	require.NoError(t, err)
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	fullRequest, reason := requestRiskFullRequest(storage, c.Request.Header.Get("Content-Type"))

	assert.Empty(t, fullRequest)
	assert.Contains(t, reason, "格式不正确")
	assert.NotContains(t, reason, "secret-key")
}

func TestRequestRiskFullRequestRejectsJSONShapedBodyWithoutContentType(t *testing.T) {
	gin.SetMode(gin.TestMode)
	payload := `{"model":"gpt-test","api_secret":"secret-value"`
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(payload))
	storage, err := common.GetBodyStorage(c)
	require.NoError(t, err)
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	fullRequest, reason := requestRiskFullRequest(storage, "text/plain")

	assert.Empty(t, fullRequest)
	assert.Contains(t, reason, "疑似 JSON")
	assert.NotContains(t, reason, "secret-value")
}

func TestRequestRiskFullRequestDoesNotRecordFormEncodedSecrets(t *testing.T) {
	gin.SetMode(gin.TestMode)
	payload := `api_key=secret-key&prompt=hello`
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(payload))
	c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	storage, err := common.GetBodyStorage(c)
	require.NoError(t, err)
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	fullRequest, reason := requestRiskFullRequest(storage, c.Request.Header.Get("Content-Type"))

	assert.Empty(t, fullRequest)
	assert.Contains(t, reason, "非 JSON")
	assert.NotContains(t, reason, "secret-key")
}

func TestPopulateRequestRiskProfileDefersLogOnlyDetails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	payload := `{"model":"gpt-test","messages":[{"role":"user","content":"你好"}]}`
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(payload))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	input := service.RequestRiskInput{}

	populateRequestRiskProfile(c, &input)

	assert.Equal(t, "你好", input.Text)
	assert.Empty(t, input.ExtractedText)
	assert.Empty(t, input.FullRequest)

	populateRequestRiskLogDetails(c, &input)

	assert.Equal(t, "你好", input.ExtractedText)
	assert.JSONEq(t, payload, input.FullRequest)
}

func TestRequestRiskFullRequestRejectsExcessiveJSONDepth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	payload := strings.Repeat(`{"nested":`, requestRiskMaxSanitizeDepth+1) + `{"api_key":"secret-value"}` + strings.Repeat("}", requestRiskMaxSanitizeDepth+1)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(payload))
	c.Request.Header.Set("Content-Type", "application/json")
	storage, err := common.GetBodyStorage(c)
	require.NoError(t, err)
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	fullRequest, reason := requestRiskFullRequest(storage, c.Request.Header.Get("Content-Type"))

	assert.Empty(t, fullRequest)
	assert.Contains(t, reason, "嵌套过深")
	assert.NotContains(t, reason, "secret-value")
}

func TestRequestRiskSensitiveFieldCoversCredentialVariants(t *testing.T) {
	tests := []struct {
		key       string
		sensitive bool
	}{
		{key: "api_secret", sensitive: true},
		{key: "auth-token", sensitive: true},
		{key: "id_token", sensitive: true},
		{key: "session_token", sensitive: true},
		{key: "private_key", sensitive: true},
		{key: "x_api_key", sensitive: true},
		{key: "secret_access_key", sensitive: true},
		{key: "credentials", sensitive: true},
		{key: "openai_api_key", sensitive: true},
		{key: "github_token", sensitive: true},
		{key: "aws_secret_access_key", sensitive: true},
		{key: "max_tokens", sensitive: false},
		{key: "token_count", sensitive: false},
	}

	for _, test := range tests {
		t.Run(test.key, func(t *testing.T) {
			assert.Equal(t, test.sensitive, requestRiskSensitiveField(test.key))
		})
	}
}

func TestRequestRiskFullRequestSkipsMultipartBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader("multipart payload"))
	c.Request.Header.Set("Content-Type", "multipart/form-data; boundary=test")
	storage, err := common.GetBodyStorage(c)
	require.NoError(t, err)
	t.Cleanup(func() { common.CleanupBodyStorage(c) })

	fullRequest, reason := requestRiskFullRequest(storage, c.Request.Header.Get("Content-Type"))

	assert.Empty(t, fullRequest)
	assert.Contains(t, reason, "multipart")
}

func TestRequestRiskGuardBlocksRepeatedModelSweep(t *testing.T) {
	withMiddlewareRequestRiskSettings(t, system_setting.RequestRiskModeEnforce)
	oldRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = oldRedisEnabled })

	handled := 0
	engine := newRequestRiskTestEngine(51002, 52002, &handled)
	models := []string{"gpt-a", "gpt-b", "gpt-c", "gpt-d"}
	var resp *httptest.ResponseRecorder
	for _, model := range models {
		resp = performRequestRiskRequest(engine, model, "hi")
	}

	require.NotNil(t, resp)
	assert.Equal(t, http.StatusTooManyRequests, resp.Code)
	assert.NotEmpty(t, resp.Header().Get("Retry-After"))
	assert.Contains(t, resp.Body.String(), string(types.ErrorCodeRequestProbeRateLimited))
	assert.Equal(t, 3, handled)
}

func TestRequestRiskGuardRejectsActiveBlockBeforeReadingBody(t *testing.T) {
	withMiddlewareRequestRiskSettings(t, system_setting.RequestRiskModeEnforce)
	oldRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = oldRedisEnabled })

	handled := 0
	engine := newRequestRiskTestEngine(51008, 52008, &handled)
	for _, model := range []string{"gpt-a", "gpt-b", "gpt-c", "gpt-d"} {
		performRequestRiskRequest(engine, model, "hi")
	}

	payload := `{"model":"gpt-e","messages":[{"role":"user","content":"blocked"}]}`
	body := &trackingReadCloser{reader: strings.NewReader(payload)}
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	request.Body = body
	request.ContentLength = int64(len(payload))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)

	assert.Equal(t, http.StatusTooManyRequests, response.Code)
	assert.Zero(t, body.bytesRead)
	assert.Equal(t, 3, handled)
}

func TestRequestRiskGuardObserveModeDoesNotBlock(t *testing.T) {
	withMiddlewareRequestRiskSettings(t, system_setting.RequestRiskModeObserve)
	oldRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = oldRedisEnabled })

	handled := 0
	engine := newRequestRiskTestEngine(51003, 52003, &handled)
	for _, model := range []string{"gpt-a", "gpt-b", "gpt-c", "gpt-d"} {
		resp := performRequestRiskRequest(engine, model, "test")
		assert.Equal(t, http.StatusOK, resp.Code)
	}
	assert.Equal(t, 4, handled)
}

func newJimengRequestRiskTestEngine(userID int, tokenID int, handled *int) *gin.Engine {
	engine := gin.New()
	engine.Use(BodyStorageCleanup())
	engine.Use(func(c *gin.Context) {
		c.Set("id", userID)
		c.Set("token_id", tokenID)
		c.Set("group", "default")
		c.Next()
	})
	engine.Use(RequestRiskGuard())
	engine.Use(JimengRequestConvert())
	engine.POST("/jimeng/", func(c *gin.Context) {
		(*handled)++
		c.Status(http.StatusOK)
	})
	return engine
}

func TestRequestRiskGuardSeesJimengValidationFailures(t *testing.T) {
	withMiddlewareRequestRiskSettings(t, system_setting.RequestRiskModeEnforce)
	oldRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = oldRedisEnabled })

	handled := 0
	engine := newJimengRequestRiskTestEngine(51006, 52006, &handled)
	body := `{"req_key":"jimeng-test","prompt":"ping"}`
	firstRequest := httptest.NewRequest(http.MethodPost, "/jimeng/", strings.NewReader(body))
	firstRequest.Header.Set("Content-Type", "application/json")
	first := httptest.NewRecorder()
	engine.ServeHTTP(first, firstRequest)

	secondRequest := httptest.NewRequest(http.MethodPost, "/jimeng/", strings.NewReader(body))
	secondRequest.Header.Set("Content-Type", "application/json")
	second := httptest.NewRecorder()
	engine.ServeHTTP(second, secondRequest)

	assert.Equal(t, http.StatusBadRequest, first.Code)
	assert.Equal(t, http.StatusTooManyRequests, second.Code)
	assert.Contains(t, second.Body.String(), string(types.ErrorCodeRequestProbeRateLimited))
	assert.Zero(t, handled)
}

func TestRequestRiskGuardTracksJimengReqKeyModelSweep(t *testing.T) {
	withMiddlewareRequestRiskSettings(t, system_setting.RequestRiskModeEnforce)
	oldRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = oldRedisEnabled })

	handled := 0
	engine := newJimengRequestRiskTestEngine(51007, 52007, &handled)
	var response *httptest.ResponseRecorder
	for _, model := range []string{"jimeng-a", "jimeng-b", "jimeng-c", "jimeng-d"} {
		body := `{"req_key":"` + model + `","prompt":"hi"}`
		request := httptest.NewRequest(http.MethodPost, "/jimeng/?Action=CVSync2AsyncSubmitTask", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		response = httptest.NewRecorder()
		engine.ServeHTTP(response, request)
	}

	require.NotNil(t, response)
	assert.Equal(t, http.StatusTooManyRequests, response.Code)
	assert.Contains(t, response.Body.String(), string(types.ErrorCodeRequestProbeRateLimited))
	assert.Equal(t, 3, handled)
}

func TestPlaygroundRequestedGroupIsResolvedBeforeRiskWhitelist(t *testing.T) {
	withMiddlewareRequestRiskSettings(t, system_setting.RequestRiskModeEnforce, "vip")
	oldRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = oldRedisEnabled })

	handled := 0
	engine := gin.New()
	engine.Use(BodyStorageCleanup())
	engine.Use(func(c *gin.Context) {
		c.Set("id", 51004)
		c.Set("group", "default")
		c.Set("user_group", "default")
		c.Next()
	})
	engine.Use(ResolvePlaygroundGroup())
	engine.Use(RequestRiskGuard())
	engine.POST("/pg/chat/completions", func(c *gin.Context) {
		handled++
		c.Status(http.StatusOK)
	})

	for _, model := range []string{"gpt-a", "gpt-b", "gpt-c", "gpt-d"} {
		body := `{"model":"` + model + `","group":"vip","messages":[{"role":"user","content":"hi"}]}`
		req := httptest.NewRequest(http.MethodPost, "/pg/chat/completions", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp := httptest.NewRecorder()
		engine.ServeHTTP(resp, req)
		assert.Equal(t, http.StatusOK, resp.Code)
	}

	assert.Equal(t, 4, handled)
}

func TestResolvePlaygroundGroupDefersInvalidRequestsToDownstreamProtection(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "malformed json", body: `{"group":`},
		{name: "unauthorized group", body: `{"model":"gpt-a","group":"not-allowed"}`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			protectionReached := false
			engine := gin.New()
			engine.Use(BodyStorageCleanup())
			engine.Use(func(c *gin.Context) {
				c.Set("group", "default")
				c.Set("user_group", "default")
				c.Next()
			})
			engine.Use(ResolvePlaygroundGroup())
			engine.Use(func(c *gin.Context) {
				protectionReached = true
				c.Next()
			})
			engine.POST("/pg/chat/completions", func(c *gin.Context) {
				c.Status(http.StatusBadRequest)
			})

			req := httptest.NewRequest(http.MethodPost, "/pg/chat/completions", strings.NewReader(test.body))
			req.Header.Set("Content-Type", "application/json")
			resp := httptest.NewRecorder()
			engine.ServeHTTP(resp, req)

			assert.True(t, protectionReached)
			assert.Equal(t, http.StatusBadRequest, resp.Code)
		})
	}
}

func TestResolvePlaygroundGroupBoundsPreAdmissionBodyRead(t *testing.T) {
	payload := `{"model":"gpt-a","group":"vip","padding":"` + strings.Repeat("x", playgroundGroupPreResolveMaxBytes) + `"}`
	body := &trackingReadCloser{reader: strings.NewReader(payload)}
	groupBeforeDownstream := ""
	bytesReadBeforeDownstream := 0
	replayedBody := ""

	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set("group", "default")
		c.Set("user_group", "default")
		c.Next()
	})
	engine.Use(ResolvePlaygroundGroup())
	engine.Use(func(c *gin.Context) {
		groupBeforeDownstream = c.GetString("group")
		bytesReadBeforeDownstream = body.bytesRead
		c.Next()
	})
	engine.POST("/pg/chat/completions", func(c *gin.Context) {
		data, err := io.ReadAll(c.Request.Body)
		require.NoError(t, err)
		replayedBody = string(data)
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/pg/chat/completions", nil)
	req.Body = body
	req.ContentLength = int64(len(payload))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	engine.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusOK, resp.Code)
	assert.Equal(t, "default", groupBeforeDownstream)
	assert.Equal(t, playgroundGroupPreResolveMaxBytes+1, bytesReadBeforeDownstream)
	assert.Equal(t, payload, replayedBody)
}

func TestResolvePlaygroundGroupUsesHeaderWithoutReadingLargeBody(t *testing.T) {
	payload := `{"model":"gpt-a","group":"vip","padding":"` + strings.Repeat("x", playgroundGroupPreResolveMaxBytes) + `"}`
	body := &trackingReadCloser{reader: strings.NewReader(payload)}
	resolvedGroup := ""

	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set("group", "default")
		c.Set("user_group", "default")
		c.Next()
	})
	engine.Use(ResolvePlaygroundGroup())
	engine.POST("/pg/chat/completions", func(c *gin.Context) {
		var ok bool
		resolvedGroup, ok = applyPlaygroundGroup(c, c.GetString("group"), "vip")
		require.True(t, ok)
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/pg/chat/completions", nil)
	req.Body = body
	req.ContentLength = int64(len(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(playgroundGroupHeader, "vip")
	resp := httptest.NewRecorder()
	engine.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusOK, resp.Code)
	assert.Equal(t, "vip", resolvedGroup)
	assert.Zero(t, body.bytesRead)
}

func TestApplyPlaygroundGroupRejectsHeaderBodyMismatch(t *testing.T) {
	require.NoError(t, appI18n.Init())
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPost, "/pg/chat/completions", strings.NewReader(`{"group":"default"}`))
	context.Set(playgroundPreResolvedGroupContext, "vip")

	group, ok := applyPlaygroundGroup(context, "vip", "default")

	assert.False(t, ok)
	assert.Empty(t, group)
	assert.Equal(t, http.StatusBadRequest, recorder.Code)
}

func TestRequestRiskGuardRecordsFailuresBeforeChannelSelection(t *testing.T) {
	withMiddlewareRequestRiskSettings(t, system_setting.RequestRiskModeEnforce)
	promptSettings, ok := config.GlobalConfig.Get("prompt_filter_setting").(*system_setting.PromptFilterSettings)
	require.True(t, ok)
	previousPromptSettings := *promptSettings
	promptSettings.BlockStatusCode = http.StatusBadRequest
	t.Cleanup(func() { *promptSettings = previousPromptSettings })
	oldRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = oldRedisEnabled })

	handled := 0
	engine := gin.New()
	engine.Use(BodyStorageCleanup())
	engine.Use(func(c *gin.Context) {
		c.Set("id", 51005)
		c.Set("token_id", 52005)
		c.Set("group", "default")
		c.Next()
	})
	engine.Use(RequestRiskGuard())
	engine.POST("/v1/chat/completions", func(c *gin.Context) {
		handled++
		c.Status(http.StatusBadRequest)
	})

	first := performRequestRiskRequest(engine, "missing-model", "repeat this request")
	second := performRequestRiskRequest(engine, "missing-model", "repeat this request")
	third := performRequestRiskRequest(engine, "missing-model", "repeat this request")

	assert.Equal(t, http.StatusBadRequest, first.Code)
	assert.Equal(t, http.StatusBadRequest, second.Code)
	assert.Equal(t, http.StatusTooManyRequests, third.Code)
	assert.Equal(t, 2, handled)
}

func TestShouldRecordRequestRiskFailureUsesPromptFilterMarker(t *testing.T) {
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{}`))
	context.Status(http.StatusBadRequest)

	assert.True(t, shouldRecordRequestRiskFailure(context))
	common.SetContextKey(context, constant.ContextKeyPromptFilterBlocked, true)
	assert.False(t, shouldRecordRequestRiskFailure(context))
}

func TestWriteRequestProtectionResponseUsesMatchedRouteAfterAdapterRewrite(t *testing.T) {
	for _, route := range []string{
		"/kling/v1/videos/text2video",
		"/jimeng/",
	} {
		t.Run(route, func(t *testing.T) {
			engine := gin.New()
			engine.POST(route, func(c *gin.Context) {
				c.Request.URL.Path = "/v1/video/generations"
				writeRequestProtectionResponse(
					c,
					time.Second,
					"请求过于频繁",
					types.ErrorCodeRequestProbeRateLimited,
				)
			})

			request := httptest.NewRequest(http.MethodPost, route, strings.NewReader(`{}`))
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, request)

			var payload map[string]interface{}
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &payload))
			assert.Equal(t, http.StatusTooManyRequests, response.Code)
			assert.Equal(t, string(types.ErrorCodeRequestProbeRateLimited), payload["code"])
			assert.NotEmpty(t, payload["message"])
			assert.NotContains(t, payload, "error")
		})
	}
}

func TestIsModelGenerationRequestSkipsStatusQueries(t *testing.T) {
	tests := []struct {
		method   string
		path     string
		rawQuery string
		want     bool
	}{
		{method: http.MethodPost, path: "/v1/chat/completions", want: true},
		{method: http.MethodGet, path: "/v1/realtime", want: true},
		{method: http.MethodGet, path: "/v1/videos/task-id", want: false},
		{method: http.MethodPost, path: "/suno/fetch", want: false},
		{method: http.MethodPost, path: "/mj/task/list-by-condition", want: false},
		{method: http.MethodPost, path: "/jimeng/", rawQuery: "Action=CVSync2AsyncGetResult", want: false},
		{method: http.MethodPost, path: "/jimeng/", rawQuery: "Action=FooGetResult", want: true},
	}

	for _, test := range tests {
		t.Run(test.method+" "+test.path, func(t *testing.T) {
			context, _ := gin.CreateTestContext(httptest.NewRecorder())
			context.Request = httptest.NewRequest(test.method, test.path, strings.NewReader("{}"))
			context.Request.URL.RawQuery = test.rawQuery
			assert.Equal(t, test.want, isModelGenerationRequest(context))
		})
	}
}

func TestIsRequestConcurrencyCandidateIncludesRealtime(t *testing.T) {
	tests := []struct {
		method string
		path   string
		want   bool
	}{
		{method: http.MethodGet, path: "/v1/realtime", want: true},
		{method: http.MethodPost, path: "/v1/chat/completions", want: true},
		{method: http.MethodGet, path: "/v1/videos/task-id", want: false},
	}

	for _, test := range tests {
		t.Run(test.method+" "+test.path, func(t *testing.T) {
			context, _ := gin.CreateTestContext(httptest.NewRecorder())
			context.Request = httptest.NewRequest(test.method, test.path, strings.NewReader("{}"))
			assert.Equal(t, test.want, isRequestConcurrencyCandidate(context))
		})
	}
}
