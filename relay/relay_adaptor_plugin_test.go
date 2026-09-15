package relay

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	pluginruntime "github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestGetTaskPlatformPrefersPinnedTaskPluginKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("platform", "legacy-platform")
	c.Set("channel_type", constant.ChannelTypeKling)
	c.Set("task_plugin_key", "google")

	assert.Equal(t, constant.TaskPlatform("google"), GetTaskPlatform(c))
}

func TestGetTaskAdaptorForRequestUsesPinnedPlugin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	plugin, ok := pluginruntime.DefaultRegistry.Generation().Get("google")
	require.True(t, ok)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set(pluginruntime.ContextKeyPinnedPlugin, pluginruntime.PinnedPlugin{Plugin: plugin})

	platform, adaptor := getTaskAdaptorForRequest(c, constant.TaskPlatform("legacy-platform"))
	require.NotNil(t, adaptor)
	assert.Equal(t, constant.TaskPlatform("google"), platform)
	_, ok = adaptor.(channel.TaskPluginAdaptor)
	assert.True(t, ok)
}

func TestLegacyTaskAdaptorStillAvailable(t *testing.T) {
	assert.NotNil(t, GetTaskAdaptor(constant.TaskPlatformSuno))
}

func TestGetTaskAdaptorUsesSoraForNewAPI(t *testing.T) {
	assert.NotNil(t, GetTaskAdaptor(constant.TaskPlatform(fmt.Sprintf("%d", constant.ChannelTypeNewAPI))))
}

func TestPluginTaskDoesNotUseLegacyRealtimeFetch(t *testing.T) {
	task := &model.Task{
		PrivateData: model.TaskPrivateData{
			Execution: &model.TaskExecutionSnapshot{
				TaskPlugin: &model.TaskPluginSnapshot{
					Key:     "google",
					Version: "1.0.0",
				},
			},
		},
	}

	assert.False(t, shouldUseLegacyRealtimeFetch(task))
	assert.True(t, shouldUseLegacyRealtimeFetch(&model.Task{}))
	assert.False(t, shouldUseLegacyRealtimeFetch(nil))
}

func TestPluginRealtimeFetchUsesPinnedPluginAndPersistsLatestState(t *testing.T) {
	var requestedPath, requestedKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		requestedKey = r.Header.Get("X-Task-Key")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"SUCCESS","progress":"100%","remoteUrl":"https://cdn.example/video.mp4"}`))
	}))
	defer server.Close()

	db, err := gorm.Open(sqlite.Open("file:task-plugin-realtime?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.TaskPlugin{}, &model.Task{}))

	originalDB := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = originalDB
		sqlDB, _ := db.DB()
		_ = sqlDB.Close()
	})

	source := `
