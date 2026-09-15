package taskcommon

import (
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/require"
)

func TestResolvePersistedResultURL(t *testing.T) {
	previous := system_setting.ServerAddress
	system_setting.ServerAddress = "https://api.example.com"
	t.Cleanup(func() { system_setting.ServerAddress = previous })

	url, ok := ResolvePersistedResultURL("task_1", "https://cdn.example/a.mp4", "", false)
	require.True(t, ok)
	require.Equal(t, "https://cdn.example/a.mp4", url)

	url, ok = ResolvePersistedResultURL("task_1", "", "https://cdn.example/remote.mp4", false)
	require.True(t, ok)
	require.Equal(t, "https://cdn.example/remote.mp4", url)

	url, ok = ResolvePersistedResultURL("task_1", "data:video/mp4;base64,AA", "", false)
	require.True(t, ok)
	require.Equal(t, "data:video/mp4;base64,AA", url)

	_, ok = ResolvePersistedResultURL("task_1", "", "", false)
	require.False(t, ok)

	url, ok = ResolvePersistedResultURL("task_1", "", "", true)
	require.True(t, ok)
	require.Equal(t, BuildProxyURL("task_1"), url)

	url, ok = ResolvePersistedResultURL("task_1", "gs://bucket/video.mp4", "", false)
	require.True(t, ok)
	require.Equal(t, "gs://bucket/video.mp4", url)

	url, ok = ResolvePersistedResultURL("task_1", "", "gs://bucket/remote.mp4", false)
	require.True(t, ok)
	require.Equal(t, "gs://bucket/remote.mp4", url)
}

func TestGCSURIToHTTPS(t *testing.T) {
	url, ok := GCSURIToHTTPS("gs://bucket/path/video.mp4")
	require.True(t, ok)
	require.Equal(t, "https://storage.googleapis.com/bucket/path/video.mp4", url)

	_, ok = GCSURIToHTTPS("gs://bucket")
	require.False(t, ok)

	_, ok = GCSURIToHTTPS("https://cdn.example/video.mp4")
	require.False(t, ok)
}

func TestApplyTaskSuccessResultFailsWithoutURLWhenProxyNotAllowed(t *testing.T) {
	task := &model.Task{TaskID: "task_missing"}
	ApplyTaskSuccessResult(task, "", "", "", 99, false)
	require.Equal(t, model.TaskStatus(model.TaskStatusFailure), task.Status)
	require.Equal(t, MissingResultURLReason, task.FailReason)
	require.Empty(t, task.PrivateData.ResultURL)
	require.Equal(t, int64(99), task.FinishTime)
}

func TestAllowsEmptyProxyResult(t *testing.T) {
	require.True(t, AllowsEmptyProxyResult(constant.TaskPlatformSuno, 0))
	require.True(t, AllowsEmptyProxyResult(constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeSora)), 0))
	require.True(t, AllowsEmptyProxyResult(constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeOpenAI)), 0))
	require.False(t, AllowsEmptyProxyResult(constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeGemini)), 0))
	require.False(t, AllowsEmptyProxyResult(constant.TaskPlatformImage, 0))
	require.False(t, AllowsEmptyProxyResult("gemini", 0))
	require.False(t, AllowsEmptyProxyResult("kling", constant.ChannelTypeKling))
	require.True(t, AllowsEmptyProxyResult("custom-sora-plugin", constant.ChannelTypeSora))
	require.True(t, AllowsEmptyProxyResult("openai-video", constant.ChannelTypeOpenAI))
	require.True(t, AllowsEmptyProxyResult("nested-newapi", constant.ChannelTypeNewAPI))
	require.True(t, AllowsEmptyProxyResult(constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeNewAPI)), 0))
	require.False(t, AllowsEmptyProxyResult("custom-sora-plugin", constant.ChannelTypeKling))
}

