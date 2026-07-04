package middleware

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type noBytesBodyStorage struct {
	*bytes.Reader
	size        int64
	bytesCalled bool
}

func (s *noBytesBodyStorage) Bytes() ([]byte, error) {
	s.bytesCalled = true
	return nil, errors.New("Bytes must not be called")
}

func (s *noBytesBodyStorage) Size() int64 {
	return s.size
}

func (s *noBytesBodyStorage) IsDisk() bool {
	return true
}

func (s *noBytesBodyStorage) Close() error {
	return nil
}

type trackingBodyStorage struct {
	*bytes.Reader
	data   []byte
	closed bool
}

func newTrackingBodyStorage(data []byte) *trackingBodyStorage {
	return &trackingBodyStorage{
		Reader: bytes.NewReader(data),
		data:   data,
	}
}

func (s *trackingBodyStorage) Bytes() ([]byte, error) {
	return s.data, nil
}

func (s *trackingBodyStorage) Size() int64 {
	return int64(len(s.data))
}

func (s *trackingBodyStorage) IsDisk() bool {
	return false
}

func (s *trackingBodyStorage) Close() error {
	s.closed = true
	return nil
}

func withPromptComplianceSettings(t *testing.T, enabled bool) {
	t.Helper()
	setupPromptComplianceLogDB(t)
	oldEnabled := setting.CheckSensitiveEnabled
	oldPromptEnabled := setting.CheckSensitiveOnPromptEnabled
	oldWords := append([]string(nil), setting.SensitiveWords...)
	oldConfig := config.GlobalConfig.ExportAllConfigs()
	setting.CheckSensitiveEnabled = enabled
	setting.CheckSensitiveOnPromptEnabled = enabled
	setting.SensitiveWords = nil
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"prompt_filter_setting.mode":              system_setting.DefaultPromptFilterMode,
		"prompt_filter_setting.threshold":         "50",
		"prompt_filter_setting.strict_threshold":  "90",
		"prompt_filter_setting.max_text_length":   "81920",
		"prompt_filter_setting.message":           system_setting.DefaultPromptFilterMessage,
		"prompt_filter_setting.block_status_code": "460",
		"prompt_filter_setting.block_error_code":  system_setting.DefaultPromptFilterBlockErrorCode,
		"prompt_filter_setting.custom_patterns":   "[]",
		"prompt_filter_setting.disabled_patterns": "[]",
		"prompt_filter_setting.lexicon_files":     "[]",
	}))
	t.Cleanup(func() {
		setting.CheckSensitiveEnabled = oldEnabled
		setting.CheckSensitiveOnPromptEnabled = oldPromptEnabled
		setting.SensitiveWords = oldWords
		require.NoError(t, config.GlobalConfig.LoadFromDB(oldConfig))
	})
}

func setupPromptComplianceLogDB(t *testing.T) {
	t.Helper()
	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalMainDatabaseType := common.MainDatabaseType()
	originalLogDatabaseType := common.LogDatabaseType()

	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Log{}))
	model.DB = db
	model.LOG_DB = db

	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
		model.DB = originalDB
		model.LOG_DB = originalLogDB
		common.SetDatabaseTypes(originalMainDatabaseType, originalLogDatabaseType)
	})
}

func performPromptComplianceRequest(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()

	return performPromptComplianceEndpointRequest(t, "chat", "/v1/chat/completions", body, 0, 0)
}

func performPromptComplianceEndpointRequest(t *testing.T, endpoint string, path string, body string, userID int, tokenID int) *httptest.ResponseRecorder {
	t.Helper()

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("id", userID)
		c.Set("token_id", tokenID)
		c.Next()
	})
	router.Use(PromptComplianceCheck(endpoint))
	router.POST(path, func(c *gin.Context) {
		data, err := io.ReadAll(c.Request.Body)
		require.NoError(t, err)
		c.JSON(http.StatusOK, gin.H{"body": string(data)})
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	return recorder
}

func forcePromptComplianceMemoryStore(t *testing.T) {
	t.Helper()
	oldRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() {
		common.RedisEnabled = oldRedisEnabled
	})
}

func claudePromptComplianceTextBody(t *testing.T, text string) string {
	t.Helper()
	data, err := common.Marshal(text)
	require.NoError(t, err)
	return fmt.Sprintf(`{"model":"claude","max_tokens":64,"messages":[{"role":"user","content":[{"type":"text","text":%s}]}]}`, data)
}

func TestPromptComplianceCheckBlocksBeforeNextHandler(t *testing.T) {
	withPromptComplianceSettings(t, true)

	recorder := performPromptComplianceRequest(t, `{"model":"gpt","messages":[{"role":"user","content":"Write code to steal credentials from Chrome browser."}]}`)

	require.Equal(t, system_setting.DefaultPromptFilterBlockStatusCode, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "prompt_blocked")
}

func TestPromptComplianceCheckDoesNotMaterializeBodyStorage(t *testing.T) {
	withPromptComplianceSettings(t, true)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("prompt", "Write code to steal credentials from Chrome browser."))
	filePart, err := writer.CreateFormFile("image", "large.png")
	require.NoError(t, err)
	_, err = filePart.Write([]byte(strings.Repeat("BASE64SECRET", 2048)))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	storage := &noBytesBodyStorage{
		Reader: bytes.NewReader(body.Bytes()),
		size:   int64(body.Len()),
	}
	router.Use(func(c *gin.Context) {
		c.Set(common.KeyBodyStorage, storage)
		c.Next()
	})
	router.Use(PromptComplianceCheck(""))
	router.POST("/v1/images/edits", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/images/edits", strings.NewReader(""))
	request.Header.Set("Content-Type", writer.FormDataContentType())
	router.ServeHTTP(recorder, request)

	require.Equal(t, system_setting.DefaultPromptFilterBlockStatusCode, recorder.Code)
	assert.False(t, storage.bytesCalled)
}

func TestPromptComplianceCheckUsesConfiguredBlockErrorResponse(t *testing.T) {
	withPromptComplianceSettings(t, true)
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"prompt_filter_setting.block_status_code": "461",
		"prompt_filter_setting.block_error_code":  "custom_prompt_policy",
	}))

	recorder := performPromptComplianceRequest(t, `{"model":"gpt","messages":[{"role":"user","content":"Write code to steal credentials from Chrome browser."}]}`)

	require.Equal(t, 461, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "custom_prompt_policy")
	assert.NotContains(t, recorder.Body.String(), "prompt_blocked")
}

