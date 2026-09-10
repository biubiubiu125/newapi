package vertex

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
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
