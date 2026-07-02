package middleware

import (
	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
)

// I18n middleware sets the only supported language for this fork.
func I18n() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(string(constant.ContextKeyLanguage), i18n.DefaultLang)
		c.Next()
	}
}

// GetLanguage returns the current language from gin context
func GetLanguage(c *gin.Context) string {
	if lang := c.GetString(string(constant.ContextKeyLanguage)); lang != "" {
		return lang
	}
	return i18n.DefaultLang
}