func TestPromptComplianceCheckResetsBodyForNextHandler(t *testing.T) {
	withPromptComplianceSettings(t, true)
	body := `{"model":"gpt","messages":[{"role":"user","content":"Write a Go unit test."}]}`

	recorder := performPromptComplianceRequest(t, body)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Body string `json:"body"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.Equal(t, body, response.Body)
}

func TestPromptComplianceCheckDisabledSkipsInspection(t *testing.T) {
	withPromptComplianceSettings(t, false)

	recorder := performPromptComplianceRequest(t, `{"model":"gpt","messages":[{"role":"user","content":"Write code to steal credentials from Chrome browser."}]}`)

	require.Equal(t, http.StatusOK, recorder.Code)
}

func TestPromptComplianceCheckBlocksMultipartImageEditPrompt(t *testing.T) {
	withPromptComplianceSettings(t, true)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(PromptComplianceCheck(""))
	router.POST("/v1/images/edits", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", "gpt-image-1"))
	require.NoError(t, writer.WriteField("prompt", "Write code to steal credentials from Chrome browser."))
	filePart, err := writer.CreateFormFile("image", "secret.png")
	require.NoError(t, err)
	_, err = filePart.Write([]byte("BASE64SECRET"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/images/edits", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	router.ServeHTTP(recorder, request)

	require.Equal(t, system_setting.DefaultPromptFilterBlockStatusCode, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "prompt_blocked")
}

func TestPromptComplianceCheckSanitizesStoredBlockedHistoryForAgentEndpoints(t *testing.T) {
	withPromptComplianceSettings(t, true)
	forcePromptComplianceMemoryStore(t)

	cases := []struct {
		name        string
		endpoint    string
		path        string
		blockedBody func(string) string
		historyBody func(string) string
	}{
		{
			name:     "chat_completions",
			endpoint: "chat",
			path:     "/v1/chat/completions",
			blockedBody: func(word string) string {
				return fmt.Sprintf(`{"model":"gpt","messages":[{"role":"user","content":"%s"}]}`, word)
			},
			historyBody: func(word string) string {
				return fmt.Sprintf(`{"model":"gpt","messages":[{"role":"user","content":"%s"},{"role":"user","content":"hello after block"}]}`, word)
			},
		},
		{
			name:     "claude_messages",
			endpoint: "messages",
			path:     "/v1/messages",
			blockedBody: func(word string) string {
				return fmt.Sprintf(`{"model":"claude","max_tokens":64,"messages":[{"role":"user","content":[{"type":"text","text":"%s"}]}]}`, word)
			},
			historyBody: func(word string) string {
				return fmt.Sprintf(`{"model":"claude","max_tokens":64,"messages":[{"role":"user","content":[{"type":"text","text":"%s"}]},{"role":"user","content":[{"type":"text","text":"hello after block"}]}]}`, word)
			},
		},
		{
			name:     "responses",
			endpoint: "responses",
			path:     "/v1/responses",
			blockedBody: func(word string) string {
				return fmt.Sprintf(`{"model":"gpt","input":[{"role":"user","content":[{"type":"input_text","text":"%s"}]}]}`, word)
			},
			historyBody: func(word string) string {
				return fmt.Sprintf(`{"model":"gpt","input":[{"role":"user","content":[{"type":"input_text","text":"%s"}]},{"role":"user","content":[{"type":"input_text","text":"hello after block"}]}]}`, word)
			},
		},
	}

	for index, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			word := fmt.Sprintf("blocked_history_%s_%d", tc.name, index)
			setting.SensitiveWords = []string{word}

			blockedRecorder := performPromptComplianceEndpointRequest(t, tc.endpoint, tc.path, tc.blockedBody(word), 0, 0)
			require.Equal(t, system_setting.DefaultPromptFilterBlockStatusCode, blockedRecorder.Code)

			recoveredRecorder := performPromptComplianceEndpointRequest(t, tc.endpoint, tc.path, tc.historyBody(word), 0, 0)
			require.Equal(t, http.StatusOK, recoveredRecorder.Code)
			assert.Equal(t, "1", recoveredRecorder.Header().Get("X-Prompt-Filter-History-Sanitized"))

			var response struct {
				Body string `json:"body"`
			}
			require.NoError(t, common.Unmarshal(recoveredRecorder.Body.Bytes(), &response))
			assert.NotContains(t, response.Body, word)
			assert.Contains(t, response.Body, "hello after block")
		})
	}
}

func TestRecoverPromptFilterBlockedHistoryClosesPreviousBodyStorage(t *testing.T) {
	withPromptComplianceSettings(t, true)
	forcePromptComplianceMemoryStore(t)
	setting.SensitiveWords = []string{"close_storage_blocked"}
	userID := 90101
	tokenID := 90102
	service.RecordPromptFilterBlockedMessage(userID, tokenID, "close_storage_blocked", []service.PromptFilterMatch{{Term: "close_storage_blocked"}})

	body := []byte(`{"model":"claude","max_tokens":64,"messages":[{"role":"user","content":[{"type":"text","text":"close_storage_blocked"}]},{"role":"user","content":[{"type":"text","text":"hello after block"}]}]}`)
	storage := newTrackingBodyStorage(body)
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("id", userID)
	c.Set("token_id", tokenID)

	require.True(t, recoverPromptFilterBlockedHistory(c, storage, "messages", service.PromptFilterVerdict{}))
	assert.True(t, storage.closed)
	assert.Equal(t, "1", recorder.Header().Get("X-Prompt-Filter-History-Sanitized"))
	common.CleanupBodyStorage(c)
}

func TestPromptComplianceCheckSanitizesAdjacentSyntheticAssistantAPIError460(t *testing.T) {
	withPromptComplianceSettings(t, true)
	forcePromptComplianceMemoryStore(t)

	cases := []struct {
		name        string
		endpoint    string
		path        string
		blockedBody func(string) string
		historyBody func(string) string
	}{
		{
			name:     "chat_completions",
			endpoint: "chat",
			path:     "/v1/chat/completions",
			blockedBody: func(word string) string {
				return fmt.Sprintf(`{"model":"gpt","messages":[{"role":"user","content":"%s"}]}`, word)
			},
			historyBody: func(word string) string {
				return fmt.Sprintf(`{"model":"gpt","messages":[{"role":"user","content":"%s"},{"role":"assistant","content":"API Error: 460 request blocked by %s"},{"role":"user","content":"hello after block"}]}`, word, word)
			},
		},
		{
			name:     "claude_messages",
			endpoint: "messages",
			path:     "/v1/messages",
			blockedBody: func(word string) string {
				return fmt.Sprintf(`{"model":"claude","max_tokens":64,"messages":[{"role":"user","content":[{"type":"text","text":"%s"}]}]}`, word)
			},
			historyBody: func(word string) string {
				return fmt.Sprintf(`{"model":"claude","max_tokens":64,"messages":[{"role":"user","content":[{"type":"text","text":"%s"}]},{"role":"assistant","content":[{"type":"text","text":"API Error: 460 request blocked by %s"}]},{"role":"user","content":[{"type":"text","text":"hello after block"}]}]}`, word, word)
			},
		},
		{
			name:     "responses",
			endpoint: "responses",
			path:     "/v1/responses",
			blockedBody: func(word string) string {
				return fmt.Sprintf(`{"model":"gpt","input":[{"role":"user","content":[{"type":"input_text","text":"%s"}]}]}`, word)
			},
			historyBody: func(word string) string {
				return fmt.Sprintf(`{"model":"gpt","input":[{"role":"user","content":[{"type":"input_text","text":"%s"}]},{"role":"assistant","content":[{"type":"output_text","text":"API Error: 460 request blocked by %s"}]},{"role":"user","content":[{"type":"input_text","text":"hello after block"}]}]}`, word, word)
			},
		},
	}

	for index, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			word := fmt.Sprintf("blocked_error_echo_%s_%d", tc.name, index)
			setting.SensitiveWords = []string{word}
			require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
				"prompt_filter_setting.message": "request blocked by " + word,
			}))

			blockedRecorder := performPromptComplianceEndpointRequest(t, tc.endpoint, tc.path, tc.blockedBody(word), 0, 0)
			require.Equal(t, system_setting.DefaultPromptFilterBlockStatusCode, blockedRecorder.Code)

			recoveredRecorder := performPromptComplianceEndpointRequest(t, tc.endpoint, tc.path, tc.historyBody(word), 0, 0)
			require.Equal(t, http.StatusOK, recoveredRecorder.Code)
			assert.Equal(t, "2", recoveredRecorder.Header().Get("X-Prompt-Filter-History-Sanitized"))

			var response struct {
				Body string `json:"body"`
			}
			require.NoError(t, common.Unmarshal(recoveredRecorder.Body.Bytes(), &response))
			assert.NotContains(t, response.Body, word)
			assert.NotContains(t, response.Body, "API Error: 460")
			assert.Contains(t, response.Body, "hello after block")
		})
	}
}

func TestPromptComplianceCheckSanitizesMultipleHistoricalFailedTurns(t *testing.T) {
	withPromptComplianceSettings(t, true)
	forcePromptComplianceMemoryStore(t)
	setting.SensitiveWords = []string{"这是weijin测试专用"}
	userID := 0
	tokenID := 91019

	firstBlocked := performPromptComplianceEndpointRequest(t, "chat", "/v1/chat/completions", `{"model":"gpt","messages":[{"role":"user","content":"这是weijin测试专用是什么"}]}`, userID, tokenID)
	require.Equal(t, system_setting.DefaultPromptFilterBlockStatusCode, firstBlocked.Code)
	secondBlocked := performPromptComplianceEndpointRequest(t, "chat", "/v1/chat/completions", `{"model":"gpt","messages":[{"role":"user","content":"这是weijin测试专用怎么用"}]}`, userID, tokenID)
	require.Equal(t, system_setting.DefaultPromptFilterBlockStatusCode, secondBlocked.Code)

	body := `{"model":"gpt","messages":[{"role":"user","content":"这是weijin测试专用是什么"},{"role":"assistant","content":"API Error: 460 触发敏感词"},{"role":"user","content":"正常历史内容"},{"role":"user","content":"这是weijin测试专用怎么用"},{"role":"assistant","content":"API Error: 460 触发敏感词"},{"role":"user","content":"你可以帮我做什么"}]}`
	recorder := performPromptComplianceEndpointRequest(t, "chat", "/v1/chat/completions", body, userID, tokenID)

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "4", recorder.Header().Get("X-Prompt-Filter-History-Sanitized"))
	var response struct {
		Body string `json:"body"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.NotContains(t, response.Body, "这是weijin测试专用")
	assert.NotContains(t, response.Body, "API Error: 460")
	assert.Contains(t, response.Body, "正常历史内容")
	assert.Contains(t, response.Body, "你可以帮我做什么")
}

