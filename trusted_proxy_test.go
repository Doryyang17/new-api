package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigureTrustedProxyPolicyLeavesIPEnforcementDisabledWithoutConfig(t *testing.T) {
	t.Cleanup(func() { common.SetClientIPTrustConfigured(false) })
	t.Setenv(trustedProxiesEnv, "")
	t.Setenv(trustedIPHeadersEnv, "")
	common.SetClientIPTrustConfigured(true)

	engine := gin.New()
	require.NoError(t, configureTrustedProxyPolicy(engine))
	assert.False(t, common.IsClientIPTrustConfigured())
}

func TestConfigureTrustedProxyPolicySupportsDirectConnections(t *testing.T) {
	t.Cleanup(func() { common.SetClientIPTrustConfigured(false) })
	t.Setenv(trustedProxiesEnv, "none")
	t.Setenv(trustedIPHeadersEnv, "X-Forwarded-For")

	engine := gin.New()
	engine.GET("/ip", func(c *gin.Context) {
		c.String(http.StatusOK, c.ClientIP())
	})
	require.NoError(t, configureTrustedProxyPolicy(engine))

	req := httptest.NewRequest(http.MethodGet, "/ip", nil)
	req.RemoteAddr = "203.0.113.9:1234"
	req.Header.Set("X-Forwarded-For", "198.51.100.7")
	resp := httptest.NewRecorder()
	engine.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusOK, resp.Code)
	assert.Equal(t, "203.0.113.9", resp.Body.String())
	assert.True(t, common.IsClientIPTrustConfigured())
}

func TestConfigureTrustedProxyPolicyUsesHeadersFromTrustedProxy(t *testing.T) {
	t.Cleanup(func() { common.SetClientIPTrustConfigured(false) })
	t.Setenv(trustedProxiesEnv, "10.0.0.0/8")
	t.Setenv(trustedIPHeadersEnv, "CF-Connecting-IP,X-Forwarded-For")

	engine := gin.New()
	engine.GET("/ip", func(c *gin.Context) {
		c.String(http.StatusOK, c.ClientIP())
	})
	require.NoError(t, configureTrustedProxyPolicy(engine))

	req := httptest.NewRequest(http.MethodGet, "/ip", nil)
	req.RemoteAddr = "10.0.0.5:1234"
	req.Header.Set("CF-Connecting-IP", "198.51.100.8")
	resp := httptest.NewRecorder()
	engine.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusOK, resp.Code)
	assert.Equal(t, "198.51.100.8", resp.Body.String())
}
