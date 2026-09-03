package openai

import (
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvertOpenAIResponsesRequestPreservesQwenMaxModelID(t *testing.T) {
	info := &relaycommon.RelayInfo{
		OriginModelName: "qwen-max",
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "qwen-max"},
	}

	result, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(nil, info, dto.OpenAIResponsesRequest{
		Model: "qwen-max",
	})

	require.NoError(t, err)
	converted, ok := result.(dto.OpenAIResponsesRequest)
	require.True(t, ok)
	assert.Equal(t, "qwen-max", converted.Model)
	assert.Nil(t, converted.Reasoning)
	assert.Equal(t, "", info.GetReasoningEffort())
}
