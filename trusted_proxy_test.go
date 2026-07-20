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

func TestConfigureTrustedProxyPolicyUsesSafeDefaultsWithoutEnablingIPRisk(t *testing.T) {
	t.Cleanup(func() { common.SetClientIPTrustConfigured(false) })
	t.Setenv(trustedProxiesEnv, "")
	t.Setenv(trustedIPHeadersEnv, "")
	common.SetClientIPTrustConfigured(true)

	engine := gin.New()
	engine.GET("/ip", func(c *gin.Context) {
		c.String(http.StatusOK, c.ClientIP())
	})
	require.NoError(t, configureTrustedProxyPolicy(engine))

	publicReq := httptest.NewRequest(http.MethodGet, "/ip", nil)
	publicReq.RemoteAddr = "198.51.100.10:1234"
	publicReq.Header.Set("X-Forwarded-For", "203.0.113.10")
	publicResp := httptest.NewRecorder()
	engine.ServeHTTP(publicResp, publicReq)
	assert.Equal(t, "198.51.100.10", publicResp.Body.String())

	privateReq := httptest.NewRequest(http.MethodGet, "/ip", nil)
	privateReq.RemoteAddr = "172.20.0.2:1234"
	privateReq.Header.Set("X-Forwarded-For", "203.0.113.11")
	privateResp := httptest.NewRecorder()
	engine.ServeHTTP(privateResp, privateReq)
	assert.Equal(t, "203.0.113.11", privateResp.Body.String())

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
