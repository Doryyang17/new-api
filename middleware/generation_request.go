package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func isModelGenerationRequest(c *gin.Context) bool {
	if c == nil || c.Request == nil {
		return false
	}
	path := c.Request.URL.Path
	if c.Request.Method == http.MethodGet && path == "/v1/realtime" {
		return true
	}
	if c.Request.Method != http.MethodPost || c.Request.Body == nil {
		return false
	}
	if path == "/suno/fetch" || strings.Contains(path, "/mj/task/list-by-condition") {
		return false
	}
	if strings.HasPrefix(path, "/jimeng") && c.Query("Action") == jimengGetResultAction {
		return false
	}
	return true
}

func isRequestConcurrencyCandidate(c *gin.Context) bool {
	return isModelGenerationRequest(c)
}
