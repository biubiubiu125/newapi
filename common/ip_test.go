package common

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestGetClientIPUsesTrustedProxyHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTrustedProxyTestConfig(t)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("CF-Connecting-IP", "203.0.113.10")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = req

	if got := GetClientIP(c); got != "203.0.113.10" {
		t.Fatalf("GetClientIP() = %q, want 203.0.113.10", got)
	}
}

func TestGetClientIPIgnoresHeadersFromUntrustedRemote(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTrustedProxyTestConfig(t)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "198.51.100.2:12345"
	req.Header.Set("CF-Connecting-IP", "203.0.113.10")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = req

	if got := GetClientIP(c); got != "198.51.100.2" {
		t.Fatalf("GetClientIP() = %q, want 198.51.100.2", got)
	}
}

func TestGetClientIPDoesNotTrustHeadersByDefault(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Cleanup(InitTrustedProxyConfig)
	InitTrustedProxyConfig()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("CF-Connecting-IP", "203.0.113.10")
	req.Header.Set("X-Forwarded-For", "203.0.113.12")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = req

	if got := GetClientIP(c); got != "127.0.0.1" {
		t.Fatalf("GetClientIP() = %q, want 127.0.0.1", got)
	}
}

func TestGetClientIPAllowsEmptyEnvToDisableTrust(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Cleanup(InitTrustedProxyConfig)
	t.Setenv(trustedProxyHeadersEnv, "")
	t.Setenv(trustedProxyCidrsEnv, "")
	InitTrustedProxyConfig()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("CF-Connecting-IP", "203.0.113.10")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = req

	if got := GetClientIP(c); got != "127.0.0.1" {
		t.Fatalf("GetClientIP() = %q, want 127.0.0.1", got)
	}
}

func TestGetClientIPUsesForwardedForNearestUntrustedIP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTrustedProxyTestConfig(t)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.5:12345"
	req.Header.Set("X-Forwarded-For", "bad-ip, 203.0.113.11, 198.51.100.9")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = req

	if got := GetClientIP(c); got != "198.51.100.9" {
		t.Fatalf("GetClientIP() = %q, want 198.51.100.9", got)
	}
}

func TestGetClientIPSkipsTrustedForwardedForHops(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTrustedProxyTestConfig(t)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("X-Forwarded-For", "203.0.113.12, 10.0.0.6")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = req

	if got := GetClientIP(c); got != "203.0.113.12" {
		t.Fatalf("GetClientIP() = %q, want 203.0.113.12", got)
	}
}

func TestGetClientIPDoesNotTrustPrivateRemoteByDefault(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Cleanup(InitTrustedProxyConfig)
	t.Setenv(trustedProxyHeadersEnv, "X-Forwarded-For")
	t.Setenv(trustedProxyCidrsEnv, "")
	InitTrustedProxyConfig()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.5:12345"
	req.Header.Set("X-Forwarded-For", "203.0.113.12")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = req

	if got := GetClientIP(c); got != "10.0.0.5" {
		t.Fatalf("GetClientIP() = %q, want 10.0.0.5", got)
	}
}

func TestIsPrivateIPIncludesIPv6UniqueLocal(t *testing.T) {
	if !IsPrivateIP(ParseIP("fd00::1")) {
		t.Fatal("IsPrivateIP(fd00::1) = false, want true")
	}
}

func initTrustedProxyTestConfig(t *testing.T) {
	t.Helper()
	t.Cleanup(InitTrustedProxyConfig)
	t.Setenv(trustedProxyHeadersEnv, "CF-Connecting-IP,True-Client-IP,X-Real-IP,X-Forwarded-For")
	t.Setenv(trustedProxyCidrsEnv, "127.0.0.0/8,10.0.0.0/8")
	InitTrustedProxyConfig()
}
