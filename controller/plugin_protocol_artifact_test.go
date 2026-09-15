package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	pluginruntime "github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestTaskPluginProtocolRendererContextUsesStoredArtifactsWithoutPluginHook(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(&model.TaskPlugin{}))
	previousDB := model.DB
	model.DB = database
	t.Cleanup(func() {
		model.DB = previousDB
		sqlDB, _ := database.DB()
		_ = sqlDB.Close()
	})

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

	task := &model.Task{
		ID:     1,
		TaskID: "task_protocol_stored_artifact",
		Status: model.TaskStatusSuccess,
		PrivateData: model.TaskPrivateData{
			Execution: &model.TaskExecutionSnapshot{
				TaskPlugin: &model.TaskPluginSnapshot{
					Key:     "removed-plugin",
					Version: "1.0.0",
				},
			},
			ArtifactRefs: map[string]model.TaskArtifactStorageRef{
				"video": {
					Backend:   "s3",
					Bucket:    "newapi-artifacts",
					ObjectKey: "task-artifacts/task_protocol_stored_artifact/video",
					Type:      "video",
					MimeType:  "video/mp4",
				},
			},
		},
	}

	rendererContext, err := taskPluginProtocolRendererContext(
		t.Context(),
		pluginruntime.ProtocolRequestContext{},
		pluginruntime.PinnedEndpoint{},
		task,
		func(taskID, artifactKey string) (string, error) {
			return "/v1/tasks/" + taskID + "/artifacts/" + artifactKey, nil
		},
	)
	require.NoError(t, err)
	artifacts, ok := rendererContext["artifacts"].(map[string]any)
	require.True(t, ok)
	artifact, ok := artifacts["video"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "video/mp4", artifact["mimeType"])
	assert.Equal(t, "/v1/tasks/task_protocol_stored_artifact/artifacts/video", artifact["url"])
}

func TestTaskPluginProtocolRendererContextOmitsArtifactsDuringRetryableSettlementReview(t *testing.T) {
	task := &model.Task{
		ID:               2,
		TaskID:           "task_protocol_review_artifact",
		Status:           model.TaskStatusSuccess,
		SettlementStatus: model.TaskSettlementStatusReview,
		NextPollAt:       9999999999,
		PrivateData: model.TaskPrivateData{
			ArtifactRefs: map[string]model.TaskArtifactStorageRef{
				"video": {
					Backend:   "s3",
					Bucket:    "newapi-artifacts",
					ObjectKey: "task-artifacts/task_protocol_review_artifact/video",
					Type:      "video",
					MimeType:  "video/mp4",
				},
			},
		},
	}

	rendererContext, err := taskPluginProtocolRendererContext(
		t.Context(),
		pluginruntime.ProtocolRequestContext{},
		pluginruntime.PinnedEndpoint{},
		task,
		func(taskID, artifactKey string) (string, error) {
			return "/v1/tasks/" + taskID + "/artifacts/" + artifactKey, nil
		},
	)
	require.NoError(t, err)
	_, ok := rendererContext["artifacts"]
	require.False(t, ok)
}
