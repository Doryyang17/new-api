package router

import (
	"bytes"
	"embed"
	"encoding/json"
	"net/http"
	"os"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-contrib/gzip"
	"github.com/gin-contrib/static"
	"github.com/gin-gonic/gin"
)

// ThemeAssets holds the embedded frontend assets for both themes.
type ThemeAssets struct {
	DefaultBuildFS   embed.FS
	DefaultIndexPage []byte
	ClassicBuildFS   embed.FS
	ClassicIndexPage []byte
}

type frontendAvailabilityStatus struct {
	Enabled           bool   `json:"enabled"`
	Unavailable       bool   `json:"unavailable"`
	Message           string `json:"message"`
	Code              string `json:"code"`
	Timezone          string `json:"timezone"`
	UnavailableStart  string `json:"unavailable_start"`
	UnavailableEnd    string `json:"unavailable_end"`
	RetryAfterSeconds int    `json:"retry_after_seconds"`
}

type indexFallbackFileSystem struct {
	inner static.ServeFileSystem
}

func (i *indexFallbackFileSystem) Exists(prefix string, path string) bool {
	if isIndexPagePath(path) {
		return false
	}
	return i.inner.Exists(prefix, path)
}

func (i *indexFallbackFileSystem) Open(name string) (http.File, error) {
	if isIndexPagePath(name) {
		return nil, os.ErrNotExist
	}
	return i.inner.Open(name)
}

func SetWebRouter(router *gin.Engine, assets ThemeAssets) {
	defaultFS := common.EmbedFolder(assets.DefaultBuildFS, "web/default/dist")
	classicFS := common.EmbedFolder(assets.ClassicBuildFS, "web/classic/dist")
	themeFS := &indexFallbackFileSystem{
		inner: common.NewThemeAwareFS(defaultFS, classicFS),
	}

	router.Use(gzip.Gzip(gzip.DefaultCompression))
	router.Use(middleware.GlobalWebRateLimit())
	router.Use(middleware.Cache())
	router.Use(static.Serve("/", themeFS))
	router.NoRoute(func(c *gin.Context) {
		c.Set(middleware.RouteTagKey, "web")
		availabilityStatus := system_setting.GetAvailabilityStatus()
		if isAPINotFoundPath(c.Request.RequestURI) {
			if isModelAPIPath(c.Request.RequestURI) && availabilityStatus.Unavailable {
				middleware.HandleSystemAvailability(c)
				return
			}
			controller.RelayNotFound(c)
			return
		}
		if strings.HasPrefix(c.Request.RequestURI, "/assets") {
			controller.RelayNotFound(c)
			return
		}
		if availabilityStatus.Unavailable {
			c.Header("Cache-Control", "no-store")
		} else {
			c.Header("Cache-Control", "no-cache")
		}
		var indexPage []byte
		if common.GetTheme() == "classic" {
			indexPage = assets.ClassicIndexPage
		} else {
			indexPage = assets.DefaultIndexPage
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", injectAvailabilityStatus(indexPage, availabilityStatus))
	})
}

func injectAvailabilityStatus(page []byte, status system_setting.AvailabilityStatus) []byte {
	payload := frontendAvailabilityStatus{
		Enabled:           status.Enabled,
		Unavailable:       status.Unavailable,
		Message:           status.Message,
		Code:              status.Code,
		Timezone:          status.Timezone,
		UnavailableStart:  status.UnavailableStart,
		UnavailableEnd:    status.UnavailableEnd,
		RetryAfterSeconds: status.RetryAfterSeconds,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return page
	}
	injection := append([]byte("\n<script>window.__NEW_API_AVAILABILITY__="), encoded...)
	injection = append(injection, []byte(";</script>\n")...)
	if bytes.Contains(page, []byte("</head>")) {
		return bytes.Replace(page, []byte("</head>"), append(injection, []byte("</head>")...), 1)
	}
	out := append([]byte(nil), page...)
	return append(out, injection...)
}

func isIndexPagePath(path string) bool {
	return strings.TrimPrefix(path, "/") == "index.html"
}

func isAPINotFoundPath(path string) bool {
	return strings.HasPrefix(path, "/v1") ||
		strings.HasPrefix(path, "/v1beta") ||
		strings.HasPrefix(path, "/api") ||
		strings.HasPrefix(path, "/pg") ||
		strings.HasPrefix(path, "/mj") ||
		strings.Contains(path, "/mj/") ||
		strings.HasPrefix(path, "/suno") ||
		strings.HasPrefix(path, "/kling/") ||
		strings.HasPrefix(path, "/jimeng")
}

func isModelAPIPath(path string) bool {
	return strings.HasPrefix(path, "/v1") ||
		strings.HasPrefix(path, "/v1beta") ||
		strings.HasPrefix(path, "/pg") ||
		strings.HasPrefix(path, "/mj") ||
		strings.Contains(path, "/mj/") ||
		strings.HasPrefix(path, "/suno") ||
		strings.HasPrefix(path, "/kling/") ||
		strings.HasPrefix(path, "/jimeng")
}
