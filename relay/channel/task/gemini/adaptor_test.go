package gemini

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/require"
)

func TestConvertToOpenAIVideoIncludesResultURL(t *testing.T) {
	adaptor := &TaskAdaptor{}
	task := &model.Task{
		TaskID:     "task_gemini_openai_video",
		Status:     model.TaskStatusSuccess,
		Progress:   "100%",
		CreatedAt:  123,
		FinishTime: 456,
		Properties: model.Properties{
			OriginModelName:   "veo-user-facing",
			UpstreamModelName: "veo-3.0-generate-001",
		},
	}
	task.PrivateData.ResultURL = "https://example.com/video.mp4"
	task.PrivateData.UpstreamTaskID = "models/veo-3.0-generate-001/operations/abc"

	rendered, err := adaptor.ConvertToOpenAIVideo(task)
	require.NoError(t, err)

	var video dto.OpenAIVideo
	require.NoError(t, common.Unmarshal(rendered, &video))
	require.Equal(t, "https://example.com/video.mp4", video.Metadata["url"])
	require.Equal(t, "veo-user-facing", video.Model)
}

func TestConvertToOpenAIVideoRewritesGeminiFilesURL(t *testing.T) {
	previous := system_setting.ServerAddress
	system_setting.ServerAddress = "https://api.example.com"
	t.Cleanup(func() { system_setting.ServerAddress = previous })

	adaptor := &TaskAdaptor{}
	task := &model.Task{
		TaskID:     "task_gemini_files_openai",
		Status:     model.TaskStatusSuccess,
		Progress:   "100%",
		FinishTime: 456,
	}
	task.PrivateData.ResultURL = "https://generativelanguage.googleapis.com/v1beta/files/abc:download?alt=media"

	rendered, err := adaptor.ConvertToOpenAIVideo(task)
	require.NoError(t, err)

	var video dto.OpenAIVideo
	require.NoError(t, common.Unmarshal(rendered, &video))
	require.Equal(t, "https://api.example.com/v1/videos/task_gemini_files_openai/content", video.Metadata["url"])
	require.Equal(t, dto.VideoStatusCompleted, video.Status)
}

func TestConvertToOpenAIVideoRewritesFilesGoogleapisURL(t *testing.T) {
	previous := system_setting.ServerAddress
	system_setting.ServerAddress = "https://api.example.com"
	t.Cleanup(func() { system_setting.ServerAddress = previous })

	adaptor := &TaskAdaptor{}
	task := &model.Task{
		TaskID:     "task_files_googleapis_openai",
		Status:     model.TaskStatusSuccess,
		Progress:   "100%",
		FinishTime: 456,
	}
	task.PrivateData.ResultURL = "https://files.googleapis.com/v1beta/files/abc:download?alt=media"

	rendered, err := adaptor.ConvertToOpenAIVideo(task)
	require.NoError(t, err)

	var video dto.OpenAIVideo
	require.NoError(t, common.Unmarshal(rendered, &video))
	require.Equal(t, "https://api.example.com/v1/videos/task_files_googleapis_openai/content", video.Metadata["url"])
	require.Equal(t, dto.VideoStatusCompleted, video.Status)
}

func TestConvertToOpenAIVideoRewritesChannelMediaHostURL(t *testing.T) {
	previous := system_setting.ServerAddress
	system_setting.ServerAddress = "https://api.example.com"
	t.Cleanup(func() { system_setting.ServerAddress = previous })

	adaptor := &TaskAdaptor{}
	task := &model.Task{
		TaskID:     "task_channel_host_openai",
		Status:     model.TaskStatusSuccess,
		Progress:   "100%",
		FinishTime: 456,
	}
	task.PrivateData.ResultURL = "https://gemini.internal/legacy/video.mp4"
	task.PrivateData.ResultProxyHosts = []string{"gemini.internal"}

	rendered, err := adaptor.ConvertToOpenAIVideo(task)
	require.NoError(t, err)

	var video dto.OpenAIVideo
	require.NoError(t, common.Unmarshal(rendered, &video))
	require.Equal(t, "https://api.example.com/v1/videos/task_channel_host_openai/content", video.Metadata["url"])
	require.Equal(t, dto.VideoStatusCompleted, video.Status)
}

func TestConvertToOpenAIVideoHidesRetryableSettlementReview(t *testing.T) {
	adaptor := &TaskAdaptor{}
	task := &model.Task{
		TaskID:           "task_gemini_review_openai",
		Status:           model.TaskStatusSuccess,
		SettlementStatus: model.TaskSettlementStatusReview,
		Progress:         "100%",
		FinishTime:       456,
		NextPollAt:       789,
	}
	task.PrivateData.ResultURL = "https://cdn.example/video.mp4"

	rendered, err := adaptor.ConvertToOpenAIVideo(task)
	require.NoError(t, err)

	var video dto.OpenAIVideo
	require.NoError(t, common.Unmarshal(rendered, &video))
	require.Equal(t, dto.VideoStatusInProgress, video.Status)
	require.Equal(t, 99, video.Progress)
	require.Zero(t, video.CompletedAt)
	require.Empty(t, video.Metadata["url"])
}

func TestParseTaskResultFailsWhenDoneWithoutVideoURI(t *testing.T) {
	adaptor := &TaskAdaptor{}
	ti, err := adaptor.ParseTaskResult([]byte(`{"name":"ops/1","done":true,"response":{}}`))
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusFailure, ti.Status)
	require.Contains(t, ti.Reason, "gemini video uri missing")
	require.Equal(t, "100%", ti.Progress)
}