export const meta = {apiVersion: 1, key: "historical-realtime", name: "Historical Realtime", version: "1.0.0", author: {name: "Test"}, channelTypes: [24], models: ["model"], fetchMode: "per_task"};
export function buildSubmitRequest() { return {}; }
export function parseSubmitResponse() { return {}; }
export function buildQueryRequest(ctx) {
  return {url: ctx.baseUrl + "/query/" + ctx.taskId, method: "GET", headers: {"X-Task-Key": ctx.apiKey}};
}
export function parseTaskResult(ctx, body) {
  return {status: body.status, progress: body.progress, remoteUrl: body.remoteUrl};
}
`
	sum := sha256.Sum256([]byte(source))
	require.NoError(t, db.Create(&model.TaskPlugin{
		Key:        "historical-realtime",
		APIVersion: 1,
		Version:    "1.0.0",
		Source:     source,
		SourceHash: fmt.Sprintf("%x", sum[:]),
		Enabled:    true,
		Active:     true,
	}).Error)

	baseURL := server.URL
	require.NoError(t, db.Create(&model.Channel{
		Id:      9911,
		Type:    constant.ChannelTypeGemini,
		Key:     "channel-secret",
		BaseURL: &baseURL,
		Status:  1,
	}).Error)
	task := &model.Task{
		TaskID:    "task-realtime",
		Platform:  constant.TaskPlatform("historical-realtime"),
		ChannelId: 9911,
		Status:    model.TaskStatusInProgress,
		Progress:  "50%",
		PrivateData: model.TaskPrivateData{
			Key:            "task-secret",
			UpstreamTaskID: "upstream-1",
			Execution:      &model.TaskExecutionSnapshot{TaskPlugin: &model.TaskPluginSnapshot{Key: "historical-realtime", Version: "1.0.0"}},
		},
	}
	require.NoError(t, db.Create(task).Error)

	require.Nil(t, tryRealtimeFetch(context.Background(), task, true))
	assert.Equal(t, "/query/upstream-1", requestedPath)
	assert.Equal(t, "task-secret", requestedKey)

	var reloaded model.Task
	require.NoError(t, db.First(&reloaded, task.ID).Error)
	assert.Equal(t, model.TaskStatus(model.TaskStatusSuccess), reloaded.Status)
	assert.Equal(t, "100%", reloaded.Progress)
	assert.Equal(t, "https://cdn.example/video.mp4", reloaded.PrivateData.ResultURL)
	assert.Equal(t, `{"progress":"100%","remoteUrl":"https://cdn.example/video.mp4","status":"SUCCESS"}`, string(reloaded.Data))
}

func TestLegacyRealtimeFetchUsesPersistedTaskKey(t *testing.T) {
	const operationName = "projects/test/locations/us-central1/models/veo-3.0-generate-001/operations/op-1"
	var requestedKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedKey = r.Header.Get("x-goog-api-key")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(fmt.Sprintf(`{
			"name": %q,
			"done": true,
			"response": {
				"generateVideoResponse": {
					"generatedVideos": [{"video": {"uri": "https://cdn.example/video.mp4"}}]
				}
			}
		}`, operationName)))
	}))
	defer server.Close()

	db, err := gorm.Open(sqlite.Open("file:legacy-realtime-task-key?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Task{}))

	originalDB := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = originalDB
		sqlDB, _ := db.DB()
		_ = sqlDB.Close()
	})

	baseURL := server.URL
	require.NoError(t, db.Create(&model.Channel{
		Id:      9912,
		Type:    constant.ChannelTypeGemini,
		Key:     "channel-secret",
		BaseURL: &baseURL,
		Status:  1,
	}).Error)

	task := &model.Task{
		TaskID:    "task-legacy-realtime-key",
		ChannelId: 9912,
		Status:    model.TaskStatusInProgress,
		PrivateData: model.TaskPrivateData{
			Key:            "task-secret",
			UpstreamTaskID: taskcommon.EncodeLocalTaskID(operationName),
		},
	}
	require.NoError(t, db.Create(task).Error)

	require.Nil(t, tryRealtimeFetch(context.Background(), task, true))
	assert.Equal(t, "task-secret", requestedKey)
}

func TestResolveTaskPluginForTaskUsesPersistedVersion(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:task-plugin-resolver?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.TaskPlugin{}))

	originalDB := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = originalDB
		sqlDB, _ := db.DB()
		_ = sqlDB.Close()
	})

	source := `
export const meta = {apiVersion: 1, key: "historical-resolver", name: "Historical Resolver", version: "1.2.3", author: {name: "Test"}, models: ["model"], fetchMode: "per_task"};
export function buildSubmitRequest() { return {}; }
export function parseSubmitResponse() { return {}; }
export function buildQueryRequest() { return {}; }
export function parseTaskResult() { return {}; }
`
	sum := sha256.Sum256([]byte(source))
	require.NoError(t, db.Create(&model.TaskPlugin{
		Key:        "historical-resolver",
		APIVersion: 1,
		Version:    "1.2.3",
		Source:     source,
		SourceHash: fmt.Sprintf("%x", sum[:]),
		Enabled:    true,
		Active:     true,
	}).Error)

	task := &model.Task{
		Platform: constant.TaskPlatform("historical-resolver"),
		PrivateData: model.TaskPrivateData{
			Execution: &model.TaskExecutionSnapshot{
				TaskPlugin: &model.TaskPluginSnapshot{
					Key:        "historical-resolver",
					Version:    "1.2.3",
					Generation: 999,
				},
			},
		},
	}
	plugin, generation, err := ResolveTaskPluginForTask(task)
	require.NoError(t, err)
	require.NotNil(t, plugin)
	assert.Nil(t, generation)
	assert.Equal(t, "historical-resolver", plugin.Meta.Key)
	assert.Equal(t, "1.2.3", plugin.Meta.Version)
}

func TestResolveTaskPluginForTaskUsesSnapshotSourceWhenPersistedVersionIsGone(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:task-plugin-resolver-snapshot-source?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.TaskPlugin{}))

	originalDB := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = originalDB
		sqlDB, _ := db.DB()
		_ = sqlDB.Close()
	})

	const source = `
