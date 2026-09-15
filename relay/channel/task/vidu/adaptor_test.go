package vidu

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
		TaskID:   "task_vidu_openai_video",
		Status:   model.TaskStatusSuccess,
		Progress: "100%",
		Data:     []byte(`{"state":"success","creations":[{"url":"https://vidu.example/raw.mp4"}]}`),
	}
	task.PrivateData.ResultURL = "https://api.example/v1/videos/task_vidu_openai_video/content"

	rendered, err := adaptor.ConvertToOpenAIVideo(task)
	require.NoError(t, err)

	var video dto.OpenAIVideo
	require.NoError(t, common.Unmarshal(rendered, &video))
	require.Equal(t, "https://api.example/v1/videos/task_vidu_openai_video/content", video.Metadata["url"])
}

func TestConvertToOpenAIVideoOmitsProviderURLWhenResultURLMissing(t *testing.T) {
	adaptor := &TaskAdaptor{}
	task := &model.Task{
		TaskID: "task_vidu_missing_result",
		Status: model.TaskStatusSuccess,
		Data:   []byte(`{"state":"success","creations":[{"url":"https://vidu.example/raw.mp4"}]}`),
	}

	rendered, err := adaptor.ConvertToOpenAIVideo(task)
	require.NoError(t, err)

	var video dto.OpenAIVideo
	require.NoError(t, common.Unmarshal(rendered, &video))
	require.Empty(t, video.Metadata["url"])
}
