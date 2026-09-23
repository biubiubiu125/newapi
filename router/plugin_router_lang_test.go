package router

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

func TestImportPluginDispatchStateCopiesUserSettingLanguage(t *testing.T) {
	require.NoError(t, i18n.Init())
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(importPluginDispatchState())
	engine.GET("/plugin-lang", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"id":   c.GetInt("id"),
			"lang": i18n.GetLangFromContext(c),
		})
	})

	state := &pluginDispatchState{
		language: "zh-CN",
		userID:   42,
		userSetting: dto.UserSetting{
			Language: "en",
		},
		hasUserSetting: true,
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/plugin-lang", nil)
	req.Header.Set("Accept-Language", "zh-CN")
	req = req.WithContext(context.WithValue(req.Context(), pluginDispatchStateKey{}, state))
	engine.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body), "body=%s", rec.Body.String())
	require.Equal(t, float64(42), body["id"])
	require.Equal(t, "en", body["lang"])
}
