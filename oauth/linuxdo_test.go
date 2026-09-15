package oauth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestLinuxDORedirectURIMatchesPublicOAuthCallback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previous := system_setting.ServerAddress
	system_setting.ServerAddress = "https://panel.example.com"
	t.Cleanup(func() { system_setting.ServerAddress = previous })

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "http://api.example.com/api/oauth/linuxdo?code=abc", nil)
	c.Request.Host = "api.example.com"

	require.Equal(t, "https://panel.example.com/oauth/linuxdo", linuxDORedirectURI(c))
}

func TestLinuxDORedirectURIRejectsLocalServerAddress(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previous := system_setting.ServerAddress
	system_setting.ServerAddress = "http://localhost:3000"
	t.Cleanup(func() { system_setting.ServerAddress = previous })

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "https://panel.example.com/api/oauth/linuxdo?code=abc", nil)
	c.Request.Host = "evil.example.com"
	c.Request.Header.Set("X-Forwarded-Proto", "https")

	require.Empty(t, linuxDORedirectURI(c))
	_, err := oauthCallbackURIOrError(c, "/oauth/linuxdo")
	require.Error(t, err)
	require.Contains(t, err.Error(), "oauth server address is not configured")
}

func TestLinuxDORedirectURIRejectsEmptyServerAddress(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previous := system_setting.ServerAddress
	system_setting.ServerAddress = ""
	t.Cleanup(func() { system_setting.ServerAddress = previous })

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "https://panel.example.com/api/oauth/linuxdo?code=abc", nil)
	c.Request.Host = "panel.example.com"
	c.Request.Header.Set("X-Forwarded-Proto", "https")

	require.Empty(t, linuxDORedirectURI(c))
	_, err := oauthCallbackURIOrError(c, "/oauth/linuxdo")
	require.Error(t, err)
	require.Contains(t, err.Error(), "oauth server address is not configured")
}
