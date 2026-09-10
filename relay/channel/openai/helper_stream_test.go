package openai

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestHandleGeminiFormatKeepsStateAcrossStreamChunks(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)

	info := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatGemini,
		OriginModelName: "gpt-test",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gpt-test",
		},
	}
	content := "hello"
	raw, err := common.Marshal(dto.ChatCompletionsStreamResponse{
		Id:    "chatcmpl_test",
		Model: "gpt-test",
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Index: 0,
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
				Content: &content,
			},
		}},
	})
	require.NoError(t, err)

	require.NoError(t, handleGeminiFormat(ctx, string(raw), info))
	state, ok := info.ChatToGeminiStreamState.(*relayconvert.ResponseStreamState)
	require.True(t, ok)
	require.NotNil(t, state)
	require.Equal(t, types.RelayFormatOpenAI, state.From)
	require.Equal(t, types.RelayFormat(types.RelayFormatGemini), state.To)
	require.Contains(t, recorder.Body.String(), "hello")

	secondContent := " world"
	secondRaw, err := common.Marshal(dto.ChatCompletionsStreamResponse{
		Id:    "chatcmpl_test",
		Model: "gpt-test",
		Choices: []dto.ChatCompletionsStreamResponseChoice{{
			Index: 0,
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
				Content: &secondContent,
			},
		}},
	})
	require.NoError(t, err)
	require.NoError(t, handleGeminiFormat(ctx, string(secondRaw), info))
	secondState, ok := info.ChatToGeminiStreamState.(*relayconvert.ResponseStreamState)
	require.True(t, ok)
	require.Same(t, state, secondState)
	require.Contains(t, recorder.Body.String(), "world")
}
