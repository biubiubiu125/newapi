package oauth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGitHubCallbackURIUsesConfiguredServerAddress(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previous := system_setting.ServerAddress
	system_setting.ServerAddress = "https://panel.example.com"
	t.Cleanup(func() { system_setting.ServerAddress = previous })

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "http://api.example.com/api/oauth/github?code=abc", nil)
	c.Request.Host = "api.example.com"

	uri, err := oauthCallbackURIOrError(c, "/oauth/github")
	require.NoError(t, err)
	require.Equal(t, "https://panel.example.com/oauth/github", uri)
}

func TestGitHubCallbackURIRejectsLocalServerAddress(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previous := system_setting.ServerAddress
	system_setting.ServerAddress = "http://127.0.0.1:3000"
	t.Cleanup(func() { system_setting.ServerAddress = previous })

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "http://127.0.0.1:3000/api/oauth/github?code=abc", nil)
	c.Request.Host = "127.0.0.1:3000"

	_, err := oauthCallbackURIOrError(c, "/oauth/github")
	require.Error(t, err)
	require.Contains(t, err.Error(), "oauth server address is not configured")
}

func TestGitHubCallbackURIRejectsEmptyServerAddress(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previous := system_setting.ServerAddress
	system_setting.ServerAddress = ""
	t.Cleanup(func() { system_setting.ServerAddress = previous })

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "https://panel.example.com/api/oauth/github?code=abc", nil)
	c.Request.Host = "panel.example.com"

	_, err := oauthCallbackURIOrError(c, "/oauth/github")
	require.Error(t, err)
	require.Contains(t, err.Error(), "oauth server address is not configured")
}
