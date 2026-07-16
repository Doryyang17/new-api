package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestWriteAvailabilityResponseIsTerminalForAllProtocols(t *testing.T) {
	gin.SetMode(gin.TestMode)
	status := system_setting.AvailabilityStatus{
		Message:           "curfew active",
		Code:              system_setting.AvailabilityRejectCode,
		RetryAfterSeconds: 3600,
	}
	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{
			name: "dashboard api",
			path: "/api/test",
			expected: `{
				"success": false,
				"message": "curfew active",
				"code": "system_curfew"
			}`,
		},
		{
			name: "claude messages",
			path: "/v1/messages",
			expected: `{
				"type": "error",
				"error": {
					"type": "system_curfew",
					"message": "curfew active"
				}
			}`,
		},
		{
			name: "midjourney",
			path: "/mj/submit/imagine",
			expected: `{
				"description": "curfew active",
				"type": "permission_error",
				"code": "system_curfew"
			}`,
		},
		{
			name: "task api",
			path: "/suno/submit/music",
			expected: `{
				"code": "system_curfew",
				"message": "curfew active"
			}`,
		},
		{
			name: "openai responses",
			path: "/v1/responses",
			expected: `{
				"error": {
					"message": "curfew active",
					"type": "permission_error",
					"param": "",
					"code": "system_curfew"
				}
			}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(recorder)
			context.Request = httptest.NewRequest(http.MethodPost, test.path, nil)

			writeAvailabilityResponse(context, status)

			require.Equal(t, http.StatusForbidden, recorder.Code)
			require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
			require.Empty(t, recorder.Header().Get("Retry-After"))
			require.JSONEq(t, test.expected, recorder.Body.String())
		})
	}
}
