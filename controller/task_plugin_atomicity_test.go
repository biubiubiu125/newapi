package controller

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestUploadTaskPluginRollsBackOnSyncFailure(t *testing.T) {
	database, err := gorm.Open(sqlite.Open("file:task-plugin-upload-rollback?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(&model.TaskPlugin{}))

	previousDB := model.DB
	model.DB = database
	t.Cleanup(func() {
		model.DB = previousDB
		sqlDB, _ := database.DB()
		_ = sqlDB.Close()
		_ = jsplugin.DefaultRegistry.SetGenerationPreparer(nil)
		_ = jsplugin.DefaultRegistry.ReplaceOverrides(nil)
	})

	require.NoError(t, jsplugin.DefaultRegistry.SetGenerationPreparer(func(candidate, _ *jsplugin.RoutingGeneration) (jsplugin.PreparedRoutingGeneration, error) {
		if candidate != nil {
			if _, ok := candidate.Get("rollback-upload"); ok {
				return jsplugin.PreparedRoutingGeneration{Generation: candidate}, errors.New("forced publish failure")
			}
		}
		return jsplugin.PreparedRoutingGeneration{Generation: candidate}, nil
	}))

	ginContext, recorder := newTaskPluginTestContext(t, http.MethodPost, "/api/task-plugins", `{
		"source":"export const meta = {apiVersion:1,key:\"rollback-upload\",name:\"Rollback Upload\",version:\"1.0.0\",author:{name:\"Test\"},models:[\"rollback-upload-model\"],fetchMode:\"per_task\"};export function buildSubmitRequest(){return {}}export function parseSubmitResponse(){return {}}export function buildQueryRequest(){return {}}export function parseTaskResult(){return {}}",
		"enabled":true,
		"force":true
	}`)

	UploadTaskPlugin(ginContext)

	var response struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.False(t, response.Success)
	var plugins []model.TaskPlugin
	require.NoError(t, database.Where("key = ?", "rollback-upload").Find(&plugins).Error)
	assert.Empty(t, plugins)
}

func TestUploadTaskPluginSucceedsWhenAnotherPluginIsBroken(t *testing.T) {
	database, err := gorm.Open(sqlite.Open("file:task-plugin-upload-partial?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(&model.TaskPlugin{}))

	previousDB := model.DB
	model.DB = database
	t.Cleanup(func() {
		model.DB = previousDB
		sqlDB, _ := database.DB()
		_ = sqlDB.Close()
		_ = jsplugin.DefaultRegistry.SetGenerationPreparer(nil)
		_ = jsplugin.DefaultRegistry.ReplaceOverrides(nil)
	})

	require.NoError(t, database.Create(&model.TaskPlugin{
		Key:        "broken-plugin",
		APIVersion: 1,
		Version:    "1.0.0",
		Source:     `export const meta = {`,
		SourceHash: "broken",
		Enabled:    true,
		Active:     true,
	}).Error)

	ginContext, recorder := newTaskPluginTestContext(t, http.MethodPost, "/api/task-plugins", `{
		"source":"export const meta = {apiVersion:1,key:\"healthy-upload\",name:\"Healthy Upload\",version:\"1.0.0\",author:{name:\"Test\"},models:[\"healthy-upload-model\"],fetchMode:\"per_task\"};export function buildSubmitRequest(){return {}}export function parseSubmitResponse(){return {}}export function buildQueryRequest(){return {}}export function parseTaskResult(){return {}}",
		"enabled":false
	}`)

	UploadTaskPlugin(ginContext)

	var response struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success, recorder.Body.String())
	var plugins []model.TaskPlugin
	require.NoError(t, database.Where("key = ?", "healthy-upload").Find(&plugins).Error)
	require.Len(t, plugins, 1)
}

func TestDeleteTaskPluginVersionRollsBackOnSyncFailure(t *testing.T) {
	database, err := gorm.Open(sqlite.Open("file:task-plugin-delete-rollback?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(&model.Channel{}, &model.Task{}, &model.TaskPlugin{}))

	previousDB := model.DB
	model.DB = database
	t.Cleanup(func() {
		model.DB = previousDB
		sqlDB, _ := database.DB()
		_ = sqlDB.Close()
		_ = jsplugin.DefaultRegistry.SetGenerationPreparer(nil)
		_ = jsplugin.DefaultRegistry.ReplaceOverrides(nil)
	})

	key := "delete-rollback"
	activeKey := key
	version1Source := taskPluginAtomicitySource(key, "1.0.0")
	version2Source := taskPluginAtomicitySource(key, "2.0.0")
	require.NoError(t, database.Create(&model.TaskPlugin{
		Key:        key,
		APIVersion: 1,
		Version:    "1.0.0",
		Source:     version1Source,
		SourceHash: taskPluginAtomicitySourceHash(version1Source),
		Enabled:    true,
		Active:     false,
		CreatedAt:  1000,
	}).Error)
	require.NoError(t, database.Create(&model.TaskPlugin{
		Key:        key,
		APIVersion: 1,
		Version:    "2.0.0",
		Source:     version2Source,
		SourceHash: taskPluginAtomicitySourceHash(version2Source),
		Enabled:    true,
		Active:     true,
		ActiveKey:  &activeKey,
		CreatedAt:  2000,
	}).Error)
	triggerPublishFailure := installDeferredTaskPluginPublishFailure(t)
	triggerPublishFailure()

	ginContext, recorder := newTaskPluginTestContext(t, http.MethodDelete, "/api/task-plugins/delete-rollback/versions/2.0.0", "")
	ginContext.Params = gin.Params{{Key: "key", Value: key}, {Key: "version", Value: "2.0.0"}}

	DeleteTaskPluginVersion(ginContext)

	var response struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.False(t, response.Success)
	var active model.TaskPlugin
	require.NoError(t, database.Where("key = ? AND version = ?", key, "2.0.0").First(&active).Error)
	assert.True(t, active.Active)
	assert.True(t, active.Enabled)
	var older model.TaskPlugin
	require.NoError(t, database.Where("key = ? AND version = ?", key, "1.0.0").First(&older).Error)
	assert.False(t, older.Active)
}

func TestSetTaskPluginStatusRollsBackOnSyncFailure(t *testing.T) {
	database, err := gorm.Open(sqlite.Open("file:task-plugin-status-rollback?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(&model.Channel{}, &model.Task{}, &model.TaskPlugin{}))

	previousDB := model.DB
	model.DB = database
	t.Cleanup(func() {
		model.DB = previousDB
		sqlDB, _ := database.DB()
		_ = sqlDB.Close()
		_ = jsplugin.DefaultRegistry.SetGenerationPreparer(nil)
		_ = jsplugin.DefaultRegistry.ReplaceOverrides(nil)
	})

	key := "status-rollback"
	activeKey := key
	source := taskPluginAtomicitySource(key, "1.0.0")
	require.NoError(t, database.Create(&model.TaskPlugin{
		Key:        key,
		APIVersion: 1,
		Version:    "1.0.0",
		Source:     source,
		SourceHash: taskPluginAtomicitySourceHash(source),
		Enabled:    true,
		Active:     true,
		ActiveKey:  &activeKey,
		CreatedAt:  1000,
	}).Error)
	compiled, err := jsplugin.CompilePlugin(source, jsplugin.Options{Key: key, Version: "1.0.0"})
	require.NoError(t, err)
	require.NoError(t, jsplugin.DefaultRegistry.ReplaceOverrides([]*jsplugin.LoadedPlugin{compiled}))
	triggerPublishFailure := installDeferredTaskPluginPublishFailure(t)
	triggerPublishFailure()

	ginContext, recorder := newTaskPluginTestContext(t, http.MethodPost, "/api/task-plugins/status-rollback/status", `{"enabled":false}`)
	ginContext.Params = gin.Params{{Key: "key", Value: key}}

	SetTaskPluginStatus(ginContext)

	var response struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.False(t, response.Success)
	var plugin model.TaskPlugin
	require.NoError(t, database.Where("key = ? AND version = ?", key, "1.0.0").First(&plugin).Error)
	assert.True(t, plugin.Active)
	assert.True(t, plugin.Enabled)
}

func TestActivateTaskPluginRollsBackOnSyncFailure(t *testing.T) {
	database, err := gorm.Open(sqlite.Open("file:task-plugin-activate-rollback?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(&model.TaskPlugin{}))

	previousDB := model.DB
	model.DB = database
	t.Cleanup(func() {
		model.DB = previousDB
		sqlDB, _ := database.DB()
		_ = sqlDB.Close()
		_ = jsplugin.DefaultRegistry.SetGenerationPreparer(nil)
		_ = jsplugin.DefaultRegistry.ReplaceOverrides(nil)
	})

	key := "activate-rollback"
	activeKey := key
	olderSource := taskPluginAtomicitySource(key, "1.0.0")
	newerSource := taskPluginAtomicitySource(key, "2.0.0")
	require.NoError(t, database.Create(&model.TaskPlugin{
		Key:        key,
		APIVersion: 1,
		Version:    "1.0.0",
		Source:     olderSource,
		SourceHash: taskPluginAtomicitySourceHash(olderSource),
		Enabled:    true,
		Active:     false,
		CreatedAt:  1000,
	}).Error)
	require.NoError(t, database.Create(&model.TaskPlugin{
		Key:        key,
		APIVersion: 1,
		Version:    "2.0.0",
		Source:     newerSource,
		SourceHash: taskPluginAtomicitySourceHash(newerSource),
		Enabled:    true,
		Active:     true,
		ActiveKey:  &activeKey,
		CreatedAt:  2000,
	}).Error)
	triggerPublishFailure := installDeferredTaskPluginPublishFailure(t)
	triggerPublishFailure()

	ginContext, recorder := newTaskPluginTestContext(t, http.MethodPost, "/api/task-plugins/activate-rollback/activate", `{"version":"1.0.0"}`)
	ginContext.Params = gin.Params{{Key: "key", Value: key}}

	ActivateTaskPlugin(ginContext)

	var response struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.False(t, response.Success)
	var active model.TaskPlugin
	require.NoError(t, database.Where("key = ? AND version = ?", key, "2.0.0").First(&active).Error)
	assert.True(t, active.Active)
	assert.True(t, active.Enabled)
	var older model.TaskPlugin
	require.NoError(t, database.Where("key = ? AND version = ?", key, "1.0.0").First(&older).Error)
	assert.False(t, older.Active)
}

func TestTaskPluginRollbackDoesNotClobberConcurrentSuccess(t *testing.T) {
	database, err := gorm.Open(sqlite.Open("file:task-plugin-activate-concurrent-rollback?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(&model.Channel{}, &model.Task{}, &model.TaskPlugin{}))

	previousDB := model.DB
	model.DB = database
	t.Cleanup(func() {
		model.DB = previousDB
		sqlDB, _ := database.DB()
		_ = sqlDB.Close()
		_ = jsplugin.DefaultRegistry.SetGenerationPreparer(nil)
		_ = jsplugin.DefaultRegistry.ReplaceOverrides(nil)
	})

	key := "activate-concurrent-rollback"
	activeKey := key
	olderSource := taskPluginAtomicitySource(key, "1.0.0")
	newerSource := taskPluginAtomicitySource(key, "2.0.0")
	require.NoError(t, database.Create(&model.TaskPlugin{
		Key:        key,
		APIVersion: 1,
		Version:    "1.0.0",
		Source:     olderSource,
		SourceHash: taskPluginAtomicitySourceHash(olderSource),
		Enabled:    true,
		Active:     true,
		ActiveKey:  &activeKey,
		CreatedAt:  1000,
	}).Error)
	require.NoError(t, database.Create(&model.TaskPlugin{
		Key:        key,
		APIVersion: 1,
		Version:    "2.0.0",
		Source:     newerSource,
		SourceHash: taskPluginAtomicitySourceHash(newerSource),
		Enabled:    true,
		Active:     false,
		CreatedAt:  2000,
	}).Error)

	var callCount int32
	started := make(chan struct{})
	release := make(chan struct{})
	require.NoError(t, jsplugin.DefaultRegistry.SetGenerationPreparer(func(candidate, _ *jsplugin.RoutingGeneration) (jsplugin.PreparedRoutingGeneration, error) {
		switch atomic.AddInt32(&callCount, 1) {
		case 1:
			return jsplugin.PreparedRoutingGeneration{Generation: candidate}, nil
		case 2:
			close(started)
			<-release
			return jsplugin.PreparedRoutingGeneration{Generation: candidate}, errors.New("forced publish failure")
		default:
			return jsplugin.PreparedRoutingGeneration{Generation: candidate}, nil
		}
	}))

	var wg sync.WaitGroup
	recorderA := httptest.NewRecorder()
	contextA, _ := gin.CreateTestContext(recorderA)
	contextA.Request = httptest.NewRequest(http.MethodPost, "/api/task-plugins/activate-concurrent-rollback/activate", strings.NewReader(`{"version":"2.0.0"}`))
	contextA.Request.Header.Set("Content-Type", "application/json")
	contextA.Params = gin.Params{{Key: "key", Value: key}}

	wg.Add(1)
	go func() {
		defer wg.Done()
		ActivateTaskPlugin(contextA)
	}()

	<-started

	recorderB := httptest.NewRecorder()
	contextB, _ := gin.CreateTestContext(recorderB)
	contextB.Request = httptest.NewRequest(http.MethodDelete, "/api/task-plugins/activate-concurrent-rollback/versions/1.0.0", strings.NewReader(""))
	contextB.Request.Header.Set("Content-Type", "application/json")
	contextB.Params = gin.Params{{Key: "key", Value: key}, {Key: "version", Value: "1.0.0"}}

	bStarted := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		close(bStarted)
		DeleteTaskPluginVersion(contextB)
	}()

	<-bStarted

	mutationObserved := false
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		var plugins []model.TaskPlugin
		err := database.Where("key = ?", key).Order("created_at DESC, id DESC").Find(&plugins).Error
		if err == nil && len(plugins) == 1 && plugins[0].Version == "2.0.0" && plugins[0].Active {
			mutationObserved = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	close(release)
	wg.Wait()

	var responseA struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	var responseB struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, json.Unmarshal(recorderA.Body.Bytes(), &responseA))
	require.NoError(t, json.Unmarshal(recorderB.Body.Bytes(), &responseB))
	require.False(t, responseA.Success)
	require.True(t, responseB.Success, recorderB.Body.String())

	if !mutationObserved {
		t.Log("concurrent delete did not surface before rollback release")
	}

	var plugins []model.TaskPlugin
	require.NoError(t, database.Where("key = ?", key).Order("created_at DESC, id DESC").Find(&plugins).Error)
	require.Len(t, plugins, 1)
	assert.Equal(t, "2.0.0", plugins[0].Version)
	assert.True(t, plugins[0].Active)
	assert.True(t, plugins[0].Enabled)
	var deleted model.TaskPlugin
	assert.ErrorIs(t, database.Where("key = ? AND version = ?", key, "1.0.0").First(&deleted).Error, gorm.ErrRecordNotFound)
}

func newTaskPluginTestContext(t *testing.T, method, path, body string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(method, path, strings.NewReader(body))
	context.Request.Header.Set("Content-Type", "application/json")
	return context, recorder
}

func taskPluginAtomicitySource(key, version string) string {
	return fmt.Sprintf(`export const meta = {apiVersion:1,key:%q,name:%q,version:%q,author:{name:"Test"},models:[%q],fetchMode:"per_task"};export function buildSubmitRequest(){return {}}export function parseSubmitResponse(){return {}}export function buildQueryRequest(){return {}}export function parseTaskResult(){return {}}`, key, key, version, key+"-model")
}

func taskPluginAtomicitySourceHash(source string) string {
	sum := sha256.Sum256([]byte(source))
	return fmt.Sprintf("%x", sum)
}

func installDeferredTaskPluginPublishFailure(t *testing.T) func() {
	t.Helper()
	fail := false
	require.NoError(t, jsplugin.DefaultRegistry.SetGenerationPreparer(func(candidate, _ *jsplugin.RoutingGeneration) (jsplugin.PreparedRoutingGeneration, error) {
		if fail {
			return jsplugin.PreparedRoutingGeneration{Generation: candidate}, errors.New("forced publish failure")
		}
		return jsplugin.PreparedRoutingGeneration{Generation: candidate}, nil
	}))
	return func() {
		fail = true
	}
}
