package doubao

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/require"
)

func TestConvertToOpenAIVideoUsesGetResultURL(t *testing.T) {
	adaptor := &TaskAdaptor{}
	task := &model.Task{
		TaskID:   "task_doubao_openai_video",
		Status:   model.TaskStatusSuccess,
		Progress: "100%",
		Data:     []byte(`{"status":"succeeded","content":{"video_url":"https://doubao.example/raw.mp4"}}`),
	}
	task.PrivateData.ResultURL = "https://api.example/v1/videos/task_doubao_openai_video/content"

	rendered, err := adaptor.ConvertToOpenAIVideo(task)
	require.NoError(t, err)

	var video dto.OpenAIVideo
	require.NoError(t, common.Unmarshal(rendered, &video))
	require.Equal(t, dto.VideoStatusCompleted, video.Status)
	require.Equal(t, "https://api.example/v1/videos/task_doubao_openai_video/content", video.Metadata["url"])
}

func TestConvertToOpenAIVideoOmitsProviderURLWhenResultURLMissing(t *testing.T) {
	adaptor := &TaskAdaptor{}
	task := &model.Task{
		TaskID: "task_doubao_missing_result",
		Status: model.TaskStatusFailure,
		Data:   []byte(`{"status":"succeeded","content":{"video_url":"https://doubao.example/raw.mp4"}}`),
	}

	rendered, err := adaptor.ConvertToOpenAIVideo(task)
	require.NoError(t, err)

	var video dto.OpenAIVideo
	require.NoError(t, common.Unmarshal(rendered, &video))
	require.Equal(t, dto.VideoStatusFailed, video.Status)
	require.Empty(t, video.Metadata["url"])
}

func TestParseTaskResultUnknownStatusWithErrorIsFailure(t *testing.T) {
	adaptor := &TaskAdaptor{}
	ti, err := adaptor.ParseTaskResult([]byte(`{"status":"mystery","error":{"code":"bad","message":"boom"}}`))
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusFailure, ti.Status)
	require.Equal(t, "boom", ti.Reason)
	require.Equal(t, "100%", ti.Progress)
}
