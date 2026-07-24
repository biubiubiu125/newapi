package router

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestSetApiRouterDoesNotPanic(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	require.NotPanics(t, func() {
		SetApiRouter(engine)
	})
}

func TestReferralAdminAndUserRoutesAreDistinct(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetApiRouter(engine)

	paths := map[string]bool{}
	for _, route := range engine.Routes() {
		paths[route.Method+" "+route.Path] = true
	}

	require.True(t, paths["GET /api/user/referral/commissions"])
	require.True(t, paths["GET /api/user/admin/referral/commissions"])
	require.True(t, paths["GET /api/user/admin/referral/badges"])
	require.True(t, paths["POST /api/user/admin/referral/upload"])
	require.True(t, paths["POST /api/user/admin/referral/commission-jobs/backfill-redemptions"])
	require.True(t, paths["POST /api/user/referral/upload"])
}

func TestAdminUsersSummaryRouteIsRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetApiRouter(engine)

	paths := map[string]bool{}
	for _, route := range engine.Routes() {
		paths[route.Method+" "+route.Path] = true
	}

	require.True(t, paths["GET /api/user/admin/users/summary"])
}

func TestAuthzAndChannelPermissionRoutesAreRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetApiRouter(engine)

	paths := map[string]bool{}
	for _, route := range engine.Routes() {
		paths[route.Method+" "+route.Path] = true
	}

	require.True(t, paths["GET /api/authz/catalog"])
	require.True(t, paths["POST /api/channel/:id/status"])
	require.True(t, paths["POST /api/channel/status/batch"])
	require.True(t, paths["POST /api/channel/fetch_models"])
	require.True(t, paths["POST /api/channel/codex/oauth/start"])
	require.True(t, paths["POST /api/channel/:id/codex/oauth/complete"])
	require.True(t, paths["GET /api/channel/upstream_updates/task/:task_id"])
}

func TestFlowQuotaDataRoutesAreRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetApiRouter(engine)

	paths := map[string]bool{}
	for _, route := range engine.Routes() {
		paths[route.Method+" "+route.Path] = true
	}

	require.True(t, paths["GET /api/data/flow"])
	require.True(t, paths["GET /api/data/flow/self"])
}

func TestReferralAssetRoutesSupportApiAndPublicPaths(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/referral-assets/*path", controller.GetReferralAsset)
	SetApiRouter(engine)

	paths := map[string]bool{}
	for _, route := range engine.Routes() {
		paths[route.Method+" "+route.Path] = true
	}

	require.True(t, paths["GET /api/referral-assets/*path"])
	require.True(t, paths["GET /referral-assets/*path"])
}

func setupRouterAuthTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	common.RedisEnabled = false
	common.ReferralEnabled = true
	common.CryptoSecret = "router-test-secret"
	common.SessionSecret = "router-test-session-secret"

	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db
	require.NoError(t, db.AutoMigrate(
		&model.User{},
		&model.UserLoginIdentifier{},
		&model.ReferralAffiliate{},
		&model.ReferralBinding{},
		&model.ReferralClick{},
		&model.ReferralCommissionAccount{},
		&model.ReferralCommission{},
		&model.ReferralCommissionLedger{},
		&model.ReferralWithdrawal{},
		&model.ReferralWithdrawalItem{},
		&model.ReferralSettlementBatch{},
		&model.ReferralCommissionJob{},
		&model.ReferralAdminAuditLog{},
		&model.ReferralAsset{},
	))
	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func newSessionRouter() *gin.Engine {
	engine := gin.New()
	engine.Use(middleware.I18n())
	store := cookie.NewStore([]byte(common.SessionSecret))
	store.Options(sessions.Options{
		Path:     "/",
		MaxAge:   2592000,
		HttpOnly: true,
	})
	engine.Use(sessions.Sessions("session", store))
	SetApiRouter(engine)
	return engine
}

func issueSessionCookie(t *testing.T, engine *gin.Engine, user *model.User) string {
	t.Helper()

	engine.GET("/_test/session", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("id", user.Id)
		session.Set("username", user.Username)
		session.Set("role", user.Role)
		session.Set("status", user.Status)
		session.Set("group", user.Group)
		require.NoError(t, session.Save())
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/_test/session", nil)
	req.Header.Set("New-Api-User", fmt.Sprintf("%d", user.Id))
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNoContent, rec.Code)
	cookies := rec.Result().Cookies()
	require.NotEmpty(t, cookies)
	return cookies[0].String()
}

func TestReferralAdminApiRejectsCommonUser(t *testing.T) {
	db := setupRouterAuthTestDB(t)
	user := &model.User{
		Username:    "common-user",
		Password:    "12345678",
		DisplayName: "Common User",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
	}
	require.NoError(t, db.Create(user).Error)

	engine := newSessionRouter()
	cookieValue := issueSessionCookie(t, engine, user)

	req := httptest.NewRequest(http.MethodGet, "/api/user/admin/referral/overview", nil)
	req.Header.Set("New-Api-User", fmt.Sprintf("%d", user.Id))
	req.Header.Set("Cookie", cookieValue)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"success":false`)
}

func TestReferralAdminApiAllowsAdminUser(t *testing.T) {
	db := setupRouterAuthTestDB(t)
	user := &model.User{
		Username:    "admin-user",
		Password:    "12345678",
		DisplayName: "Admin User",
		Role:        common.RoleAdminUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
	}
	require.NoError(t, db.Create(user).Error)

	engine := newSessionRouter()
	cookieValue := issueSessionCookie(t, engine, user)

	req := httptest.NewRequest(http.MethodGet, "/api/user/admin/referral/overview", nil)
	req.Header.Set("New-Api-User", fmt.Sprintf("%d", user.Id))
	req.Header.Set("Cookie", cookieValue)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"success":true`)
}