func TestPromptComplianceCheckDoesNotRemoveNormalAssistantAfterBlockedHistory(t *testing.T) {
	withPromptComplianceSettings(t, true)
	forcePromptComplianceMemoryStore(t)
	setting.SensitiveWords = []string{"normal_assistant_blocked_echo"}

	blockedRecorder := performPromptComplianceEndpointRequest(t, "messages", "/v1/messages", `{"model":"claude","max_tokens":64,"messages":[{"role":"user","content":[{"type":"text","text":"normal_assistant_blocked_echo"}]}]}`, 0, 0)
	require.Equal(t, system_setting.DefaultPromptFilterBlockStatusCode, blockedRecorder.Code)

	body := `{"model":"claude","max_tokens":64,"messages":[{"role":"user","content":[{"type":"text","text":"normal_assistant_blocked_echo"}]},{"role":"assistant","content":[{"type":"text","text":"normal assistant repeats normal_assistant_blocked_echo"}]},{"role":"user","content":[{"type":"text","text":"hello after block"}]}]}`
	recorder := performPromptComplianceEndpointRequest(t, "messages", "/v1/messages", body, 0, 0)

	require.Equal(t, system_setting.DefaultPromptFilterBlockStatusCode, recorder.Code)
	assert.Empty(t, recorder.Header().Get("X-Prompt-Filter-History-Sanitized"))
}

func TestPromptComplianceCheckDoesNotSanitizeBlockedLineBundledIntoCurrentUser(t *testing.T) {
	withPromptComplianceSettings(t, true)
	forcePromptComplianceMemoryStore(t)
	setting.SensitiveWords = []string{"bundled_current_blocked_line"}

	blockedRecorder := performPromptComplianceEndpointRequest(t, "messages", "/v1/messages", `{"model":"claude","max_tokens":64,"messages":[{"role":"user","content":[{"type":"text","text":"bundled_current_blocked_line"}]}]}`, 0, 0)
	require.Equal(t, system_setting.DefaultPromptFilterBlockStatusCode, blockedRecorder.Code)

	body := `{"model":"claude","max_tokens":64,"messages":[{"role":"user","content":[{"type":"text","text":"bundled_current_blocked_line\nhello after block"}]}]}`
	recorder := performPromptComplianceEndpointRequest(t, "messages", "/v1/messages", body, 0, 0)

	require.Equal(t, system_setting.DefaultPromptFilterBlockStatusCode, recorder.Code)
	assert.Empty(t, recorder.Header().Get("X-Prompt-Filter-History-Sanitized"))
}

func TestPromptComplianceCheckDoesNotRemoveCurrentTextBlocksDuringRecovery(t *testing.T) {
	withPromptComplianceSettings(t, true)
	forcePromptComplianceMemoryStore(t)
	setting.SensitiveWords = []string{"empty_text_blocked_line"}

	blockedRecorder := performPromptComplianceEndpointRequest(t, "messages", "/v1/messages", `{"model":"claude","max_tokens":64,"messages":[{"role":"user","content":[{"type":"text","text":"empty_text_blocked_line"}]}]}`, 0, 0)
	require.Equal(t, system_setting.DefaultPromptFilterBlockStatusCode, blockedRecorder.Code)

	body := `{"model":"claude","max_tokens":64,"messages":[{"role":"user","content":[{"type":"text","text":"context"},{"type":"text","text":"empty_text_blocked_line"},{"type":"text","text":"hello after block"}]}]}`
	recorder := performPromptComplianceEndpointRequest(t, "messages", "/v1/messages", body, 0, 0)

	require.Equal(t, system_setting.DefaultPromptFilterBlockStatusCode, recorder.Code)
	assert.Empty(t, recorder.Header().Get("X-Prompt-Filter-History-Sanitized"))
}

