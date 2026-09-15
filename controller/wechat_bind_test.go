package controller

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestWeChatBindRejectsPersonalAccessToken(t *testing.T) {
	previous := common.WeChatAuthEnabled
	common.WeChatAuthEnabled = true
	t.Cleanup(func() { common.WeChatAuthEnabled = previous })

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	_, router := gin.CreateTestContext(w)
	router.Use(func(c *gin.Context) {
		c.Set("id", 42)
		c.Set("use_access_token", true)
		c.Next()
	})
	router.POST("/api/oauth/wechat/bind", WeChatBind)

	req := httptest.NewRequest(http.MethodPost, "/api/oauth/wechat/bind", strings.NewReader(`{"code":"wx-code"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var response map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.Equal(t, false, response["success"])
	require.Contains(t, fmt.Sprint(response["message"]), "当前认证方式不支持绑定微信")
}

func TestUnbindCustomOAuthRejectsPersonalAccessToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	_, router := gin.CreateTestContext(w)
	router.Use(func(c *gin.Context) {
		c.Set("id", 42)
		c.Set("use_access_token", true)
		c.Next()
	})
	router.DELETE("/api/user/oauth/bindings/:provider_id", UnbindCustomOAuth)

	req := httptest.NewRequest(http.MethodDelete, "/api/user/oauth/bindings/7", nil)
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var response map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.Equal(t, false, response["success"])
	require.Contains(t, fmt.Sprint(response["message"]), "当前认证方式不支持解绑")
}
