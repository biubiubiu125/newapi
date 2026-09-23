package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
)

func TestSetupRequiredFollowsAcceptLanguage(t *testing.T) {
	require.NoError(t, i18n.Init())
	previous := constant.Setup
	constant.Setup = false
	t.Cleanup(func() { constant.Setup = previous })

	run := func(accept string) map[string]any {
		t.Helper()
		gin.SetMode(gin.TestMode)
		engine := gin.New()
		engine.Use(I18n(), SetupRequired())
		engine.GET("/api/user/reset", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"success": true})
		})
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/user/reset", nil)
		req.Header.Set("Accept-Language", accept)
		engine.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
		var body map[string]any
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body), "body=%s", rec.Body.String())
		return body
	}

	en := run("en-US")
	require.Equal(t, false, en["success"])
	require.Equal(t, "System is not initialized", en["message"])

	zh := run("zh-CN")
	require.Equal(t, false, zh["success"])
	require.Equal(t, "系统尚未初始化", zh["message"])
}
