package middleware

import (
	"fmt"
	"math"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func RegistrationCodeRiskCheck() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !common.RegistrationCodeRegisterEnabled {
			c.Next()
			return
		}
		keys := service.BuildRegistrationRiskKeys(c.ClientIP(), c.Request.Header)
		blocked, retryAfter := service.IsRegistrationRiskBlocked(keys)
		if !blocked {
			c.Next()
			return
		}
		retrySeconds := int(math.Ceil(retryAfter.Seconds()))
		if retrySeconds < 1 {
			retrySeconds = 1
		}
		c.Header("Retry-After", fmt.Sprintf("%d", retrySeconds))
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "注册尝试过多，请稍后再试",
		})
		c.Abort()
	}
}
