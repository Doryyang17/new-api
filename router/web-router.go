package router

import (
	"bytes"
	"embed"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-contrib/gzip"
	"github.com/gin-contrib/static"
	"github.com/gin-gonic/gin"
)

// WebAssets holds the embedded dashboard frontend assets.
type WebAssets struct {
	BuildFS   embed.FS
	IndexPage []byte
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

func SetWebRouter(router *gin.Engine, assets WebAssets, pluginDispatcher gin.HandlerFunc) {
	frontendFS := common.EmbedFolder(assets.BuildFS, "web/dist")

	router.NoRoute(
		pluginDispatcher,
		middleware.RouteTag("web"),
		gzip.Gzip(gzip.DefaultCompression),
		middleware.AccessTokenAudit(),
		middleware.GlobalWebRateLimit(),
		middleware.Cache(),
		static.Serve("/", frontendFS),
		func(c *gin.Context) {
			availabilityStatus := system_setting.GetAvailabilityStatus()
			if isAPINotFoundPath(c.Request.RequestURI) || strings.HasPrefix(c.Request.RequestURI, "/assets") {
				if isModelAPIPath(c.Request.RequestURI) && availabilityStatus.Unavailable {
					middleware.HandleSystemAvailability(c)
					return
				}
				controller.RelayNotFound(c)
				return
			}
			if availabilityStatus.Unavailable {
				c.Header("Cache-Control", "no-store")
			} else {
				c.Header("Cache-Control", "no-cache")
			}
			c.Data(http.StatusOK, "text/html; charset=utf-8", injectAvailabilityStatus(assets.IndexPage, availabilityStatus))
		},
	)
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
	encoded, err := common.Marshal(payload)
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
