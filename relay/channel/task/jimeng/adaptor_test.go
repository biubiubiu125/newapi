package jimeng

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/require"
)

func TestIsNewAPIRelayRejectsOfficialJimengKeys(t *testing.T) {
	require.True(t, isNewAPIRelay("sk-nested-newapi"))
	require.False(t, isNewAPIRelay("accessKey|secretKey"))
	require.False(t, isNewAPIRelay("sk-access|secretKey"))
	require.False(t, isNewAPIRelay("eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJhY2Nlc3MifQ.signature"))
	require.False(t, isNewAPIRelay(""))

	nested := &TaskAdaptor{ChannelType: constant.ChannelTypeNewAPI}
	require.True(t, nested.usesNewAPIRelay("not-an-sk-key"))
}

func TestParseTaskResultMapsGeneratingToInProgress(t *testing.T) {
	adaptor := &TaskAdaptor{}
	ti, err := adaptor.ParseTaskResult([]byte(`{"code":10000,"data":{"status":"generating"}}`))
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusInProgress, ti.Status)
	require.Equal(t, "50%", ti.Progress)
	require.Empty(t, ti.Url)
}

func TestParseTaskResultDoneDoesNotOverrideAPIFailure(t *testing.T) {
	adaptor := &TaskAdaptor{}
	ti, err := adaptor.ParseTaskResult([]byte(`{"code":10001,"message":"busy","data":{"status":"done","video_url":"https://jimeng.example/raw.mp4"}}`))
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusFailure, ti.Status)
	require.Equal(t, "busy", ti.Reason)
	require.Empty(t, ti.Url)
}

func TestParseTaskResultDonePersistsVideoURL(t *testing.T) {
	adaptor := &TaskAdaptor{}
	ti, err := adaptor.ParseTaskResult([]byte(`{"code":10000,"data":{"status":"done","video_url":"https://jimeng.example/raw.mp4"}}`))
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusSuccess, ti.Status)
	require.Equal(t, "https://jimeng.example/raw.mp4", ti.Url)
	require.Equal(t, "100%", ti.Progress)
}

func TestConvertToOpenAIVideoUsesGetResultURL(t *testing.T) {
	adaptor := &TaskAdaptor{}
	task := &model.Task{
		TaskID:   "task_jimeng_openai_video",
		Status:   model.TaskStatusSuccess,
		Progress: "100%",
		Data:     []byte(`{"code":10000,"data":{"status":"done","video_url":"https://jimeng.example/raw.mp4"}}`),
	}
	task.PrivateData.ResultURL = "https://api.example/v1/videos/task_jimeng_openai_video/content"

	rendered, err := adaptor.ConvertToOpenAIVideo(task)
	require.NoError(t, err)

	var video dto.OpenAIVideo
	require.NoError(t, common.Unmarshal(rendered, &video))
	require.Equal(t, "https://api.example/v1/videos/task_jimeng_openai_video/content", video.Metadata["url"])
}

func TestFetchResultURIUsesCallerBaseURLForNestedRelay(t *testing.T) {
	adaptor := &TaskAdaptor{baseURL: "https://stale.example"}
	require.Equal(
		t,
		"https://nested.example/jimeng/?Action=CVSync2AsyncGetResult&Version=2022-08-31",
		adaptor.fetchResultURI("https://nested.example", "sk-nested-newapi"),
	)
	require.Equal(
		t,
		"https://official.example/?Action=CVSync2AsyncGetResult&Version=2022-08-31",
		adaptor.fetchResultURI("https://official.example", "ak|sk"),
	)
}
