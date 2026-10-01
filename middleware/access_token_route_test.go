package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDashboardAccessTokenCannotPerformAdminWritesOrMintRelayTokens(t *testing.T) {
	setupDashboardAuthMiddlewareTest(t)
	gin.SetMode(gin.TestMode)

	adminToken := "admin-pat-money-write-01"
	admin := &model.User{
		Username: "admin-pat-money", Password: "password-placeholder", Role: common.RoleAdminUser,
		Status: common.UserStatusEnabled, Group: "default", AccessToken: &adminToken, AuthVersion: 1,
		AffCode: "middleware-aff-admin-pat-money", Quota: 1000,
	}
	require.NoError(t, model.DB.Create(admin).Error)
	userToken := "user-pat-mint-token-01"
	user := createMiddlewarePATUser(t, "user-pat-mint", userToken)

	now := time.Now().Unix()
	session := &model.UserSession{
		SID: "admin-session-money", UserID: admin.Id, Version: 1, UserAuthVersion: admin.AuthVersion,
		Status: model.UserSessionStatusActive, RefreshHash: "refresh-hash", LoginMethod: "password",
		LastActiveAt: now, ExpiresAt: now + 3600,
	}
	require.NoError(t, model.CreateUserSession(session))
	sessionToken, _, err := service.IssueAccessToken(service.AuthIdentity{
		UserID: admin.Id, SessionID: session.SID, UserAuthVersion: admin.AuthVersion, SessionVersion: session.Version,
	})
	require.NoError(t, err)

	reached := false
	mark := func(c *gin.Context) {
		reached = true
		c.Status(http.StatusNoContent)
	}
	router := gin.New()
	router.POST("/api/user/manage", AdminAuth(), mark)
	router.POST("/api/user/topup/complete", AdminAuth(), mark)
	router.POST("/api/redemption/", AdminAuth(), mark)
	router.POST("/api/user/admin/finance/payment-orphans/:id/credit", AdminAuth(), mark)
	router.POST("/api/user/admin/referral/withdrawals/:id/pay", AdminAuth(), mark)
	router.POST("/api/subscription/admin/bind", AdminAuth(), mark)
	router.POST("/api/token/", UserAuth(), mark)
	router.PUT("/api/token/", UserAuth(), mark)
	router.DELETE("/api/token/:id", UserAuth(), mark)
	router.POST("/api/token/batch", UserAuth(), mark)
	router.POST("/api/token/batch/keys", UserAuth(), mark)
	router.POST("/api/token/:id/key", UserAuth(), mark)
	router.GET("/api/token/", UserAuth(), mark)
	router.GET("/api/user/", AdminAuth(), mark)

	forbidden := []struct {
		method, path, token string
	}{
		{http.MethodPost, "/api/user/manage", adminToken},
		{http.MethodPost, "/api/user/topup/complete", adminToken},
		{http.MethodPost, "/api/redemption/", adminToken},
		{http.MethodPost, "/api/user/admin/finance/payment-orphans/9/credit", adminToken},
		{http.MethodPost, "/api/user/admin/referral/withdrawals/9/pay", adminToken},
		{http.MethodPost, "/api/subscription/admin/bind", adminToken},
		{http.MethodPost, "/api/token/", userToken},
		{http.MethodPut, "/api/token/", userToken},
		{http.MethodDelete, "/api/token/4", userToken},
		{http.MethodPost, "/api/token/batch", userToken},
		{http.MethodPost, "/api/token/batch/keys", userToken},
		{http.MethodPost, "/api/token/4/key", userToken},
	}
	for _, tc := range forbidden {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			reached = false
			response := httptest.NewRecorder()
			request := httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{}`))
			request.Header.Set("Authorization", "Bearer "+tc.token)
			router.ServeHTTP(response, request)
			assert.False(t, reached)
			assert.Equal(t, http.StatusForbidden, response.Code)
			assert.Contains(t, response.Body.String(), "ACCESS_TOKEN_FORBIDDEN")
		})
	}

	allowed := []struct {
		method, path, token string
	}{
		{http.MethodGet, "/api/token/", userToken},
		{http.MethodGet, "/api/user/", adminToken},
		{http.MethodPost, "/api/user/manage", sessionToken},
	}
	for _, tc := range allowed {
		t.Run("allow "+tc.method+" "+tc.path, func(t *testing.T) {
			reached = false
			response := httptest.NewRecorder()
			request := httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{}`))
			request.Header.Set("Authorization", tc.token)
			if tc.token == sessionToken || tc.token == adminToken || tc.token == userToken {
				request.Header.Set("Authorization", "Bearer "+tc.token)
			}
			router.ServeHTTP(response, request)
			assert.Equal(t, http.StatusNoContent, response.Code)
			assert.True(t, reached)
		})
	}
	require.NotZero(t, user.Id)
}
