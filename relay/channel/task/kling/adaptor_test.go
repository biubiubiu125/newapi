package kling

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/require"
)

func TestIsNewAPIRelayRejectsOfficialKlingKeys(t *testing.T) {
	require.True(t, isNewAPIRelay("sk-nested-newapi"))
	require.False(t, isNewAPIRelay("accessKey|secretKey"))
	require.False(t, isNewAPIRelay("sk-access|secretKey"))
	require.False(t, isNewAPIRelay("eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJhY2Nlc3MifQ.signature"))
}

func TestConvertToOpenAIVideoUsesGetResultURL(t *testing.T) {
	adaptor := &TaskAdaptor{}
	task := &model.Task{
		TaskID:   "task_kling_openai_video",
		Status:   model.TaskStatusSuccess,
		Progress: "100%",
		Data: []byte(`{
			"code": 0,
			"data": {
				"created_at": 123,
				"updated_at": 456,
				"task_result": {
					"videos": [{"url": "https://kling.example/raw.mp4", "duration": "5"}]
				}
			}
		}`),
	}
	task.PrivateData.ResultURL = "https://api.example/v1/videos/task_kling_openai_video/content"

	rendered, err := adaptor.ConvertToOpenAIVideo(task)
	require.NoError(t, err)

	var video dto.OpenAIVideo
	require.NoError(t, common.Unmarshal(rendered, &video))
	require.Equal(t, "https://api.example/v1/videos/task_kling_openai_video/content", video.Metadata["url"])
	require.Equal(t, "5", video.Seconds)
}

func TestConvertToOpenAIVideoUsesPublicFailReasonAndDropsExtraMetadata(t *testing.T) {
	adaptor := &TaskAdaptor{}
	task := &model.Task{
		TaskID:     "task_kling_failed",
		Status:     model.TaskStatusFailure,
		Progress:   "100%",
		FailReason: "billing accounting failed after task submission: pq: password authentication failed",
		Data: []byte(`{
			"code": 1001,
			"message": "https://kling.example/debug",
			"data": {
				"task_status": "failed",
				"task_status_msg": "internal provider dump",
				"task_result": {
					"videos": [{"url": "https://kling.example/raw.mp4", "duration": "5"}]
				}
			}
		}`),
	}

	rendered, err := adaptor.ConvertToOpenAIVideo(task)
	require.NoError(t, err)

	var video dto.OpenAIVideo
	require.NoError(t, common.Unmarshal(rendered, &video))
	require.Equal(t, dto.VideoStatusFailed, video.Status)
	require.Nil(t, video.Metadata)
	require.NotNil(t, video.Error)
	require.Equal(t, model.TaskPublicAccountingFailReason, video.Error.Message)
	require.Equal(t, "task_failed", video.Error.Code)
	require.NotContains(t, string(rendered), "kling.example")
	require.NotContains(t, string(rendered), "pq:")
}

func TestConvertToOpenAIVideoOmitsProviderURLWhenResultURLMissing(t *testing.T) {
	adaptor := &TaskAdaptor{}
	task := &model.Task{
		TaskID: "task_kling_missing_result",
		Status: model.TaskStatusSuccess,
		Data: []byte(`{
			"code": 0,
			"data": {
				"task_result": {
					"videos": [{"url": "https://kling.example/raw.mp4", "duration": "5"}]
				}
			}
		}`),
	}

	rendered, err := adaptor.ConvertToOpenAIVideo(task)
	require.NoError(t, err)

	var video dto.OpenAIVideo
	require.NoError(t, common.Unmarshal(rendered, &video))
	require.Empty(t, video.Metadata["url"])
	require.Equal(t, "5", video.Seconds)
}
