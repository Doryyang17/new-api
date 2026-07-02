package i18n

import (
	"embed"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
	"gopkg.in/yaml.v3"

	"github.com/QuantumNous/new-api/common"
)

const (
	LangZhCN    = "zh-CN"
	DefaultLang = LangZhCN
)

//go:embed locales/*.yaml
var localeFS embed.FS

var (
	bundle     *i18n.Bundle
	localizers = make(map[string]*i18n.Localizer)
	mu         sync.RWMutex
	initOnce   sync.Once
)

// Init initializes the i18n bundle and loads all translation files
func Init() error {
	var initErr error
	initOnce.Do(func() {
		bundle = i18n.NewBundle(language.Chinese)
		bundle.RegisterUnmarshalFunc("yaml", yaml.Unmarshal)

		// Load the only maintained backend translation file.
		files := []string{"locales/zh-CN.yaml"}
		for _, file := range files {
			_, err := bundle.LoadMessageFileFS(localeFS, file)
			if err != nil {
				initErr = err
				return
			}
		}

		localizers[LangZhCN] = i18n.NewLocalizer(bundle, LangZhCN)

		// Set the TranslateMessage function in common package
		common.TranslateMessage = T
	})
	return initErr
}

// GetLocalizer returns a localizer for the specified language
func GetLocalizer(lang string) *i18n.Localizer {
	lang = normalizeLang(lang)

	mu.RLock()
	loc, ok := localizers[lang]
	mu.RUnlock()

	if ok {
		return loc
	}

	// Create new localizer for unknown language (fallback to default)
	mu.Lock()
	defer mu.Unlock()

	// Double-check after acquiring write lock
	if loc, ok = localizers[lang]; ok {
		return loc
	}

	loc = i18n.NewLocalizer(bundle, lang, DefaultLang)
	localizers[lang] = loc
	return loc
}

// T translates a message key using the language from gin context
func T(c *gin.Context, key string, args ...map[string]any) string {
	lang := GetLangFromContext(c)
	return Translate(lang, key, args...)
}

// Translate translates a message key for the specified language
func Translate(lang, key string, args ...map[string]any) string {
	loc := GetLocalizer(lang)

	config := &i18n.LocalizeConfig{
		MessageID: key,
	}

	if len(args) > 0 && args[0] != nil {
		config.TemplateData = args[0]
	}

	msg, err := loc.Localize(config)
	if err != nil {
		// Return key as fallback if translation not found
		return key
	}
	return msg
}

// userLangLoaderFunc is a function that loads user language from database/cache
// It's set by the model package to avoid circular imports
var userLangLoaderFunc func(userId int) string

// SetUserLangLoader sets the function to load user language (called from model package)
func SetUserLangLoader(loader func(userId int) string) {
	userLangLoaderFunc = loader
}

// GetLangFromContext returns the only supported language for this fork.
func GetLangFromContext(c *gin.Context) string {
	return DefaultLang
}

// ParseAcceptLanguage keeps compatibility with old callers but always returns Chinese.
func ParseAcceptLanguage(header string) string {
	return DefaultLang
}

// normalizeLang normalizes all language codes to the only supported format.
func normalizeLang(lang string) string {
	return LangZhCN
}

// SupportedLanguages returns a list of supported language codes
func SupportedLanguages() []string {
	return []string{LangZhCN}
}

// IsSupported checks if a language code is supported
func IsSupported(lang string) bool {
	return normalizeLang(lang) == LangZhCN
}
