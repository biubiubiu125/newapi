package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestDistributeStopsWhenSelectedChannelHasNoEnabledKey(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Channel{}))

	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = previousDB
		_ = sqlDB.Close()
	})

	require.NoError(t, db.Create(&model.Channel{
		Id:     1,
		Key:    "",
		Status: common.ChannelStatusEnabled,
		ChannelInfo: model.ChannelInfo{
			IsMultiKey: true,
		},
	}).Error)

	nextCalled := false
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		common.SetContextKey(c, constant.ContextKeyTokenSpecificChannelId, "1")
		c.Next()
	})
	engine.POST("/v1/chat/completions", Distribute(), func(c *gin.Context) {
		nextCalled = true
		c.Status(http.StatusNoContent)
	})

	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)

	require.False(t, nextCalled)
	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"channel:no_available_key"`)
}

func TestDistributeSkipsChannelWithNoEnabledKeyAndUsesFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}))

	previousDB := model.DB
	previousCache := common.MemoryCacheEnabled
	model.DB = db
	common.MemoryCacheEnabled = true
	t.Cleanup(func() {
		model.DB = previousDB
		common.MemoryCacheEnabled = previousCache
		_ = sqlDB.Close()
	})

	high := int64(10)
	low := int64(0)
	weight := uint(100)
	require.NoError(t, db.Create(&model.Channel{
		Id:       11,
		Type:     1,
		Key:      "dead-1\ndead-2",
		Status:   common.ChannelStatusEnabled,
		Name:     "dead-channel",
		Group:    "default",
		Models:   "gpt-4o",
		Priority: &high,
		Weight:   &weight,
		ChannelInfo: model.ChannelInfo{
			IsMultiKey:         true,
			MultiKeySize:       2,
			MultiKeyStatusList: map[int]int{0: common.ChannelStatusAutoDisabled, 1: common.ChannelStatusAutoDisabled},
		},
	}).Error)
	require.NoError(t, db.Create(&model.Channel{
		Id:       12,
		Type:     1,
		Key:      "live-key",
		Status:   common.ChannelStatusEnabled,
		Name:     "live-channel",
		Group:    "default",
		Models:   "gpt-4o",
		Priority: &low,
		Weight:   &weight,
	}).Error)
	model.InitChannelCache()

	nextCalled := false
	selectedID := 0
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		common.SetContextKey(c, constant.ContextKeyUsingGroup, "default")
		c.Next()
	})
	engine.POST("/v1/chat/completions", Distribute(), func(c *gin.Context) {
		nextCalled = true
		selectedID = common.GetContextKeyInt(c, constant.ContextKeyChannelId)
		c.Status(http.StatusNoContent)
	})

	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)

	require.True(t, nextCalled, "status=%d body=%s", recorder.Code, recorder.Body.String())
	require.Equal(t, http.StatusNoContent, recorder.Code)
	require.Equal(t, 12, selectedID)
}

func TestDistributeAllowsTaskFetchWithoutSelectingChannel(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, path := range []string{
		"/v1/video/generations/task_abc",
		"/mj/task/task_abc/fetch",
	} {
		t.Run(path, func(t *testing.T) {
			nextCalled := false
			engine := gin.New()
			engine.Use(func(c *gin.Context) {
				c.Set("id", 1)
				common.SetContextKey(c, constant.ContextKeyUsingGroup, "default")
				c.Next()
			})
			engine.GET(path, Distribute(), func(c *gin.Context) {
				nextCalled = true
				c.Status(http.StatusNoContent)
			})

			request := httptest.NewRequest(http.MethodGet, path, nil)
			recorder := httptest.NewRecorder()
			engine.ServeHTTP(recorder, request)

			require.True(t, nextCalled, "fetch should reach handler, status=%d body=%s", recorder.Code, recorder.Body.String())
			require.Equal(t, http.StatusNoContent, recorder.Code)
		})
	}
}

func TestDistributeAllowsFetchWhenTokenModelLimitEnabledWithoutOriginModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	nextCalled := false
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set("id", 1)
		common.SetContextKey(c, constant.ContextKeyUsingGroup, "default")
		common.SetContextKey(c, constant.ContextKeyTokenModelLimitEnabled, true)
		common.SetContextKey(c, constant.ContextKeyTokenModelLimit, map[string]bool{"mj_imagine": true})
		c.Next()
	})
	engine.GET("/mj/task/:id/fetch", Distribute(), func(c *gin.Context) {
		nextCalled = true
		c.Status(http.StatusNoContent)
	})

	request := httptest.NewRequest(http.MethodGet, "/mj/task/task_abc/fetch", nil)
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)

	require.True(t, nextCalled, "fetch should reach handler, status=%d body=%s", recorder.Code, recorder.Body.String())
	require.Equal(t, http.StatusNoContent, recorder.Code)
}

func TestGetModelRequestRemixBackfillsOriginModelAndSelectsChannel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Task{}))
	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = previousDB
		_ = sqlDB.Close()
	})
	require.NoError(t, db.Create(&model.Task{
		TaskID: "origin-video",
		UserId: 9,
		Properties: model.Properties{
			OriginModelName: "sora-2",
		},
	}).Error)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos/origin-video/remix", strings.NewReader(`{"prompt":"again"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "video_id", Value: "origin-video"}}
	c.Set("id", 9)

	req, shouldSelect, err := getModelRequest(c)
	require.NoError(t, err)
	require.True(t, shouldSelect)
	require.Equal(t, "sora-2", req.Model)
}

func TestDistributeHidesDatabaseErrorWhenSelectingChannel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	require.NoError(t, i18n.Init())

	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}))

	previousDB := model.DB
	previousCache := common.MemoryCacheEnabled
	model.DB = db
	common.MemoryCacheEnabled = false
	require.NoError(t, sqlDB.Close())
	t.Cleanup(func() {
		model.DB = previousDB
		common.MemoryCacheEnabled = previousCache
	})

	nextCalled := false
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		common.SetContextKey(c, constant.ContextKeyUsingGroup, "default")
		c.Next()
	})
	engine.POST("/v1/chat/completions", Distribute(), func(c *gin.Context) {
		nextCalled = true
		c.Status(http.StatusNoContent)
	})

	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)

	require.False(t, nextCalled)
	require.NotEqual(t, http.StatusNoContent, recorder.Code)
	body := strings.ToLower(recorder.Body.String())
	require.NotContains(t, body, "sql:")
	require.NotContains(t, body, "database is closed")
	require.NotContains(t, body, "sqlite")
	require.Contains(t, body, `"type":"new_api_error"`)
}
