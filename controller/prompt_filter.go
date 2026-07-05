package controller

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
)

type promptFilterTestRequest struct {
	Text     string `json:"text"`
	Endpoint string `json:"endpoint"`
	Model    string `json:"model"`
}

type promptFilterRulePatternTestRequest struct {
	Pattern string `json:"pattern"`
	Text    string `json:"text"`
}

type promptFilterLexiconUpdateRequest struct {
	Enabled  *bool   `json:"enabled,omitempty"`
	Name     *string `json:"name,omitempty"`
	Category *string `json:"category,omitempty"`
	Weight   *int    `json:"weight,omitempty"`
	Strict   *bool   `json:"strict,omitempty"`
}

type promptFilterLexiconWordsUpdateRequest struct {
	Words []string `json:"words"`
}

const promptFilterLexiconMultipartOverheadBytes int64 = 256 * 1024

func GetPromptFilterStatus(c *gin.Context) {
	settings := system_setting.GetPromptFilterSettings()
	logTotal, _ := model.CountPromptFilterLogs()
	rules := service.ListPromptFilterRules()
	lexiconFileCount, lexiconWordCount := service.PromptFilterLexiconStats(settings)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"enabled":                   setting.ShouldCheckPromptSensitive(),
			"mode":                      settings.Mode,
			"threshold":                 settings.Threshold,
			"strict_threshold":          settings.StrictThreshold,
			"log_matches":               settings.LogMatches,
			"max_text_length":           settings.MaxTextLength,
			"block_status_code":         settings.BlockStatusCode,
			"block_error_code":          settings.BlockErrorCode,
			"builtin_rule_count":        len(rules.BuiltinPatterns),
			"custom_rule_count":         len(rules.CustomPatterns),
			"disabled_rule_count":       len(rules.DisabledPatterns),
			"lexicon_file_count":        lexiconFileCount,
			"lexicon_word_count":        lexiconWordCount,
			"log_total":                 logTotal,
			"review_enabled":            settings.ReviewEnabled,
			"review_api_key_configured": system_setting.PromptFilterReviewAPIKeyConfigured(),
			"review_base_url":           settings.ReviewBaseURL,
			"review_model":              settings.ReviewModel,
			"review_timeout_seconds":    settings.ReviewTimeoutSeconds,
			"review_fail_closed":        settings.ReviewFailClosed,
		},
	})
}

func ListPromptFilterLexicons(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"items": service.ListPromptFilterLexiconFiles(),
		},
	})
}

func UploadPromptFilterLexicon(c *gin.Context) {
	if c.Request != nil && c.Request.Body != nil {
		c.Request.Body = http.MaxBytesReader(
			c.Writer,
			c.Request.Body,
			service.PromptFilterLexiconMaxUploadBytes+promptFilterLexiconMultipartOverheadBytes,
		)
	}
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		if common.IsRequestBodyTooLargeError(err) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{
				"success": false,
				"message": "词库文件不能超过 " + strconv.FormatInt(service.PromptFilterLexiconMaxUploadBytes/1024/1024, 10) + " MB",
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "请选择要上传的词库文件"})
		return
	}
	defer file.Close()

	weight, _ := strconv.Atoi(c.DefaultPostForm("weight", "100"))
	options := service.PromptFilterLexiconUploadOptions{
		Name:     c.PostForm("name"),
		Category: c.PostForm("category"),
		Weight:   weight,
		Strict:   c.DefaultPostForm("strict", "true") == "true",
		Enabled:  c.DefaultPostForm("enabled", "true") == "true",
	}
	entry, err := service.UploadPromptFilterLexicon(header.Filename, file, header.Size, options)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"file": entry,
		},
	})
}

func UpdatePromptFilterLexicon(c *gin.Context) {
	var req promptFilterLexiconUpdateRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid request body"})
		return
	}
	entry, err := service.UpdatePromptFilterLexicon(c.Param("id"), service.PromptFilterLexiconUpdate{
		Enabled:  req.Enabled,
		Name:     req.Name,
		Category: req.Category,
		Weight:   req.Weight,
		Strict:   req.Strict,
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"file": entry,
		},
	})
}

func PreviewPromptFilterLexicon(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "200"))
	preview, err := service.GetPromptFilterLexiconPreview(c.Param("id"), limit)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    preview,
	})
}

func UpdatePromptFilterLexiconWords(c *gin.Context) {
	if c.Request != nil && c.Request.Body != nil {
		c.Request.Body = http.MaxBytesReader(
			c.Writer,
			c.Request.Body,
			service.PromptFilterLexiconMaxUploadBytes+promptFilterLexiconMultipartOverheadBytes,
		)
	}
	var req promptFilterLexiconWordsUpdateRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		if common.IsRequestBodyTooLargeError(err) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{
				"success": false,
				"message": "词库内容不能超过 " + strconv.FormatInt(service.PromptFilterLexiconMaxUploadBytes/1024/1024, 10) + " MB",
			})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid request body"})
		return
	}
	entry, err := service.UpdatePromptFilterLexiconWords(c.Param("id"), req.Words)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"file": entry,
		},
	})
}

func DeletePromptFilterLexicon(c *gin.Context) {
	if err := service.DeletePromptFilterLexicon(c.Param("id")); err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
}

func GetPromptFilterRules(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    service.ListPromptFilterRules(),
	})
}

func ListPromptFilterLogs(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", c.DefaultQuery("limit", "50")))
	apiKeyID, _ := strconv.Atoi(c.Query("api_key_id"))
	logs, total, err := model.ListPromptFilterLogsPage(model.PromptFilterLogQuery{
		Page:     page,
		PageSize: pageSize,
		Source:   c.Query("source"),
		Action:   c.Query("action"),
		Endpoint: c.Query("endpoint"),
		Model:    c.Query("model"),
		APIKeyID: apiKeyID,
		Query:    c.Query("q"),
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"items":     logs,
			"total":     total,
			"page":      page,
			"page_size": pageSize,
		},
	})
}

func ClearPromptFilterLogs(c *gin.Context) {
	deleted, err := model.ClearPromptFilterLogs()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"deleted_count": deleted,
		},
	})
}

func TestPromptFilter(c *gin.Context) {
	var req promptFilterTestRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid request body"})
		return
	}
	req.Text = strings.TrimSpace(req.Text)
	if req.Text == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "text cannot be empty"})
		return
	}
	if len([]rune(req.Text)) > 20000 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "text cannot exceed 20000 characters"})
		return
	}
	verdict := service.InspectPromptTextForTest(req.Text)
	verdict.TextPreview = service.RedactedPromptFilterPreview(verdict.TextPreview, 500)
	verdict.FullText = ""
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"verdict": verdict,
		},
	})
}

func TestPromptFilterRulePattern(c *gin.Context) {
	var req promptFilterRulePatternTestRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid request body"})
		return
	}
	req.Pattern = strings.TrimSpace(req.Pattern)
	if req.Pattern == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "pattern cannot be empty"})
		return
	}
	if req.Text == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "text cannot be empty"})
		return
	}
	if len([]rune(req.Pattern)) > 5000 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "pattern cannot exceed 5000 characters"})
		return
	}
	if len([]rune(req.Text)) > 20000 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "text cannot exceed 20000 characters"})
		return
	}
	matched, err := service.TestPromptFilterPattern(req.Pattern, req.Text)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "",
			"data": gin.H{
				"matched": false,
				"error":   err.Error(),
			},
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"matched": matched,
		},
	})
}
