package i18n

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// Legacy language preferences and headers must not change this fork's Chinese output.
func TestBackendMessagesRemainChinese(t *testing.T) {
	require.NoError(t, Init())
	previous := common.DefaultLanguage
	t.Cleanup(func() { common.DefaultLanguage = previous })
	for _, language := range []string{"", "en", "zhTW", "fr-FR", "zh-CN"} {
		t.Run(language, func(t *testing.T) {
			common.DefaultLanguage = language
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
			c.Request.Header.Set("Accept-Language", language)
			c.Set(string(constant.ContextKeyUserSetting), dto.UserSetting{Language: language})
			assert.Equal(t, LangZhCN, StatedLang(c))
			assert.Equal(t, "用户额度不足, 剩余额度: $0.10", T(c, MsgQuotaUserInsufficient, map[string]any{"Remaining": "$0.10"}))
		})
	}
}

// A key missing from a locale, or a template that does not parse, reaches
// clients as the raw key.
func TestLocalesRenderEveryKey(t *testing.T) {
	require.NoError(t, Init())
	keysFile, err := parser.ParseFile(token.NewFileSet(), "keys.go", nil, parser.SkipObjectResolution)
	require.NoError(t, err)
	var declared []string
	ast.Inspect(keysFile, func(node ast.Node) bool {
		if spec, ok := node.(*ast.ValueSpec); ok {
			for _, value := range spec.Values {
				if literal, ok := value.(*ast.BasicLit); ok {
					key, err := strconv.Unquote(literal.Value)
					require.NoError(t, err)
					declared = append(declared, key)
				}
			}
		}
		return true
	})
	slices.Sort(declared)

	keysByLang := make(map[string][]string)
	for _, lang := range SupportedLanguages() {
		data, err := localeFS.ReadFile("locales/" + lang + ".yaml")
		require.NoError(t, err)
		var messages map[string]string
		require.NoError(t, yaml.Unmarshal(data, &messages))
		for key := range messages {
			keysByLang[lang] = append(keysByLang[lang], key)
		}
		slices.Sort(keysByLang[lang])
	}
	for _, lang := range SupportedLanguages() {
		assert.Equal(t, declared, keysByLang[lang], lang)
		for _, key := range keysByLang[lang] {
			text := Translate(lang, key, map[string]any{})
			assert.NotEqual(t, key, text, "%s %s", lang, key)
			assert.False(t, strings.Contains(text, "{{"), "%s %s: %s", lang, key, text)
		}
	}
}

// Web console responses carry the English source text as message_key plus its
// params, and the web console translates them. The backend sends the same body
// whatever language the request asks for.
func TestWebConsoleMessageResponse(t *testing.T) {
	require.NoError(t, Init())
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{
			name: "params",
			err:  common.NewMessage("Channel {{name}} (#{{id}}) does not exist", map[string]any{"name": "{{id}}", "id": 7}),
			want: `{"success":false,"message":"Channel {{id}} (#7) does not exist","message_key":"Channel {{name}} (#{{id}}) does not exist","message_params":{"id":7,"name":"{{id}}"}}`,
		},
		{
			name: "no params",
			err:  common.NewMessage("Invalid parameters"),
			want: `{"success":false,"message":"Invalid parameters","message_key":"Invalid parameters"}`,
		},
		{
			name: "plain error",
			err:  errors.New("record not found"),
			want: `{"success":false,"message":"record not found"}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest("GET", "/api/channel/7", nil)
			c.Request.Header.Set("Accept-Language", "zh-CN")
			common.ApiError(c, tc.err)
			assert.JSONEq(t, tc.want, recorder.Body.String())
		})
	}
}

// Server log lines are English. DEFAULT_LANGUAGE=zh-CN prints the Chinese text of
// the lines that have one in common/log_text.zh-CN.json.
func TestServerLogLanguage(t *testing.T) {
	t.Cleanup(func() { common.SetLogLanguage("") })
	const line = "channel #%d has %d unfinished tasks"
	assert.Equal(t, "channel #7 has 2 unfinished tasks", common.LogText(line, 7, 2))
	common.SetLogLanguage("zh-CN")
	assert.Equal(t, "渠道 #7 未完成的任务有: 2", common.LogText(line, 7, 2))
	assert.Equal(t, "a line without a Chinese text: x", common.LogText("a line without a Chinese text: %s", "x"))
	assert.Equal(t, "任务进度轮询开始", common.LogText("task progress polling started"))
	common.SetLogLanguage("en")
	assert.Equal(t, "channel #7 has 2 unfinished tasks", common.LogText(line, 7, 2))
}
