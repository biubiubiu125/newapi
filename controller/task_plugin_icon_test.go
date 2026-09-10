package controller

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestUploadTaskPluginStoresSidecarIconAndServesIt(t *testing.T) {
	database, err := gorm.Open(sqlite.Open("file:task-plugin-icon?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(&model.TaskPlugin{}))

	previousDB := model.DB
	model.DB = database
	t.Cleanup(func() {
		model.DB = previousDB
		sqlDB, _ := database.DB()
		_ = sqlDB.Close()
		_ = jsplugin.DefaultRegistry.ReplaceOverrides(nil)
	})

	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="1" height="1"></svg>`)
	icon := "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString(svg)
	source := taskPluginAtomicitySource("icon-plugin", "1.0.0")
	body, err := json.Marshal(map[string]any{
		"source":  source,
		"enabled": true,
		"force":   true,
		"icon":    icon,
	})
	require.NoError(t, err)

	ginContext, recorder := newTaskPluginTestContext(t, http.MethodPost, "/api/plugin/task", string(body))
	UploadTaskPlugin(ginContext)

	var response struct {
		Success bool `json:"success"`
		Data    struct {
			HasIcon bool `json:"has_icon"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success, recorder.Body.String())
	assert.True(t, response.Data.HasIcon)

	iconContext, iconRecorder := newTaskPluginTestContext(t, http.MethodGet, "/api/plugin/task/icon-plugin/icon", "")
	iconContext.Params = gin.Params{{Key: "key", Value: "icon-plugin"}}
	GetTaskPluginIcon(iconContext)
	assert.Equal(t, http.StatusOK, iconRecorder.Code)
	assert.Equal(t, "image/svg+xml", iconRecorder.Header().Get("Content-Type"))
	assert.Equal(t, svg, iconRecorder.Body.Bytes())
}

func TestGetTaskPluginIconReturnsNotFoundWithoutSidecar(t *testing.T) {
	database, err := gorm.Open(sqlite.Open("file:task-plugin-icon-missing?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(&model.TaskPlugin{}))
	previousDB := model.DB
	model.DB = database
	t.Cleanup(func() {
		model.DB = previousDB
		sqlDB, _ := database.DB()
		_ = sqlDB.Close()
	})

	ginContext, recorder := newTaskPluginTestContext(t, http.MethodGet, "/api/plugin/task/missing-icon/icon", "")
	ginContext.Params = gin.Params{{Key: "key", Value: "missing-icon"}}
	GetTaskPluginIcon(ginContext)
	assert.Equal(t, http.StatusNotFound, recorder.Code)
}
