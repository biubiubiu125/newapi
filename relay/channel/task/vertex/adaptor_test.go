package vertex

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
		TaskID:     "task_vertex_openai_video",
		Status:     model.TaskStatusSuccess,
		Progress:   "100%",
		CreatedAt:  123,
		FinishTime: 456,
	}
	task.PrivateData.ResultURL = "https://example.com/video.mp4"

	rendered, err := adaptor.ConvertToOpenAIVideo(task)
	require.NoError(t, err)

	var video dto.OpenAIVideo
	require.NoError(t, common.Unmarshal(rendered, &video))
	require.Equal(t, "https://example.com/video.mp4", video.Metadata["url"])
}

func TestConvertToOpenAIVideoRewritesUnsignedGoogleStorageHTTPS(t *testing.T) {
	previous := system_setting.ServerAddress
	system_setting.ServerAddress = "https://api.example.com"
	t.Cleanup(func() { system_setting.ServerAddress = previous })

	adaptor := &TaskAdaptor{}
	task := &model.Task{
		TaskID:     "task_vertex_gcs_https",
		Status:     model.TaskStatusSuccess,
		Progress:   "100%",
		FinishTime: 456,
	}
	task.PrivateData.ResultURL = "https://storage.googleapis.com/bucket/video.mp4"

	rendered, err := adaptor.ConvertToOpenAIVideo(task)
	require.NoError(t, err)

	var video dto.OpenAIVideo
	require.NoError(t, common.Unmarshal(rendered, &video))
	require.Equal(t, "https://api.example.com/v1/videos/task_vertex_gcs_https/content", video.Metadata["url"])
}

func TestParseTaskResultFailsWhenDoneWithoutVideoBytes(t *testing.T) {
	adaptor := &TaskAdaptor{}
	ti, err := adaptor.ParseTaskResult([]byte(`{"name":"ops/1","done":true,"response":{}}`))
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusFailure, ti.Status)
	require.Contains(t, ti.Reason, "vertex video content missing")
	require.Equal(t, "100%", ti.Progress)
}

func TestParseTaskResultUsesInlineVideoBytes(t *testing.T) {
	adaptor := &TaskAdaptor{}
	ti, err := adaptor.ParseTaskResult([]byte(`{"done":true,"response":{"videos":[{"bytesBase64Encoded":"AAAA","mimeType":"video/mp4"}]}}`))
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusSuccess, ti.Status)
	require.Equal(t, "data:video/mp4;base64,AAAA", ti.Url)
}

func TestParseTaskResultUsesGenerateVideoURI(t *testing.T) {
	adaptor := &TaskAdaptor{}
	ti, err := adaptor.ParseTaskResult([]byte(`{"done":true,"response":{"generateVideoResponse":{"generatedVideos":[{"video":{"uri":"https://storage.googleapis.com/video.mp4"}}]}}}`))
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusSuccess, ti.Status)
	require.Equal(t, "https://storage.googleapis.com/video.mp4", ti.RemoteUrl)
}

func TestParseTaskResultUsesVideoGcsURI(t *testing.T) {
	adaptor := &TaskAdaptor{}
	ti, err := adaptor.ParseTaskResult([]byte(`{"done":true,"response":{"videos":[{"gcsUri":"gs://bucket/video.mp4"}]}}`))
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusSuccess, ti.Status)
	require.Equal(t, "gs://bucket/video.mp4", ti.RemoteUrl)
}
