package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOaiResponsesToChatStreamHandlerEmitsCompletedImageResult(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })

	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	body := strings.Join([]string{
		`data: {"type":"response.completed","response":{"id":"resp_1","object":"response","created_at":1710000000,"model":"gpt-image-2","output":[{"type":"image_generation_call","quality":"high","size":"1024x1024","result":"https://example.com/image.png"}],"usage":{"input_tokens":3,"output_tokens":4,"total_tokens":7}}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gpt-image-2",
		},
		IsStream:    true,
		RelayFormat: types.RelayFormatOpenAI,
	}

	usage, err := OaiResponsesToChatStreamHandler(c, info, resp)

	require.Nil(t, err)
	require.Equal(t, 3, usage.PromptTokens)
	require.Equal(t, 4, usage.CompletionTokens)
	require.Equal(t, 7, usage.TotalTokens)
	require.True(t, c.GetBool("image_generation_call"))
	require.Equal(t, "high", c.GetString("image_generation_call_quality"))
	require.Equal(t, "1024x1024", c.GetString("image_generation_call_size"))
	require.Contains(t, recorder.Body.String(), `![image](https://example.com/image.png)`)
	require.Contains(t, recorder.Body.String(), `"finish_reason":"stop"`)
	require.Contains(t, recorder.Body.String(), `data: [DONE]`)
	require.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"))
}

func TestOaiResponsesToChatStreamHandlerAppendsCompletedImageAfterTextDelta(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })

	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	body := strings.Join([]string{
		`data: {"type":"response.output_text.delta","delta":"done"}`,
		``,
		`data: {"type":"response.completed","response":{"id":"resp_1","object":"response","created_at":1710000000,"model":"gpt-image-2","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"done"}]},{"type":"image_generation_call","quality":"high","size":"1024x1024","result":"https://example.com/image.png"}],"usage":{"input_tokens":3,"output_tokens":4,"total_tokens":7}}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gpt-image-2",
		},
		IsStream:    true,
		RelayFormat: types.RelayFormatOpenAI,
	}

	usage, err := OaiResponsesToChatStreamHandler(c, info, resp)

	require.Nil(t, err)
	require.Equal(t, 7, usage.TotalTokens)
	require.Contains(t, recorder.Body.String(), `"content":"done"`)
	require.Contains(t, recorder.Body.String(), `![image](https://example.com/image.png)`)
	require.Contains(t, recorder.Body.String(), `data: [DONE]`)
}