func TestParseTaskResultUsesInlineVideoBytes(t *testing.T) {
	adaptor := &TaskAdaptor{}
	ti, err := adaptor.ParseTaskResult([]byte(`{"name":"ops/1","done":true,"response":{"videos":[{"bytesBase64Encoded":"AAAA","mimeType":"video/mp4"}]}}`))
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusSuccess, ti.Status)
	require.Equal(t, "data:video/mp4;base64,AAAA", ti.Url)
	require.Equal(t, "100%", ti.Progress)
}

func TestParseTaskResultKeepsURIOnSuccess(t *testing.T) {
	adaptor := &TaskAdaptor{}
	ti, err := adaptor.ParseTaskResult([]byte(`{"name":"ops/1","done":true,"response":{"generateVideoResponse":{"generatedVideos":[{"video":{"uri":"https://generativelanguage.googleapis.com/v1/files/abc"}}]}}}`))
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusSuccess, ti.Status)
	require.Equal(t, "https://generativelanguage.googleapis.com/v1/files/abc", ti.RemoteUrl)
	require.Equal(t, "100%", ti.Progress)
}

func TestParseTaskResultUsesHTTPVideoStringAsRemoteURL(t *testing.T) {
	adaptor := &TaskAdaptor{}
	ti, err := adaptor.ParseTaskResult([]byte(`{"name":"ops/1","done":true,"response":{"video":"https://cdn.example/video.mp4"}}`))
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusSuccess, ti.Status)
	require.Equal(t, "https://cdn.example/video.mp4", ti.RemoteUrl)
	require.Empty(t, ti.Url)
	require.Equal(t, "100%", ti.Progress)
}

func TestParseTaskResultFailsGCSVideoString(t *testing.T) {
	adaptor := &TaskAdaptor{}
	ti, err := adaptor.ParseTaskResult([]byte(`{"name":"ops/1","done":true,"response":{"video":"gs://bucket/video.mp4"}}`))
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusFailure, ti.Status)
	require.Contains(t, ti.Reason, "gemini gcs uri not retrievable")
	require.Empty(t, ti.RemoteUrl)
	require.Empty(t, ti.Url)
	require.Equal(t, "100%", ti.Progress)
}

func TestParseTaskResultFailsGCSGeneratedVideoURI(t *testing.T) {
	adaptor := &TaskAdaptor{}
	ti, err := adaptor.ParseTaskResult([]byte(`{"name":"ops/1","done":true,"response":{"generateVideoResponse":{"generatedVideos":[{"video":{"uri":"gs://bucket/generated.mp4"}}]}}}`))
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusFailure, ti.Status)
	require.Contains(t, ti.Reason, "gemini gcs uri not retrievable")
	require.Empty(t, ti.RemoteUrl)
	require.Equal(t, "100%", ti.Progress)
}

func TestParseTaskResultFailsUnsignedGCSHTTPS(t *testing.T) {
	adaptor := &TaskAdaptor{}
	ti, err := adaptor.ParseTaskResult([]byte(`{"name":"ops/1","done":true,"response":{"video":"https://storage.googleapis.com/bucket/video.mp4"}}`))
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusFailure, ti.Status)
	require.Contains(t, ti.Reason, "gemini gcs uri not retrievable")
	require.Empty(t, ti.RemoteUrl)
	require.Empty(t, ti.Url)
	require.Equal(t, "100%", ti.Progress)
}

func TestParseTaskResultFailsUnsignedGCSGeneratedVideoHTTPS(t *testing.T) {
	adaptor := &TaskAdaptor{}
	ti, err := adaptor.ParseTaskResult([]byte(`{"name":"ops/1","done":true,"response":{"generateVideoResponse":{"generatedVideos":[{"video":{"uri":"https://storage.cloud.google.com/bucket/generated.mp4"}}]}}}`))
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusFailure, ti.Status)
	require.Contains(t, ti.Reason, "gemini gcs uri not retrievable")
	require.Empty(t, ti.RemoteUrl)
	require.Equal(t, "100%", ti.Progress)
}

func TestParseTaskResultKeepsSignedGCSHTTPS(t *testing.T) {
	adaptor := &TaskAdaptor{}
	signed := "https://storage.googleapis.com/bucket/video.mp4?X-Goog-Algorithm=GOOG4-RSA-SHA256&X-Goog-Credential=sa%40proj.iam.gserviceaccount.com%2F20240101%2Fauto%2Fstorage%2Fgoog4_request&X-Goog-Date=20240101T000000Z&X-Goog-Expires=3600&X-Goog-SignedHeaders=host&X-Goog-Signature=deadbeef"
	ti, err := adaptor.ParseTaskResult([]byte(`{"name":"ops/1","done":true,"response":{"video":"` + signed + `"}}`))
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusSuccess, ti.Status)
	require.Equal(t, signed, ti.RemoteUrl)
	require.Empty(t, ti.Url)
	require.Equal(t, "100%", ti.Progress)
}

func TestParseTaskResultUsesDataVideoStringAsURL(t *testing.T) {
	adaptor := &TaskAdaptor{}
	ti, err := adaptor.ParseTaskResult([]byte(`{"name":"ops/1","done":true,"response":{"video":"data:video/mp4;base64,AAAA"}}`))
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusSuccess, ti.Status)
	require.Equal(t, "data:video/mp4;base64,AAAA", ti.Url)
	require.Empty(t, ti.RemoteUrl)
}
