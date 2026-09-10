package controller

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	pluginruntime "github.com/QuantumNous/new-api/pkg/jsplugin"
	relaychannel "github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProjectTaskArtifactsUsesTaskPluginExecution(t *testing.T) {
	task := &model.Task{
		TaskID:   "task_google_artifact",
		Platform: constant.TaskPlatform("google"),
		Status:   model.TaskStatusSuccess,
		Action:   constant.TaskActionTextToVideo,
		Data:     []byte(`{"response":{"generateVideoResponse":{"generatedVideos":[{"video":{"uri":"https://storage.googleapis.com/example/video.mp4"}}]}}}`),
	}
	task.PrivateData.Execution = &model.TaskExecutionSnapshot{
		TaskPlugin: &model.TaskPluginSnapshot{Key: "google", Version: "1.0.1"},
	}

	artifacts, err := projectTaskArtifacts(task)
	require.NoError(t, err)
	require.Len(t, artifacts, 1)
	assert.Equal(t, "video", artifacts[0].Key)
	assert.Equal(t, "video", artifacts[0].Type)
}

func TestProjectTaskArtifactsUsesVertexInlineVideoResult(t *testing.T) {
	task := &model.Task{
		TaskID:   "task_vertex_artifact",
		Platform: constant.TaskPlatform("vertex-ai"),
		Status:   model.TaskStatusSuccess,
		Action:   constant.TaskActionTextToVideo,
		Data: []byte(`{"name":"projects/test/locations/us-central1/operations/op-1","done":true,` +
			`"response":{"videos":[{"bytesBase64Encoded":"aGVsbG8=","mimeType":"video/mp4"}]}}`),
	}
	task.PrivateData.Execution = &model.TaskExecutionSnapshot{
		TaskPlugin: &model.TaskPluginSnapshot{Key: "vertex-ai", Version: "1.0.1"},
	}

	artifacts, err := projectTaskArtifacts(task)
	require.NoError(t, err)
	require.Equal(t, []relaychannel.TaskArtifact{{Key: "video", Type: "video", MimeType: "video/mp4"}}, artifacts)
}

func TestMergeTaskArtifactsCapsAtSixtyFourItems(t *testing.T) {
	primary := make([]relaychannel.TaskArtifact, 64)
	for i := range primary {
		primary[i] = relaychannel.TaskArtifact{
			Key:  fmt.Sprintf("primary-%02d", i),
			Type: "video",
		}
	}
	fallback := []relaychannel.TaskArtifact{
		{Key: "fallback-1", Type: "video"},
		{Key: "fallback-2", Type: "video"},
	}

	merged := mergeTaskArtifacts(primary, fallback)
	require.Len(t, merged, 64)
	assert.Equal(t, primary[0].Key, merged[0].Key)
	assert.Equal(t, primary[63].Key, merged[63].Key)
}

func TestProjectTaskArtifactsFallsBackToStoredArtifactsWhenPluginIsUnavailable(t *testing.T) {
	task := &model.Task{
		ID:     1,
		TaskID: "task_stored_artifact_fallback",
		Status: model.TaskStatusSuccess,
		PrivateData: model.TaskPrivateData{
			ArtifactRefs: map[string]model.TaskArtifactStorageRef{
				"video": {
					Backend:   "s3",
					Bucket:    "newapi-artifacts",
					ObjectKey: "task-artifacts/task_stored_artifact_fallback/video",
					MimeType:  "video/mp4",
					Size:      6,
				},
			},
		},
	}
	t.Setenv(system_setting.TaskArtifactStoreModeEnv, system_setting.TaskArtifactStoreModeS3)
	t.Setenv(system_setting.TaskArtifactStoreS3EndpointEnv, "https://artifacts.example.test")
	t.Setenv(system_setting.TaskArtifactStoreS3BucketEnv, "newapi-artifacts")
	t.Setenv(system_setting.TaskArtifactStoreS3RegionEnv, "us-east-1")
	t.Setenv(system_setting.TaskArtifactStoreS3AccessKeyEnv, "test-access")
	t.Setenv(system_setting.TaskArtifactStoreS3SecretKeyEnv, "test-secret")
	t.Setenv(system_setting.TaskArtifactStoreS3PrefixEnv, "task-artifacts")
	require.NoError(t, service.ConfigureTaskArtifactStore())
	t.Cleanup(func() {
		t.Setenv(system_setting.TaskArtifactStoreModeEnv, system_setting.TaskArtifactStoreModeUpstream)
		_ = service.ConfigureTaskArtifactStore()
	})

	artifacts, err := projectTaskArtifacts(task)
	require.NoError(t, err)
	require.Len(t, artifacts, 1)
	assert.Equal(t, "video", artifacts[0].Key)
	assert.Equal(t, "video", artifacts[0].Type)
	assert.Equal(t, "video/mp4", artifacts[0].MimeType)
}

