package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestTokenOrUserAuthSessionBranchWritesUserContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte("token-or-user-auth-test"))))
	router.Use(func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("id", 42)
		session.Set("status", common.UserStatusEnabled)
		session.Set("username", "session-owner")
		session.Set("group", "vip")
		c.Next()
	})
	router.GET("/proxy", TokenOrUserAuth(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"id":               c.GetInt("id"),
			"username":         c.GetString("username"),
			"context_username": common.GetContextKeyString(c, constant.ContextKeyUserName),
			"group":            c.GetString("group"),
			"user_group":       c.GetString("user_group"),
			"using_group":      common.GetContextKeyString(c, constant.ContextKeyUsingGroup),
		})
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/proxy", nil))

	require.Equal(t, http.StatusOK, recorder.Code)
	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	require.EqualValues(t, 42, body["id"])
	require.Equal(t, "session-owner", body["username"])
	require.Equal(t, "session-owner", body["context_username"])
	require.Equal(t, "vip", body["group"])
	require.Equal(t, "vip", body["user_group"])
	require.Equal(t, "vip", body["using_group"])
}

func TestTokenOrUserAuthSessionBranchPrefersCurrentUserCache(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}))

	oldDB := model.DB
	oldRedisEnabled := common.RedisEnabled
	model.DB = db
	common.RedisEnabled = false
	t.Cleanup(func() {
		model.DB = oldDB
		common.RedisEnabled = oldRedisEnabled
	})

	require.NoError(t, model.DB.Create(&model.User{
		Id:       43,
		Username: "current-owner",
		Password: "password123",
		Group:    "current-group",
		Status:   common.UserStatusEnabled,
	}).Error)

	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte("token-or-user-auth-current-test"))))
	router.Use(func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("id", 43)
		session.Set("status", common.UserStatusEnabled)
		session.Set("username", "stale-owner")
		session.Set("group", "stale-group")
		c.Next()
	})
	router.GET("/proxy", TokenOrUserAuth(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"username":         c.GetString("username"),
			"context_username": common.GetContextKeyString(c, constant.ContextKeyUserName),
			"group":            c.GetString("group"),
			"user_group":       c.GetString("user_group"),
			"using_group":      common.GetContextKeyString(c, constant.ContextKeyUsingGroup),
		})
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/proxy", nil))

	require.Equal(t, http.StatusOK, recorder.Code)
	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	require.Equal(t, "current-owner", body["username"])
	require.Equal(t, "current-owner", body["context_username"])
	require.Equal(t, "current-group", body["group"])
	require.Equal(t, "current-group", body["user_group"])
	require.Equal(t, "current-group", body["using_group"])
}
