package sora

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSoraBuildRequestBodyReturnsReplayablePassThroughBody(t *testing.T) {
	payload := []byte("opaque-sora-request-body")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(payload))
	c.Request.Header.Set("Content-Type", "application/octet-stream")
	defer common.CleanupBodyStorage(c)

	info := &relaycommon.RelayInfo{}
	body, err := (&TaskAdaptor{}).BuildRequestBody(c, info)
	require.NoError(t, err)
	replayable, ok := body.(common.ReplayableBody)
	require.True(t, ok)

	sent, err := io.ReadAll(body)
	require.NoError(t, err)
	assert.Equal(t, payload, sent)
	assert.EqualValues(t, len(payload), replayable.Size())

	replayBody, err := replayable.NewReader()
	require.NoError(t, err)
	replay, err := io.ReadAll(replayBody)
	require.NoError(t, err)
	require.NoError(t, replayBody.Close())
	assert.Equal(t, payload, replay)
}

func TestConvertToOpenAIVideoProjectsResultURLAndDropsUnknownFields(t *testing.T) {
	adaptor := &TaskAdaptor{}
	task := &model.Task{
		TaskID:   "task_sora_openai_video",
		Status:   model.TaskStatusSuccess,
		Progress: "100%",
		Data: []byte(`{"id":"upstream-id","object":"video","status":"completed","seconds":"8","secret":"leak","error":{"message":"https://provider.example/debug","code":"upstream"},"metadata":{"url":"https://provider.example/raw.mp4","signed":"leak"}}`),
		Properties: model.Properties{
			OriginModelName: "sora-2",
		},
	}
	task.PrivateData.ResultURL = "https://api.example/v1/videos/task_sora_openai_video/content"

	rendered, err := adaptor.ConvertToOpenAIVideo(task)
	require.NoError(t, err)

	var video dto.OpenAIVideo
	require.NoError(t, common.Unmarshal(rendered, &video))
	require.Equal(t, "task_sora_openai_video", video.ID)
	require.Equal(t, "sora-2", video.Model)
	require.Equal(t, "https://api.example/v1/videos/task_sora_openai_video/content", video.Metadata["url"])
	require.Len(t, video.Metadata, 1)
	require.Equal(t, "8", video.Seconds)
	require.Nil(t, video.Error)
	require.NotContains(t, string(rendered), "secret")
	require.NotContains(t, string(rendered), "upstream-id")
	require.NotContains(t, string(rendered), "provider.example")
	require.NotContains(t, string(rendered), "signed")
}
