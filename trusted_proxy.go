package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

const (
	trustedProxiesEnv   = "TRUSTED_PROXIES"
	trustedIPHeadersEnv = "TRUSTED_IP_HEADERS"
)

func configureTrustedProxyPolicy(engine *gin.Engine) error {
	rawProxies := strings.TrimSpace(os.Getenv(trustedProxiesEnv))
	if err := configureTrustedProxies(engine); err != nil {
		return err
	}

	if rawProxies == "" {
		common.SetClientIPTrustConfigured(false)
		common.SysLog("TRUSTED_PROXIES is not configured; only private proxy ranges are trusted and IP-based request risk enforcement is disabled")
		return nil
	}

	if rawHeaders := strings.TrimSpace(os.Getenv(trustedIPHeadersEnv)); rawHeaders != "" {
		headers := splitNonEmptyCSV(rawHeaders)
		if len(headers) == 0 {
			return fmt.Errorf("%s must contain at least one header", trustedIPHeadersEnv)
		}
		engine.RemoteIPHeaders = headers
	}

	common.SetClientIPTrustConfigured(true)
	common.SysLog("trusted proxy policy configured; IP-based request risk enforcement is enabled")
	return nil
}

func splitNonEmptyCSV(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}