func TestApplyTaskSuccessResultPersistsOriginalDataAndGCSLocators(t *testing.T) {
	previous := system_setting.ServerAddress
	system_setting.ServerAddress = "https://api.example.com"
	t.Cleanup(func() { system_setting.ServerAddress = previous })

	dataTask := &model.Task{TaskID: "task_inline"}
	ApplyTaskSuccessResult(dataTask, "data:video/mp4;base64,AAAA", "", "ready", 99, false)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), dataTask.Status)
	require.Equal(t, "data:video/mp4;base64,AAAA", dataTask.PrivateData.ResultURL)
	require.Equal(t, BuildProxyURL("task_inline"), dataTask.GetResultURL())

	gcsTask := &model.Task{TaskID: "task_gcs"}
	ApplyTaskSuccessResult(gcsTask, "gs://bucket/video.mp4", "", "vertex ready", 100, false)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), gcsTask.Status)
	require.Equal(t, "gs://bucket/video.mp4", gcsTask.PrivateData.ResultURL)
	require.Equal(t, BuildProxyURL("task_gcs"), gcsTask.GetResultURL())
	require.Equal(t, "vertex ready", gcsTask.FailReason)
}

func TestApplyTaskSuccessResultFailsOversizedDataURL(t *testing.T) {
	oversized := "data:video/mp4;base64," + strings.Repeat("A", MaxInlineResultURLBytes)
	task := &model.Task{TaskID: "task_inline_too_large"}
	ApplyTaskSuccessResult(task, oversized, "", "ready", 99, false)
	require.Equal(t, model.TaskStatus(model.TaskStatusFailure), task.Status)
	require.Equal(t, InlineResultTooLargeReason, task.FailReason)
	require.Empty(t, task.PrivateData.ResultURL)
	require.Equal(t, int64(99), task.FinishTime)
}

func TestApplyPublicOpenAIVideoProjectionOwnsPublicFields(t *testing.T) {
	task := &model.Task{
		TaskID:     "task_openai_video_projection",
		Status:     model.TaskStatusFailure,
		Progress:   "100%",
		FailReason: "billing accounting failed after task submission: pq: password authentication failed",
		FinishTime: 99,
	}
	video := dto.NewOpenAIVideo()
	video.Error = &dto.OpenAIVideoError{Message: "https://provider.example/debug", Code: "upstream"}
	video.Metadata = map[string]any{
		"url":    "https://provider.example/raw.mp4",
		"signed": "leak",
	}
	video.CompletedAt = 123

	ApplyPublicOpenAIVideoProjection(task, video)

	require.Equal(t, dto.VideoStatusFailed, video.Status)
	require.Equal(t, 100, video.Progress)
	require.Zero(t, video.CompletedAt)
	require.Nil(t, video.Metadata)
	require.NotNil(t, video.Error)
	require.Equal(t, model.TaskPublicAccountingFailReason, video.Error.Message)
	require.Equal(t, "task_failed", video.Error.Code)
}

func TestApplyPublicOpenAIVideoProjectionClearsErrorOnSuccess(t *testing.T) {
	task := &model.Task{
		TaskID:   "task_openai_video_success",
		Status:   model.TaskStatusSuccess,
		Progress: "100%",
		PrivateData: model.TaskPrivateData{
			ResultURL: "https://api.example/v1/videos/task_openai_video_success/content",
		},
	}
	video := dto.NewOpenAIVideo()
	video.Error = &dto.OpenAIVideoError{Message: "provider leftover", Code: "upstream"}
	video.Metadata = map[string]any{
		"url":    "https://provider.example/raw.mp4",
		"signed": "leak",
	}

	ApplyPublicOpenAIVideoProjection(task, video)

	require.Equal(t, dto.VideoStatusCompleted, video.Status)
	require.Nil(t, video.Error)
	require.Equal(t, map[string]any{"url": "https://api.example/v1/videos/task_openai_video_success/content"}, video.Metadata)
}

func TestApplyTaskSuccessResultAllowsEmptyProxy(t *testing.T) {
	previous := system_setting.ServerAddress
	system_setting.ServerAddress = "https://api.example.com"
	t.Cleanup(func() { system_setting.ServerAddress = previous })

	task := &model.Task{TaskID: "task_sora"}
	ApplyTaskSuccessResult(task, "", "", "sora ready", 100, true)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), task.Status)
	require.Equal(t, BuildProxyURL("task_sora"), task.PrivateData.ResultURL)
	require.Equal(t, "sora ready", task.FailReason)
	require.Equal(t, model.TaskSettlementStatusPending, task.SettlementStatus)
}
