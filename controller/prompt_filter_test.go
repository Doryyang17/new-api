package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestUpdatePromptFilterLexiconWordsRejectsOversizedBody(t *testing.T) {
	gin.SetMode(gin.TestMode)

	payloadSize := int(service.PromptFilterLexiconMaxUploadBytes + promptFilterLexiconMultipartOverheadBytes + 1024)
	request := httptest.NewRequest(http.MethodPut, "/api/prompt-filter/lexicons/test/words", strings.NewReader(`{"words":["`+strings.Repeat("x", payloadSize)+`"]}`))
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = request
	ctx.Params = gin.Params{{Key: "id", Value: "test"}}

	UpdatePromptFilterLexiconWords(ctx)

	assert.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "词库内容不能超过")
}