func TestProjectTaskArtifactsFallsBackToStoredArtifactsWhenPluginArtifactsAreInvalid(t *testing.T) {
	originalRegistry := pluginruntime.DefaultRegistry
	registry := pluginruntime.NewRegistry()
	pluginruntime.DefaultRegistry = registry
	t.Cleanup(func() {
		pluginruntime.DefaultRegistry = originalRegistry
	})

	const invalidArtifactsSource = `
export const meta = {apiVersion: 1, key: "invalid-artifact-plugin", name: "Invalid Artifact Plugin", version: "1.0.0", author: {name: "Test"}, models: ["invalid-artifact-plugin"], fetchMode: "per_task"};
export function buildSubmitRequest(ctx) { return {url: ctx.baseUrl + "/submit", method: "POST", body: {prompt: "x"}}; }
export function parseSubmitResponse(ctx, resp) { return {taskId: resp.body.id, taskData: {}}; }
export function buildQueryRequest(ctx) { return {url: ctx.baseUrl + "/tasks/" + ctx.taskId, method: "GET"}; }
export function parseTaskResult(ctx, body) { return {status: "SUCCESS"}; }
export function listArtifacts() { return [{ key: "bad key", type: "video" }]; }
export function buildContentRequest() { return {url: "https://example.com/video.mp4", method: "GET"}; }
`
	_, err := registry.RegisterFactory(invalidArtifactsSource, pluginruntime.Options{})
	require.NoError(t, err)

	task := &model.Task{
		ID:       1,
		TaskID:   "task_invalid_plugin_artifacts",
		Platform: constant.TaskPlatform("invalid-artifact-plugin"),
		Status:   model.TaskStatusSuccess,
		PrivateData: model.TaskPrivateData{
			Execution: &model.TaskExecutionSnapshot{
				TaskPlugin: &model.TaskPluginSnapshot{
					Key:     "invalid-artifact-plugin",
					Version: "1.0.0",
				},
			},
			ArtifactRefs: map[string]model.TaskArtifactStorageRef{
				"video": {
					Backend:   "s3",
					Bucket:    "newapi-artifacts",
					ObjectKey: "task-artifacts/task_invalid_plugin_artifacts/video",
					Type:      "video",
					MimeType:  "video/mp4",
				},
			},
		},
	}
	t.Setenv(system_setting.TaskArtifactStoreModeEnv, system_setting.TaskArtifactStoreModeS3)
	t.Setenv(system_setting.TaskArtifactStoreS3EndpointEnv, "https://artifacts.example.test")
	t.Setenv(system_setting.TaskArtifactStoreS3BucketEnv, "newapi-artifacts")
	t.Setenv(system_setting.TaskArtifactStoreS3RegionEnv, "us-east-1")
	t.Setenv(system_setting.TaskArtifactStoreS3AccessKeyEnv, "test-access")
	t.Setenv(system_setting.TaskArtifactStoreS3SecretKeyEnv, "test-secret")
	t.Setenv(system_setting.TaskArtifactStoreS3PrefixEnv, "task-artifacts")
	require.NoError(t, service.ConfigureTaskArtifactStore())
	t.Cleanup(func() {
		t.Setenv(system_setting.TaskArtifactStoreModeEnv, system_setting.TaskArtifactStoreModeUpstream)
		_ = service.ConfigureTaskArtifactStore()
	})

	artifacts, err := projectTaskArtifacts(task)
	require.NoError(t, err)
	require.Len(t, artifacts, 1)
	assert.Equal(t, "video", artifacts[0].Key)
	assert.Equal(t, "video", artifacts[0].Type)
	assert.Equal(t, "video/mp4", artifacts[0].MimeType)
}