export const meta = {apiVersion: 1, key: "historical-factory-snapshot", name: "Historical Factory Snapshot", version: "2.0.0", author: {name: "Test"}, models: ["historical-factory-snapshot-model"], fetchMode: "per_task"};
export function buildSubmitRequest() { return {}; }
export function parseSubmitResponse() { return {}; }
export function buildQueryRequest() { return {}; }
export function parseTaskResult() { return {}; }
`
	rawPrivateData, err := json.Marshal(map[string]any{
		"execution": map[string]any{
			"task_plugin": map[string]any{
				"key":     "historical-factory-snapshot",
				"version": "2.0.0",
				"source":  source,
			},
		},
	})
	require.NoError(t, err)
	var privateData model.TaskPrivateData
	require.NoError(t, json.Unmarshal(rawPrivateData, &privateData))

	plugin, generation, err := ResolveTaskPluginForTask(&model.Task{
		Platform:    constant.TaskPlatform("historical-factory-snapshot"),
		PrivateData: privateData,
	})
	require.NoError(t, err)
	require.Nil(t, generation)
	require.NotNil(t, plugin)
	assert.Equal(t, "historical-factory-snapshot", plugin.Meta.Key)
	assert.Equal(t, "2.0.0", plugin.Meta.Version)
}

func TestResolveTaskPluginForTaskPrefersChangedSnapshotSourceOverSameVersionRuntime(t *testing.T) {
	originalRegistry := pluginruntime.DefaultRegistry
	registry := pluginruntime.NewRegistry()
	pluginruntime.DefaultRegistry = registry
	t.Cleanup(func() {
		pluginruntime.DefaultRegistry = originalRegistry
	})

	const currentSource = `
export const meta = {apiVersion: 1, key: "hist-factory-source-change", name: "Historical Factory Source Change", version: "1.0.0", author: {name: "Test"}, models: ["hist-factory-source-change-model"], fetchMode: "per_task"};
export function buildSubmitRequest() { return {current: true}; }
export function parseSubmitResponse() { return {}; }
export function buildQueryRequest() { return {}; }
export function parseTaskResult() { return {}; }
`
	const snapshotSource = `
export const meta = {apiVersion: 1, key: "hist-factory-source-change", name: "Historical Factory Source Change", version: "1.0.0", author: {name: "Test"}, models: ["hist-factory-source-change-model"], fetchMode: "per_task"};
export function buildSubmitRequest() { return {snapshot: true}; }
export function parseSubmitResponse() { return {}; }
export function buildQueryRequest() { return {}; }
export function parseTaskResult() { return {}; }
`
	_, err := registry.RegisterFactory(currentSource, pluginruntime.Options{})
	require.NoError(t, err)

	plugin, generation, err := ResolveTaskPluginForTask(&model.Task{
		Platform: constant.TaskPlatform("hist-factory-source-change"),
		PrivateData: model.TaskPrivateData{
			Execution: &model.TaskExecutionSnapshot{
				TaskPlugin: &model.TaskPluginSnapshot{
					Key:     "hist-factory-source-change",
					Version: "1.0.0",
					Source:  snapshotSource,
				},
			},
		},
	})
	require.NoError(t, err)
	require.Nil(t, generation)
	require.NotNil(t, plugin)
	assert.Equal(t, snapshotSource, plugin.Source)
}

func TestResolveTaskPluginForTaskRejectsSnapshotIdentityMismatch(t *testing.T) {
	const snapshotSource = `
export const meta = {apiVersion: 1, key: "different-plugin", name: "Different Plugin", version: "1.0.0", author: {name: "Test"}, models: ["different-plugin-model"], fetchMode: "per_task"};
export function buildSubmitRequest() { return {}; }
export function parseSubmitResponse() { return {}; }
export function buildQueryRequest() { return {}; }
export function parseTaskResult() { return {}; }
`

	_, _, err := ResolveTaskPluginForTask(&model.Task{
		Platform: constant.TaskPlatform("snapshot-plugin"),
		PrivateData: model.TaskPrivateData{
			Execution: &model.TaskExecutionSnapshot{
				TaskPlugin: &model.TaskPluginSnapshot{
					Key:     "snapshot-plugin",
					Version: "1.0.0",
					Source:  snapshotSource,
				},
			},
		},
	})
	require.Error(t, err)
}