func TestPromptComplianceCheckRemovesClaudeSystemMessagesDuringRecovery(t *testing.T) {
	withPromptComplianceSettings(t, true)
	forcePromptComplianceMemoryStore(t)
	setting.SensitiveWords = []string{"claude_system_blocked_history"}

	blockedRecorder := performPromptComplianceEndpointRequest(t, "messages", "/v1/messages", `{"model":"claude","max_tokens":64,"messages":[{"role":"user","content":[{"type":"text","text":"claude_system_blocked_history"}]}]}`, 0, 0)
	require.Equal(t, system_setting.DefaultPromptFilterBlockStatusCode, blockedRecorder.Code)

	body := `{"model":"claude","max_tokens":64,"messages":[{"role":"user","content":[{"type":"text","text":"claude_system_blocked_history"}]},{"role":"user","content":[{"type":"text","text":"hello after block"}]},{"role":"system","content":"agent metadata"}]}`
	recorder := performPromptComplianceEndpointRequest(t, "messages", "/v1/messages", body, 0, 0)

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "2", recorder.Header().Get("X-Prompt-Filter-History-Sanitized"))
	var response struct {
		Body string `json:"body"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.NotContains(t, response.Body, "claude_system_blocked_history")
	assert.NotContains(t, response.Body, `"role":"system"`)
	assert.Contains(t, response.Body, "hello after block")
}

func TestPromptComplianceCheckDoesNotRecoverByDroppingClaudeSystemOnly(t *testing.T) {
	withPromptComplianceSettings(t, true)
	forcePromptComplianceMemoryStore(t)
	setting.SensitiveWords = []string{"claude_system_only_blocked"}

	body := `{"model":"claude","max_tokens":64,"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]},{"role":"system","content":"claude_system_only_blocked"}]}`
	recorder := performPromptComplianceEndpointRequest(t, "messages", "/v1/messages", body, 0, 0)

	require.Equal(t, system_setting.DefaultPromptFilterBlockStatusCode, recorder.Code)
	assert.Empty(t, recorder.Header().Get("X-Prompt-Filter-History-Sanitized"))
}

func TestPromptComplianceCheckDoesNotSanitizeCurrentBlockedMessage(t *testing.T) {
	withPromptComplianceSettings(t, true)
	forcePromptComplianceMemoryStore(t)
	setting.SensitiveWords = []string{"blocked_history_current", "blocked_current_message"}

	blockedRecorder := performPromptComplianceEndpointRequest(t, "chat", "/v1/chat/completions", `{"model":"gpt","messages":[{"role":"user","content":"blocked_history_current"}]}`, 0, 0)
	require.Equal(t, system_setting.DefaultPromptFilterBlockStatusCode, blockedRecorder.Code)

	currentBlockedRecorder := performPromptComplianceEndpointRequest(t, "chat", "/v1/chat/completions", `{"model":"gpt","messages":[{"role":"user","content":"blocked_history_current"},{"role":"user","content":"blocked_current_message"}]}`, 0, 0)
	require.Equal(t, system_setting.DefaultPromptFilterBlockStatusCode, currentBlockedRecorder.Code)
	assert.Empty(t, currentBlockedRecorder.Header().Get("X-Prompt-Filter-History-Sanitized"))
}

func TestPromptComplianceCheckSanitizesBlockedHistoryEmbeddedInFlattenedCurrentUserMessage(t *testing.T) {
	withPromptComplianceSettings(t, true)
	forcePromptComplianceMemoryStore(t)
	setting.SensitiveWords = []string{"这是weijin测试专用"}
	userID := 0
	tokenID := 91002

	blockedRecorder := performPromptComplianceEndpointRequest(t, "messages", "/v1/messages", claudePromptComplianceTextBody(t, "这是weijin测试专用是什么"), userID, tokenID)
	require.Equal(t, system_setting.DefaultPromptFilterBlockStatusCode, blockedRecorder.Code)

	flattenedContext := strings.Repeat("context ", 90) +
		`你好！有什么我可以帮你的吗？ 这是weijin测试专用是什么 你可以帮我做什么 ` +
		`x-anthropic-billing-header: cc_version=2.1.198; cc_entrypoint=cli; You are Claude Code, Anthropic's official CLI for Claude.`
	body := `{"model":"claude","max_tokens":64,"messages":[{"role":"assistant","content":[{"type":"text","text":"API Error: 460 触发敏感词"}]},{"role":"user","content":[{"type":"text","text":"` + flattenedContext + `"}]}]}`
	recorder := performPromptComplianceEndpointRequest(t, "messages", "/v1/messages", body, userID, tokenID)

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "2", recorder.Header().Get("X-Prompt-Filter-History-Sanitized"))
	var response struct {
		Body string `json:"body"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.NotContains(t, response.Body, "这是weijin测试专用")
	assert.NotContains(t, response.Body, "API Error: 460")
	assert.Contains(t, response.Body, "你可以帮我做什么")
}

func TestPromptComplianceCheckSanitizesClaudeCodeFlattenedHistoryWithoutAPIErrorEcho(t *testing.T) {
	withPromptComplianceSettings(t, true)
	forcePromptComplianceMemoryStore(t)
	setting.SensitiveWords = []string{"这是weijin测试专用"}
	userID := 0
	tokenID := 91020

	blockedRecorder := performPromptComplianceEndpointRequest(t, "messages", "/v1/messages", claudePromptComplianceTextBody(t, "这是weijin测试专用是什么"), userID, tokenID)
	require.Equal(t, system_setting.DefaultPromptFilterBlockStatusCode, blockedRecorder.Code)

	flattenedContext := strings.Repeat("context ", 90) +
		`你好！有什么我可以帮你的吗？ 这是weijin测试专用是什么 你可以帮我做什么 ` +
		`x-anthropic-billing-header: cc_version=2.1.198; cc_entrypoint=sdk-cli; You are a Claude agent, built on Anthropic's Claude Agent SDK.`
	body := `{"model":"claude","max_tokens":64,"messages":[{"role":"user","content":[{"type":"text","text":"` + flattenedContext + `"}]}]}`
	recorder := performPromptComplianceEndpointRequest(t, "messages", "/v1/messages", body, userID, tokenID)

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "1", recorder.Header().Get("X-Prompt-Filter-History-Sanitized"))
	var response struct {
		Body string `json:"body"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.NotContains(t, response.Body, "这是weijin测试专用")
	assert.Contains(t, response.Body, "你可以帮我做什么")
}

func TestPromptComplianceCheckRecordsBlockedPhraseFromClaudeCodeFlattenedRequest(t *testing.T) {
	withPromptComplianceSettings(t, true)
	forcePromptComplianceMemoryStore(t)
	setting.SensitiveWords = []string{"这是weijin测试专用"}
	userID := 0
	tokenID := 91022

	firstFlattened := strings.Repeat("context ", 90) +
		`你好！有什么我可以帮你的吗？ 这是weijin测试专用是什么 ` +
		`x-anthropic-billing-header: cc_version=2.1.198; cc_entrypoint=sdk-cli; You are a Claude agent, built on Anthropic's Claude Agent SDK.`
	firstBody := `{"model":"claude","max_tokens":64,"messages":[{"role":"user","content":[{"type":"text","text":"` + firstFlattened + `"}]}]}`
	blockedRecorder := performPromptComplianceEndpointRequest(t, "messages", "/v1/messages", firstBody, userID, tokenID)
	require.Equal(t, system_setting.DefaultPromptFilterBlockStatusCode, blockedRecorder.Code)

	secondFlattened := strings.Repeat("context ", 90) +
		`你好！有什么我可以帮你的吗？ The following deferred tools are now available via ToolSearch. Their schemas are NOT loaded. ` +
		`The user is greeting me in Chinese. 你好！有什么我可以帮你的吗？ 这是weijin测试专用是什么 你可以帮我做什么 ` +
		`x-anthropic-billing-header: cc_version=2.1.198; cc_entrypoint=sdk-cli; You are a Claude agent, built on Anthropic's Claude Agent SDK.`
	secondBody := `{"model":"claude","max_tokens":64,"messages":[{"role":"user","content":[{"type":"text","text":"` + secondFlattened + `"}]}]}`
	recorder := performPromptComplianceEndpointRequest(t, "messages", "/v1/messages", secondBody, userID, tokenID)

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "1", recorder.Header().Get("X-Prompt-Filter-History-Sanitized"))
	var response struct {
		Body string `json:"body"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.NotContains(t, response.Body, "这是weijin测试专用")
	assert.Contains(t, response.Body, "你可以帮我做什么")
}

func TestPromptComplianceCheckDoesNotSanitizeClaudeCodeFlattenedCurrentMatchWithoutRecordedHistory(t *testing.T) {
	withPromptComplianceSettings(t, true)
	forcePromptComplianceMemoryStore(t)
	setting.SensitiveWords = []string{"这是weijin测试专用"}

	flattenedContext := strings.Repeat("context ", 90) +
		`你好！有什么我可以帮你的吗？ The following deferred tools are now available via ToolSearch. Their schemas are NOT loaded. ` +
		`The user is greeting me in Chinese. 你好！有什么我可以帮你的吗？ 这是weijin测试专用是什么 你可以帮我做什么 ` +
		`x-anthropic-billing-header: cc_version=2.1.198; cc_entrypoint=sdk-cli; You are a Claude agent, built on Anthropic's Claude Agent SDK.`
	body := `{"model":"claude","max_tokens":64,"messages":[{"role":"user","content":[{"type":"text","text":"` + flattenedContext + `"}]}]}`
	recorder := performPromptComplianceEndpointRequest(t, "messages", "/v1/messages", body, 0, 91023)

	require.Equal(t, system_setting.DefaultPromptFilterBlockStatusCode, recorder.Code)
	assert.Empty(t, recorder.Header().Get("X-Prompt-Filter-History-Sanitized"))
}

func TestPromptComplianceCheckSanitizesClaudeCodeStructuredContentBlocks(t *testing.T) {
	withPromptComplianceSettings(t, true)
	forcePromptComplianceMemoryStore(t)
	setting.SensitiveWords = []string{"这是weijin测试专用"}
	userID := 0
	tokenID := 91024

	blockedRecorder := performPromptComplianceEndpointRequest(t, "messages", "/v1/messages", claudePromptComplianceTextBody(t, "这是weijin测试专用是什么"), userID, tokenID)
	require.Equal(t, system_setting.DefaultPromptFilterBlockStatusCode, blockedRecorder.Code)

	contextText, err := common.Marshal(strings.Repeat("context ", 90) + "The following deferred tools are now available via ToolSearch. Their schemas are NOT loaded.")
	require.NoError(t, err)
	body := fmt.Sprintf(`{"model":"claude","max_tokens":64,"messages":[{"role":"user","content":[{"type":"text","text":%s},{"type":"text","text":"你好"}]},{"role":"system","content":"The following deferred tools are now available via ToolSearch. Their schemas are NOT loaded."},{"role":"assistant","content":[{"type":"text","text":"你好！有什么我可以帮你的吗？"}]},{"role":"user","content":[{"type":"text","text":"这是weijin测试专用是什么\n"},{"type":"text","text":"你可以帮我做什么","cache_control":{"type":"ephemeral"}}]}]}`, contextText)
	recorder := performPromptComplianceEndpointRequest(t, "messages", "/v1/messages", body, userID, tokenID)

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "2", recorder.Header().Get("X-Prompt-Filter-History-Sanitized"))
	var response struct {
		Body string `json:"body"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.NotContains(t, response.Body, "这是weijin测试专用")
	assert.Contains(t, response.Body, "你可以帮我做什么")
}

func TestPromptComplianceCheckDoesNotSanitizeClaudeCodeFlattenedHistoryWithoutTrailingUserText(t *testing.T) {
	withPromptComplianceSettings(t, true)
	forcePromptComplianceMemoryStore(t)
	setting.SensitiveWords = []string{"这是weijin测试专用"}
	userID := 0
	tokenID := 91021

	blockedRecorder := performPromptComplianceEndpointRequest(t, "messages", "/v1/messages", claudePromptComplianceTextBody(t, "这是weijin测试专用是什么"), userID, tokenID)
	require.Equal(t, system_setting.DefaultPromptFilterBlockStatusCode, blockedRecorder.Code)

	flattenedContext := strings.Repeat("context ", 90) +
		`你好！有什么我可以帮你的吗？ 这是weijin测试专用是什么 ` +
		`x-anthropic-billing-header: cc_version=2.1.198; cc_entrypoint=sdk-cli; You are a Claude agent, built on Anthropic's Claude Agent SDK.`
	body := `{"model":"claude","max_tokens":64,"messages":[{"role":"user","content":[{"type":"text","text":"` + flattenedContext + `"}]}]}`
	recorder := performPromptComplianceEndpointRequest(t, "messages", "/v1/messages", body, userID, tokenID)

	require.Equal(t, system_setting.DefaultPromptFilterBlockStatusCode, recorder.Code)
	assert.Empty(t, recorder.Header().Get("X-Prompt-Filter-History-Sanitized"))
}

func TestPromptComplianceCheckSanitizesBlockedInputEmbeddedInClaudeSystemContext(t *testing.T) {
	withPromptComplianceSettings(t, true)
	forcePromptComplianceMemoryStore(t)
	setting.SensitiveWords = []string{"这是weijin测试专用"}
	userID := 0
	tokenID := 91004

	blockedRecorder := performPromptComplianceEndpointRequest(t, "messages", "/v1/messages", claudePromptComplianceTextBody(t, "这是weijin测试专用是什么"), userID, tokenID)
	require.Equal(t, system_setting.DefaultPromptFilterBlockStatusCode, blockedRecorder.Code)

	flattenedSystem := strings.Repeat("context ", 90) +
		`你好！有什么我可以帮你的吗？ 这是weijin测试专用是什么 你可以帮我做什么 ` +
		`嘿，很高兴你问！我是 Claude，可以帮你做很多事情。 ` +
		`你的上下文是否还有我没发出去的信息？ x-anthropic-billing-header: cc_version=2.1.198; cc_entrypoint=sdk-cli; You are a Claude agent, built on Anthropic's Claude Agent SDK.`
	systemPrefix, err := common.Marshal(strings.Repeat("context ", 90))
	require.NoError(t, err)
	systemHistory, err := common.Marshal(strings.TrimPrefix(flattenedSystem, strings.Repeat("context ", 90)))
	require.NoError(t, err)
	systemData := fmt.Sprintf(`[{"type":"text","text":%s},{"type":"text","text":%s}]`, systemPrefix, systemHistory)
	body := fmt.Sprintf(`{"model":"claude","max_tokens":64,"system":%s,"messages":[{"role":"user","content":[{"type":"text","text":"你的上下文是否还有我没发出去的信息？"}]}]}`, systemData)
	recorder := performPromptComplianceEndpointRequest(t, "messages", "/v1/messages", body, userID, tokenID)

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "1", recorder.Header().Get("X-Prompt-Filter-History-Sanitized"))
	var response struct {
		Body string `json:"body"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.NotContains(t, response.Body, "这是weijin测试专用")
	assert.Contains(t, response.Body, "你的上下文是否还有我没发出去的信息？")
}

func TestPromptComplianceCheckSanitizesHistoricalUserMessageWithLocalAPIError460(t *testing.T) {
	withPromptComplianceSettings(t, true)
	forcePromptComplianceMemoryStore(t)
	setting.SensitiveWords = []string{"这是weijin测试专用"}

	body := `{"model":"claude","max_tokens":64,"messages":[{"role":"user","content":[{"type":"text","text":"这是weijin测试专用是什么"}]},{"role":"assistant","content":[{"type":"text","text":"API Error: 460 触发敏感词"}]},{"role":"user","content":[{"type":"text","text":"你的上下文是否还有我没发出去的信息？"}]}]}`
	recorder := performPromptComplianceEndpointRequest(t, "messages", "/v1/messages", body, 0, 0)

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "2", recorder.Header().Get("X-Prompt-Filter-History-Sanitized"))
	var response struct {
		Body string `json:"body"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.NotContains(t, response.Body, "这是weijin测试专用")
	assert.NotContains(t, response.Body, "API Error: 460")
	assert.Contains(t, response.Body, "你的上下文是否还有我没发出去的信息？")
}

func TestPromptComplianceCheckSanitizesHistoricalSystemReminderUserMessageByBlockedInput(t *testing.T) {
	withPromptComplianceSettings(t, true)
	forcePromptComplianceMemoryStore(t)
	setting.SensitiveWords = []string{"这是weijin测试专用"}

	blockedRecorder := performPromptComplianceEndpointRequest(t, "messages", "/v1/messages", claudePromptComplianceTextBody(t, "这是weijin测试专用是什么"), 0, 0)
	require.Equal(t, system_setting.DefaultPromptFilterBlockStatusCode, blockedRecorder.Code)

	body := `{"model":"claude","max_tokens":64,"messages":[{"role":"user","content":[{"type":"text","text":"<system-reminder>history contains 这是weijin测试专用是什么</system-reminder>"}]},{"role":"user","content":[{"type":"text","text":"你可以帮我做什么"}]}]}`
	recorder := performPromptComplianceEndpointRequest(t, "messages", "/v1/messages", body, 0, 0)

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "1", recorder.Header().Get("X-Prompt-Filter-History-Sanitized"))
	var response struct {
		Body string `json:"body"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.NotContains(t, response.Body, "这是weijin测试专用")
	assert.Contains(t, response.Body, "你可以帮我做什么")
}

func TestPromptComplianceCheckSanitizesShortCurrentWhenMessageAggregateIsFlattened(t *testing.T) {
	withPromptComplianceSettings(t, true)
	forcePromptComplianceMemoryStore(t)
	setting.SensitiveWords = []string{"这是weijin测试专用"}

	blockedRecorder := performPromptComplianceEndpointRequest(t, "messages", "/v1/messages", claudePromptComplianceTextBody(t, "这是weijin测试专用是什么"), 0, 0)
	require.Equal(t, system_setting.DefaultPromptFilterBlockStatusCode, blockedRecorder.Code)

	body := `{"model":"claude","max_tokens":64,"messages":[{"role":"user","content":[{"type":"text","text":"<system-reminder>` + strings.Repeat("context ", 90) + `</system-reminder>"}]},{"role":"system","content":[{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1.198; cc_entrypoint=sdk-cli; You are a Claude agent, built on Anthropic's Claude Agent SDK."}]},{"role":"assistant","content":[{"type":"text","text":"API Error: 460 触发敏感词"}]},{"role":"user","content":[{"type":"text","text":"这是weijin测试专用是什么 你可以帮我做什么"}]}]}`
	recorder := performPromptComplianceEndpointRequest(t, "messages", "/v1/messages", body, 0, 0)

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "3", recorder.Header().Get("X-Prompt-Filter-History-Sanitized"))
	var response struct {
		Body string `json:"body"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.NotContains(t, response.Body, "这是weijin测试专用")
	assert.NotContains(t, response.Body, "API Error: 460")
	assert.Contains(t, response.Body, "你可以帮我做什么")
}

func TestPromptComplianceCheckDoesNotSanitizeCurrentWhenHistoricalMessageWasRemoved(t *testing.T) {
	withPromptComplianceSettings(t, true)
	forcePromptComplianceMemoryStore(t)
	setting.SensitiveWords = []string{"这是weijin测试专用"}
	userID := 0
	tokenID := 91012

	blockedRecorder := performPromptComplianceEndpointRequest(t, "messages", "/v1/messages", claudePromptComplianceTextBody(t, "这是weijin测试专用是什么"), userID, tokenID)
	require.Equal(t, system_setting.DefaultPromptFilterBlockStatusCode, blockedRecorder.Code)

	body := `{"model":"claude","max_tokens":64,"messages":[{"role":"user","content":[{"type":"text","text":"<system-reminder>` + strings.Repeat("context ", 90) + `</system-reminder>"}]},{"role":"system","content":[{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1.198; cc_entrypoint=sdk-cli; You are a Claude agent, built on Anthropic's Claude Agent SDK."}]},{"role":"assistant","content":[{"type":"text","text":"你好！我是 Claude，有什么可以帮你的吗？"}]},{"role":"user","content":[{"type":"text","text":"这是weijin测试专用是什么 你可以帮我做什么"}]},{"role":"assistant","content":[{"type":"text","text":"我可以帮你做很多事情"}]},{"role":"user","content":[{"type":"text","text":"这次我主动再问：这是weijin测试专用是什么"}]}]}`
	recorder := performPromptComplianceEndpointRequest(t, "messages", "/v1/messages", body, userID, tokenID)

	require.Equal(t, system_setting.DefaultPromptFilterBlockStatusCode, recorder.Code)
	assert.Empty(t, recorder.Header().Get("X-Prompt-Filter-History-Sanitized"))

	recoveredBody := `{"model":"claude","max_tokens":64,"messages":[{"role":"user","content":[{"type":"text","text":"<system-reminder>` + strings.Repeat("context ", 90) + `</system-reminder>"}]},{"role":"system","content":[{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1.198; cc_entrypoint=sdk-cli; You are a Claude agent, built on Anthropic's Claude Agent SDK."}]},{"role":"assistant","content":[{"type":"text","text":"你好！我是 Claude，有什么可以帮你的吗？"}]},{"role":"user","content":[{"type":"text","text":"这是weijin测试专用是什么 你可以帮我做什么"}]},{"role":"assistant","content":[{"type":"text","text":"我可以帮你做很多事情"}]},{"role":"user","content":[{"type":"text","text":"这次我主动再问：这是weijin测试专用是什么 后续正常消息是否还能发送"}]}]}`
	recoveredRecorder := performPromptComplianceEndpointRequest(t, "messages", "/v1/messages", recoveredBody, userID, tokenID)

	require.Equal(t, system_setting.DefaultPromptFilterBlockStatusCode, recoveredRecorder.Code)
	assert.Empty(t, recoveredRecorder.Header().Get("X-Prompt-Filter-History-Sanitized"))
}

func TestPromptComplianceCheckDoesNotSanitizeFlattenedCurrentWithoutBlockedHistory(t *testing.T) {
	withPromptComplianceSettings(t, true)
	forcePromptComplianceMemoryStore(t)
	setting.SensitiveWords = []string{"这是weijin测试专用"}
	userID := 0
	tokenID := 91006

	flattenedCurrent := strings.Repeat("context ", 90) +
		`当前问题：这是weijin测试专用是什么 你可以帮我做什么 x-anthropic-billing-header: cc_version=2.1.198; cc_entrypoint=cli; You are Claude Code, Anthropic's official CLI for Claude.`
	recorder := performPromptComplianceEndpointRequest(t, "messages", "/v1/messages", claudePromptComplianceTextBody(t, flattenedCurrent), userID, tokenID)

	require.Equal(t, system_setting.DefaultPromptFilterBlockStatusCode, recorder.Code)
	assert.Empty(t, recorder.Header().Get("X-Prompt-Filter-History-Sanitized"))
}

func TestPromptComplianceCheckDoesNotSanitizeFlattenedCurrentFromOldBlockedCountWithoutRequestEvidence(t *testing.T) {
	withPromptComplianceSettings(t, true)
	forcePromptComplianceMemoryStore(t)
	setting.SensitiveWords = []string{"这是weijin测试专用"}
	userID := 0
	tokenID := 91007

	blockedRecorder := performPromptComplianceEndpointRequest(t, "messages", "/v1/messages", claudePromptComplianceTextBody(t, "这是weijin测试专用是什么"), userID, tokenID)
	require.Equal(t, system_setting.DefaultPromptFilterBlockStatusCode, blockedRecorder.Code)

	flattenedCurrent := strings.Repeat("context ", 90) +
		`当前问题：这是weijin测试专用是什么 x-anthropic-billing-header: cc_version=2.1.198; cc_entrypoint=cli; You are Claude Code, Anthropic's official CLI for Claude.`
	recorder := performPromptComplianceEndpointRequest(t, "messages", "/v1/messages", claudePromptComplianceTextBody(t, flattenedCurrent), userID, tokenID)

	require.Equal(t, system_setting.DefaultPromptFilterBlockStatusCode, recorder.Code)
	assert.Empty(t, recorder.Header().Get("X-Prompt-Filter-History-Sanitized"))
}

func TestPromptComplianceCheckDoesNotSanitizeCurrentKeywordVariantFromOldBlockedCount(t *testing.T) {
	withPromptComplianceSettings(t, true)
	forcePromptComplianceMemoryStore(t)
	setting.SensitiveWords = []string{"这是weijin测试专用"}
	userID := 0
	tokenID := 91017

	blockedRecorder := performPromptComplianceEndpointRequest(t, "messages", "/v1/messages", claudePromptComplianceTextBody(t, "这是weijin测试专用是什么"), userID, tokenID)
	require.Equal(t, system_setting.DefaultPromptFilterBlockStatusCode, blockedRecorder.Code)

	flattenedCurrent := strings.Repeat("context ", 90) +
		`当前问题：这是weijin测试专用作用是什么 后续正常问题 x-anthropic-billing-header: cc_version=2.1.198; cc_entrypoint=cli; You are Claude Code, Anthropic's official CLI for Claude.`
	recorder := performPromptComplianceEndpointRequest(t, "messages", "/v1/messages", claudePromptComplianceTextBody(t, flattenedCurrent), userID, tokenID)

	require.Equal(t, system_setting.DefaultPromptFilterBlockStatusCode, recorder.Code)
	assert.Empty(t, recorder.Header().Get("X-Prompt-Filter-History-Sanitized"))
}

func TestPromptComplianceCheckDoesNotSanitizeClaudeCurrentInputBeforeToolInstructions(t *testing.T) {
	withPromptComplianceSettings(t, true)
	forcePromptComplianceMemoryStore(t)
	setting.SensitiveWords = []string{"这是weijin测试专用"}
	userID := 0
	tokenID := 91009

	blockedRecorder := performPromptComplianceEndpointRequest(t, "messages", "/v1/messages", claudePromptComplianceTextBody(t, "这是weijin测试专用是什么"), userID, tokenID)
	require.Equal(t, system_setting.DefaultPromptFilterBlockStatusCode, blockedRecorder.Code)

	body := `{"model":"claude","max_tokens":64,"messages":[{"role":"user","content":[{"type":"text","text":"<system-reminder>` + strings.Repeat("context ", 90) + `</system-reminder> 这是weijin测试专用是什么 The following deferred tools are now available via ToolSearch."}]},{"role":"user","content":[{"type":"text","text":"The following deferred tools are now available via ToolSearch. Their schemas are NOT loaded."}]}]}`
	recorder := performPromptComplianceEndpointRequest(t, "messages", "/v1/messages", body, userID, tokenID)

	require.Equal(t, system_setting.DefaultPromptFilterBlockStatusCode, recorder.Code)
	assert.Empty(t, recorder.Header().Get("X-Prompt-Filter-History-Sanitized"))
}

func TestPromptComplianceCheckDoesNotSanitizeRepeatedBlockedKeywordInFlattenedCurrentUserMessage(t *testing.T) {
	withPromptComplianceSettings(t, true)
	forcePromptComplianceMemoryStore(t)
	setting.SensitiveWords = []string{"这是weijin测试专用"}
	userID := 0
	tokenID := 91008

	blockedRecorder := performPromptComplianceEndpointRequest(t, "messages", "/v1/messages", claudePromptComplianceTextBody(t, "这是weijin测试专用是什么"), userID, tokenID)
	require.Equal(t, system_setting.DefaultPromptFilterBlockStatusCode, blockedRecorder.Code)

	flattenedCurrent := strings.Repeat("context ", 90) +
		`你好！有什么我可以帮你的吗？ 这是weijin测试专用是什么 你可以帮我做什么 ` +
		`这次我主动再问：这是weijin测试专用是什么 ` +
		`x-anthropic-billing-header: cc_version=2.1.198; cc_entrypoint=sdk-cli; You are a Claude agent, built on Anthropic's Claude Agent SDK.`
	recorder := performPromptComplianceEndpointRequest(t, "messages", "/v1/messages", claudePromptComplianceTextBody(t, flattenedCurrent), userID, tokenID)

	require.Equal(t, system_setting.DefaultPromptFilterBlockStatusCode, recorder.Code)
	assert.Empty(t, recorder.Header().Get("X-Prompt-Filter-History-Sanitized"))
}

func TestPromptComplianceCheckDoesNotSanitizeExplicitCurrentReaskInFlattenedCurrentUserMessage(t *testing.T) {
	withPromptComplianceSettings(t, true)
	forcePromptComplianceMemoryStore(t)
	setting.SensitiveWords = []string{"这是weijin测试专用"}
	userID := 0
	tokenID := 91018

	blockedRecorder := performPromptComplianceEndpointRequest(t, "messages", "/v1/messages", claudePromptComplianceTextBody(t, "这是weijin测试专用是什么"), userID, tokenID)
	require.Equal(t, system_setting.DefaultPromptFilterBlockStatusCode, blockedRecorder.Code)

	flattenedCurrent := strings.Repeat("context ", 90) +
		`这次我主动再问：这是weijin测试专用是什么 ` +
		`x-anthropic-billing-header: cc_version=2.1.198; cc_entrypoint=sdk-cli; You are a Claude agent, built on Anthropic's Claude Agent SDK.`
	body := `{"model":"claude","max_tokens":64,"messages":[{"role":"assistant","content":[{"type":"text","text":"API Error: 460 触发敏感词"}]},{"role":"user","content":[{"type":"text","text":"` + flattenedCurrent + `"}]}]}`
	recorder := performPromptComplianceEndpointRequest(t, "messages", "/v1/messages", body, userID, tokenID)

	require.Equal(t, system_setting.DefaultPromptFilterBlockStatusCode, recorder.Code)
	assert.Empty(t, recorder.Header().Get("X-Prompt-Filter-History-Sanitized"))
}

func TestPromptComplianceCheckSanitizesMultipleBlockedOccurrencesAfterRepeatedFlattenedFailures(t *testing.T) {
	withPromptComplianceSettings(t, true)
	forcePromptComplianceMemoryStore(t)
	setting.SensitiveWords = []string{"这是weijin测试专用"}
	userID := 0
	tokenID := 91010

	blockedRecorder := performPromptComplianceEndpointRequest(t, "messages", "/v1/messages", claudePromptComplianceTextBody(t, "这是weijin测试专用是什么"), userID, tokenID)
	require.Equal(t, system_setting.DefaultPromptFilterBlockStatusCode, blockedRecorder.Code)

	repeatedBlockedContext := strings.Repeat("context ", 90) +
		`你好！有什么我可以帮你的吗？ 这是weijin测试专用是什么 你可以帮我做什么 ` +
		`这次我主动再问：这是weijin测试专用是什么 ` +
		`x-anthropic-billing-header: cc_version=2.1.198; cc_entrypoint=cli; You are Claude Code, Anthropic's official CLI for Claude.`
	repeatedRecorder := performPromptComplianceEndpointRequest(t, "messages", "/v1/messages", claudePromptComplianceTextBody(t, repeatedBlockedContext), userID, tokenID)
	require.Equal(t, system_setting.DefaultPromptFilterBlockStatusCode, repeatedRecorder.Code)

	normalContextAfterTwoFailures := strings.Repeat("context ", 90) +
		`你好！有什么我可以帮你的吗？ 这是weijin测试专用是什么 你可以帮我做什么 ` +
		`第二次失败记录 这是weijin测试专用是什么 后续正常消息是否还能发送 ` +
		`x-anthropic-billing-header: cc_version=2.1.198; cc_entrypoint=cli; You are Claude Code, Anthropic's official CLI for Claude.`
	body := `{"model":"claude","max_tokens":64,"messages":[{"role":"assistant","content":[{"type":"text","text":"API Error: 460 触发敏感词"}]},{"role":"assistant","content":[{"type":"text","text":"API Error: 460 触发敏感词"}]},{"role":"user","content":[{"type":"text","text":"` + normalContextAfterTwoFailures + `"}]}]}`
	recorder := performPromptComplianceEndpointRequest(t, "messages", "/v1/messages", body, userID, tokenID)

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "3", recorder.Header().Get("X-Prompt-Filter-History-Sanitized"))
	var response struct {
		Body string `json:"body"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.NotContains(t, response.Body, "这是weijin测试专用")
	assert.NotContains(t, response.Body, "API Error: 460")
	assert.Contains(t, response.Body, "后续正常消息是否还能发送")
}

func TestPromptComplianceCheckSanitizesRepeatedFlattenedFailuresWithoutAPIErrorEcho(t *testing.T) {
	withPromptComplianceSettings(t, true)
	forcePromptComplianceMemoryStore(t)
	setting.SensitiveWords = []string{"这是weijin测试专用"}
	userID := 0
	tokenID := 91025

	blockedRecorder := performPromptComplianceEndpointRequest(t, "messages", "/v1/messages", claudePromptComplianceTextBody(t, "这是weijin测试专用是什么"), userID, tokenID)
	require.Equal(t, system_setting.DefaultPromptFilterBlockStatusCode, blockedRecorder.Code)

	repeatedBlockedContext := strings.Repeat("context ", 90) +
		`你好！有什么我可以帮你的吗？ 这是weijin测试专用是什么 你可以帮我做什么 ` +
		`这次我主动再问：这是weijin测试专用是什么 ` +
		`x-anthropic-billing-header: cc_version=2.1.198; cc_entrypoint=cli; You are Claude Code, Anthropic's official CLI for Claude.`
	repeatedRecorder := performPromptComplianceEndpointRequest(t, "messages", "/v1/messages", claudePromptComplianceTextBody(t, repeatedBlockedContext), userID, tokenID)
	require.Equal(t, system_setting.DefaultPromptFilterBlockStatusCode, repeatedRecorder.Code)

	normalContextAfterTwoFailures := strings.Repeat("context ", 90) +
		`你好！有什么我可以帮你的吗？ 这是weijin测试专用是什么 你可以帮我做什么 ` +
		`第二次失败记录 这是weijin测试专用是什么 后续正常消息是否还能发送 ` +
		`x-anthropic-billing-header: cc_version=2.1.198; cc_entrypoint=cli; You are Claude Code, Anthropic's official CLI for Claude.`
	recorder := performPromptComplianceEndpointRequest(t, "messages", "/v1/messages", claudePromptComplianceTextBody(t, normalContextAfterTwoFailures), userID, tokenID)

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "1", recorder.Header().Get("X-Prompt-Filter-History-Sanitized"))
	var response struct {
		Body string `json:"body"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.NotContains(t, response.Body, "这是weijin测试专用")
	assert.Contains(t, response.Body, "后续正常消息是否还能发送")
}

func TestPromptComplianceCheckSanitizesResponsesStringInputWithBlockedHistoryEvidence(t *testing.T) {
	withPromptComplianceSettings(t, true)
	forcePromptComplianceMemoryStore(t)
	setting.SensitiveWords = []string{"这是weijin测试专用"}
	userID := 0
	tokenID := 91014

	blockedRecorder := performPromptComplianceEndpointRequest(t, "responses", "/v1/responses", `{"model":"gpt","input":"这是weijin测试专用是什么"}`, userID, tokenID)
	require.Equal(t, system_setting.DefaultPromptFilterBlockStatusCode, blockedRecorder.Code)

	input := strings.Repeat("context ", 90) +
		"\nAPI Error: 460 触发敏感词！渠道特殊，请遵纪守法，本站禁止色情/恐怖/涉z/NSFW等，多次触发将封号！" +
		"\n这是weijin测试专用是什么\n你可以帮我做什么\nx-anthropic-billing-header: cc_version=2.1.198; cc_entrypoint=cli;"
	inputJSON, err := common.Marshal(input)
	require.NoError(t, err)
	recorder := performPromptComplianceEndpointRequest(t, "responses", "/v1/responses", `{"model":"gpt","input":`+string(inputJSON)+`}`, userID, tokenID)

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "1", recorder.Header().Get("X-Prompt-Filter-History-Sanitized"))
	var response struct {
		Body string `json:"body"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.NotContains(t, response.Body, "这是weijin测试专用")
	assert.NotContains(t, response.Body, "API Error: 460")
	assert.Contains(t, response.Body, "你可以帮我做什么")
}

func TestPromptComplianceCheckSanitizesResponsesStringInputWithFlattenedBlockedHistory(t *testing.T) {
	withPromptComplianceSettings(t, true)
	forcePromptComplianceMemoryStore(t)
	setting.SensitiveWords = []string{"这是weijin测试专用"}
	userID := 0
	tokenID := 91019

	blockedRecorder := performPromptComplianceEndpointRequest(t, "responses", "/v1/responses", `{"model":"gpt","input":"这是weijin测试专用是什么"}`, userID, tokenID)
	require.Equal(t, system_setting.DefaultPromptFilterBlockStatusCode, blockedRecorder.Code)

	input := strings.Repeat("context ", 90) +
		"\n你好！有什么我可以帮你的吗？ 这是weijin测试专用是什么 你可以帮我做什么" +
		"\nx-anthropic-billing-header: cc_version=2.1.198; cc_entrypoint=cli;"
	inputJSON, err := common.Marshal(input)
	require.NoError(t, err)
	recorder := performPromptComplianceEndpointRequest(t, "responses", "/v1/responses", `{"model":"gpt","input":`+string(inputJSON)+`}`, userID, tokenID)

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "1", recorder.Header().Get("X-Prompt-Filter-History-Sanitized"))
	var response struct {
		Body string `json:"body"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.NotContains(t, response.Body, "这是weijin测试专用")
	assert.NotContains(t, response.Body, "API Error: 460")
	assert.Contains(t, response.Body, "你可以帮我做什么")
}

func TestPromptComplianceCheckSanitizesResponsesStringInputRepeatedFailuresWithoutAPIErrorEcho(t *testing.T) {
	withPromptComplianceSettings(t, true)
	forcePromptComplianceMemoryStore(t)
	setting.SensitiveWords = []string{"这是weijin测试专用"}
	userID := 0
	tokenID := 91026

	blockedRecorder := performPromptComplianceEndpointRequest(t, "responses", "/v1/responses", `{"model":"gpt","input":"这是weijin测试专用是什么"}`, userID, tokenID)
	require.Equal(t, system_setting.DefaultPromptFilterBlockStatusCode, blockedRecorder.Code)

	repeatedBlockedInput := strings.Repeat("context ", 90) +
		"\n你好！有什么我可以帮你的吗？ 这是weijin测试专用是什么 你可以帮我做什么" +
		"\n这次我主动再问：这是weijin测试专用是什么" +
		"\nx-anthropic-billing-header: cc_version=2.1.198; cc_entrypoint=cli;"
	repeatedInputJSON, err := common.Marshal(repeatedBlockedInput)
	require.NoError(t, err)
	repeatedRecorder := performPromptComplianceEndpointRequest(t, "responses", "/v1/responses", `{"model":"gpt","input":`+string(repeatedInputJSON)+`}`, userID, tokenID)
	require.Equal(t, system_setting.DefaultPromptFilterBlockStatusCode, repeatedRecorder.Code)

	input := strings.Repeat("context ", 90) +
		"\n你好！有什么我可以帮你的吗？ 这是weijin测试专用是什么 你可以帮我做什么" +
		"\n第二次失败记录 这是weijin测试专用是什么 后续正常消息是否还能发送" +
		"\nx-anthropic-billing-header: cc_version=2.1.198; cc_entrypoint=cli;"
	inputJSON, err := common.Marshal(input)
	require.NoError(t, err)
	recorder := performPromptComplianceEndpointRequest(t, "responses", "/v1/responses", `{"model":"gpt","input":`+string(inputJSON)+`}`, userID, tokenID)

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "1", recorder.Header().Get("X-Prompt-Filter-History-Sanitized"))
	var response struct {
		Body string `json:"body"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.NotContains(t, response.Body, "这是weijin测试专用")
	assert.NotContains(t, response.Body, "API Error: 460")
	assert.Contains(t, response.Body, "后续正常消息是否还能发送")
}

func TestPromptComplianceCheckDoesNotSanitizeResponsesStringInputWithoutBlockedHistory(t *testing.T) {
	withPromptComplianceSettings(t, true)
	forcePromptComplianceMemoryStore(t)
	setting.SensitiveWords = []string{"这是weijin测试专用"}
	userID := 0
	tokenID := 91021

	input := strings.Repeat("context ", 90) +
		"\n当前问题：这是weijin测试专用是什么 你可以帮我做什么" +
		"\nx-anthropic-billing-header: cc_version=2.1.198; cc_entrypoint=cli;"
	inputJSON, err := common.Marshal(input)
	require.NoError(t, err)
	recorder := performPromptComplianceEndpointRequest(t, "responses", "/v1/responses", `{"model":"gpt","input":`+string(inputJSON)+`}`, userID, tokenID)

	require.Equal(t, system_setting.DefaultPromptFilterBlockStatusCode, recorder.Code)
	assert.Empty(t, recorder.Header().Get("X-Prompt-Filter-History-Sanitized"))
}

func TestPromptComplianceCheckDoesNotSanitizeResponsesStringInputCurrentReaskWithoutErrorEvidence(t *testing.T) {
	withPromptComplianceSettings(t, true)
	forcePromptComplianceMemoryStore(t)
	setting.SensitiveWords = []string{"这是weijin测试专用"}
	userID := 0
	tokenID := 91016

	blockedRecorder := performPromptComplianceEndpointRequest(t, "responses", "/v1/responses", `{"model":"gpt","input":"这是weijin测试专用是什么"}`, userID, tokenID)
	require.Equal(t, system_setting.DefaultPromptFilterBlockStatusCode, blockedRecorder.Code)

	input := strings.Repeat("context ", 90) +
		"\n当前问题：这是weijin测试专用是什么\nx-anthropic-billing-header: cc_version=2.1.198; cc_entrypoint=cli;"
	inputJSON, err := common.Marshal(input)
	require.NoError(t, err)
	recorder := performPromptComplianceEndpointRequest(t, "responses", "/v1/responses", `{"model":"gpt","input":`+string(inputJSON)+`}`, userID, tokenID)

	require.Equal(t, system_setting.DefaultPromptFilterBlockStatusCode, recorder.Code)
	assert.Empty(t, recorder.Header().Get("X-Prompt-Filter-History-Sanitized"))
}

func TestPromptComplianceCheckDoesNotSanitizeResponsesStringInputRepeatedBlockedReask(t *testing.T) {
	withPromptComplianceSettings(t, true)
	forcePromptComplianceMemoryStore(t)
	setting.SensitiveWords = []string{"这是weijin测试专用"}
	userID := 0
	tokenID := 91020

	blockedRecorder := performPromptComplianceEndpointRequest(t, "responses", "/v1/responses", `{"model":"gpt","input":"这是weijin测试专用是什么"}`, userID, tokenID)
	require.Equal(t, system_setting.DefaultPromptFilterBlockStatusCode, blockedRecorder.Code)

	input := strings.Repeat("context ", 90) +
		"\n你好！有什么我可以帮你的吗？ 这是weijin测试专用是什么 你可以帮我做什么" +
		"\n这次我主动再问：这是weijin测试专用是什么" +
		"\nx-anthropic-billing-header: cc_version=2.1.198; cc_entrypoint=cli;"
	inputJSON, err := common.Marshal(input)
	require.NoError(t, err)
	recorder := performPromptComplianceEndpointRequest(t, "responses", "/v1/responses", `{"model":"gpt","input":`+string(inputJSON)+`}`, userID, tokenID)

	require.Equal(t, system_setting.DefaultPromptFilterBlockStatusCode, recorder.Code)
	assert.Empty(t, recorder.Header().Get("X-Prompt-Filter-History-Sanitized"))
}
