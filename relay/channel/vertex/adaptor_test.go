package vertex

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvertGeminiRequestRemovesFunctionCallAndResponseIDs(t *testing.T) {
	request := &dto.GeminiChatRequest{
		Contents: []dto.GeminiChatContent{{
			Role: "user",
			Parts: []dto.GeminiPart{{
				FunctionCall: &dto.FunctionCall{
					FunctionName: "lookup",
					Arguments:    map[string]any{"city": "Paris"},
				},
				FunctionResponse: &dto.GeminiFunctionResponse{
					Name:     "lookup",
					Response: map[string]interface{}{"temperature": 20},
					ID:       json.RawMessage(`"response_1"`),
				},
			}},
		}},
	}

	_, err := (&Adaptor{}).ConvertGeminiRequest(nil, &relaycommon.RelayInfo{}, request)
	require.NoError(t, err)
	assert.Empty(t, request.Contents[0].Parts[0].FunctionCall.ID)
	assert.Nil(t, request.Contents[0].Parts[0].FunctionResponse.ID)
}

func TestConvertOpenAIRequestRemovesFunctionCallAndResponseIDs(t *testing.T) {
	request := &dto.GeneralOpenAIRequest{
		Model: "gemini-2.5-flash",
		Messages: []dto.Message{{
			Role:    "assistant",
			Content: "",
		}},
	}
	request.Messages[0].SetToolCalls([]dto.ToolCallRequest{{
		ID:   "call_1",
		Type: "function",
		Function: dto.FunctionRequest{
			Name:      "lookup",
			Arguments: `{"city":"Paris"}`,
		},
	}})

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	converted, err := (&Adaptor{RequestMode: RequestModeGemini}).ConvertOpenAIRequest(c, &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gemini-2.5-flash",
		},
	}, request)
	require.NoError(t, err)
	geminiRequest, ok := converted.(*dto.GeminiChatRequest)
	require.True(t, ok)
	require.NotNil(t, geminiRequest.Contents[0].Parts[0].FunctionCall)
	assert.Empty(t, geminiRequest.Contents[0].Parts[0].FunctionCall.ID)
}
